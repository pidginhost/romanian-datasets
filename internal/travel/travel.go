// Package travel builds the daily allowances and accommodation ceilings for travel abroad of annex 1 to
// HG 518/1995, by country, linked to the countries dataset.
package travel

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
	"github.com/pidginhost/romanian-datasets/internal/history"
	"github.com/pidginhost/romanian-datasets/internal/portal"
)

// Act is the page of HG 518/1995 on the Legislative Portal, which shows the text in force.
const Act = portal.Address + "7037"

// maxPage is many times the size of the page.
const maxPage = 8 << 20

// Get gives the body of one public page.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the portal over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "text/html") }
}

// Changes gives, by country as the act writes it, a change of the table a person read in the act that made
// it. A row that changes and is not here stops the build.
var Changes = map[string]history.Change{}

// Names gives the alpha-2 code of the countries the act names another way than the countries dataset; ""
// for one that no longer exists.
var Names = map[string]string{
	"Africa de Sud (Republica)":                                   "ZA",
	"Anglia (Regatul Unit al Marii Britanii și Irlandei de Nord)": "GB",
	"Antile":                       "",
	"Birmania":                     "MM",
	"Bosnia și Herzegovina":        "BA",
	"Capul Verde":                  "CV",
	"Cehia (Republica)":            "CZ",
	"Centrafricană (Republica)":    "CF",
	"Chineză (Republica Populară)": "CN",
	"Coasta de Fildeș":             "CI",
	"Coreea (RPD)":                 "KP",
	"Dominicană (Republica)":       "DO",
	"Kuwait":                       "KW",
	"Macedonia":                    "MK",
	"Myanmar":                      "MM",
	"Olanda":                       "NL",
	"Federația Rusă":               "RU",
	"Salvador":                     "SV",
	"S.U.A.":                       "US",
	"Surinam":                      "SR",
	"Vatican":                      "VA",
	"Zair":                         "CD",
}

// currencies are the currencies of the table, by how the act writes them.
var currencies = map[string]string{"USD": "USD", "euro": "EUR"}

// Country is a country of the countries dataset.
type Country struct {
	Alpha2      string  `json:"alpha2"`
	NameRO      string  `json:"name_ro"`
	ValidTo     *string `json:"valid_to"`
	InvoiceOnly bool    `json:"invoice_only"`
}

// Allowance is one row of the table.
type Allowance struct {
	Country string `json:"country"`
	// Alpha2 is the code of the country in the countries dataset, or null for one that no longer exists.
	Alpha2   *string `json:"alpha2"`
	Currency string  `json:"currency"`
	// The allowances of categories I and II, per day.
	DailyI          int     `json:"daily_i"`
	AccommodationI  int     `json:"accommodation_i"`
	DailyII         int     `json:"daily_ii"`
	AccommodationII int     `json:"accommodation_ii"`
	ValidFrom       *string `json:"valid_from"`
	ValidTo         *string `json:"valid_to"`
	ChangeAct       *string `json:"act"`
}

// Key identifies a row in its file.
func (a Allowance) Key() string { return history.Key(a.Country, a.ValidFrom) }

func (a Allowance) id() string { return a.Country }

func (a Allowance) content() string {
	x, _ := json.Marshal(Allowance{Country: a.Country, Alpha2: a.Alpha2, Currency: a.Currency, DailyI: a.DailyI, AccommodationI: a.AccommodationI, DailyII: a.DailyII, AccommodationII: a.AccommodationII})
	return string(x)
}

var (
	rowPattern  = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellPattern = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	tagPattern  = regexp.MustCompile(`<[^>]*>`)
)

func text(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))), " ")
}

