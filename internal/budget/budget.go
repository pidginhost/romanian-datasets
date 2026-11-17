// Package budget builds the classification of public finance indicators (OMFP 1954/2005) from the annexes
// the Ministry of Finance publishes as workbooks: the codes of revenue and expense of each budget, and the
// departmental codes.
package budget

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
	"github.com/pidginhost/romanian-datasets/internal/sheet"
)

// Page is the page of the Ministry of Finance that links the annexes in force.
const Page = "https://mfinante.gov.ro/ro/domenii/buget/clasificatiile-bugetare"

// Site is the address the links of the page are relative to.
const Site = "https://mfinante.gov.ro"

// maxPage and maxBook are many times the largest page and workbook.
const (
	maxPage = 8 << 20
	maxBook = 32 << 20
)

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

// Annexes names the budget of each annex of indicators, by the label of its link. An annex the page links
// and that is not here or in Departmental stops the build.
var Annexes = map[string]string{
	"Anexa I":             "all public budgets, extended form",
	"Anexa I (economica)": "all public budgets, economic classification",
	"Anexa 1":             "state budget",
	"Anexa 2":             "local budgets",
	"Anexa 3":             "state social insurance budget",
	"Anexa 4":             "unemployment insurance budget",
	"Anexa 5":             "national single health insurance fund budget",
	"Anexa 6":             "external credits",
	"Anexa 7":             "internal credits",
	"Anexa 8":             "non-refundable external funds budget",
	"Anexa 9":             "state treasury budget",
	"Anexa 10":            "public institutions financed in full or in part from own revenues",
}

// Departmental are the annexes of departmental codes, by the label of their link.
var Departmental = map[string]bool{"Anexa II": true, "Anexa III": true}

// Indicator is one code of the classification.
type Indicator struct {
	Year   int    `json:"year"`
	Annex  string `json:"annex"`
	Budget string `json:"budget"`
	Part   string `json:"part"`
	View   string `json:"view"`
	Code   string `json:"code"`
	Level  string `json:"level"`
	// Parent is the longest code above this one that its sheet has, or null.
	Parent  *string  `json:"parent"`
	Name    string   `json:"name"`
	Section *string  `json:"section"`
	Notes   []string `json:"notes"`
	Remark  *string  `json:"remark"`
}

// Key identifies an indicator in its file.
func (i Indicator) Key() string {
	return fmt.Sprintf("%d|%s|%s|%s|%s", i.Year, i.Annex, i.Part, i.View, i.Code)
}

// Department is one departmental code.
type Department struct {
	Year  int    `json:"year"`
	Annex string `json:"annex"`
	// Section is the heading above the code: an annex numbers each of its sections from 1.
	Section *string `json:"section"`
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Remark  *string `json:"remark"`
}

// Key identifies a department in its file.
func (d Department) Key() string {
	section := ""
	if d.Section != nil {
		section = *d.Section
	}
	return fmt.Sprintf("%d|%s|%s|%s", d.Year, d.Annex, section, d.Code)
}

