// Package statements builds the formats of the annual financial statements of companies set by OMFP
// 1802/2014: the balance sheet, the abridged balance sheet and the profit and loss account, each row with
// its number, the accounts it is computed from and the rows it sums.
package statements

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
	"github.com/pidginhost/romanian-datasets/internal/history"
	"github.com/pidginhost/romanian-datasets/internal/portal"
)

// Order is the page of OMFP 1802/2014, which lists its consolidated forms.
const Order = portal.Address + "250771"

// maxPage is many times the size of the consolidated annex.
const maxPage = 32 << 20

// Get gives the body of one public page.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the portal over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "text/html") }
}

// Changes gives, by form and row, a change of a format a person read in the act that made it. A row that
// changes and is not here stops the build.
var Changes = map[string]history.Change{}

// Forms names each format by the heading above its table. The longer heading comes first.
var Forms = []struct{ Heading, Form, Code string }{
	{"BILANȚUL PRESCURTAT", "abridged-balance-sheet", "10"},
	{"BILANȚ", "balance-sheet", "10"},
	{"CONTUL DE PROFIT ȘI PIERDERE", "profit-and-loss", "20"},
}

// Term is one account of the formula of a row.
type Term struct {
	// Sign is +, - or +/- where the row takes the balance of the account either way.
	Sign    string `json:"sign"`
	Account string `json:"account"`
	// Part is true for "din ct.": only the part of the account that fits the row.
	Part bool `json:"part"`
	// Mark is the stars the format puts after the account, which a note of the format explains.
	Mark *string `json:"mark"`
}

// Line is one row of a format.
type Line struct {
	Form  string `json:"form"`
	Code  string `json:"code"`
	Order int    `json:"order"`
	// Row is the number of the row, as 01, or null for a heading.
	Row  *string `json:"row"`
	Name string  `json:"name"`
	// Side is the side of the balance a row of two takes: Sold C or Sold D.
	Side      *string `json:"side"`
	Accounts  *string `json:"accounts"`
	Terms     []Term  `json:"account_terms"`
	Rows      *string `json:"rows"`
	ValidFrom *string `json:"valid_from"`
	ValidTo   *string `json:"valid_to"`
	ChangeAct *string `json:"act"`
}

func (l Line) id() string {
	if l.Row != nil {
		return l.Form + "|" + *l.Row
	}
	return fmt.Sprintf("%s|h%03d", l.Form, l.Order)
}

// Key identifies a line in its file.
func (l Line) Key() string { return history.Key(l.id(), l.ValidFrom) }

func (l Line) content() string {
	x, _ := json.Marshal(Line{Form: l.Form, Code: l.Code, Order: l.Order, Row: l.Row, Name: l.Name, Side: l.Side, Accounts: l.Accounts, Terms: l.Terms, Rows: l.Rows})
	return string(x)
}

