// Package taxoffices builds the fiscal units of ANAF and the localities each serves, from the page of
// territorial competence ANAF publishes.
package taxoffices

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

// Page is the page of ANAF that lists the fiscal units and the localities they serve.
const Page = "https://static.anaf.ro/static/10/Anaf/AsistentaContribuabili_r/telefoane_judete/Regiuni.htm"

// maxPage is many times the size of the page.
const maxPage = 8 << 20

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the page over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "text/html") }
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

// County is a county of SIRUTA.
type County struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

// sectorType is the SIRUTA type of a sector of Bucharest, which is served like an administrative unit.
const sectorType = 6

// Names gives the SIRUTA codes of the localities ANAF writes another way than SIRUTA, by county code and
// the name as ANAF writes it: an older spelling, a short form, two units without the comma between them,
// or a sector of Bucharest.
var Names = map[int]map[string][]int{
	1:  {"Crăciunelul de Jos": {4188}, "Vadul Moților": {8229}},
	3:  {"Berevoiești": {14405}},
	5:  {"Săcuieni": {30915}, "Suplacul de Barcău": {31262}},
	8:  {"Ticuș": {42076}},
	13: {"Valul lui Traian": {63125}, "August": {60598}},
	15: {"Moroieni": {68182}, "Crânguri": {67167}},
	16: {"Lipov": {72953}, "Mischii Mârșani": {73246, 73317}},
	20: {"Telicu Inferior": {86936}},
	21: {"Buiești": {179793}},
	22: {"Golăești": {97090}, "Al.I.Cuza": {95612}, "Prisecani": {98603}},
	23: {"Ștefănești": {105419}, "Dărăști": {102525}, "Dragomirești": {102605}},
	27: {"Bârgăoani": {121466}, "Piatra Șimului": {123479}, "Gherăiești": {122668}},
	29: {"Șotriile": {135547}},
	32: {"Bratei": {144349}},
	33: {"Mitocul Dragomirnei": {146325}, "Dohasca": {148006}},
	34: {"Roșiorii de Vede": {151870}, "Trivale-Moșteni": {154754}},
	38: {"Gușoieni": {170444}},
	39: {"Andreiașu": {175126}, "Tâmboiești": {178046}},
	40: {"Sectorul 1": {179141}, "Sectorul 2": {179150}, "Sectorul 3": {179169}, "Sectorul 4": {179178}, "Sectorul 5": {179187}, "Sectorul 6": {179196}},
}

// kinds name the kind of a unit by the start of its name.
var kinds = []struct{ prefix, kind string }{
	{"Direcția Generală Regională a Finanțelor Publice", "regional"},
	{"Administrația Județeană a Finanțelor Publice", "county"},
	{"Administrația Sectorului", "sector"},
	{"Unitatea Fiscală Municipală", "municipal"},
	{"Unitatea Fiscală Orășenească", "town"},
	{"Unitatea Fiscală Comunală", "commune"},
	{"Serviciul fiscal comunal", "commune"},
	{"Administrația Fiscală pentru Contribuabili Mijlocii", "medium-taxpayers"},
	{"Administrația Fiscală pentru Contribuabili Nerezidenți", "non-residents"},
}

// Unit is a fiscal unit.
type Unit struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Regional *string `json:"regional"`
	County   *int    `json:"county"`
	Contact  string  `json:"contact"`
}

// Area is a locality and the fiscal unit that serves it.
type Area struct {
	SIRUTA int    `json:"siruta"`
	UAT    int    `json:"uat"`
	Unit   string `json:"unit"`
	Name   string `json:"name"`
}

var (
	regionalPattern = regexp.MustCompile(`(?s)<button class="accordion"[^>]*>\s*<h3[^>]*>(.*?)</h3>(.*?)</button>`)
	chapterPattern  = regexp.MustCompile(`(?s)<p class="stilCapitol">(.*?)</p>`)
	rowPattern      = regexp.MustCompile(`(?s)<tr>\s*<td>(.*?)</td>\s*<td[^>]*>(.*?)</td>`)
	namePattern     = regexp.MustCompile(`(?s)^\s*<strong>(.*?)</strong>`)
	breakPattern    = regexp.MustCompile(`(?i)<br\s*/?>`)
	tagPattern      = regexp.MustCompile(`<[^>]*>`)
	placePrefix     = regexp.MustCompile(`^(MUNICIPIUL|ORAȘ|ORAŞ)\s+`)
)

