// Package siruta builds the register of administrative-territorial units of Romania (SIRUTA) from the
// newest edition the National Institute of Statistics (INS) publishes on its site. INS writes the names
// without Romanian letters; they are taken from the copy INS publishes on data.gov.ro.
package siruta

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nwaples/rardecode/v2"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Page is the INS page that links the editions.
const Page = "https://insse.ro/cms/ro/siruta"

// The limits are many times the size of the page, of an archive and of a table in it.
const (
	maxPage    = 4 << 20
	maxArchive = 64 << 20
	maxTable   = 32 << 20
)

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

// Edition is one archive of the register: the one for the first half of a year, or the one for the whole year.
type Edition struct {
	Year    int
	Annual  bool
	Address string
}

// Label names the edition as INS does: S1 2026, AN 2025.
func (e Edition) Label() string {
	if e.Annual {
		return fmt.Sprintf("AN %d", e.Year)
	}
	return fmt.Sprintf("S1 %d", e.Year)
}

// newer is true when e describes a later state of the register than o: the annual edition comes after
// the first half of the same year.
func (e Edition) newer(o Edition) bool {
	if e.Year != o.Year {
		return e.Year > o.Year
	}
	return e.Annual && !o.Annual
}

var archiveLink = regexp.MustCompile(`(?i)href="((?:https://insse\.ro)?/cms/files/siruta/\d{4}/SIRUTA_(AN|S1)_(\d{4})\.rar)"`)

// Newest reads the INS page and gives the newest edition it links.
func Newest(page io.Reader) (Edition, error) {
	text, err := io.ReadAll(io.LimitReader(page, maxPage))
	if err != nil {
		return Edition{}, err
	}
	var best *Edition
	for _, m := range archiveLink.FindAllSubmatch(text, -1) {
		year, _ := strconv.Atoi(string(m[3]))
		address := string(m[1])
		if strings.HasPrefix(address, "/") {
			address = "https://insse.ro" + address
		}
		e := Edition{Year: year, Annual: strings.EqualFold(string(m[2]), "AN"), Address: address}
		if best == nil || e.newer(*best) {
			best = &e
		}
	}
	if best == nil {
		return Edition{}, fmt.Errorf("the INS page links no SIRUTA archive")
	}
	return *best, nil
}

// unpack gives the named files of a RAR archive, found by their base name in any case.
// Every name must be there once.
func unpack(r io.Reader, names ...string) (map[string][]byte, error) {
	archive, err := rardecode.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("not a RAR archive: %w", err)
	}
	wanted := map[string]string{}
	for _, n := range names {
		wanted[strings.ToLower(n)] = n
	}
	found := map[string][]byte{}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		name, ok := wanted[strings.ToLower(path.Base(header.Name))]
		if !ok || header.IsDir {
			continue
		}
		if _, twice := found[name]; twice {
			return nil, fmt.Errorf("the archive has %s twice", name)
		}
		data, err := io.ReadAll(io.LimitReader(archive, maxTable+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if len(data) > maxTable {
			return nil, fmt.Errorf("%s is larger than a SIRUTA table", name)
		}
		found[name] = data
	}
	for _, n := range names {
		if found[n] == nil {
			return nil, fmt.Errorf("the archive has no %s", n)
		}
	}
	return found, nil
}

// Unit is one unit of the register: a county, a municipality, town or commune, or a locality or Bucharest
// sector in one of them.
type Unit struct {
	Code int    `json:"code"`
	Name string `json:"name"`
	// Parent is the code of the unit above, or null for a county.
	Parent *int `json:"parent"`
	Type   int  `json:"type"`
	Level  int  `json:"level"`
	// Urban is true for an urban unit and false for a rural one, and null for a county.
	Urban      *bool   `json:"urban"`
	County     int     `json:"county"`
	Region     int     `json:"region"`
	PostalCode *string `json:"postal_code"`
	NUTS3      string  `json:"nuts3"`
	LAU        *string `json:"lau"`
}

// County is a county or Bucharest.
type County struct {
	Code     int    `json:"code"`
	Mnemonic string `json:"mnemonic"`
	Name     string `json:"name"`
	Unit     int    `json:"unit"`
	Region   int    `json:"region"`
}

// Region is a development region.
type Region struct {
	Code int    `json:"code"`
	Unit int    `json:"unit"`
	Name string `json:"name"`
}

// Type is a type of unit.
type Type struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