var (
	annexPattern   = regexp.MustCompile(`DetaliiDocumentAfis/(\d+)">REGLEMENT`)
	tablePattern   = regexp.MustCompile(`(?is)<table.*?</table>`)
	rowPattern     = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellPattern    = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	tagPattern     = regexp.MustCompile(`<[^>]*>`)
	scriptPattern  = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>`)
	numberPattern  = regexp.MustCompile(`^\d{2,3}$`)
	legendPattern  = regexp.MustCompile(`^A( B)?( \d)*$`)
	sidePattern    = regexp.MustCompile(`^Sold [CD]$`)
	accountsGroup  = regexp.MustCompile(`\((?:din )?ct\.?\s[^()]*\)`)
	rowsGroup      = regexp.MustCompile(`\(rd\.[^()]*\)`)
	termPattern    = regexp.MustCompile(`([+\-±])?\s*(din\s+)?(?:ct\.?\s*)?(\d{3,4})(\*+)?`)
	leftoverDigits = regexp.MustCompile(`\d`)
)

func text(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))), " ")
}

// terms splits a formula of accounts, as "ct. 301 + 302 +/- 308 - din ct. 4428**", into its accounts.
func terms(formula string) ([]Term, error) {
	inner := strings.TrimSuffix(strings.TrimPrefix(formula, "("), ")")
	inner = strings.ReplaceAll(inner, "+/-", "±")
	var out []Term
	rest := inner
	for _, m := range termPattern.FindAllStringSubmatchIndex(inner, -1) {
		sign := "+"
		if m[2] >= 0 {
			sign = inner[m[2]:m[3]]
		}
		if sign == "±" {
			sign = "+/-"
		}
		t := Term{Sign: sign, Account: inner[m[6]:m[7]], Part: m[4] >= 0}
		if m[8] >= 0 {
			mark := inner[m[8]:m[9]]
			t.Mark = &mark
		}
		out = append(out, t)
		rest = strings.Replace(rest, inner[m[0]:m[1]], "", 1)
	}
	if len(out) == 0 || leftoverDigits.MatchString(rest) {
		return nil, fmt.Errorf("the formula %q has parts the build does not read", formula)
	}
	return out, nil
}

// parts is what the text of a row holds: its name and its formulas.
type parts struct {
	Name     string
	Accounts *string
	Terms    []Term
	Rows     *string
}

// split reads the formulas out of the text of a row; the rest is its name.
func split(full string) (parts, error) {
	var l parts
	var accounts []string
	for _, g := range accountsGroup.FindAllString(full, -1) {
		ts, err := terms(g)
		if err != nil {
			return parts{}, err
		}
		l.Terms = append(l.Terms, ts...)
		accounts = append(accounts, strings.TrimSuffix(strings.TrimPrefix(g, "("), ")"))
	}
	if len(accounts) > 0 {
		a := strings.Join(accounts, "; ")
		l.Accounts = &a
	}
	var rows []string
	for _, g := range rowsGroup.FindAllString(full, -1) {
		rows = append(rows, strings.TrimSuffix(strings.TrimPrefix(g, "("), ")"))
	}
	if len(rows) > 0 {
		r := strings.Join(rows, "; ")
		l.Rows = &r
	}
	l.Name = strings.Join(strings.Fields(rowsGroup.ReplaceAllString(accountsGroup.ReplaceAllString(full, ""), "")), " ")
	l.Name = strings.TrimSpace(strings.TrimSuffix(l.Name, ";"))
	return l, nil
}

// ReadFormats reads the tables of the formats out of the consolidated annex.
func ReadFormats(page string) ([]Line, error) {
	page = scriptPattern.ReplaceAllString(page, "")
	var out []Line
	found := map[string]bool{}
	for _, t := range tablePattern.FindAllStringIndex(page, -1) {
		before := text(page[max(0, t[0]-1500):t[0]])
		form, code := "", ""
		for _, f := range Forms {
			if at := strings.LastIndex(before, f.Heading+" la data de"); at >= 0 && at > len(before)-200 {
				form, code = f.Form, f.Code
				break
			}
		}
		if form == "" {
			continue
		}
		if found[form] {
			return nil, fmt.Errorf("two tables of the %s", form)
		}
		found[form] = true
		order := 0
		seen := map[string]bool{}
		for _, r := range rowPattern.FindAllStringSubmatch(page[t[0]:t[1]], -1) {
			var cells []string
			for _, c := range cellPattern.FindAllStringSubmatch(r[1], -1) {
				cells = append(cells, text(c[1]))
			}
			// The number of the row is the first cell that is only a number of two or three digits; the cells
			// before it name the row.
			var name []string
			var row, side *string
			for _, c := range cells {
				switch {
				case row == nil && numberPattern.MatchString(c):
					n := c
					row = &n
				case row == nil && sidePattern.MatchString(c):
					s := c
					side = &s
				case row == nil && c != "":
					name = append(name, c)
				}
			}
			full := strings.Join(name, " ")
			// The second row of a pair has only its side and number: it takes the name of the first.
			if full == "" && side != nil && row != nil && len(out) > 0 && out[len(out)-1].Form == form && out[len(out)-1].Side != nil {
				prev := out[len(out)-1]
				order++
				l := Line{Form: form, Code: code, Order: order, Row: row, Side: side, Name: prev.Name, Accounts: prev.Accounts, Terms: prev.Terms, Rows: prev.Rows}
				if seen[*row] {
					return nil, fmt.Errorf("%s: row %s is numbered twice", form, *row)
				}
				seen[*row] = true
				out = append(out, l)
				continue
			}
			if full == "" || legendPattern.MatchString(full) || strings.HasPrefix(full, "Denumirea") || strings.HasPrefix(full, "Începutul") || strings.HasPrefix(full, "Precedent") {
				continue
			}
			order++
			l := Line{Form: form, Code: code, Order: order, Row: row, Side: side}
			if row != nil {
				if seen[*row] {
					return nil, fmt.Errorf("%s: row %s is numbered twice", form, *row)
				}
				seen[*row] = true
			}
			p, err := split(full)
			if err != nil {
				return nil, fmt.Errorf("%s, row %v: %w", form, row, err)
			}
			l.Name, l.Accounts, l.Terms, l.Rows = p.Name, p.Accounts, p.Terms, p.Rows
			out = append(out, l)
		}
	}
	for _, f := range Forms {
		if !found[f.Form] {
			return nil, fmt.Errorf("the annex has no table of the %s", f.Form)
		}
	}
	return out, nil
}

// PublishedRows reads lines.jsonl of the published dataset.
func PublishedRows(r io.Reader) ([]Line, error) {
	var out []Line
	d := json.NewDecoder(r)
	for d.More() {
		var l Line
		if err := d.Decode(&l); err != nil {
			return nil, fmt.Errorf("published lines: %w", err)
		}
		out = append(out, l)
	}
	return out, nil
}

func formOrder(form string) int {
	for i, f := range Forms {
		if f.Form == form {
			return i
		}
	}
	return len(Forms)
}

// less orders the lines by form, then as the format lists them.
func less(lines map[string]int) func(a, b string) bool {
	return func(a, b string) bool {
		fa, _, _ := strings.Cut(a, "|")
		fb, _, _ := strings.Cut(b, "|")
		if fa != fb {
			return formOrder(fa) < formOrder(fb)
		}
		return lines[a] < lines[b]
	}
}

// Dataset reads the newest consolidated form of the order and builds the dataset, keeping the published rows.
func Dataset(get Get, published []Line) (dataset.Dataset, error) {
	page, err := portal.Read(get, Order, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	forms, err := portal.Forms(page)
	if err != nil {
		return dataset.Dataset{}, err
	}
	newest := forms[len(forms)-1]
	formPage, err := portal.Read(get, portal.Address+newest.ID, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	m := annexPattern.FindStringSubmatch(formPage)
	if m == nil {
		return dataset.Dataset{}, fmt.Errorf("the consolidated form %s links no annex", newest.ID)
	}
	annex, err := portal.Read(get, portal.Address+m[1], maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	now, err := ReadFormats(annex)
	if err != nil {
		return dataset.Dataset{}, fmt.Errorf("annex %s: %w", m[1], err)
	}
	position := map[string]int{}
	for _, l := range published {
		position[l.id()] = l.Order
	}
	for _, l := range now {
		position[l.id()] = l.Order
	}
	lines, err := history.Merge(now, published, Line.id, Line.content,
		func(l *Line) (**string, **string, **string) { return &l.ValidFrom, &l.ValidTo, &l.ChangeAct }, Changes, less(position))
	if err != nil {
		return dataset.Dataset{}, err
	}
	var records []any
	for _, l := range lines {
		records = append(records, l)
	}
	return dataset.Dataset{
		Name:  "financial-statements",
		Title: "Formats of the annual financial statements (OMFP 1802/2014)",
		Description: "The formats of the annual financial statements of companies in the regulations approved by OMFP 1802/2014: the balance sheet " +
			"(form 10 of the large and medium entities), the abridged balance sheet (form 10 of the small and micro entities) and the profit and loss " +
			"account (form 20). Each row has its number, which the ANAF forms use, its name, the accounts it is computed from as the format writes " +
			"them and split into terms (sign, account, part of an account for \"din ct.\", stars of a note), and the rows it sums. A heading has no number. " +
			"Side marks the row of a pair that takes the credit (Sold C) or debit (Sold D) balance. The formats are those in force on " +
			"2026-01-01, the first day the dataset covers; a later change gives the day it applies from and the act. Forms 30 and 40 are set each year " +
			"by another order and are not here.",
		Source: dataset.Source{
			Name:    "Legislative Portal, consolidated OMFP 1802/2014 (" + Order + ")",
			URL:     Order,
			Licence: "Legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Monthly,
		Update:    "When an order changes the formats",
		Files: []dataset.File{{
			Name: "lines.jsonl",
			Fields: []dataset.Field{
				{Name: "form", Description: "balance-sheet, abridged-balance-sheet or profit-and-loss"},
				{Name: "code", Description: "The code of the form at ANAF: 10 for a balance sheet, 20 for the profit and loss account"},
				{Name: "order", Description: "The place of the line in its format, from 1"},
				{Name: "row", Description: "The number of the row, as 01, or null for a heading. With form and valid_from it is unique in the file"},
				{Name: "name", Description: "The name as the format writes it, without its formulas"},
				{Name: "side", Description: "Sold C or Sold D for a row of a pair, or null"},
				{Name: "accounts", Description: "The formula of accounts as the format writes it, or null"},
				{Name: "account_terms", Description: "The accounts of the formula, each with sign (+, - or +/-), account, part (true for \"din ct.\") and mark, or null"},
				{Name: "rows", Description: "The formula of rows as the format writes it, as \"rd. 01 la 06\", or null"},
				{Name: "valid_from", Description: "The day the line applies from, YYYY-MM-DD, or null for a line in force on 2026-01-01"},
				{Name: "valid_to", Description: "The last day the line applies, or null while it does"},
				{Name: "act", Description: "The act that changed the line, or null for a line in force on 2026-01-01"},
			},
			Records: records,
		}},
	}, nil
}