// text gives the words of an HTML fragment.
func text(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))), " ")
}

// contact gives the lines of a contact fragment, up to the general assistance below the rule, joined by
// semicolons.
func contact(fragment string) string {
	if i := strings.Index(fragment, "<hr>"); i >= 0 {
		fragment = fragment[:i]
	}
	var lines []string
	for _, l := range breakPattern.Split(fragment, -1) {
		if t := text(l); t != "" {
			lines = append(lines, t)
		}
	}
	return strings.Join(lines, "; ")
}

// fold writes a name in capitals without Romanian letters, spaces or signs, to compare names. The i and the
// a with circumflex are one sound written two ways, and ANAF writes some names with a tilde for a breve.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case 'Ă', 'Â', 'Î', 'Ã':
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

// ID turns the name of a unit into its identifier: lower case letters without Romanian letters, digits and
// dashes.
func ID(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch r {
		case 'ă', 'â', 'ã':
			r = 'a'
		case 'î':
			r = 'i'
		case 'ș', 'ş':
			r = 's'
		case 'ț', 'ţ':
			r = 't'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	return b.String()
}

func kind(name string) (string, error) {
	for _, k := range kinds {
		if strings.HasPrefix(name, k.prefix) {
			return k.kind, nil
		}
	}
	return "", fmt.Errorf("the unit %q is of a kind the build does not know", name)
}

// matcher finds the SIRUTA unit of a locality in a county.
type matcher struct {
	byCounty map[int][]Place
	byCode   map[int]Place
	counties map[string]int
}

func newMatcher(places []Place, counties []County) matcher {
	m := matcher{byCounty: map[int][]Place{}, byCode: map[int]Place{}, counties: map[string]int{}}
	for _, p := range places {
		m.byCounty[p.County] = append(m.byCounty[p.County], p)
		m.byCode[p.Code] = p
	}
	for _, c := range counties {
		name := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(c.Name, "JUDEȚUL "), "JUDEŢUL "), "MUNICIPIUL ")
		m.counties[fold(name)] = c.Code
	}
	return m
}

// uat gives the administrative unit a SIRUTA place is in: itself for a unit or a sector, its parent for a
// locality of a unit.
func (m matcher) uat(p Place) (int, error) {
	if p.Level == 2 || p.Type == sectorType {
		return p.Code, nil
	}
	if p.Parent == nil {
		return 0, fmt.Errorf("SIRUTA %d has no parent", p.Code)
	}
	return *p.Parent, nil
}

// match gives the SIRUTA places of a name: an administrative unit of the county, else a village of it.
func (m matcher) match(county int, name string) ([]Place, error) {
	if codes, ok := Names[county][name]; ok {
		var out []Place
		for _, c := range codes {
			p, ok := m.byCode[c]
			if !ok || p.County != county {
				return nil, fmt.Errorf("Names gives %d for %q, which is not in its county in SIRUTA", c, name)
			}
			out = append(out, p)
		}
		return out, nil
	}
	// The second pass also takes a plain I for the a with circumflex, as SIRUTA writes some names (RAMETEA
	// for Rimetea).
	loose := func(s string) string { return strings.ReplaceAll(fold(s), "I", "A") }
	for _, compare := range []func(string) string{fold, loose} {
		for _, level := range []int{2, 3} {
			var found []Place
			for _, p := range m.byCounty[county] {
				if p.Level == level && compare(placePrefix.ReplaceAllString(p.Name, "")) == compare(name) {
					found = append(found, p)
				}
			}
			if len(found) == 1 {
				return found, nil
			}
			if len(found) > 1 {
				return nil, fmt.Errorf("%q matches %d places of SIRUTA in county %d: a person decides", name, len(found), county)
			}
		}
	}
	return nil, fmt.Errorf("%q is not in SIRUTA in county %d: a person decides", name, county)
}

