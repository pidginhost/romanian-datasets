// Package nc8 builds the Combined Nomenclature of goods by year, as the National Institute of Statistics
// publishes it for Intrastat: sections, chapters, headings, the unnamed levels between them and the eight
// digit codes, with their names in Romanian and English and the supplementary unit.
package nc8

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Page is the page of INS that lists the files of the nomenclature for the declarations of a year.
const Page = "http://www.intrastat.ro/di2.php"

// address gives the file of a year. The site offers no https: each file is checked by the SHA-256 in Years.
func address(year int) string { return fmt.Sprintf("http://www.intrastat.ro/doc/CN_%d.xml", year) }

const (
	maxPage = 1 << 20
	// maxFile is many times the size of the file of a year.
	maxFile = 64 << 20
)

// Year is the file of the nomenclature of one year, with the SHA-256 a person read it with.
type Year struct {
	Year   int
	SHA256 string
}

// Years are the years the dataset holds. A person adds the file of a new year when INS publishes it, after
// reading it, with its SHA-256. A file that changes stops the build.
var Years = []Year{
	{Year: 2026, SHA256: "e00b9c69e33cf7f2527dd10b563fb319a89a34ac6eabd2148eeb920ccc817a73"},
}

// Source reads the page and the file of a year.
type Source struct {
	Page func() ([]byte, error)
	File func(year Year) ([]byte, error)
}

// FromSite reads INS over plain http, the only way its site answers. The page only tells which years exist;
// a file is taken only with its recorded SHA-256.
func FromSite(client *http.Client) Source {
	return Source{
		Page: func() ([]byte, error) {
			return fetch.Unchecked(client, Page, maxPage)
		},
		File: func(y Year) ([]byte, error) { return fetch.Pinned(client, address(y.Year), maxFile, y.SHA256) },
	}
}

// Item is one line of the nomenclature of a year.
type Item struct {
	Year int `json:"year"`
	// Code is the code as INS writes it: a Roman number for a section, two digits for a chapter, four for a
	// heading, six for a subheading, eight for a code that goods are declared under, and @ with a number for
	// a level the nomenclature marks only with dashes.
	Code   string  `json:"code"`
	Level  int     `json:"level"`
	Parent *string `json:"parent"`
	NameRO string  `json:"name_ro"`
	NameEN string  `json:"name_en"`
	// Unit is the supplementary unit, as p/st or m2, or null.
	Unit *string `json:"supplementary_unit"`
	// Declarable is true for an eight digit code.
	Declarable bool `json:"declarable"`
}

type file struct {
	Version string `xml:"Version"`
	Items   []struct {
		Code   string `xml:"Code"`
		NameEN string `xml:"NameEn"`
		NameRO string `xml:"NameRo"`
		Level  string `xml:"Level"`
		Ref    string `xml:"RefCode"`
		Parent string `xml:"parentCode"`
	} `xml:"Item"`
}

var (
	codePattern = regexp.MustCompile(`^(?:[IVX]+|\d{2}|\d{4}|\d{6}|\d{8}|@\d+)$`)
	eightDigits = regexp.MustCompile(`^\d{8}$`)
	yearLink    = regexp.MustCompile(`/doc/CN_(\d{4})\.xml`)
	sectionCode = regexp.MustCompile(`^[IVX]+$`)
)

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// Read reads the file of a year.
func Read(data []byte, year int) ([]Item, error) {
	var f file
	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
		return nil, fmt.Errorf("CN %d: %w", year, err)
	}
	if strings.TrimSpace(f.Version) != strconv.Itoa(year) {
		return nil, fmt.Errorf("the file of %d is of version %q", year, f.Version)
	}
	var out []Item
	seen := map[string]bool{}
	for _, x := range f.Items {
		it := Item{Year: year, Code: clean(x.Code), NameRO: clean(x.NameRO), NameEN: clean(x.NameEN)}
		if !codePattern.MatchString(it.Code) {
			return nil, fmt.Errorf("CN %d: the code %q is not known to the build", year, it.Code)
		}
		if seen[it.Code] {
			return nil, fmt.Errorf("CN %d: the code %s is written twice", year, it.Code)
		}
		seen[it.Code] = true
		level, err := strconv.Atoi(clean(x.Level))
		if err != nil || level < 1 {
			return nil, fmt.Errorf("CN %d, %s: the level %q", year, it.Code, x.Level)
		}
		it.Level = level
		if p := clean(x.Parent); p != "" {
			it.Parent = &p
		} else if !sectionCode.MatchString(it.Code) {
			return nil, fmt.Errorf("CN %d: the code %s has no parent", year, it.Code)
		}
		if u := clean(x.Ref); u != "" {
			it.Unit = &u
		}
		if it.NameRO == "" || it.NameEN == "" {
			return nil, fmt.Errorf("CN %d: the code %s has no name", year, it.Code)
		}
		it.Declarable = eightDigits.MatchString(it.Code)
		out = append(out, it)
	}
	for _, it := range out {
		if it.Parent != nil && !seen[*it.Parent] {
			return nil, fmt.Errorf("CN %d: the parent %s of %s is not in the file", year, *it.Parent, it.Code)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CN %d: no codes", year)
	}
	return out, nil
}

