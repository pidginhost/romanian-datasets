// Package mfp builds the localities of the nomenclature of the Ministry of Finance (MFP), which ANAF uses in
// addresses, with the SIRUTA code each matches.
package mfp

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Folder is where MFP publishes its nomenclatures.
const Folder = "https://www.mfinante.gov.ro/static/40/Mfp/nomenclatoare/"

// Counties is the list of counties; its date of change is the date in the names of the locality files.
const Counties = Folder + "nomJudete.xml"

// Types is the list of locality types.
const Types = Folder + "nomTipLocalitati.xml"

// maxFile is many times the largest file.
const maxFile = 32 << 20

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// Modified gives the time a public file last changed.
type Modified func(address string) (time.Time, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) (Get, Modified) {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "text/xml") },
		func(address string) (time.Time, error) { return fetch.Modified(client, address) }
}

// LocalityFile is the address of the localities of a county, published on a day.
func LocalityFile(county int, day time.Time) string {
	return fmt.Sprintf("%snomLocalitati_%d_%s.xml", Folder, county, day.Format("02.01.2006"))
}

// Place is a unit of SIRUTA.
type Place struct {
	Code   int    `json:"code"`
	Name   string `json:"name"`
	Parent *int   `json:"parent"`
	Type   int    `json:"type"`
	County int    `json:"county"`
	Level  int    `json:"level"`
}

// SIRUTACounty is a county of SIRUTA.
type SIRUTACounty struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

// sectorType is the SIRUTA type of a sector of Bucharest, an administrative unit at the third level.
const sectorType = 6

// unitTypes are the MFP types of an administrative unit; the others are localities inside one.
var unitTypes = map[string]bool{"Mun": true, "MRJ": true, "Orş": true, "Com": true, "Sec": true}