// Types are the types of unit, as the INS methodology lists them.
var Types = []Type{
	{1, "Municipiu reședință de județ, reședință a municipiului București"},
	{2, "Oraș ce aparține de județ, altul decât oraș reședință de județ"},
	{3, "Comună"},
	{4, "Municipiu, altul decât reședință de județ"},
	{5, "Oraș reședință de județ"},
	{6, "Sector al municipiului București"},
	{9, "Localitate componentă, reședință de municipiu"},
	{10, "Localitate componentă a unui municipiu, alta decât reședință de municipiu"},
	{11, "Sat ce aparține de municipiu"},
	{17, "Localitate componentă reședință a orașului"},
	{18, "Localitate componentă a unui oraș, alta decât reședință de oraș"},
	{19, "Sat ce aparține unui oraș"},
	{22, "Sat reședință de comună"},
	{23, "Sat ce aparține de comună, altul decât reședință de comună"},
	{40, "Județ, municipiul București"},
}

// Register is the whole of one edition.
type Register struct {
	Units    []Unit
	Counties []County
	Regions  []Region
	// Unmarked are the codes whose name has no Romanian letters because the two copies spell it differently.
	Unmarked []int
}

func number(row map[string]string, field string) (int, error) {
	n, err := strconv.Atoi(row[field])
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number", field, row[field])
	}
	return n, nil
}

func numbers(row map[string]string, fields ...string) ([]int, error) {
	var out []int
	for _, f := range fields {
		n, err := number(row, f)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

var (
	postalCode = regexp.MustCompile(`^[0-9]{6}$`)
	nuts3      = regexp.MustCompile(`^RO[0-9]{3}$`)
)

// Read builds the register from the three INS tables and the names with Romanian letters.
func Read(units, counties, regions []byte, marked map[int]string) (Register, error) {
	var reg Register

	regionRows, err := readDBF(regions)
	if err != nil {
		return reg, fmt.Errorf("ZONE.DBF: %w", err)
	}
	regionCodes := map[int]bool{}
	for _, row := range regionRows {
		n, err := numbers(row, "ZONA", "SIRUTA")
		if err != nil {
			return reg, fmt.Errorf("ZONE.DBF: %w", err)
		}
		if regionCodes[n[0]] {
			return reg, fmt.Errorf("ZONE.DBF: region %d twice", n[0])
		}
		regionCodes[n[0]] = true
		reg.Regions = append(reg.Regions, Region{Code: n[0], Unit: n[1], Name: strings.Join(strings.Fields(row["DENZONA"]), " ")})
	}

	countyRows, err := readDBF(counties)
	if err != nil {
		return reg, fmt.Errorf("JUD.DBF: %w", err)
	}
	countyAt := map[int]int{}
	for _, row := range countyRows {
		n, err := numbers(row, "JUD", "ZONA")
		if err != nil {
			return reg, fmt.Errorf("JUD.DBF: %w", err)
		}
		if _, twice := countyAt[n[0]]; twice {
			return reg, fmt.Errorf("JUD.DBF: county %d twice", n[0])
		}
		if !regionCodes[n[1]] {
			return reg, fmt.Errorf("JUD.DBF: county %d is in region %d, which ZONE.DBF does not have", n[0], n[1])
		}
		countyAt[n[0]] = len(reg.Counties)
		reg.Counties = append(reg.Counties, County{Code: n[0], Mnemonic: row["MNEMONIC"], Region: n[1]})
	}

	typeCodes := map[int]bool{}
	for _, t := range Types {
		typeCodes[t.Code] = true
	}

	unitRows, err := readDBF(units)
	if err != nil {
		return reg, fmt.Errorf("siruta.dbf: %w", err)
	}
	level := map[int]int{}
	for _, row := range unitRows {
		n, err := numbers(row, "SIRUTA", "NIV", "SIRSUP", "TIP", "JUD", "REGIUNE", "CODP")
		if err != nil {
			return reg, fmt.Errorf("siruta.dbf: %w", err)
		}
		u := Unit{Code: n[0], Level: n[1], Type: n[3], County: n[4], Region: n[5], NUTS3: row["NUTS3"]}
		name := strings.Join(strings.Fields(row["DENLOC"]), " ")
		fail := func(format string, args ...any) error {
			return fmt.Errorf("siruta.dbf, unit %d %s: %s", u.Code, name, fmt.Sprintf(format, args...))
		}

		switch {
		case u.Code <= 0 || name == "":
			return reg, fail("no code or no name")
		case level[u.Code] != 0:
			return reg, fail("listed twice")
		case u.Level < 1 || u.Level > 3:
			return reg, fail("level %d", u.Level)
		case !typeCodes[u.Type]:
			return reg, fail("type %d is not in the INS methodology", u.Type)
		case !regionCodes[u.Region]:
			return reg, fail("region %d is not in ZONE.DBF", u.Region)
		case !nuts3.MatchString(u.NUTS3):
			return reg, fail("NUTS3 %q", u.NUTS3)
		}
		at, known := countyAt[u.County]
		if !known {
			return reg, fail("county %d is not in JUD.DBF", u.County)
		}
		level[u.Code] = u.Level

		if u.Level == 1 {
			// A county hangs from the country, code 1, which is not a unit of the register.
			if n[2] != 1 {
				return reg, fail("a county under %d", n[2])
			}
			if reg.Counties[at].Unit != 0 {
				return reg, fail("county %d has a second unit", u.County)
			}
			reg.Counties[at].Unit = u.Code
		} else {
			parent := n[2]
			u.Parent = &parent
		}

		switch row["MED"] {
		case "0":
			if u.Level != 1 {
				return reg, fail("no environment")
			}
		case "1", "3":
			urban := row["MED"] == "1"
			u.Urban = &urban
		default:
			return reg, fail("environment %q", row["MED"])
		}

		if n[6] != 0 {
			code := fmt.Sprintf("%06d", n[6])
			if !postalCode.MatchString(code) {
				return reg, fail("postal code %d", n[6])
			}
			u.PostalCode = &code
		}
		if lau := row["LAU"]; lau != "" {
			u.LAU = &lau
		}

		u.Name = name
		if m, ok := marked[u.Code]; ok {
			var same bool
			if u.Name, same = withLetters(name, m); !same {
				reg.Unmarked = append(reg.Unmarked, u.Code)
			}
		} else {
			reg.Unmarked = append(reg.Unmarked, u.Code)
		}
		reg.Units = append(reg.Units, u)
	}
	if len(reg.Units) == 0 {
		return reg, fmt.Errorf("siruta.dbf has no units")
	}

	for _, u := range reg.Units {
		if u.Parent == nil {
			continue
		}
		if parentLevel := level[*u.Parent]; parentLevel != u.Level-1 {
			return reg, fmt.Errorf("unit %d %s: parent %d is not one level up", u.Code, u.Name, *u.Parent)
		}
	}
	names := map[int]string{}
	for _, u := range reg.Units {
		names[u.Code] = u.Name
	}
	for i, c := range reg.Counties {
		if c.Unit == 0 {
			return reg, fmt.Errorf("county %d %s has no unit in siruta.dbf", c.Code, c.Mnemonic)
		}
		reg.Counties[i].Name = names[c.Unit]
	}

	sort.Slice(reg.Units, func(i, j int) bool { return reg.Units[i].Code < reg.Units[j].Code })
	sort.Slice(reg.Counties, func(i, j int) bool { return reg.Counties[i].Code < reg.Counties[j].Code })
	sort.Slice(reg.Regions, func(i, j int) bool { return reg.Regions[i].Code < reg.Regions[j].Code })
	sort.Ints(reg.Unmarked)
	return reg, nil
}

// PublishedCodes reads the units file of the published dataset and gives its codes.
func PublishedCodes(r io.Reader) ([]int, error) {
	var codes []int
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var u struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &u); err != nil {
			return nil, fmt.Errorf("published units: %w", err)
		}
		codes = append(codes, u.Code)
	}
	return codes, scanner.Err()
}