// fold writes a name in capitals without Romanian letters, accents, spaces or signs, to compare names.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case 'Ă', 'Â', 'À', 'Á', 'Ã', 'Ä', 'Å':
			r = 'A'
		case 'Î', 'Ì', 'Í', 'Ï':
			r = 'I'
		case 'È', 'É', 'Ê', 'Ë':
			r = 'E'
		case 'Ò', 'Ó', 'Ô', 'Õ', 'Ö':
			r = 'O'
		case 'Ù', 'Ú', 'Û', 'Ü':
			r = 'U'
		case 'Ç':
			r = 'C'
		case 'Ñ':
			r = 'N'
		case 'Ș', 'Ş':
			r = 'S'
		case 'Ț', 'Ţ':
			r = 'T'
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ReadTable reads annex 1 out of the page of the act, and links each country to the countries dataset.
func ReadTable(page string, countries []Country) ([]Allowance, error) {
	byName := map[string][]string{}
	known := map[string]bool{}
	for _, c := range countries {
		if c.ValidTo == nil && !c.InvoiceOnly {
			byName[fold(c.NameRO)] = append(byName[fold(c.NameRO)], c.Alpha2)
			known[c.Alpha2] = true
		}
	}
	var out []Allowance
	seen := map[string]bool{}
	for _, r := range rowPattern.FindAllStringSubmatch(page, -1) {
		var cells []string
		for _, c := range cellPattern.FindAllStringSubmatch(r[1], -1) {
			cells = append(cells, text(c[1]))
		}
		if len(cells) != 7 || cells[0] == "0" {
			continue
		}
		if _, err := strconv.Atoi(cells[0]); err != nil {
			return nil, fmt.Errorf("a row of the table without its number: %q", cells)
		}
		a := Allowance{Country: cells[1]}
		currency, ok := currencies[cells[2]]
		if !ok {
			return nil, fmt.Errorf("%s: the currency %q is not known to the build", a.Country, cells[2])
		}
		a.Currency = currency
		var amounts [4]int
		for k := range amounts {
			n, err := strconv.Atoi(cells[3+k])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%s: the amount %q", a.Country, cells[3+k])
			}
			amounts[k] = n
		}
		a.DailyI, a.AccommodationI, a.DailyII, a.AccommodationII = amounts[0], amounts[1], amounts[2], amounts[3]

		if code, ok := Names[a.Country]; ok {
			if code != "" {
				if !known[code] {
					return nil, fmt.Errorf("Names gives %s for %s, which the countries dataset does not have", code, a.Country)
				}
				if len(byName[fold(a.Country)]) > 0 {
					return nil, fmt.Errorf("%s is in Names, but its name now matches the countries dataset: remove it", a.Country)
				}
				c := code
				a.Alpha2 = &c
			}
		} else {
			codes := byName[fold(a.Country)]
			if len(codes) != 1 {
				return nil, fmt.Errorf("%s matches %d countries of the countries dataset: a person adds it to Names", a.Country, len(codes))
			}
			a.Alpha2 = &codes[0]
		}
		if seen[a.Country] {
			return nil, fmt.Errorf("%s is listed twice", a.Country)
		}
		seen[a.Country] = true
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the page has no table of allowances")
	}
	return out, nil
}

// PublishedRows reads allowances.jsonl of the published dataset.
func PublishedRows(r io.Reader) ([]Allowance, error) {
	var out []Allowance
	d := json.NewDecoder(r)
	for d.More() {
		var a Allowance
		if err := d.Decode(&a); err != nil {
			return nil, fmt.Errorf("published allowances: %w", err)
		}
		out = append(out, a)
	}
	return out, nil
}

// Dataset reads the act and builds the dataset, keeping the published rows.
func Dataset(get Get, countries []Country, published []Allowance) (dataset.Dataset, error) {
	page, err := portal.Read(get, Act, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	now, err := ReadTable(page, countries)
	if err != nil {
		return dataset.Dataset{}, err
	}
	rows, err := history.Merge(now, published, Allowance.id, Allowance.content,
		func(a *Allowance) (**string, **string, **string) { return &a.ValidFrom, &a.ValidTo, &a.ChangeAct }, Changes,
		func(a, b string) bool { return fold(a) < fold(b) })
	if err != nil {
		return dataset.Dataset{}, err
	}
	var records []any
	for _, a := range rows {
		records = append(records, a)
	}
	return dataset.Dataset{
		Name:  "travel-allowances",
		Title: "Allowances for travel abroad (HG 518/1995)",
		Description: "The daily allowance (diurna) and the accommodation ceiling per day for a mission abroad, by country, in USD or EUR, as annex 1 " +
			"to HG 518/1995 sets them for the staff of public authorities and institutions: category II is for ministers, secretaries of state, " +
			"senior civil servants and the other functions the annex names, category I for everyone else. The Fiscal Code refers to these amounts " +
			"for the part of a daily allowance that is not taxed. Countries are as the act writes them, with alpha2 from the countries dataset where the country " +
			"still exists. The table is the one in force on 2026-01-01, the first day the dataset covers; a later change gives the day it applies " +
			"from and the act. The allowance for travel inside Romania (HG 714/2018) is not a table and is not here.",
		Source: dataset.Source{
			Name:    "Legislative Portal, HG 518/1995 in force, annex 1 (" + Act + ")",
			URL:     Act,
			Licence: "Legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Monthly,
		Update:    "When a government decision changes the table",
		Files: []dataset.File{{
			Name: "allowances.jsonl",
			Fields: []dataset.Field{
				{Name: "country", Description: "The country as the act writes it. With valid_from it is unique in the file"},
				{Name: "alpha2", Description: "The code of the country in the countries dataset, or null for one that no longer exists"},
				{Name: "currency", Description: "USD or EUR"},
				{Name: "daily_i", Description: "The daily allowance of category I, per day"},
				{Name: "accommodation_i", Description: "The accommodation ceiling of category I, per day"},
				{Name: "daily_ii", Description: "The daily allowance of category II, per day"},
				{Name: "accommodation_ii", Description: "The accommodation ceiling of category II, per day"},
				{Name: "valid_from", Description: "The day the row applies from, YYYY-MM-DD, or null for a row in force on 2026-01-01"},
				{Name: "valid_to", Description: "The last day the row applies, or null while it does"},
				{Name: "act", Description: "The act that changed the row, or null for a row in force on 2026-01-01"},
			},
			Records: records,
		}},
	}, nil
}
