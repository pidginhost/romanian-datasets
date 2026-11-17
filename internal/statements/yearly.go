package statements

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/history"
	"github.com/pidginhost/romanian-datasets/internal/portal"
)

// YearOrder is the order of the Ministry of Finance that sets forms 30 and 40 for one financial year.
type YearOrder struct {
	Year int
	Act  string
	// Page is the number of the order on the Legislative Portal.
	Page string
}

// Orders lists the orders, one for each financial year. A person adds the order of a new year when it is
// published, usually in the last weeks of that year.
var Orders = []YearOrder{
	{Year: 2025, Act: "OMF 2036/2025", Page: "306521"},
}

// YearForms names the forms by the entities the order says they are for, as it writes them after
// "Structura formularului*)". A form not here stops the build: a person reads it and adds it.
var YearForms = map[string]string{
	"„Date informative“ (cod 30) pentru entitățile prevăzute la pct. 2.1 din anexa nr. 1 la ordin":                                                                                                "informative-data",
	"„Date informative“ (cod 30) pentru entitățile prevăzute la pct. 2.2 și 2.3 din anexa nr. 1 la ordin și pentru entitățile prevăzute la pct. 2.1 din anexa nr. 2 la ordin":                     "informative-data-small",
	"„Situația activelor imobilizate“ (cod 40) pentru entitățile prevăzute la pct. 2.1 , 2.2 și 2.3 din anexa nr. 1 la ordin și pentru entitățile prevăzute la pct. 2.1 din anexa nr. 2 la ordin": "fixed-assets",
}

// YearChanges gives, by id (year, form and row), a change of a form a person read in the act that amended
// the order. A row that changes and is not here stops the build.
var YearChanges = map[string]history.Change{}

// FormLine is one row of form 30 or 40 for one financial year.
type FormLine struct {
	Year int    `json:"year"`
	Act  string `json:"set_by"`
	Form string `json:"form"`
	Code string `json:"code"`
	// For is the entities the order says the form is for, as it writes them.
	For   string `json:"entities"`
	Order int    `json:"order"`
	// Section is the part of the form the row is in, as the form writes its heading.
	Section   *string `json:"section"`
	Row       *string `json:"row"`
	Name      string  `json:"name"`
	Side      *string `json:"side"`
	Accounts  *string `json:"accounts"`
	Terms     []Term  `json:"account_terms"`
	Rows      *string `json:"rows"`
	ValidFrom *string `json:"valid_from"`
	ValidTo   *string `json:"valid_to"`
	ChangeAct *string `json:"act"`
}

func (l FormLine) id() string {
	if l.Row != nil {
		return fmt.Sprintf("%d|%s|%s", l.Year, l.Form, *l.Row)
	}
	return fmt.Sprintf("%d|%s|h%03d", l.Year, l.Form, l.Order)
}

// Key identifies a line in its file.
func (l FormLine) Key() string { return history.Key(l.id(), l.ValidFrom) }

func (l FormLine) content() string {
	x, _ := json.Marshal(FormLine{Year: l.Year, Act: l.Act, Form: l.Form, Code: l.Code, For: l.For, Order: l.Order, Section: l.Section,
		Row: l.Row, Name: l.Name, Side: l.Side, Accounts: l.Accounts, Terms: l.Terms, Rows: l.Rows})
	return string(x)
}

var (
	structurePattern = regexp.MustCompile(`Structura formularului\*?\)? (.+?) este următoarea`)
	titlePattern     = regexp.MustCompile(`SITUAȚIA [A-ZĂÂÎȘȚ ,]+?(?: la data de|$)`)
	codeCell         = regexp.MustCompile(`(?i)^cod (30|40)\b`)
	headingPattern   = regexp.MustCompile(`^(?:[IVX]+|[A-Z]|\d+)\.\s`)
)