// Published gives the year and code of each published item, as year|code.
func Published(r io.Reader) (map[string]bool, error) {
	out := map[string]bool{}
	d := json.NewDecoder(r)
	for d.More() {
		var it Item
		if err := d.Decode(&it); err != nil {
			return nil, fmt.Errorf("published items: %w", err)
		}
		out[fmt.Sprintf("%d|%s", it.Year, it.Code)] = true
	}
	return out, nil
}

// Dataset reads the file of each year and builds the dataset. A year INS lists after the last one in Years
// stops the build, so that a person adds it.
func Dataset(src Source, published map[string]bool) (dataset.Dataset, error) {
	page, err := src.Page()
	if err != nil {
		return dataset.Dataset{}, err
	}
	last := 0
	for _, y := range Years {
		last = max(last, y.Year)
	}
	listed := map[int]bool{}
	for _, m := range yearLink.FindAllStringSubmatch(string(page), -1) {
		y, _ := strconv.Atoi(m[1])
		listed[y] = true
		if y > last {
			return dataset.Dataset{}, fmt.Errorf("INS lists the nomenclature of %d: a person reads it and adds it to Years with its SHA-256", y)
		}
	}
	if !listed[last] {
		return dataset.Dataset{}, fmt.Errorf("the page of INS no longer lists the nomenclature of %d", last)
	}
	var items []Item
	for _, y := range Years {
		data, err := src.File(y)
		if err != nil {
			return dataset.Dataset{}, err
		}
		got, err := Read(data, y.Year)
		if err != nil {
			return dataset.Dataset{}, err
		}
		items = append(items, got...)
	}
	have := map[string]bool{}
	for _, it := range items {
		have[fmt.Sprintf("%d|%s", it.Year, it.Code)] = true
	}
	var gone []string
	for k := range published {
		if !have[k] {
			gone = append(gone, k)
		}
	}
	if len(gone) > 0 {
		sort.Strings(gone)
		return dataset.Dataset{}, fmt.Errorf("published items are gone: %s; a year is never removed", strings.Join(gone, ", "))
	}
	// The items of a year stay in the order of the file, which is the order of the nomenclature.
	sort.SliceStable(items, func(i, j int) bool { return items[i].Year < items[j].Year })
	var records []any
	for _, it := range items {
		records = append(records, it)
	}
	var years []string
	for _, y := range Years {
		years = append(years, strconv.Itoa(y.Year))
	}
	return dataset.Dataset{
		Name:  "combined-nomenclature",
		Title: "Combined Nomenclature of goods (NC8), by year",
		Description: "The Combined Nomenclature of the European Union, as the National Institute of Statistics publishes it each year for Intrastat " +
			"declarations: sections, chapters, headings and subheadings, the levels the nomenclature marks only with dashes, and the eight digit " +
			"codes that goods are declared under, which SAF-T (D406) also asks for in ProductCommodityCode. Each line has its year, its parent, its " +
			"names in Romanian (without diacritics, as INS writes them) and English, and the supplementary unit where there is one. A code is valid " +
			"for its year only; the years are kept. Years held: " + strings.Join(years, ", ") + ". The codes SAF-T adds for its own use " +
			"(99500000, 99999999) are not in the nomenclature and not here.",
		Source: dataset.Source{
			Name:    "National Institute of Statistics, Intrastat, the files CN_<year>.xml (" + Page + ")",
			URL:     Page,
			Licence: "The Combined Nomenclature is Annex I to Council Regulation (EEC) 2658/87, legislation not subject to copyright (Law 8/1996, article 9); INS states no licence for its files",
		},
		Frequency: dataset.Monthly,
		Update:    "Once a year, when INS publishes the nomenclature of the next year and a person adds it",
		Files: []dataset.File{{
			Name: "items.jsonl",
			Fields: []dataset.Field{
				{Name: "year", Description: "The year the nomenclature is for. With code it is unique in the file"},
				{Name: "code", Description: "The code: a Roman number for a section, two digits for a chapter, four for a heading, six for a subheading, eight for a declarable code, @ and a number for a level of dashes"},
				{Name: "level", Description: "The level of the line in the tree, as INS gives it, from 1 for a section"},
				{Name: "parent", Description: "The code of the line above, or null for a section"},
				{Name: "name_ro", Description: "The name in Romanian, as INS writes it"},
				{Name: "name_en", Description: "The name in English"},
				{Name: "supplementary_unit", Description: "The supplementary unit, as p/st, m2 or l, or null"},
				{Name: "declarable", Description: "True for an eight digit code, which goods are declared under"},
			},
			Records: records,
		}},
	}, nil
}
