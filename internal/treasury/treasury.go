// Package treasury builds the units of the State Treasury and the IBAN of their accounts, from the lists
// ANAF publishes for each treasury.
package treasury

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Index is the page of ANAF that links the list of every county and of Bucharest.
const Index = "https://www.anaf.ro/anaf/internet/ANAF/asistenta_contribuabili/plata_oblig_fiscale/coduri_iban"

// Folder is where ANAF keeps the county pages and the lists.
const Folder = "https://static.anaf.ro/static/10/Anaf/AsistentaContribuabili_r/iban2014/"

// maxPage and maxList are many times the largest page and list.
const (
	maxPage = 4 << 20
	maxList = 32 << 20
)

// budgetPages are pages of the folder that list budget chapters, not the treasuries of a county.
var budgetPages = map[string]bool{"bugetstat": true, "bugetlocal": true, "bass": true, "fnuass": true, "somaj": true}

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

var (
	countyPagePattern = regexp.MustCompile(`(?i)href="(?:https?://static\.anaf\.ro/static/10/Anaf/AsistentaContribuabili_r/iban2014/)?([A-Za-z_]+)\.htm"`)
	listPattern       = regexp.MustCompile(`(?i)iban_TREZ(\d{3})_TREZ(\d{3})\.pdf`)
	titlePattern      = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	countyPattern     = regexp.MustCompile(`(?i)^.*?jude\S*\s+(.+)$`)
	treasuryPrefix    = regexp.MustCompile(`^Trezorerie operativa\s+`)
	placePrefix       = regexp.MustCompile(`^(Municipiul|Municipiu|Orasul|Oras|Comunala)\s+`)
	sirutaPrefix      = regexp.MustCompile(`^(MUNICIPIUL|ORAȘ|ORAŞ)\s+`)
)

// Bucharest is the SIRUTA county code of Bucharest, whose lists the index links directly.
const Bucharest = 40

// Localities gives the SIRUTA code of a treasury whose name ANAF writes another way than SIRUTA: shortened
// (Tg., Sf., Tr.), with an older spelling, or a sector of Bucharest.
var Localities = map[string]int{
	"022": 9459,   // Chisinau Cris: Chisineu-Cris
	"188": 51207,  // Otelul Rosu: Otelu Rosu
	"256": 63394,  // Sf.Gheorghe
	"257": 63740,  // Tg.Secuiesc
	"308": 75472,  // Tg.Bujor
	"336": 77812,  // Tg.Jiu
	"337": 78329,  // Tg.Carbunesti
	"352": 83133,  // Odorheiul Secuiesc
	"408": 95471,  // Tg.Frumos
	"437": 106559, // Sighetul-Marmatiei
	"441": 106979, // Viseul de Sus
	"461": 109773, // Drobeta Tr.Severin
	"476": 114319, // Tg.Mures
	"493": 121055, // Tg.Neamt
	"539": 131069, // Boldesti Scaieni: Boldesti-Scaeni
	"563": 139884, // Simleul Silvaniei
	"592": 146502, // Campulung M.: Campulung Moldovenesc
	"701": 179141, // Sector 1
	"702": 179150, // Sector 2
	"703": 179169, // Sector 3
	"704": 179178, // Sector 4
	"705": 179187, // Sector 5
	"706": 179196, // Sector 6
}

// NoLocality are treasuries named after no administrative unit: Ilfov serves the county, and Gura Lotrului
// is not a unit in SIRUTA.
var NoLocality = map[string]bool{"421": true, "676": true}

// Place is a unit of SIRUTA the build matches treasuries to.
type Place struct {
	Code   int    `json:"code"`
	Name   string `json:"name"`
	County int    `json:"county"`
	Level  int    `json:"level"`
}

// County is a county of SIRUTA.
type County struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

// fold writes a name in capitals without Romanian letters, spaces or signs, to compare names.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case 'Ă', 'Â':
			r = 'A'
		case 'Î':
			r = 'I'
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