func read(get Get, address string, limit int64) ([]byte, error) {
	body, err := get(address)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", address)
	}
	return data, nil
}

// latestNames reads the newest CSV of SIRUTA on data.gov.ro that can be read.
func latestNames(get Get) (map[int]string, error) {
	answer, err := read(get, Catalogue, maxCatalogue)
	if err != nil {
		return nil, err
	}
	addresses, err := csvAddresses(bytes.NewReader(answer))
	if err != nil {
		return nil, err
	}
	var tried []string
	for _, address := range addresses {
		data, err := read(get, address, maxCSV)
		if err == nil {
			var names map[int]string
			if names, err = markedNames(bytes.NewReader(data)); err == nil {
				return names, nil
			}
		}
		tried = append(tried, fmt.Sprintf("%s: %v", address, err))
	}
	return nil, fmt.Errorf("no SIRUTA CSV of data.gov.ro could be read:\n%s", strings.Join(tried, "\n"))
}

// Dataset reads the newest INS edition and the names of data.gov.ro, and builds the dataset.
// published are the codes of the dataset as published before; a code missing from the edition stops the
// build, because the register gives no day on which a code ended and a code is never removed.
func Dataset(get Get, published []int) (dataset.Dataset, error) {
	page, err := read(get, Page, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	edition, err := Newest(bytes.NewReader(page))
	if err != nil {
		return dataset.Dataset{}, err
	}
	archive, err := read(get, edition.Address, maxArchive)
	if err != nil {
		return dataset.Dataset{}, err
	}
	tables, err := unpack(bytes.NewReader(archive), "siruta.dbf", "JUD.DBF", "ZONE.DBF")
	if err != nil {
		return dataset.Dataset{}, fmt.Errorf("%s: %w", edition.Address, err)
	}
	marked, err := latestNames(get)
	if err != nil {
		return dataset.Dataset{}, err
	}
	reg, err := Read(tables["siruta.dbf"], tables["JUD.DBF"], tables["ZONE.DBF"], marked)
	if err != nil {
		return dataset.Dataset{}, fmt.Errorf("SIRUTA %s: %w", edition.Label(), err)
	}

	have := map[int]bool{}
	for _, u := range reg.Units {
		have[u.Code] = true
	}
	var missing []string
	for _, code := range published {
		if !have[code] {
			missing = append(missing, strconv.Itoa(code))
		}
	}
	if len(missing) > 0 {
		return dataset.Dataset{}, fmt.Errorf("SIRUTA %s no longer has the published codes %s: a person decides how they end", edition.Label(), strings.Join(missing, ", "))
	}

	return build(edition, reg), nil
}

func build(edition Edition, reg Register) dataset.Dataset {
	var units, counties, regions, types []any
	for _, u := range reg.Units {
		units = append(units, u)
	}
	for _, c := range reg.Counties {
		counties = append(counties, c)
	}
	for _, r := range reg.Regions {
		regions = append(regions, r)
	}
	for _, t := range Types {
		types = append(types, t)
	}

	return dataset.Dataset{
		Name:  "siruta",
		Title: "Administrative-territorial units of Romania (SIRUTA)",
		Description: "The register of administrative-territorial units of Romania kept by the National Institute of Statistics (INS), edition " + edition.Label() + ": " +
			"counties and Bucharest, municipalities, towns and communes, and the localities and Bucharest sectors in them, with their SIRUTA codes. " +
			"Names are in capitals as INS writes them. INS writes them without Romanian letters; the letters come from the copy INS publishes on data.gov.ro, " +
			"used only where its letters are the INS letters with the marks, and written with the comma below. " +
			fmt.Sprintf("%d names keep the INS spelling without the marks, because the two copies spell them differently. ", len(reg.Unmarked)) +
			"The register has no history and no code has left it since 2013. A code is never removed here: if INS drops one, the dataset is not rebuilt until a person decides how it ends. " +
			"The check digit of the INS methodology does not hold for some codes, so do not reject a code by it.",
		Source: dataset.Source{
			Name: "National Institute of Statistics (INS), SIRUTA register, edition " + edition.Label() + " (" + edition.Address + "); " +
				"names with Romanian letters from the SIRUTA files INS publishes on data.gov.ro",
			URL:     Page,
			Licence: "CC BY 4.0, under which INS publishes SIRUTA on data.gov.ro (https://data.gov.ro/organization/institutul-national-de-statistica)",
		},
		Frequency: dataset.Monthly,
		Update:    "INS publishes a new edition twice a year, for the first half and for the whole year",
		Files: []dataset.File{
			{
				Name: "units.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The SIRUTA code"},
					{Name: "name", Description: "The name, in capitals: JUDEȚUL ALBA, MUNICIPIUL ALBA IULIA, ALBA IULIA"},
					{Name: "parent", Description: "The code of the unit it belongs to, or null for a county. A locality belongs to its municipality, town or commune, which belongs to its county"},
					{Name: "type", Description: "The type of unit, a code of types.jsonl"},
					{Name: "level", Description: "1 for a county or Bucharest; 2 for a municipality, town or commune; 3 for a locality or a Bucharest sector"},
					{Name: "urban", Description: "True for an urban unit, false for a rural one, null for a county"},
					{Name: "county", Description: "The county, a code of counties.jsonl"},
					{Name: "region", Description: "The development region, a code of regions.jsonl"},
					{Name: "postal_code", Description: "The postal code INS gives a locality, six digits, or null. A town with streets has more postal codes than this one"},
					{Name: "nuts3", Description: "The NUTS 3 code of its county"},
					{Name: "lau", Description: "The LAU code of a municipality, town or commune, or null"},
				},
				Records: units,
			},
			{
				Name: "counties.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The county code of INS, the county field of units.jsonl. Bucharest is 40"},
					{Name: "mnemonic", Description: "The county letters: AB for Alba, B for Bucharest"},
					{Name: "name", Description: "The name of the county unit in units.jsonl"},
					{Name: "unit", Description: "The SIRUTA code of the county"},
					{Name: "region", Description: "The development region, a code of regions.jsonl"},
				},
				Records: counties,
			},
			{
				Name: "regions.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The number of the development region, 1 to 8"},
					{Name: "unit", Description: "The SIRUTA code INS gives the region; it is not a unit of units.jsonl"},
					{Name: "name", Description: "The name, as INS writes it, without Romanian letters"},
				},
				Records: regions,
			},
			{
				Name: "types.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The type code"},
					{Name: "name", Description: "The name of the type, from the INS methodology"},
				},
				Records: types,
			},
		},
	}
}