// Locality is one row of the MFP nomenclature.
type Locality struct {
	County int    `json:"county"`
	Code   int    `json:"code"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	// Parent is the MFP code of the locality this one belongs to.
	Parent *int    `json:"parent"`
	Postal *string `json:"postal_code"`
	// MFPSIRUTA is the SIRUTA code MFP gives, which is wrong for many administrative units.
	MFPSIRUTA *int `json:"mfp_siruta"`
	SIRUTA    *int `json:"siruta"`
	UAT       *int `json:"uat"`
	// Match says how SIRUTA was found: code, name, or null when it was not.
	Match *string `json:"match"`
}

// Key identifies a locality in its file.
func (l Locality) Key() string { return fmt.Sprintf("%d|%d", l.County, l.Code) }

// Type is a locality type.
type Type struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type row map[string]string

// rows reads a nomenclature file: a root with one rand element for each row, whose children are its fields.
func rows(r io.Reader) ([]row, error) {
	decoder := xml.NewDecoder(io.LimitReader(r, maxFile))
	var out []row
	var current row
	field := ""
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("not a nomenclature: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch {
			case t.Name.Local == "rand":
				current = row{}
			case current != nil:
				field = t.Name.Local
				current[field] = ""
			}
		case xml.CharData:
			if current != nil && field != "" {
				current[field] += string(t)
			}
		case xml.EndElement:
			if t.Name.Local == "rand" {
				out = append(out, current)
				current = nil
			}
			field = ""
		}
	}
	for _, r := range out {
		for k, v := range r {
			r[k] = strings.TrimSpace(v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the nomenclature has no rows")
	}
	return out, nil
}

var prefix = regexp.MustCompile(`^(MUNICIPIUL|ORAȘ|ORAŞ)\s+`)

// fold writes a name in capitals without Romanian letters, spaces or signs, to compare names. The i and the
// a with circumflex are one sound written two ways.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(prefix.ReplaceAllString(strings.ToUpper(s), "")) {
		switch r {
		case 'Ă', 'Â', 'Î':
			r = 'A'
		case 'Ș', 'Ş':
			r = 'S'
		case 'Ț', 'Ţ':
			r = 'T'
		}
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// loose also takes a plain I for the a with circumflex, as SIRUTA writes some names.
func loose(s string) string { return strings.ReplaceAll(fold(s), "I", "A") }

func sameName(mfp string, p Place) bool {
	if p.Type == sectorType {
		// MFP names a sector by its number alone.
		return strings.HasSuffix(fold(p.Name), "SECTORUL"+fold(mfp))
	}
	return fold(mfp) == fold(p.Name) || loose(mfp) == loose(p.Name)
}

// matcher finds the SIRUTA place of an MFP locality.
type matcher struct {
	byCode   map[int]Place
	byCounty map[int][]Place
}

func (m matcher) uat(p Place) *int {
	if p.Level == 2 || p.Type == sectorType || p.Parent == nil {
		c := p.Code
		return &c
	}
	c := *p.Parent
	return &c
}

// match takes the SIRUTA code MFP gives when it is in the county and names the same place; else the one
// place of the county with that name, at the level of the MFP type; else none.
func (m matcher) match(l Locality) (*int, *int, *string) {
	if l.MFPSIRUTA != nil {
		if p, ok := m.byCode[*l.MFPSIRUTA]; ok && p.County == l.County && sameName(l.Name, p) {
			how := "code"
			code := p.Code
			return &code, m.uat(p), &how
		}
	}
	if l.Type == "?" {
		return nil, nil, nil
	}
	unit := unitTypes[l.Type]
	for _, compare := range []func(string) string{fold, loose} {
		var found []Place
		for _, p := range m.byCounty[l.County] {
			atLevel := p.Level >= 2 && (p.Level == 3 && p.Type != sectorType) != unit
			if atLevel && compare(p.Name) == compare(l.Name) {
				found = append(found, p)
			}
		}
		if len(found) == 1 {
			how := "name"
			code := found[0].Code
			return &code, m.uat(found[0]), &how
		}
		if len(found) > 1 {
			return nil, nil, nil
		}
	}
	return nil, nil, nil
}

func number(r row, field string, allowZero bool) (*int, error) {
	v, ok := r[field]
	if !ok {
		return nil, fmt.Errorf("the row has no %s", field)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a number", field, v)
	}
	if n == 0 && allowZero {
		return nil, nil
	}
	return &n, nil
}

// ReadLocalities reads the localities of one county.
func ReadLocalities(r io.Reader, county int, types map[string]bool) ([]Locality, error) {
	all, err := rows(r)
	if err != nil {
		return nil, err
	}
	var out []Locality
	for i, r := range all {
		fail := func(err error) ([]Locality, error) { return nil, fmt.Errorf("row %d: %w", i+1, err) }
		c, err := number(r, "JUD_COD", false)
		if err != nil {
			return fail(err)
		}
		if *c != county {
			return fail(fmt.Errorf("the row is of county %d", *c))
		}
		code, err := number(r, "COD", false)
		if err != nil {
			return fail(err)
		}
		parent, err := number(r, "LOC_COD", true)
		if err != nil {
			return fail(err)
		}
		siruta, err := number(r, "COD_SIRUTA", true)
		if err != nil {
			return fail(err)
		}
		if !types[r["TPL_COD"]] {
			return fail(fmt.Errorf("the type %q is not in the list of types", r["TPL_COD"]))
		}
		if r["DENUMIRE"] == "" {
			return fail(fmt.Errorf("no name"))
		}
		var postal *string
		if p := r["COD_POSTAL"]; p != "" && p != "0" {
			postal = &p
		}
		out = append(out, Locality{County: county, Code: *code, Name: r["DENUMIRE"], Type: r["TPL_COD"], Parent: parent, Postal: postal, MFPSIRUTA: siruta})
	}
	return out, nil
}

// Published are the keys of localities.jsonl as published before.
type Published []string

// PublishedKeys reads localities.jsonl of the published dataset.
func PublishedKeys(r io.Reader) (Published, error) {
	var out Published
	decoder := json.NewDecoder(r)
	for decoder.More() {
		var l Locality
		if err := decoder.Decode(&l); err != nil {
			return nil, fmt.Errorf("published localities: %w", err)
		}
		out = append(out, l.Key())
	}
	return out, nil
}

func read(get Get, address string) ([]row, error) {
	body, err := get(address)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	out, err := rows(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	return out, nil
}

// Dataset reads the newest nomenclature and builds the dataset. A county MFP and SIRUTA name differently,
// a locality listed twice, or a locality published before and missing now stops the build.
func Dataset(get Get, modified Modified, places []Place, counties []SIRUTACounty, published Published) (dataset.Dataset, error) {
	day, err := modified(Counties)
	if err != nil {
		return dataset.Dataset{}, err
	}
	countyRows, err := read(get, Counties)
	if err != nil {
		return dataset.Dataset{}, err
	}
	typeRows, err := read(get, Types)
	if err != nil {
		return dataset.Dataset{}, err
	}

	sirutaCounties := map[int]string{}
	for _, c := range counties {
		sirutaCounties[c.Code] = c.Name
	}
	var codes []int
	for _, r := range countyRows {
		c, err := number(r, "COD", false)
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("%s: %w", Counties, err)
		}
		name, ok := sirutaCounties[*c]
		if !ok || !strings.HasSuffix(fold(name), fold(r["DENUMIRE"])) {
			return dataset.Dataset{}, fmt.Errorf("the MFP county %d %q is not the SIRUTA county %d %q", *c, r["DENUMIRE"], *c, name)
		}
		codes = append(codes, *c)
	}
	sort.Ints(codes)

	types := map[string]bool{}
	var typeRecords []any
	for _, r := range typeRows {
		if r["COD"] == "" || r["DENUMIRE"] == "" || types[r["COD"]] {
			return dataset.Dataset{}, fmt.Errorf("%s: a type without a code or name, or twice", Types)
		}
		types[r["COD"]] = true
		typeRecords = append(typeRecords, Type{Code: r["COD"], Name: r["DENUMIRE"]})
	}
	sort.Slice(typeRecords, func(i, j int) bool { return typeRecords[i].(Type).Code < typeRecords[j].(Type).Code })

	m := matcher{byCode: map[int]Place{}, byCounty: map[int][]Place{}}
	for _, p := range places {
		m.byCode[p.Code] = p
		m.byCounty[p.County] = append(m.byCounty[p.County], p)
	}

	var localities []Locality
	seen := map[string]bool{}
	matched := map[string]int{}
	for _, c := range codes {
		address := LocalityFile(c, day)
		body, err := get(address)
		if err != nil {
			return dataset.Dataset{}, err
		}
		got, err := ReadLocalities(body, c, types)
		body.Close()
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("%s: %w", address, err)
		}
		for _, l := range got {
			if seen[l.Key()] {
				return dataset.Dataset{}, fmt.Errorf("%s: the locality %d is listed twice", address, l.Code)
			}
			seen[l.Key()] = true
			l.SIRUTA, l.UAT, l.Match = m.match(l)
			how := "none"
			if l.Match != nil {
				how = *l.Match
			}
			matched[how]++
			localities = append(localities, l)
		}
	}
	sort.Slice(localities, func(i, j int) bool {
		if localities[i].County != localities[j].County {
			return localities[i].County < localities[j].County
		}
		return localities[i].Code < localities[j].Code
	})

	var missing []string
	for _, k := range published {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("MFP no longer has the published localities %s: a person decides", strings.Join(missing, ", "))
	}

	var records []any
	for _, l := range localities {
		records = append(records, l)
	}
	return dataset.Dataset{
		Name:  "mfp-localities",
		Title: "Localities of the Ministry of Finance with their SIRUTA code",
		Description: "The localities of the nomenclature of the Ministry of Finance (MFP), whose codes ANAF uses in addresses, with the SIRUTA code each matches. " +
			"MFP gives a SIRUTA code of its own, kept in mfp_siruta, which is wrong for many administrative units (1008 for Alba Iulia, which SIRUTA does not have). " +
			"siruta is that code when SIRUTA has it in the same county under the same name (match code); otherwise the one place of the county with the name, " +
			"an administrative unit for a municipality, town, commune or sector and a locality for the others (match name); otherwise null. " +
			fmt.Sprintf("Of %d localities, %d match by code, %d by name, and %d not: ", len(localities), matched["code"], matched["name"], matched["none"]) +
			"places SIRUTA no longer has, names written another way, the entries for people without a home in Romania, and a few test rows. " +
			"uat is the administrative unit or Bucharest sector of siruta. Names are as MFP writes them.",
		Source: dataset.Source{
			Name:    "Ministry of Finance, nomenclatures of counties, locality types and localities (" + Folder + ")",
			URL:     Counties,
			Licence: "Facts taken from the nomenclatures: the codes, names, types and postal codes of the localities. MFP states no licence for them",
		},
		Frequency: dataset.Monthly,
		Update:    "When MFP changes its nomenclature, which it republishes often",
		Files: []dataset.File{
			{
				Name: "localities.jsonl",
				Fields: []dataset.Field{
					{Name: "county", Description: "The county, a code of counties.jsonl in siruta; MFP uses the same codes"},
					{Name: "code", Description: "The MFP code of the locality. With county it is unique in the file"},
					{Name: "name", Description: "The name as MFP writes it"},
					{Name: "type", Description: "The MFP type, a code of types.jsonl"},
					{Name: "parent", Description: "The MFP code of the locality this one belongs to, in the same county, or null"},
					{Name: "postal_code", Description: "The postal code MFP gives, or null"},
					{Name: "mfp_siruta", Description: "The SIRUTA code MFP gives, or null for 0"},
					{Name: "siruta", Description: "The SIRUTA place the locality matches, a code of units.jsonl in siruta, or null"},
					{Name: "uat", Description: "The administrative unit or Bucharest sector of siruta, or null"},
					{Name: "match", Description: "code, name, or null: how siruta was found"},
				},
				Records: records,
			},
			{
				Name: "types.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The MFP code of the type"},
					{Name: "name", Description: "The name as MFP writes it"},
				},
				Records: typeRecords,
			},
		},
	}, nil
}