// Unit is a unit of the State Treasury that has a list.
type Unit struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Parent is the code of the county treasury it belongs to; 000 for Bucharest.
	Parent string `json:"county_treasury"`
	County int    `json:"county"`
	SIRUTA *int   `json:"siruta"`
}

// Entry is one account, with the units that have it.
type Entry struct {
	Source    *string  `json:"source"`
	Code      string   `json:"account"`
	Name      string   `json:"name"`
	Suffix    string   `json:"iban_suffix"`
	Published string   `json:"published"`
	Units     []string `json:"units"`
}

// Key identifies an account in its file.
func (e Entry) Key() string {
	source := ""
	if e.Source != nil {
		source = *e.Source
	}
	return source + "|" + e.Code
}

// Source is a funding source.
type Source struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func read(get Get, address string, limit int64) ([]byte, error) {
	body, err := get(address)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", address)
	}
	return data, nil
}

// list is a list to read: its units and the county it is in, by the name its page gives.
type list struct {
	parent, code string
	county       string
}

// lists reads the index and the county pages and gives every list. A unit linked twice stops the build.
func lists(get Get) ([]list, error) {
	index, err := read(get, Index, maxPage)
	if err != nil {
		return nil, err
	}
	var out []list
	seen := map[string]bool{}
	add := func(parent, code, county string) error {
		if seen[code] {
			if county == "" {
				return nil
			}
			return fmt.Errorf("the treasury %s is linked twice", code)
		}
		seen[code] = true
		out = append(out, list{parent, code, county})
		return nil
	}
	for _, m := range listPattern.FindAllStringSubmatch(string(index), -1) {
		if m[1] != "000" {
			return nil, fmt.Errorf("the index links the list of %s, which is not in Bucharest", m[2])
		}
		if err := add(m[1], m[2], ""); err != nil {
			return nil, err
		}
	}
	pages := map[string]bool{}
	for _, m := range countyPagePattern.FindAllStringSubmatch(string(index), -1) {
		if !budgetPages[strings.ToLower(m[1])] {
			pages[m[1]] = true
		}
	}
	var names []string
	for p := range pages {
		names = append(names, p)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("the index links no county page")
	}
	for _, p := range names {
		page, err := read(get, Folder+p+".htm", maxPage)
		if err != nil {
			return nil, err
		}
		t := titlePattern.FindSubmatch(page)
		if t == nil {
			return nil, fmt.Errorf("the page %s has no title", p)
		}
		title := words(html.UnescapeString(string(t[1])))
		c := countyPattern.FindStringSubmatch(title)
		if c == nil {
			return nil, fmt.Errorf("the title %q of %s names no county", title, p)
		}
		found := false
		for _, m := range listPattern.FindAllStringSubmatch(string(page), -1) {
			if err := add(m[1], m[2], c[1]); err != nil {
				return nil, err
			}
			found = true
		}
		if !found {
			return nil, fmt.Errorf("the page %s links no list", p)
		}
	}
	for i := range out {
		if out[i].county == "" && out[i].parent != "000" {
			return nil, fmt.Errorf("the treasury %s has no county", out[i].code)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].code < out[j].code })
	return out, nil
}