var (
	linkPattern   = regexp.MustCompile(`(?is)<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	tagPattern    = regexp.MustCompile(`<[^>]*>`)
	yearPattern   = regexp.MustCompile(`în vigoare pentru anul (\d{4})`)
	codePattern   = regexp.MustCompile(`^\d{2}(\.\d{2}){0,3}$`)
	markPattern   = regexp.MustCompile(`\s*(\*+)\)?$`)
	notePattern   = regexp.MustCompile(`^(\*+)\)?\s*(.*)$`)
	numberPattern = regexp.MustCompile(`^\d+$`)
	wordPattern   = regexp.MustCompile(`[\pL\pN]`)
)

func text(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))), " ")
}

// code reads a code of the classification as the workbook writes it: it may end with a point or with the
// stars of a note, and a point may be written as a space. It gives the code and the stars.
func code(cell string) (string, string, bool) {
	c := strings.TrimSpace(cell)
	mark := ""
	if m := markPattern.FindStringSubmatch(c); m != nil {
		mark = m[1]
		c = strings.TrimSpace(c[:len(c)-len(m[0])])
	}
	c = strings.TrimSuffix(c, ".")
	c = strings.Join(strings.Fields(c), ".")
	return c, mark, codePattern.MatchString(c)
}

// Links reads the page and gives the year of the classification and the address of each annex by label.
func Links(page string) (int, map[string]string, error) {
	m := yearPattern.FindStringSubmatch(text(page))
	if m == nil {
		return 0, nil, fmt.Errorf("the page names no year of the classification")
	}
	year, _ := strconv.Atoi(m[1])
	out := map[string]string{}
	for _, l := range linkPattern.FindAllStringSubmatch(page, -1) {
		label := text(l[2])
		if _, ok := Annexes[label]; !ok && !Departmental[label] {
			if strings.HasPrefix(label, "Anexa") {
				return 0, nil, fmt.Errorf("the page links %q, an annex the build does not know", label)
			}
			continue
		}
		address := l[1]
		if strings.HasPrefix(address, "/") {
			address = Site + address
		}
		if !strings.HasPrefix(address, "https://") {
			return 0, nil, fmt.Errorf("the annex %s is at %q", label, address)
		}
		out[label] = address
	}
	for label := range Annexes {
		if out[label] == "" {
			return 0, nil, fmt.Errorf("the page does not link %s", label)
		}
	}
	for label := range Departmental {
		if out[label] == "" {
			return 0, nil, fmt.Errorf("the page does not link %s", label)
		}
	}
	return year, out, nil
}

var levels = map[string][3]string{
	"functional": {"chapter", "subchapter", "paragraph"},
	"economic":   {"title", "article", "paragraph"},
}

// ReadIndicators reads the sheets of an annex of indicators.
func ReadIndicators(year int, label string, sheets []sheet.Sheet) ([]Indicator, error) {
	var out []Indicator
	for _, s := range sheets {
		head := -1
		for i := range s.Rows {
			if strings.EqualFold(strings.TrimSpace(s.Cell(i, 3)), "DENUMIREA INDICATORILOR") {
				head = i
				break
			}
		}
		if head < 0 {
			return nil, fmt.Errorf("%s, sheet %s: no head of the table", label, s.Name)
		}
		view := "functional"
		if strings.EqualFold(strings.TrimSpace(s.Cell(head, 0)), "Titlu") {
			view = "economic"
		}
		part := ""
		for i := 0; i < head; i++ {
			line := strings.ToUpper(strings.Join(s.Rows[i], " "))
			switch {
			case strings.Contains(line, "VENITURI"):
				part = "revenue"
			case strings.Contains(line, "CHELTUIELI"):
				part = "expense"
			}
		}
		if part == "" {
			return nil, fmt.Errorf("%s, sheet %s: neither revenue nor expense", label, s.Name)
		}

		var rows []Indicator
		notes := map[string]string{}
		var lastNote string
		inSheet := map[string]bool{}
		section := ""
		inNotes := false
		for i := head + 1; i < len(s.Rows); i++ {
			cells := s.Rows[i]
			first := strings.TrimSpace(s.Cell(i, 0))
			if strings.HasPrefix(first, "N O T") {
				inNotes = true
				continue
			}
			if inNotes {
				line := strings.TrimSpace(strings.Join(strings.Fields(strings.Join(cells, " ")), " "))
				if m := notePattern.FindStringSubmatch(line); m != nil && line != "" {
					notes[m[1]] = m[2]
					lastNote = m[1]
				} else if lastNote != "" && line != "" {
					notes[lastNote] += " " + line
				}
				continue
			}
			name := strings.Join(strings.Fields(s.Cell(i, 3)), " ")
			var found []int
			var remarks []string
			var mark string
			codeOf := ""
			for col := 0; col < 3; col++ {
				cell := strings.TrimSpace(s.Cell(i, col))
				if cell == "" {
					continue
				}
				if c, m, ok := code(cell); ok {
					found = append(found, col)
					codeOf, mark = c, m
				} else {
					remarks = append(remarks, cell)
				}
			}
			for col := 4; col < len(cells); col++ {
				if c := strings.TrimSpace(cells[col]); c != "" {
					remarks = append(remarks, c)
				}
			}
			switch {
			case len(found) == 0 && name != "":
				section = name
				continue
			case len(found) == 0 && len(remarks) > 0:
				section = strings.Join(remarks, " ")
				continue
			case len(found) == 0:
				continue
			case len(found) > 1:
				return nil, fmt.Errorf("%s, sheet %s, row %d: two codes", label, s.Name, i+1)
			case name == "":
				return nil, fmt.Errorf("%s, sheet %s, row %d: code %s has no name", label, s.Name, i+1, codeOf)
			}
			// The stars of a note can follow the code or the name; the name stays as the ministry writes it.
			if mark == "" {
				if m := markPattern.FindStringSubmatch(name); m != nil {
					mark = m[1]
				}
			}
			col := found[0]
			ind := Indicator{Year: year, Annex: label, Budget: Annexes[label], Part: part, View: view, Code: codeOf, Level: levels[view][col], Name: name}
			// The parent is the longest code above it that the sheet has. The ministry writes some rows out
			// of place, and some codes have no code above them; those have none.
			for up := codeOf; strings.Contains(up, "."); {
				up = up[:strings.LastIndex(up, ".")]
				if inSheet[up] {
					parent := up
					ind.Parent = &parent
					break
				}
			}
			inSheet[codeOf] = true
			if section != "" {
				sec := section
				ind.Section = &sec
			}
			if mark != "" {
				ind.Notes = []string{mark}
			}
			// A stray sign in a code column is formatting, not a remark.
			var kept []string
			for _, r := range remarks {
				if wordPattern.MatchString(r) {
					kept = append(kept, r)
				}
			}
			if len(kept) > 0 {
				r := strings.Join(kept, " ")
				ind.Remark = &r
			}
			rows = append(rows, ind)
		}
		for k := range rows {
			if rows[k].Notes != nil {
				mark := rows[k].Notes[0]
				if n, ok := notes[mark]; ok {
					rows[k].Notes = []string{n}
				} else {
					rows[k].Notes = nil
				}
			}
		}
		out = append(out, rows...)
	}
	seen := map[string]bool{}
	for _, ind := range out {
		if seen[ind.Key()] {
			return nil, fmt.Errorf("%s: code %s is written twice in its part", label, ind.Code)
		}
		seen[ind.Key()] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no codes", label)
	}
	return out, nil
}

// ReadDepartments reads an annex of departmental codes: a column of codes and a column of names after it.
func ReadDepartments(year int, label string, sheets []sheet.Sheet) ([]Department, error) {
	var out []Department
	seen := map[string]bool{}
	for _, s := range sheets {
		col := -1
		head := -1
		for i := range s.Rows {
			for c := 0; c < 3; c++ {
				if strings.TrimSpace(s.Cell(i, c)) == "Cod" {
					col, head = c, i
				}
			}
			if head >= 0 {
				break
			}
		}
		if head < 0 {
			return nil, fmt.Errorf("%s, sheet %s: no head of the table", label, s.Name)
		}
		section := ""
		for i := head + 1; i < len(s.Rows); i++ {
			c := strings.TrimSpace(s.Cell(i, col))
			name := strings.Join(strings.Fields(s.Cell(i, col+1)), " ")
			if !numberPattern.MatchString(c) {
				if c != "" && name == "" {
					section = strings.Join(strings.Fields(c), " ")
				}
				continue
			}
			if name == "" {
				return nil, fmt.Errorf("%s, row %d: code %s has no name", label, i+1, c)
			}
			d := Department{Year: year, Annex: label, Code: c, Name: name}
			if section != "" {
				sec := section
				d.Section = &sec
			}
			var remarks []string
			for k := col + 2; k < len(s.Rows[i]); k++ {
				if r := strings.TrimSpace(s.Rows[i][k]); r != "" {
					remarks = append(remarks, r)
				}
			}
			if len(remarks) > 0 {
				r := strings.Join(remarks, " ")
				d.Remark = &r
			}
			if seen[d.Key()] {
				return nil, fmt.Errorf("%s: code %s is written twice", label, c)
			}
			seen[d.Key()] = true
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no codes", label)
	}
	return out, nil
}

// Published is the dataset as published before: its rows, which the build keeps for the other years.
type Published struct {
	Indicators  []Indicator
	Departments []Department
}

// ReadPublished reads the files of the published dataset.
func ReadPublished(indicators, departments io.Reader) (Published, error) {
	var p Published
	if indicators != nil {
		d := json.NewDecoder(indicators)
		for d.More() {
			var i Indicator
			if err := d.Decode(&i); err != nil {
				return p, fmt.Errorf("published indicators: %w", err)
			}
			p.Indicators = append(p.Indicators, i)
		}
	}
	if departments != nil {
		d := json.NewDecoder(departments)
		for d.More() {
			var x Department
			if err := d.Decode(&x); err != nil {
				return p, fmt.Errorf("published departments: %w", err)
			}
			p.Departments = append(p.Departments, x)
		}
	}
	return p, nil
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

func indicatorLess(a, b Indicator) bool {
	if a.Year != b.Year {
		return a.Year < b.Year
	}
	if a.Annex != b.Annex {
		return annexOrder(a.Annex) < annexOrder(b.Annex)
	}
	if a.Part != b.Part {
		return a.Part > b.Part // revenue before expense
	}
	if a.View != b.View {
		return a.View > b.View // functional before economic
	}
	return a.Code < b.Code
}

// annexOrder sorts the annexes as the page lists them: I, I (economica), then 1 to 10.
func annexOrder(label string) string {
	n := strings.TrimPrefix(label, "Anexa ")
	if v, err := strconv.Atoi(n); err == nil {
		return fmt.Sprintf("B%03d", v)
	}
	return "A" + n
}

// Dataset reads the page and the annexes of the year in force, and keeps the published rows of the other
// years. A row of the year in force published before and missing now stops the build.
func Dataset(get Get, published Published) (dataset.Dataset, error) {
	page, err := read(get, Page, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	year, links, err := Links(string(page))
	if err != nil {
		return dataset.Dataset{}, err
	}
	var labels []string
	for label := range links {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool { return annexOrder(labels[i]) < annexOrder(labels[j]) })

	var indicators []Indicator
	var departments []Department
	for _, label := range labels {
		data, err := read(get, links[label], maxBook)
		if err != nil {
			return dataset.Dataset{}, err
		}
		sheets, err := sheet.Read(data)
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("%s: %w", links[label], err)
		}
		if Departmental[label] {
			d, err := ReadDepartments(year, label, sheets)
			if err != nil {
				return dataset.Dataset{}, err
			}
			departments = append(departments, d...)
			continue
		}
		ind, err := ReadIndicators(year, label, sheets)
		if err != nil {
			return dataset.Dataset{}, err
		}
		indicators = append(indicators, ind...)
	}

	have := map[string]bool{}
	for _, i := range indicators {
		have[i.Key()] = true
	}
	for _, d := range departments {
		have["d|"+d.Key()] = true
	}
	var missing []string
	for _, i := range published.Indicators {
		switch {
		case i.Year != year:
			indicators = append(indicators, i)
		case !have[i.Key()]:
			missing = append(missing, i.Key())
		}
	}
	for _, d := range published.Departments {
		switch {
		case d.Year != year:
			departments = append(departments, d)
		case !have["d|"+d.Key()]:
			missing = append(missing, d.Key())
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("published codes of %d are gone: %s: a person decides", year, strings.Join(missing, ", "))
	}
	sort.SliceStable(indicators, func(i, j int) bool { return indicatorLess(indicators[i], indicators[j]) })
	// Departments keep the order of their annex, which numbers each section from 1.
	sort.SliceStable(departments, func(i, j int) bool {
		a, b := departments[i], departments[j]
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		return a.Annex < b.Annex
	})

	var indicatorRecords, departmentRecords []any
	for _, i := range indicators {
		indicatorRecords = append(indicatorRecords, i)
	}
	for _, d := range departments {
		departmentRecords = append(departmentRecords, d)
	}
	return dataset.Dataset{
		Name:  "budget-classification",
		Title: "Classification of public finance indicators",
		Description: "The classification of public finance indicators approved by OMFP 1954/2005, as the Ministry of Finance publishes it for each year: " +
			"for each budget, the revenue codes and the functional expense codes (chapter, subchapter, paragraph), the economic expense codes of " +
			"annex I (title, article, paragraph), and the departmental codes of annexes II and III. The second part of a code names the budget: " +
			"01.01 is chapter 01 of the state budget. A code with notes was marked by the ministry with stars; notes holds the note where the sheet " +
			"explains it. section is the heading above the code. remark is any other text the ministry wrote on the row. The rows of a year stay when " +
			"the ministry publishes the next. Names are as the ministry writes them.",
		Source: dataset.Source{
			Name:    "Ministry of Finance, classification of public finance indicators in force (" + Page + ")",
			URL:     Page,
			Licence: "Legislation, not subject to copyright (Law 8/1996, article 9): annexes of OMFP 1954/2005 as the ministry keeps them up to date",
		},
		Frequency: dataset.Monthly,
		Update:    "When the ministry changes the classification, which it does by notes several times a year",
		Files: []dataset.File{
			{
				Name: "indicators.jsonl",
				Fields: []dataset.Field{
					{Name: "year", Description: "The year the classification is in force"},
					{Name: "annex", Description: "The annex, as Anexa 1"},
					{Name: "budget", Description: "The budget the annex classifies"},
					{Name: "part", Description: "revenue or expense"},
					{Name: "view", Description: "functional, or economic for annex I (economica)"},
					{Name: "code", Description: "The code; with year, annex, part and view it is unique in the file"},
					{Name: "level", Description: "chapter, subchapter or paragraph; title, article or paragraph in the economic view"},
					{Name: "parent", Description: "The longest code above this one in its sheet, or null where the sheet has none"},
					{Name: "name", Description: "The name as the ministry writes it"},
					{Name: "section", Description: "The heading above the code, or null"},
					{Name: "notes", Description: "The note the stars of the code refer to, or null"},
					{Name: "remark", Description: "Other text on the row, or null"},
				},
				Records: indicatorRecords,
			},
			{
				Name: "departments.jsonl",
				Fields: []dataset.Field{
					{Name: "year", Description: "The year the classification is in force"},
					{Name: "annex", Description: "Anexa II for the authorities and institutions of the central and local administration, Anexa III for those financed in full from own revenues"},
					{Name: "section", Description: "The heading above the code, or null: the annex numbers each section from 1"},
					{Name: "code", Description: "The departmental code; with year, annex and section it is unique in the file"},
					{Name: "name", Description: "The name as the ministry writes it"},
					{Name: "remark", Description: "Other text on the row, as the act the code rests on, or null"},
				},
				Records: departmentRecords,
			},
		},
	}, nil
}
