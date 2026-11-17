// Package singleentry builds the categories of income and expense of independent activities kept in
// single entry, from article 68 of the Fiscal Code, read from its consolidated forms on the Legislative
// Portal.
package singleentry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
	"github.com/pidginhost/romanian-datasets/internal/portal"
)

// Code is the page of the Fiscal Code (Law 227/2015), which lists its consolidated forms.
const Code = portal.Address + "171282"

// Horizon is the first day the dataset covers.
var Horizon = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// maxPage is several times the size of a consolidated form of the code.
const maxPage = 64 << 20

// Get gives the body of one public page.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the portal over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "text/html") }
}

// Kinds name the kind of the categories of each paragraph of article 68. A paragraph not here and not in
// skipped stops the build.
var Kinds = map[string]string{
	"2":   "income",
	"3":   "not-income",
	"4":   "deductible",
	"4^1": "deductible",
	"4^2": "deductible",
	"5":   "limited",
	"7":   "non-deductible",
}

// skipped are the paragraphs of article 68 that set rules, not categories.
var skipped = map[string]bool{"1": true, "6": true, "7^1": true, "8": true, "9": true}

// Category is one letter of a paragraph of article 68, or a paragraph without letters.
type Category struct {
	Code      string   `json:"code"`
	Paragraph string   `json:"paragraph"`
	Letter    *string  `json:"letter"`
	Kind      string   `json:"kind"`
	Text      string   `json:"text"`
	Notes     []string `json:"notes"`
	ValidFrom *string  `json:"valid_from"`
	ValidTo   *string  `json:"valid_to"`
	Act       *string  `json:"act"`
}

// Key identifies a category in its file.
func (c Category) Key() string {
	if c.ValidFrom == nil {
		return c.Code + "|"
	}
	return c.Code + "|" + *c.ValidFrom
}

// unit is a category as one form gives it, with the last change the portal notes on it.
type unit struct {
	Category
	changed string
	act     string
	removed bool
}

var (
	articlePattern   = regexp.MustCompile(`^Articolul \d+(\^\d+)?$`)
	paragraphPattern = regexp.MustCompile(`^\((\d+(?:\^\d+)?)\)$`)
	letterPattern    = regexp.MustCompile(`^([a-z](?:\^\d+)?)\)$`)
	changePattern    = regexp.MustCompile(`^\(la (\d{2}-\d{2}-\d{4}),?\s*(.*)$`)
	removedPattern   = regexp.MustCompile(`^Abrogat[ăe]?\.?$`)
	spacePattern     = regexp.MustCompile(`\s+([,;.:)])`)
	openPattern      = regexp.MustCompile(`([(])\s+`)
)

func clean(parts []string) string {
	s := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	return openPattern.ReplaceAllString(spacePattern.ReplaceAllString(s, "$1"), "$1")
}