// Locate gives the county and the SIRUTA code of a treasury, by its name and the county its page names.
func Locate(code, name, county string, places []Place, counties []County) (int, *int, error) {
	countyCode := 0
	if county == "" {
		countyCode = Bucharest
	} else {
		for _, c := range counties {
			if fold(strings.TrimPrefix(strings.TrimPrefix(c.Name, "JUDEȚUL "), "JUDEŢUL ")) == fold(county) {
				countyCode = c.Code
			}
		}
		if countyCode == 0 {
			return 0, nil, fmt.Errorf("treasury %s: the county %q is not in SIRUTA", code, county)
		}
	}

	place := placePrefix.ReplaceAllString(treasuryPrefix.ReplaceAllString(name, ""), "")
	var matches []int
	for _, p := range places {
		if p.County == countyCode && p.Level == 2 && fold(sirutaPrefix.ReplaceAllString(p.Name, "")) == fold(place) {
			matches = append(matches, p.Code)
		}
	}
	known, inTable := Localities[code]
	switch {
	case NoLocality[code]:
		if len(matches) > 0 {
			return 0, nil, fmt.Errorf("treasury %s is in NoLocality, but SIRUTA has %q", code, place)
		}
		return countyCode, nil, nil
	case inTable:
		if len(matches) > 0 {
			return 0, nil, fmt.Errorf("treasury %s is in Localities, but its name now matches SIRUTA: remove it", code)
		}
		for _, p := range places {
			if p.Code == known && p.County == countyCode {
				return countyCode, &known, nil
			}
		}
		return 0, nil, fmt.Errorf("treasury %s: the SIRUTA code %d of Localities is not in its county", code, known)
	case len(matches) == 1:
		return countyCode, &matches[0], nil
	}
	return 0, nil, fmt.Errorf("treasury %s %q matches %d units of SIRUTA: a person decides", code, name, len(matches))
}

// Published are the keys of the dataset as published before.
type Published struct {
	Units    []string
	Accounts []string
}

// PublishedUnits reads units.jsonl of the published dataset.
func PublishedUnits(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for {
		var u Unit
		err := decoder.Decode(&u)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("published units: %w", err)
		}
		out = append(out, u.Code)
	}
}

// PublishedAccounts reads accounts.jsonl of the published dataset.
func PublishedAccounts(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for {
		var e Entry
		err := decoder.Decode(&e)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("published accounts: %w", err)
		}
		out = append(out, e.Key())
	}
}

