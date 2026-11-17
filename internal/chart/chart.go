// Package chart builds the general chart of accounts of OMFP 1802/2014, chapter 14 of the accounting
// regulations for companies, from the consolidated forms of the order on the Legislative Portal.
package chart

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Portal is the Legislative Portal of the Ministry of Justice.
const Portal = "https://legislatie.just.ro/Public/DetaliiDocument/"

// Order is the page of OMFP 1802/2014, which lists its consolidated forms.
const Order = Portal + "250771"

// Horizon is the first day the dataset covers: the chart in force on that day and every change after it.
var Horizon = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// maxPage is many times the size of the consolidated annex, the largest page read.
const maxPage = 32 << 20

// Get gives the body of one public page.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the portal over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "text/html") }
}

// Change is a change of chapter 14 between two consolidated forms: the day it applies from and the act
// that made it, which a person reads in the order and writes here.
type Change struct {
	Applies string
	Act     string
}

// Changes gives, by the date of a consolidated form after the horizon, the change of the chart it carries.
// A consolidated form whose chart differs from the one before and that is not here stops the build.
var Changes = map[string]Change{}

// Account is one line of the chart.
type Account struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Level is class, group, account (three digits) or subaccount (four digits).
	Level  string  `json:"level"`
	Parent *string `json:"parent"`
	// Function is A, P or A/P as the order states it, or null where it states none.
	Function *string  `json:"function"`
	Role     *string  `json:"role"`
	Notes    []string `json:"notes"`
	// ValidFrom is the day the account applies from, or null for an account in force on the horizon.
	ValidFrom *string `json:"valid_from"`
	ValidTo   *string `json:"valid_to"`
	Act       *string `json:"act"`
}