// ReadArticle reads article 68 out of the text of a consolidated form of the code.
func ReadArticle(page string) ([]unit, error) {
	all := portal.Lines(page)
	start := -1
	for i := 0; i+1 < len(all); i++ {
		if all[i] == "Articolul 68" && strings.HasPrefix(all[i+1], "Reguli generale de stabilire a venitului net anual din activități independente") {
			start = i + 2
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("the form has no article 68")
	}

	type piece struct {
		paragraph, letter string
		text, notes       []string
		changed, act      string
	}
	var pieces []*piece
	var current *piece
	paragraph := ""
	mode := "" // change, note
	var change []string
	closeChange := func() error {
		m := changePattern.FindStringSubmatch(strings.Join(change, " "))
		if m == nil || current == nil {
			return fmt.Errorf("a change note the build does not read: %q", strings.Join(change, " "))
		}
		d, err := time.Parse("02-01-2006", m[1])
		if err != nil {
			return err
		}
		current.changed = d.Format(time.DateOnly)
		current.act = clean([]string{strings.TrimSuffix(strings.TrimSpace(m[2]), ")")})
		return nil
	}
	for _, l := range all[start:] {
		if articlePattern.MatchString(l) && mode != "change" {
			break
		}
		if mode == "change" {
			change = append(change, l)
			// A note of several lines ends with a line that is only the closing parenthesis.
			if l == ")" {
				if err := closeChange(); err != nil {
					return nil, err
				}
				mode = ""
			}
			continue
		}
		switch {
		case l == "..." || l == "+":
			continue
		case strings.HasPrefix(l, "(la "):
			change = []string{l}
			if strings.HasSuffix(l, ")") {
				if err := closeChange(); err != nil {
					return nil, err
				}
				continue
			}
			mode = "change"
			continue
		case l == "Notă":
			mode = "note"
			if current != nil {
				current.notes = append(current.notes, "")
			}
			continue
		}
		if m := paragraphPattern.FindStringSubmatch(l); m != nil {
			paragraph, mode = m[1], ""
			current = &piece{paragraph: paragraph}
			pieces = append(pieces, current)
			continue
		}
		if m := letterPattern.FindStringSubmatch(l); m != nil && paragraph != "" {
			mode = ""
			current = &piece{paragraph: paragraph, letter: m[1]}
			pieces = append(pieces, current)
			continue
		}
		if current == nil {
			return nil, fmt.Errorf("article 68 starts with a line the build does not know: %q", l)
		}
		if mode == "note" {
			current.notes[len(current.notes)-1] += " " + l
			continue
		}
		current.text = append(current.text, l)
	}

	// A paragraph with letters is the lead of its letters, not a category of its own.
	hasLetters := map[string]bool{}
	for _, p := range pieces {
		if p.letter != "" {
			hasLetters[p.paragraph] = true
		}
	}
	var out []unit
	seen := map[string]bool{}
	for _, p := range pieces {
		if skipped[p.paragraph] {
			continue
		}
		kind, ok := Kinds[p.paragraph]
		if !ok {
			return nil, fmt.Errorf("paragraph (%s) of article 68 is not known to the build: a person decides", p.paragraph)
		}
		if p.letter == "" && hasLetters[p.paragraph] {
			continue
		}
		code := "68(" + p.paragraph + ")"
		u := unit{Category: Category{Paragraph: p.paragraph, Kind: kind}, changed: p.changed, act: p.act}
		if p.letter != "" {
			letter := p.letter
			code += letter
			u.Letter = &letter
		}
		u.Code = code
		u.Text = clean(p.text)
		if removedPattern.MatchString(u.Text) {
			u.removed = true
		}
		for _, n := range p.notes {
			if n = clean([]string{n}); n != "" {
				u.Notes = append(u.Notes, n)
			}
		}
		if u.Text == "" {
			return nil, fmt.Errorf("%s has no text", code)
		}
		if seen[code] {
			return nil, fmt.Errorf("%s is written twice", code)
		}
		seen[code] = true
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("article 68 has no categories")
	}
	return out, nil
}

// same says whether two forms give a category the same text. The portal's notes are not the text: a note
// can announce a change before it takes effect.
func same(a, b unit) bool {
	return a.removed == b.removed && a.Text == b.Text && a.Kind == b.Kind
}

// History joins the forms from the horizon. A category that changes, comes or goes after the horizon takes
// the day and the act from the portal's note on it in the form that has the change; without that note the
// build stops.
func History(forms []portal.Form, articles [][]unit) ([]Category, error) {
	var out []Category
	open := map[string]int{}
	last := map[string]unit{}
	for _, u := range articles[0] {
		last[u.Code] = u
		if u.removed {
			continue
		}
		open[u.Code] = len(out)
		out = append(out, u.Category)
	}
	for i := 1; i < len(articles); i++ {
		date := forms[i].Date.Format(time.DateOnly)
		now := map[string]unit{}
		for _, u := range articles[i] {
			now[u.Code] = u
		}
		for code := range last {
			if _, ok := now[code]; !ok {
				return nil, fmt.Errorf("%s is no longer in the form of %s: a person decides", code, date)
			}
		}
		for _, u := range articles[i] {
			before, had := last[u.Code]
			if had && same(before, u) {
				// The notes of the newest form go with the text in force.
				if at, ok := open[u.Code]; ok {
					out[at].Notes = u.Notes
				}
				last[u.Code] = u
				continue
			}
			if u.changed == "" || (had && u.changed == before.changed) {
				return nil, fmt.Errorf("%s changes in the form of %s without a dated note of the portal: a person decides", u.Code, date)
			}
			end, err := time.Parse(time.DateOnly, u.changed)
			if err != nil {
				return nil, err
			}
			to := end.AddDate(0, 0, -1).Format(time.DateOnly)
			if at, ok := open[u.Code]; ok {
				out[at].ValidTo = &to
				delete(open, u.Code)
			}
			last[u.Code] = u
			if u.removed {
				continue
			}
			c := u.Category
			from, act := u.changed, u.act
			c.ValidFrom, c.Act = &from, &act
			open[u.Code] = len(out)
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return codeLess(out[i], out[j])
		}
		return out[i].ValidFrom == nil || (out[j].ValidFrom != nil && *out[i].ValidFrom < *out[j].ValidFrom)
	})
	return out, nil
}