// Dataset reads every list and builds the dataset. places and counties are the SIRUTA dataset. An account
// that has another name or IBAN in two lists stops the build, as does a unit or account published before
// and missing now.
func Dataset(get Get, places []Place, counties []County, published Published) (dataset.Dataset, error) {
	all, err := lists(get)
	if err != nil {
		return dataset.Dataset{}, err
	}

	var units []Unit
	sources := map[string]string{}
	entries := map[string]*Entry{}
	for _, l := range all {
		address := fmt.Sprintf("%siban_TREZ%s_TREZ%s.pdf", Folder, l.parent, l.code)
		data, err := read(get, address, maxList)
		if err != nil {
			return dataset.Dataset{}, err
		}
		got, err := ReadList(data)
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("%s: %w", address, err)
		}
		county, siruta, err := Locate(l.code, got.Name, l.county, places, counties)
		if err != nil {
			return dataset.Dataset{}, err
		}
		units = append(units, Unit{Code: l.code, Name: got.Name, Parent: l.parent, County: county, SIRUTA: siruta})

		for letter, name := range got.Sources {
			if known, ok := sources[letter]; ok && known != name {
				return dataset.Dataset{}, fmt.Errorf("the funding source %s is %q in %s, %q in another list", letter, name, l.code, known)
			}
			sources[letter] = name
		}
		seen := map[string]bool{}
		for _, a := range got.Accounts {
			if a.IBAN[8:11] != l.code {
				return dataset.Dataset{}, fmt.Errorf("%s: the IBAN %s is not of the treasury %s", address, a.IBAN, l.code)
			}
			key := a.Source + "|" + a.Code
			if seen[key] {
				return dataset.Dataset{}, fmt.Errorf("%s: the account %s is listed twice", address, key)
			}
			seen[key] = true
			suffix := a.IBAN[11:]
			e, ok := entries[key]
			if !ok {
				var source *string
				if a.Source != "" {
					source = &a.Source
				}
				e = &Entry{Source: source, Code: a.Code, Name: a.Name, Suffix: suffix, Published: a.Published}
				entries[key] = e
			}
			switch {
			case e.Name != a.Name:
				return dataset.Dataset{}, fmt.Errorf("the account %s is %q in %s, %q in another list", key, a.Name, l.code, e.Name)
			case e.Suffix != suffix:
				return dataset.Dataset{}, fmt.Errorf("the account %s ends its IBAN in %s in %s, %s in another list", key, suffix, l.code, e.Suffix)
			}
			if a.Published < e.Published {
				e.Published = a.Published
			}
			e.Units = append(e.Units, l.code)
		}
	}

	var accounts []Entry
	for _, e := range entries {
		accounts = append(accounts, *e)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Key() < accounts[j].Key() })

	var missing []string
	have := map[string]bool{}
	for _, u := range units {
		have["unit "+u.Code] = true
	}
	for _, a := range accounts {
		have["account "+a.Key()] = true
	}
	for _, u := range published.Units {
		if !have["unit "+u] {
			missing = append(missing, "unit "+u)
		}
	}
	for _, a := range published.Accounts {
		if !have["account "+a] {
			missing = append(missing, "account "+a)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("published entries are gone: %s: a person decides", strings.Join(missing, ", "))
	}

	var unitRecords, accountRecords, sourceRecords []any
	for _, u := range units {
		unitRecords = append(unitRecords, u)
	}
	for _, a := range accounts {
		accountRecords = append(accountRecords, a)
	}
	var letters []string
	for letter := range sources {
		letters = append(letters, letter)
	}
	sort.Strings(letters)
	for _, letter := range letters {
		sourceRecords = append(sourceRecords, Source{Code: letter, Name: sources[letter]})
	}

	return dataset.Dataset{
		Name:  "treasury",
		Title: "State Treasury units and the IBAN of their accounts",
		Description: "The operative units of the State Treasury, each with its code, county and SIRUTA code, and the accounts taxes and other " +
			"payments go to, as ANAF lists them for each unit. The IBAN of an account at a unit is RO, two check digits (ISO 13616, mod 97), " +
			"TREZ, the three digits of the unit and iban_suffix: an account has the same suffix and name at every unit, so it is listed once, " +
			"with the units that have it. published is the earliest publication date ANAF gives for the account at any unit. " +
			"The accounts of the last sections of a list have no funding source. Names are as ANAF writes them, without Romanian letters. " +
			"A unit's SIRUTA code is matched by name; Ilfov and Gura Lotrului name no administrative unit and have none.",
		Source: dataset.Source{
			Name: "ANAF, IBAN codes of the State Treasury units (" + Index + ")",
			URL:  Index,
			Licence: "Facts taken from the lists: the units, which accounts each has and their IBAN. ANAF states no licence for them. " +
				"The account names are those of the budget classification, set by order of the Minister of Finance, " +
				"which is not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Monthly,
		Update:    "When ANAF publishes new lists, which it regenerates often; accounts change with the budget classification",
		Files: []dataset.File{
			{
				Name: "units.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The three digits of the unit, which follow TREZ in an IBAN"},
					{Name: "name", Description: "The name as ANAF writes it"},
					{Name: "county_treasury", Description: "The code of the county treasury the unit belongs to; 000 for Bucharest"},
					{Name: "county", Description: "The county, a code of counties.jsonl in siruta"},
					{Name: "siruta", Description: "The administrative unit the treasury is named after, a code of units.jsonl in siruta, or null"},
				},
				Records: unitRecords,
			},
			{
				Name: "sources.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The letter of the funding source, which is also the third character of an account"},
					{Name: "name", Description: "The name as ANAF writes it"},
				},
				Records: sourceRecords,
			},
			{
				Name: "accounts.jsonl",
				Fields: []dataset.Field{
					{Name: "source", Description: "The funding source, a code of sources.jsonl, or null. With account it is unique in the file"},
					{Name: "account", Description: "The code of the account, from the budget classification"},
					{Name: "name", Description: "The name of the account"},
					{Name: "iban_suffix", Description: "The last 13 characters of the IBAN, after TREZ and the unit code"},
					{Name: "published", Description: "The earliest publication date of the account at any unit, YYYY-MM-DD"},
					{Name: "units", Description: "The codes of the units that have the account"},
				},
				Records: accountRecords,
			},
		},
	}, nil
}