// ReadYearForms reads forms 30 and 40 out of the page of an order.
func ReadYearForms(page string, o YearOrder) ([]FormLine, error) {
	page = scriptPattern.ReplaceAllString(page, "")
	start := strings.LastIndex(page, "Structura formularelor")
	if start < 0 {
		return nil, fmt.Errorf("the order has no annex of forms 30 and 40")
	}
	var out []FormLine
	numbers := map[string][]int{}
	order := map[string]int{}
	for _, t := range tablePattern.FindAllStringIndex(page[start:], -1) {
		t[0], t[1] = t[0]+start, t[1]+start
		// A table of a form opens with its code; the others hold the signatures.
		first := codeCell.FindStringSubmatch(text(page[t[0]:t[1]]))
		if first == nil {
			continue
		}
		code := first[1]
		before := text(page[start:t[0]])
		m := structurePattern.FindAllStringSubmatchIndex(before, -1)
		if m == nil {
			return nil, fmt.Errorf("%s: a table of form %s without the text that names it", o.Act, code)
		}
		last := m[len(m)-1]
		entities := strings.Replace(before[last[2]:last[3]], "*) ", "", 1)
		form, ok := YearForms[entities]
		if !ok {
			return nil, fmt.Errorf("%s: a form the build does not know: %q; a person adds it to YearForms", o.Act, entities)
		}
		if !strings.Contains(entities, "(cod "+code+")") {
			return nil, fmt.Errorf("%s: a table of form %s under the text of %q", o.Act, code, entities)
		}
		// A table of form 40 is one of its parts, named by the title above it.
		var section *string
		if code == "40" {
			near := before[max(0, len(before)-400):]
			titles := titlePattern.FindAllString(near, -1)
			if titles == nil {
				return nil, fmt.Errorf("%s: a table of form 40 without its title", o.Act)
			}
			s := strings.TrimSuffix(titles[len(titles)-1], " la data de")
			section = &s
		}
		lines, err := readFormTable(page[t[0]:t[1]], section, code == "40")
		if err != nil {
			return nil, fmt.Errorf("%s, %s: %w", o.Act, form, err)
		}
		for _, l := range lines {
			order[form]++
			l.Year, l.Act, l.Form, l.Code, l.For, l.Order = o.Year, o.Act, form, code, entities, order[form]
			if l.Row != nil {
				n, _ := strconv.Atoi(*l.Row)
				numbers[form] = append(numbers[form], n)
			}
			out = append(out, l)
		}
	}
	found := map[string]bool{}
	for _, form := range YearForms {
		found[form] = false
	}
	for form, ns := range numbers {
		found[form] = true
		sort.Ints(ns)
		for i, n := range ns {
			if n != i+1 {
				return nil, fmt.Errorf("%s, %s: the rows are not numbered 1 to %d: row %d where %d was expected", o.Act, form, len(ns), n, i+1)
			}
		}
	}
	for form, ok := range found {
		if !ok {
			return nil, fmt.Errorf("%s: the order has no table of %s", o.Act, form)
		}
	}
	return out, nil
}

// readFormTable reads the rows of one table of a form. A row with "Nr. rd." opens a section of form 30; a
// table of form 40 is one section.
func readFormTable(table string, section *string, fixed bool) ([]FormLine, error) {
	var out []FormLine
	pending := false
	for _, r := range rowPattern.FindAllStringSubmatch(table, -1) {
		var cells []string
		for _, c := range cellPattern.FindAllStringSubmatch(r[1], -1) {
			cells = append(cells, text(c[1]))
		}
		var filled []string
		for _, c := range cells {
			if c != "" {
				filled = append(filled, c)
			}
		}
		// The row of column letters and numbers, as A B 1 2.
		if len(filled) == 0 || filled[0] == "A" {
			continue
		}
		var name []string
		var row, side *string
		header := false
		for _, c := range filled {
			switch {
			case c == "Nr. rd.":
				header = true
			case header:
			case row == nil && numberPattern.MatchString(c):
				n := c
				row = &n
			case row == nil && sidePattern.MatchString(c):
				s := c
				side = &s
			case row == nil:
				name = append(name, c)
			}
		}
		full := strings.Join(name, " ")
		if header {
			if fixed {
				// The header of a part of form 40: the part is named by its title.
				continue
			}
			if full == "" {
				// The section takes its name from its first row.
				pending = true
				continue
			}
			s := full
			section, pending = &s, false
			continue
		}
		if full == "" && side != nil && row != nil && len(out) > 0 && out[len(out)-1].Side != nil {
			prev := out[len(out)-1]
			out = append(out, FormLine{Section: section, Row: row, Side: side, Name: prev.Name, Accounts: prev.Accounts, Terms: prev.Terms, Rows: prev.Rows})
			continue
		}
		if row == nil && !strings.Contains(full, "(rd.") && !strings.Contains(full, "ct.") && !headingPattern.MatchString(full) && !strings.HasSuffix(full, ":") {
			// A row of column names.
			continue
		}
		p, err := split(full)
		if err != nil {
			return nil, fmt.Errorf("row %v: %w", row, err)
		}
		if pending {
			s := p.Name
			section, pending = &s, false
		}
		// A name keeps the mark of its note where the formula was taken out: no space before its comma.
		clean := strings.ReplaceAll(p.Name, " ,", ",")
		out = append(out, FormLine{Section: section, Row: row, Side: side, Name: clean, Accounts: p.Accounts, Terms: p.Terms, Rows: p.Rows})
	}
	return out, nil
}

// PublishedFormLines reads lines.jsonl of the published dataset.
func PublishedFormLines(r io.Reader) ([]FormLine, error) {
	var out []FormLine
	d := json.NewDecoder(r)
	for d.More() {
		var l FormLine
		if err := d.Decode(&l); err != nil {
			return nil, fmt.Errorf("published lines: %w", err)
		}
		out = append(out, l)
	}
	return out, nil
}