var (
	consolidationPattern = regexp.MustCompile(`title='Consolidarea din (\d{2}\.\d{2}\.\d{4})'[^>]*href='[^']*DetaliiDocument/(\d+)'`)
	annexPattern         = regexp.MustCompile(`DetaliiDocumentAfis/(\d+)">REGLEMENT`)
	tagPattern           = regexp.MustCompile(`<[^>]*>`)
	scriptPattern        = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>`)
	classPattern         = regexp.MustCompile(`^Clasa (\d) - (.+)$`)
	linePattern          = regexp.MustCompile(`^(\d{2,4})\.?\s+(.+)$`)
	notePattern          = regexp.MustCompile(`^\*(\d+)\)\s*(.*)$`)
	markPattern          = regexp.MustCompile(`\*(\d+)\)`)
	localPattern         = regexp.MustCompile(`^\^(\d+)\s*(.*)$`)
	localMarkPattern     = regexp.MustCompile(`\^(\d+)`)
	removedPattern       = regexp.MustCompile(`^(Eliminat|Abrogat)\.?$`)
	functionPattern      = regexp.MustCompile(`\s*\((A|P|A/P)\)`)
)

// lines gives the text of an HTML page, one line for each piece of text between tags.
func lines(page string) []string {
	page = scriptPattern.ReplaceAllString(page, "")
	var out []string
	for _, l := range strings.Split(tagPattern.ReplaceAllString(page, "\n"), "\n") {
		if l = strings.TrimSpace(html.UnescapeString(l)); l != "" {
			out = append(out, l)
		}
	}
	return out
}

type entry struct {
	code, text string
	// local are the ^N notes of this account.
	local map[string]string
}

// noteRef is the note a line of text goes on: a *N) note of the chapter, or a ^N note of one account.
type noteRef struct {
	key string
	of  *entry
}

// ReadChapter reads chapter 14 out of the text of the consolidated annex and gives its accounts.
func ReadChapter(page string) ([]Account, error) {
	all := lines(page)
	start := -1
	for i, l := range all {
		if strings.Contains(l, "Planul de conturi general este următorul") {
			start = i
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("the annex has no chart of accounts")
	}

	// Notes come two ways: *N) notes, mostly listed at the end of the chapter, and ^N notes of one account,
	// in a note block (headed "Nota" with a breve) after it. Text in such a block that is no note is the
	// portal's own and is left out.
	var entries []*entry
	notes := map[string]string{}
	var note *noteRef
	inChange, inBlock, inOld := false, false, false
	for _, l := range all[start+1:] {
		if strings.HasPrefix(l, "Capitolul 15") {
			break
		}
		// A note of the portal on a change: one line, or several up to a closing parenthesis.
		if inChange {
			if strings.HasSuffix(l, ")") && !linePattern.MatchString(l) {
				inChange = false
			}
			continue
		}
		// The older notes of the portal start with a row of dashes and run to the next account.
		if strings.HasPrefix(l, "-----") {
			inOld, note = true, nil
			continue
		}
		if inOld && !linePattern.MatchString(l) && !classPattern.MatchString(l) && !strings.HasPrefix(l, "(la ") && l != "Notă" && !notePattern.MatchString(l) {
			continue
		}
		inOld = false
		switch {
		case strings.HasPrefix(l, "(la "):
			inChange = !strings.HasSuffix(l, ")")
			note = nil
			continue
		case l == "...":
			continue
		case l == "Notă":
			inBlock, note = true, nil
			continue
		}
		if m := notePattern.FindStringSubmatch(l); m != nil {
			if known, ok := notes[m[1]]; ok && known != m[2] {
				return nil, fmt.Errorf("note %s is written twice", m[1])
			}
			notes[m[1]] = m[2]
			note = &noteRef{key: m[1]}
			continue
		}
		if m := localPattern.FindStringSubmatch(l); m != nil && inBlock {
			if len(entries) == 0 {
				return nil, fmt.Errorf("a note before any account")
			}
			e := entries[len(entries)-1]
			if e.local == nil {
				e.local = map[string]string{}
			}
			e.local[m[1]] = m[2]
			note = &noteRef{key: m[1], of: e}
			continue
		}
		if m := classPattern.FindStringSubmatch(l); m != nil {
			entries = append(entries, &entry{code: m[1], text: m[2]})
			inBlock, note = false, nil
			continue
		}
		if m := linePattern.FindStringSubmatch(l); m != nil {
			entries = append(entries, &entry{code: m[1], text: m[2]})
			inBlock, note = false, nil
			continue
		}
		switch {
		case note != nil && note.of == nil:
			notes[note.key] += " " + l
		case note != nil:
			note.of.local[note.key] += " " + l
		case inBlock:
			// The portal's own text in a note block.
		case len(entries) > 0:
			entries[len(entries)-1].text += " " + l
		default:
			return nil, fmt.Errorf("the chart starts with a line the build does not know: %q", l)
		}
	}

	var out []Account
	seen := map[string]bool{}
	for _, e := range entries {
		text := e.text
		// An older note of the portal can follow the name on its line, after a row of dashes.
		if at := strings.Index(text, "-----"); at >= 0 {
			text = text[:at]
		}
		text = strings.Join(strings.Fields(text), " ")
		// An account the order took out is left in the text as "Eliminat"; it may come back under another name.
		if removedPattern.MatchString(text) {
			continue
		}
		var function *string
		if fs := functionPattern.FindAllStringSubmatch(text, -1); len(fs) > 1 {
			return nil, fmt.Errorf("account %s has two functions", e.code)
		} else if len(fs) == 1 {
			f := fs[0][1]
			function = &f
		}
		var accountNotes []string
		for _, m := range markPattern.FindAllStringSubmatch(text, -1) {
			n, ok := notes[m[1]]
			if !ok {
				return nil, fmt.Errorf("account %s refers to note %s, which the chart does not have", e.code, m[1])
			}
			accountNotes = append(accountNotes, strings.Join(strings.Fields(n), " "))
		}
		for _, m := range localMarkPattern.FindAllStringSubmatch(text, -1) {
			n, ok := e.local[m[1]]
			if !ok {
				return nil, fmt.Errorf("account %s refers to its note %s, which the chart does not have", e.code, m[1])
			}
			accountNotes = append(accountNotes, strings.Join(strings.Fields(n), " "))
		}
		if len(e.local) > len(localMarkPattern.FindAllString(text, -1)) {
			return nil, fmt.Errorf("account %s has a note it does not refer to", e.code)
		}
		name := strings.TrimSpace(localMarkPattern.ReplaceAllString(markPattern.ReplaceAllString(functionPattern.ReplaceAllString(text, ""), ""), ""))
		if name == "" {
			return nil, fmt.Errorf("account %s has no name", e.code)
		}
		if seen[e.code] {
			return nil, fmt.Errorf("account %s is listed twice", e.code)
		}
		seen[e.code] = true

		a := Account{Code: e.code, Name: name, Function: function, Notes: accountNotes}
		switch len(e.code) {
		case 1:
			a.Level = "class"
		case 2:
			a.Level = "group"
		case 3:
			a.Level = "account"
		case 4:
			a.Level = "subaccount"
		}
		if len(e.code) > 1 {
			parent := e.code[:len(e.code)-1]
			if !seen[parent] {
				return nil, fmt.Errorf("account %s comes before its parent %s", e.code, parent)
			}
			a.Parent = &parent
		}
		a.Role = role(e.code)
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the chart has no accounts")
	}
	return out, nil
}

// role gives the part an account plays when the result is closed: the accounts of class 6 are expenses,
// those of class 7 income, and 121 is the result. Classes and groups have none.
func role(code string) *string {
	r := ""
	switch {
	case len(code) < 3:
		return nil
	case code == "121":
		r = "result"
	case code[0] == '6':
		r = "expense"
	case code[0] == '7':
		r = "income"
	default:
		return nil
	}
	return &r
}

// Form is a consolidated form of the order.
type Form struct {
	Date time.Time
	ID   string
}

// Forms reads the page of the order and gives its consolidated forms, the oldest first.
func Forms(page string) ([]Form, error) {
	var out []Form
	for _, m := range consolidationPattern.FindAllStringSubmatch(page, -1) {
		d, err := time.Parse("02.01.2006", m[1])
		if err != nil {
			return nil, err
		}
		out = append(out, Form{d, m[2]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the order has no consolidated forms")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

// FromHorizon gives the form in force on the horizon and those after it.
func FromHorizon(forms []Form) ([]Form, error) {
	first := -1
	for i, f := range forms {
		if !f.Date.After(Horizon) {
			first = i
		}
	}
	if first < 0 {
		return nil, fmt.Errorf("no consolidated form is in force on %s", Horizon.Format(time.DateOnly))
	}
	return forms[first:], nil
}

func read(get Get, address string) (string, error) {
	body, err := get(address)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxPage+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxPage {
		return "", fmt.Errorf("%s is larger than expected", address)
	}
	return string(data), nil
}

// chapter reads the chart of one consolidated form: the form of the order links its annex.
func chapter(get Get, form Form) ([]Account, error) {
	page, err := read(get, Portal+form.ID)
	if err != nil {
		return nil, err
	}
	m := annexPattern.FindStringSubmatch(page)
	if m == nil {
		return nil, fmt.Errorf("the consolidated form %s links no annex", form.ID)
	}
	annex, err := read(get, Portal+m[1])
	if err != nil {
		return nil, err
	}
	accounts, err := ReadChapter(annex)
	if err != nil {
		return nil, fmt.Errorf("annex %s: %w", m[1], err)
	}
	return accounts, nil
}

func same(a, b Account) bool {
	x, _ := json.Marshal(Account{Code: a.Code, Name: a.Name, Function: a.Function, Notes: a.Notes})
	y, _ := json.Marshal(Account{Code: b.Code, Name: b.Name, Function: b.Function, Notes: b.Notes})
	return string(x) == string(y)
}

// History joins the charts of the forms from the horizon: an account that changes or leaves the chart ends
// the day before its change applies, and a new or changed one starts that day.
func History(forms []Form, charts [][]Account) ([]Account, error) {
	var out []Account
	open := map[string]int{}
	for _, a := range charts[0] {
		open[a.Code] = len(out)
		out = append(out, a)
	}
	for i := 1; i < len(charts); i++ {
		now := map[string]Account{}
		for _, a := range charts[i] {
			now[a.Code] = a
		}
		differs := len(now) != len(open)
		for code, at := range open {
			if a, ok := now[code]; !ok || !same(a, out[at]) {
				differs = true
			}
		}
		if !differs {
			continue
		}
		date := forms[i].Date.Format(time.DateOnly)
		change, ok := Changes[date]
		if !ok {
			return nil, fmt.Errorf("the chart changes in the consolidated form of %s: a person reads the change and adds it to Changes", date)
		}
		applies, err := time.Parse(time.DateOnly, change.Applies)
		if err != nil {
			return nil, fmt.Errorf("Changes %s: %w", date, err)
		}
		end := applies.AddDate(0, 0, -1).Format(time.DateOnly)
		for code, at := range open {
			if a, ok := now[code]; !ok || !same(a, out[at]) {
				out[at].ValidTo = &end
				delete(open, code)
			}
		}
		for _, a := range charts[i] {
			if _, ok := open[a.Code]; ok {
				continue
			}
			from, act := change.Applies, change.Act
			a.ValidFrom, a.Act = &from, &act
			open[a.Code] = len(out)
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].ValidFrom == nil || (out[j].ValidFrom != nil && *out[i].ValidFrom < *out[j].ValidFrom)
	})
	return out, nil
}

// Key identifies an account in its file: its code and the day it applies from.
func (a Account) Key() string {
	if a.ValidFrom == nil {
		return a.Code + "|"
	}
	return a.Code + "|" + *a.ValidFrom
}

// PublishedKeys reads accounts.jsonl of the published dataset.
func PublishedKeys(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for decoder.More() {
		var a Account
		if err := decoder.Decode(&a); err != nil {
			return nil, fmt.Errorf("published accounts: %w", err)
		}
		out = append(out, a.Key())
	}
	return out, nil
}

// Dataset reads the forms of the order from the horizon and builds the dataset. An account published
// before and missing now stops the build.
func Dataset(get Get, published []string) (dataset.Dataset, error) {
	page, err := read(get, Order)
	if err != nil {
		return dataset.Dataset{}, err
	}
	all, err := Forms(page)
	if err != nil {
		return dataset.Dataset{}, err
	}
	forms, err := FromHorizon(all)
	if err != nil {
		return dataset.Dataset{}, err
	}
	var charts [][]Account
	for _, f := range forms {
		c, err := chapter(get, f)
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("consolidated form of %s: %w", f.Date.Format(time.DateOnly), err)
		}
		charts = append(charts, c)
	}
	accounts, err := History(forms, charts)
	if err != nil {
		return dataset.Dataset{}, err
	}

	have := map[string]bool{}
	for _, a := range accounts {
		have[a.Key()] = true
	}
	var missing []string
	for _, k := range published {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("published accounts are gone: %s: a person decides", strings.Join(missing, ", "))
	}

	var records []any
	for _, a := range accounts {
		records = append(records, a)
	}
	first := forms[0].Date.Format(time.DateOnly)
	last := forms[len(forms)-1].Date.Format(time.DateOnly)
	return dataset.Dataset{
		Name:  "chart-of-accounts",
		Title: "General chart of accounts (OMFP 1802/2014)",
		Description: "The general chart of accounts for double-entry bookkeeping of companies, chapter 14 of the accounting regulations approved by " +
			"OMFP 1802/2014: classes, groups, accounts and subaccounts, with the function the order gives (A for asset, P for liability, A/P for both) " +
			"where it gives one; a subaccount without one takes the function of its account. " +
			"role marks what the closing of the result needs: the accounts of class 6 are expenses, those of class 7 income, and 121 is the result. " +
			"notes are the notes of the order on the account. The chart is the one in force on " + Horizon.Format(time.DateOnly) +
			", the first day the dataset covers; an account with valid_from null was in force that day. A later change of the chart gives the day it applies " +
			"from and the act that made it. Names are as the order writes them.",
		Source: dataset.Source{
			Name:    "Legislative Portal, consolidated forms of OMFP 1802/2014 from " + first + " to " + last + " (" + Order + ")",
			URL:     Order,
			Licence: "Legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Monthly,
		Update:    "When an order changes the chart of accounts",
		Files: []dataset.File{{
			Name: "accounts.jsonl",
			Fields: []dataset.Field{
				{Name: "code", Description: "The code of the class, group, account or subaccount. With valid_from it is unique in the file"},
				{Name: "name", Description: "The name as the order writes it"},
				{Name: "level", Description: "class, group, account (three digits) or subaccount (four digits)"},
				{Name: "parent", Description: "The code one level up, or null for a class"},
				{Name: "function", Description: "A, P or A/P as the order states it, or null where it states none"},
				{Name: "role", Description: "expense, income or result for the closing of the result, or null"},
				{Name: "notes", Description: "The notes of the order on the account, or null"},
				{Name: "valid_from", Description: "The day the account applies from, YYYY-MM-DD, or null for an account in force on " + Horizon.Format(time.DateOnly)},
				{Name: "valid_to", Description: "The last day the account applies, or null while it does"},
				{Name: "act", Description: "The act that brought the account in, or null for an account in force on " + Horizon.Format(time.DateOnly)},
			},
			Records: records,
		}},
	}, nil
}