// Read reads the page and gives the units and the areas they serve. A unit or locality the build cannot
// place stops it, as does a locality served by two units.
func Read(page []byte, places []Place, counties []County) ([]Unit, []Area, error) {
	m := newMatcher(places, counties)
	var units []Unit
	var areas []Area
	ids := map[string]bool{}
	served := map[int]string{}
	addUnit := func(u Unit) error {
		if ids[u.ID] {
			return fmt.Errorf("the unit %s is listed twice", u.ID)
		}
		ids[u.ID] = true
		units = append(units, u)
		return nil
	}

	s := string(page)
	regional := regionalPattern.FindAllStringSubmatchIndex(s, -1)
	// Every directorate, chapter and row the page has must be read; a change of markup stops the build.
	switch {
	case len(regional) == 0:
		return nil, nil, fmt.Errorf("the page lists no regional directorate")
	case len(regional) != strings.Count(s, `class="accordion"`):
		return nil, nil, fmt.Errorf("the page has %d directorates, %d read", strings.Count(s, `class="accordion"`), len(regional))
	case len(chapterPattern.FindAllString(s, -1)) != strings.Count(s, `class="stilCapitol"`):
		return nil, nil, fmt.Errorf("the page has chapters the build does not read")
	case len(rowPattern.FindAllString(s, -1)) != strings.Count(s, "<td><strong>"):
		return nil, nil, fmt.Errorf("the page has units the build does not read")
	}
	for i, r := range regional {
		name := text(s[r[2]:r[3]])
		if k, err := kind(name); err != nil || k != "regional" {
			return nil, nil, fmt.Errorf("the directorate %q is not a regional one", name)
		}
		regionalID := ID(name)
		if err := addUnit(Unit{ID: regionalID, Name: name, Kind: "regional", Contact: contact(s[r[4]:r[5]])}); err != nil {
			return nil, nil, err
		}
		end := len(s)
		if i+1 < len(regional) {
			end = regional[i+1][0]
		}
		section := s[r[1]:end]
		chapters := chapterPattern.FindAllStringSubmatchIndex(section, -1)
		if len(chapters) == 0 {
			return nil, nil, fmt.Errorf("the directorate %q lists no county", name)
		}
		for j, c := range chapters {
			heading := text(section[c[2]:c[3]])
			at := strings.LastIndex(heading, "Publice ")
			if at < 0 {
				return nil, nil, fmt.Errorf("the heading %q names no county", heading)
			}
			county, ok := m.counties[fold(heading[at+len("Publice "):])]
			if !ok {
				return nil, nil, fmt.Errorf("the county of %q is not in SIRUTA", heading)
			}
			chapterEnd := len(section)
			if j+1 < len(chapters) {
				chapterEnd = chapters[j+1][0]
			}
			rows := rowPattern.FindAllStringSubmatch(section[c[1]:chapterEnd], -1)
			if len(rows) == 0 {
				return nil, nil, fmt.Errorf("%q lists no unit", heading)
			}
			for _, row := range rows {
				n := namePattern.FindStringSubmatch(row[1])
				if n == nil {
					return nil, nil, fmt.Errorf("a unit of %q has no name", heading)
				}
				unitName := text(n[1])
				k, err := kind(unitName)
				if err != nil {
					return nil, nil, err
				}
				rest := row[1][len(n[0]):]
				rest = strings.TrimPrefix(strings.TrimSpace(rest), "<br>")
				regionalOf, countyOf := regionalID, county
				unit := Unit{ID: ID(unitName), Name: unitName, Kind: k, Regional: &regionalOf, County: &countyOf, Contact: contact(rest)}
				if err := addUnit(unit); err != nil {
					return nil, nil, err
				}

				for _, locality := range strings.Split(text(row[2]), ",") {
					locality = strings.TrimSpace(locality)
					if locality == "" {
						continue
					}
					found, err := m.match(county, locality)
					if err != nil {
						return nil, nil, fmt.Errorf("%s: %w", unitName, err)
					}
					for _, p := range found {
						if other, twice := served[p.Code]; twice {
							return nil, nil, fmt.Errorf("SIRUTA %d (%s) is served by %s and %s: a person decides", p.Code, locality, other, unit.ID)
						}
						served[p.Code] = unit.ID
						u, err := m.uat(p)
						if err != nil {
							return nil, nil, err
						}
						areas = append(areas, Area{SIRUTA: p.Code, UAT: u, Unit: unit.ID, Name: locality})
					}
				}
			}
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	sort.Slice(areas, func(i, j int) bool { return areas[i].SIRUTA < areas[j].SIRUTA })
	return units, areas, nil
}

// Published are the keys of the dataset as published before.
type Published struct {
	Units []string
	Areas []int
}

// PublishedUnits reads units.jsonl of the published dataset.
func PublishedUnits(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for decoder.More() {
		var u Unit
		if err := decoder.Decode(&u); err != nil {
			return nil, fmt.Errorf("published units: %w", err)
		}
		out = append(out, u.ID)
	}
	return out, nil
}

// PublishedAreas reads areas.jsonl of the published dataset.
func PublishedAreas(r io.Reader) ([]int, error) {
	var out []int
	decoder := json.NewDecoder(r)
	for decoder.More() {
		var a Area
		if err := decoder.Decode(&a); err != nil {
			return nil, fmt.Errorf("published areas: %w", err)
		}
		out = append(out, a.SIRUTA)
	}
	return out, nil
}

// Dataset reads the page and builds the dataset. A unit or area published before and missing now stops the
// build.
func Dataset(get Get, places []Place, counties []County, published Published) (dataset.Dataset, error) {
	body, err := get(Page)
	if err != nil {
		return dataset.Dataset{}, err
	}
	defer body.Close()
	page, err := io.ReadAll(io.LimitReader(body, maxPage+1))
	if err != nil {
		return dataset.Dataset{}, err
	}
	if len(page) > maxPage {
		return dataset.Dataset{}, fmt.Errorf("%s is larger than expected", Page)
	}
	units, areas, err := Read(page, places, counties)
	if err != nil {
		return dataset.Dataset{}, err
	}

	var missing []string
	ids := map[string]bool{}
	for _, u := range units {
		ids[u.ID] = true
	}
	codes := map[int]bool{}
	for _, a := range areas {
		codes[a.SIRUTA] = true
	}
	for _, id := range published.Units {
		if !ids[id] {
			missing = append(missing, "unit "+id)
		}
	}
	for _, c := range published.Areas {
		if !codes[c] {
			missing = append(missing, fmt.Sprintf("area %d", c))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("published entries are gone: %s: a person decides", strings.Join(missing, ", "))
	}

	var unitRecords, areaRecords []any
	for _, u := range units {
		unitRecords = append(unitRecords, u)
	}
	for _, a := range areas {
		areaRecords = append(areaRecords, a)
	}
	return dataset.Dataset{
		Name:  "tax-offices",
		Title: "ANAF fiscal units and the localities they serve",
		Description: "The regional directorates of public finance, the county administrations and the fiscal units of ANAF, with their contact, " +
			"and for each locality of SIRUTA that ANAF names the unit that serves it, under the territorial competence set by Order ANAF 3631/2015. " +
			"A locality is mostly an administrative unit; ANAF names some villages and the sectors of Bucharest, and uat gives the administrative unit each is in. " +
			"To find the unit of an address, look up its village first, then its administrative unit. " +
			"Large taxpayers are served by the Directorate General for Large Taxpayers (DGAMC), which the page does not list; " +
			"medium taxpayers and non-residents have units of their own, which serve no locality here. " +
			"Names and contacts are as ANAF writes them.",
		Source: dataset.Source{
			Name:    "ANAF, territorial competence of the fiscal units (" + Page + ")",
			URL:     Page,
			Licence: "Facts taken from the page: the units, their contact and the localities each serves, which Order ANAF 3631/2015 sets. ANAF states no licence for the page",
		},
		Frequency: dataset.Monthly,
		Update:    "When ANAF changes its units or the localities they serve",
		Files: []dataset.File{
			{
				Name: "units.jsonl",
				Fields: []dataset.Field{
					{Name: "id", Description: "The identifier of the unit, made from its name, unique in the file"},
					{Name: "name", Description: "The name as ANAF writes it"},
					{Name: "kind", Description: "regional, county, sector, municipal, town, commune, medium-taxpayers or non-residents"},
					{Name: "regional", Description: "The id of the regional directorate the unit is under, or null for a directorate"},
					{Name: "county", Description: "The county, a code of counties.jsonl in siruta, or null"},
					{Name: "contact", Description: "The address and telephone lines, joined by semicolons"},
				},
				Records: unitRecords,
			},
			{
				Name: "areas.jsonl",
				Fields: []dataset.Field{
					{Name: "siruta", Description: "The locality served, a code of units.jsonl in siruta, unique in the file"},
					{Name: "uat", Description: "The administrative unit or Bucharest sector the locality is in, a code of units.jsonl in siruta"},
					{Name: "unit", Description: "The id of the fiscal unit that serves it"},
					{Name: "name", Description: "The locality as ANAF writes it"},
				},
				Records: areaRecords,
			},
		},
	}, nil
}