// readOrder reads the text of an order in force: its newest consolidated form, or the order itself while it
// has none.
func readOrder(get Get, o YearOrder) (string, error) {
	page, err := portal.Read(get, portal.Address+o.Page, maxPage)
	if err != nil {
		return "", err
	}
	if !strings.Contains(page, "Consolidarea din") {
		return page, nil
	}
	forms, err := portal.Forms(page)
	if err != nil {
		return "", err
	}
	return portal.Read(get, portal.Address+forms[len(forms)-1].ID, maxPage)
}

// YearDataset reads the order of each year and builds the dataset, keeping the published rows.
func YearDataset(get Get, published []FormLine) (dataset.Dataset, error) {
	var now []FormLine
	for _, o := range Orders {
		page, err := readOrder(get, o)
		if err != nil {
			return dataset.Dataset{}, err
		}
		lines, err := ReadYearForms(page, o)
		if err != nil {
			return dataset.Dataset{}, err
		}
		now = append(now, lines...)
	}
	position := map[string]int{}
	code := map[string]string{}
	for _, ls := range [][]FormLine{published, now} {
		for _, l := range ls {
			position[l.id()] = l.Order
			code[l.Form] = l.Code
		}
	}
	lines, err := history.Merge(now, published, FormLine.id, FormLine.content,
		func(l *FormLine) (**string, **string, **string) { return &l.ValidFrom, &l.ValidTo, &l.ChangeAct }, YearChanges,
		func(a, b string) bool {
			ya, fa := yearForm(a)
			yb, fb := yearForm(b)
			if ya != yb {
				return ya < yb
			}
			if fa != fb {
				// Form 30 comes before form 40.
				if code[fa] != code[fb] {
					return code[fa] < code[fb]
				}
				return fa < fb
			}
			return position[a] < position[b]
		})
	if err != nil {
		return dataset.Dataset{}, err
	}
	var records []any
	for _, l := range lines {
		records = append(records, l)
	}
	var acts []string
	for _, o := range Orders {
		acts = append(acts, fmt.Sprintf("%d: %s (%s%s)", o.Year, o.Act, portal.Address, o.Page))
	}
	return dataset.Dataset{
		Name:  "statement-forms",
		Title: "Forms 30 and 40 of the annual financial statements, by year",
		Description: "The forms that go with the annual financial statements and the annual accounting reports at 31 December, as the order of the " +
			"Ministry of Finance for each financial year sets them: \"Date informative\" (form 30) for the entities with a full balance sheet " +
			"(informative-data), form 30 for the small entities, the micro-entities and the entities with annual accounting reports " +
			"(informative-data-small), and \"Situația activelor imobilizate\" (form 40) for all of them (fixed-assets), in three sections: gross values, " +
			"depreciation and impairment adjustments. Each row has its number, which the ANAF forms use, its section, its name, the accounts and rows " +
			"it is computed from as the form writes them, and the accounts split into terms. The forms of entities that apply IFRS (annex 3 of the " +
			"order) are not here. A year is added when its order is published; an order amended later gives the day the change applies from and the act.",
		Source: dataset.Source{
			Name:    "Legislative Portal, the yearly order on the annual financial statements: " + strings.Join(acts, "; "),
			URL:     portal.Address + Orders[len(Orders)-1].Page,
			Licence: "Legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Monthly,
		Update:    "When the order of a new year is added or an order is amended",
		Files: []dataset.File{{
			Name: "lines.jsonl",
			Fields: []dataset.Field{
				{Name: "year", Description: "The financial year the form is for"},
				{Name: "set_by", Description: "The order that sets the form, as OMF 2036/2025"},
				{Name: "form", Description: "informative-data, informative-data-small or fixed-assets"},
				{Name: "code", Description: "The code of the form at ANAF: 30 or 40"},
				{Name: "entities", Description: "The entities the order says the form is for, as it writes them"},
				{Name: "order", Description: "The place of the line in its form, from 1"},
				{Name: "section", Description: "The part of the form the line is in, as the form writes it, or null before the first"},
				{Name: "row", Description: "The number of the row, as 01, or null for a heading. With year, form and valid_from it is unique in the file"},
				{Name: "name", Description: "The name as the form writes it, without its formulas"},
				{Name: "side", Description: "Sold C or Sold D for a row of a pair, or null"},
				{Name: "accounts", Description: "The formula of accounts as the form writes it, or null"},
				{Name: "account_terms", Description: "The accounts of the formula, each with sign (+, - or +/-), account, part (true for \"din ct.\") and mark, or null"},
				{Name: "rows", Description: "The formula of rows as the form writes it, or null"},
				{Name: "valid_from", Description: "The day the line applies from, YYYY-MM-DD, or null for a line as the order first set it"},
				{Name: "valid_to", Description: "The last day the line applies, or null while it does"},
				{Name: "act", Description: "The act that changed the line, or null for a line as the order first set it"},
			},
			Records: records,
		}},
	}, nil
}

func yearForm(id string) (string, string) {
	parts := strings.SplitN(id, "|", 3)
	return parts[0], parts[1]
}