// codeLess orders categories as the article does: by paragraph, then by letter.
func codeLess(a, b Category) bool {
	if a.Paragraph != b.Paragraph {
		return part(a.Paragraph) < part(b.Paragraph)
	}
	la, lb := "", ""
	if a.Letter != nil {
		la = *a.Letter
	}
	if b.Letter != nil {
		lb = *b.Letter
	}
	return part(la) < part(lb)
}

// part writes a paragraph or letter so that 4 < 4^1 < 5 and g < g^1 < g^2 < h sort as text.
func part(s string) string {
	base, sup, _ := strings.Cut(s, "^")
	return fmt.Sprintf("%8s^%4s", base, sup)
}

// PublishedKeys reads categories.jsonl of the published dataset.
func PublishedKeys(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for decoder.More() {
		var c Category
		if err := decoder.Decode(&c); err != nil {
			return nil, fmt.Errorf("published categories: %w", err)
		}
		out = append(out, c.Key())
	}
	return out, nil
}

// Dataset reads the forms of the code from the horizon and builds the dataset. A category published before
// and missing now stops the build.
func Dataset(get Get, published []string) (dataset.Dataset, error) {
	page, err := portal.Read(get, Code, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	all, err := portal.Forms(page)
	if err != nil {
		return dataset.Dataset{}, err
	}
	forms, err := portal.FromHorizon(all, Horizon)
	if err != nil {
		return dataset.Dataset{}, err
	}
	var articles [][]unit
	for _, f := range forms {
		text, err := portal.Read(get, portal.Address+f.ID, maxPage)
		if err != nil {
			return dataset.Dataset{}, err
		}
		a, err := ReadArticle(text)
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("form of %s: %w", f.Date.Format(time.DateOnly), err)
		}
		articles = append(articles, a)
	}
	categories, err := History(forms, articles)
	if err != nil {
		return dataset.Dataset{}, err
	}

	have := map[string]bool{}
	for _, c := range categories {
		have[c.Key()] = true
	}
	var missing []string
	for _, k := range published {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("published categories are gone: %s: a person decides", strings.Join(missing, ", "))
	}

	var records []any
	for _, c := range categories {
		records = append(records, c)
	}
	first := forms[0].Date.Format(time.DateOnly)
	lastForm := forms[len(forms)-1].Date.Format(time.DateOnly)
	return dataset.Dataset{
		Name:  "single-entry-categories",
		Title: "Income and expense categories of single-entry bookkeeping",
		Description: "The categories of income and expense of independent activities whose net income is set in the real system, on the basis of " +
			"single-entry bookkeeping: the letters of article 68 of the Fiscal Code (Law 227/2015) on gross income (income), what is not gross income " +
			"(not-income), the conditions and kinds of deductible expenses (deductible), the expenses deductible within limits (limited) and the " +
			"non-deductible expenses (non-deductible). OMFP 170/2015 sets the registers of single entry, not a chart: these categories are what the " +
			"law gives to sort its entries. text is the letter as the code writes it; notes are the notes of the portal on how a change applies, " +
			"for instance to the income of a whole year. The categories are those in force on " + Horizon.Format(time.DateOnly) +
			", the first day the dataset covers; a later change gives the day it took effect and the act, as the portal notes them.",
		Source: dataset.Source{
			Name:    "Legislative Portal, consolidated forms of the Fiscal Code (Law 227/2015) from " + first + " to " + lastForm + ", article 68 (" + Code + ")",
			URL:     Code,
			Licence: "Legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Monthly,
		Update:    "When a law changes article 68 of the Fiscal Code, which happens several times a year",
		Files: []dataset.File{{
			Name: "categories.jsonl",
			Fields: []dataset.Field{
				{Name: "code", Description: "Article, paragraph and letter: 68(5)g^1 is letter g^1 of paragraph (5). With valid_from it is unique in the file"},
				{Name: "paragraph", Description: "The paragraph of article 68"},
				{Name: "letter", Description: "The letter, or null for a paragraph without letters"},
				{Name: "kind", Description: "income, not-income, deductible, limited or non-deductible"},
				{Name: "text", Description: "The text of the letter as the code writes it"},
				{Name: "notes", Description: "The notes of the portal on how the text applies, or null"},
				{Name: "valid_from", Description: "The day the text took effect, YYYY-MM-DD, or null for a text in force on " + Horizon.Format(time.DateOnly)},
				{Name: "valid_to", Description: "The last day the text applied, or null while it does"},
				{Name: "act", Description: "The act that made the change, as the portal writes it, or null for a text in force on " + Horizon.Format(time.DateOnly)},
			},
			Records: records,
		}},
	}, nil
}
