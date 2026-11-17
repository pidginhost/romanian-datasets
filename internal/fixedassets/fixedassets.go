// Package fixedassets builds the catalogue of fixed assets of HG 2139/2004: the classification codes and the
// normal lives in years, which are the depreciation periods of the straight-line method.
package fixedassets

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

// Act is the page of HG 2139/2004 on the Legislative Portal, which shows the text in force.
const Act = portal.Address + "58613"

// maxPage is many times the size of the page.
const maxPage = 16 << 20

// Get gives the body of one public page.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the portal over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "text/html") }
}

// Changes gives, by id (code, and the variant after a bar), a change of the catalogue a person read in the
// act that made it. A row that changes and is not here stops the build.
var Changes = map[string]history.Change{}

// Asset is one row of the catalogue.
type Asset struct {
	Code string `json:"code"`
	// Variant is the letter of a row the catalogue gives for its code with another life, as a) and b) for
	// a neutral and a corrosive environment, or null.
	Variant *string `json:"variant"`
	Depth   int     `json:"depth"`
	Parent  *string `json:"parent"`
	Name    string  `json:"name"`
	// MinYears and MaxYears are the range of the normal life; null for a heading without one.
	MinYears  *int    `json:"min_years"`
	MaxYears  *int    `json:"max_years"`
	ValidFrom *string `json:"valid_from"`
	ValidTo   *string `json:"valid_to"`
	ChangeAct *string `json:"act"`
}

// ID names a row across builds.
func (a Asset) ID() string {
	if a.Variant == nil {
		return a.Code
	}
	return a.Code + "|" + *a.Variant
}

// Key identifies a row in its file.
func (a Asset) Key() string { return history.Key(a.ID(), a.ValidFrom) }

func (a Asset) content() string {
	x, _ := json.Marshal(Asset{Code: a.Code, Variant: a.Variant, Depth: a.Depth, Parent: a.Parent, Name: a.Name, MinYears: a.MinYears, MaxYears: a.MaxYears})
	return string(x)
}

var (
	rowPattern       = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellPattern      = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	tagPattern       = regexp.MustCompile(`<[^>]*>`)
	codePattern      = regexp.MustCompile(`^(\d+(?:\.\d+)*)\.?$`)
	groupPattern     = regexp.MustCompile(`(?i)^grupa\s+(\d+)\s*[.:]?\s+(.+)$`)
	headingPattern   = regexp.MustCompile(`^(\d+(?:\.\d+)+)\.?\s+(.+)$`)
	rangePattern     = regexp.MustCompile(`^(\d+)\s*-\s*(\d+)$`)
	inRangePattern   = regexp.MustCompile(`\s(\d+)\s*-\s*(\d+)\s`)
	singlePattern    = regexp.MustCompile(`^\d+$`)
	variantPattern   = regexp.MustCompile(`^([a-z])\)\s*(.+)$`)
	numberRowPattern = regexp.MustCompile(`^\d+$`)
)

func text(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))), " ")
}

func years(cell string) (*int, *int, error) {
	c := strings.TrimSpace(cell)
	if c == "" {
		return nil, nil, nil
	}
	if m := rangePattern.FindStringSubmatch(c); m != nil {
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		if lo > hi {
			return nil, nil, fmt.Errorf("the life %q runs backwards", c)
		}
		return &lo, &hi, nil
	}
	if singlePattern.MatchString(c) {
		n, _ := strconv.Atoi(c)
		return &n, &n, nil
	}
	return nil, nil, fmt.Errorf("the life %q is not a number of years or a range", c)
}

// ReadCatalogue reads the table of the catalogue out of the page of the act. It ends where the table of
// correspondence with HG 964/1998 starts.
func ReadCatalogue(page string) ([]Asset, error) {
	var out []Asset
	started := false
	seen := map[string]bool{}
	add := func(a Asset) error {
		a.Depth = strings.Count(a.Code, ".") + 1
		for up := a.Code; strings.Contains(up, "."); {
			up = up[:strings.LastIndex(up, ".")]
			if seen[up] {
				parent := up
				a.Parent = &parent
				break
			}
		}
		if a.Variant != nil {
			code := a.Code
			a.Parent = &code
		}
		if a.Depth > 1 && a.Parent == nil {
			return fmt.Errorf("code %s has no code above it", a.Code)
		}
		if seen[a.ID()] {
			return fmt.Errorf("code %s is written twice", a.ID())
		}
		seen[a.ID()] = true
		out = append(out, a)
		return nil
	}
	last := ""
	for _, r := range rowPattern.FindAllStringSubmatch(page, -1) {
		var cells []string
		for _, c := range cellPattern.FindAllStringSubmatch(r[1], -1) {
			cells = append(cells, text(c[1]))
		}
		if len(cells) == 0 {
			continue
		}
		if !started {
			started = strings.HasPrefix(cells[0], "CODUL DE CLASIFICARE")
			continue
		}
		if strings.HasPrefix(cells[0], "Clasificarea și duratele normale") {
			break
		}
		switch len(cells) {
		case 1:
			cell := cells[0]
			if m := groupPattern.FindStringSubmatch(cell); m != nil {
				if err := add(Asset{Code: m[1], Name: m[2]}); err != nil {
					return nil, err
				}
				last = m[1]
				continue
			}
			m := headingPattern.FindStringSubmatch(cell)
			if m == nil {
				return nil, fmt.Errorf("a row the build does not know: %q", cell)
			}
			// One heading row holds its life inside the name: "2.5. Alte instalații ... 20-30 și plantații".
			a := Asset{Code: m[1], Name: m[2]}
			if lm := inRangePattern.FindStringSubmatchIndex(" " + m[2] + " "); lm != nil {
				s := " " + m[2] + " "
				lo, hi, err := years(s[lm[2]:lm[3]] + "-" + s[lm[4]:lm[5]])
				if err != nil {
					return nil, err
				}
				a.MinYears, a.MaxYears = lo, hi
				a.Name = strings.Join(strings.Fields(s[:lm[0]]+" "+s[lm[1]:]), " ")
			}
			if err := add(a); err != nil {
				return nil, err
			}
			last = a.Code
		case 3:
			if numberRowPattern.MatchString(cells[0]) && cells[1] == "1" && cells[2] == "2" {
				continue
			}
			lo, hi, err := years(cells[2])
			if err != nil {
				return nil, fmt.Errorf("code %q: %w", cells[0], err)
			}
			if cells[0] == "" {
				v := variantPattern.FindStringSubmatch(cells[1])
				if v == nil || last == "" {
					return nil, fmt.Errorf("a row without a code that is no variant: %q", cells[1])
				}
				letter := v[1]
				if err := add(Asset{Code: last, Variant: &letter, Name: v[2], MinYears: lo, MaxYears: hi}); err != nil {
					return nil, err
				}
				continue
			}
			m := codePattern.FindStringSubmatch(cells[0])
			if m == nil {
				return nil, fmt.Errorf("a code the build does not read: %q", cells[0])
			}
			if err := add(Asset{Code: m[1], Name: cells[1], MinYears: lo, MaxYears: hi}); err != nil {
				return nil, err
			}
			last = m[1]
		default:
			return nil, fmt.Errorf("a row of %d cells: %q", len(cells), cells)
		}
	}
	if !started || len(out) == 0 {
		return nil, fmt.Errorf("the page has no catalogue")
	}
	return out, nil
}

// less orders codes as the catalogue does: 1.1.2 before 1.1.10, and a variant after its code.
func less(a, b string) bool {
	ca, va, _ := strings.Cut(a, "|")
	cb, vb, _ := strings.Cut(b, "|")
	pa, pb := strings.Split(ca, "."), strings.Split(cb, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	if len(pa) != len(pb) {
		return len(pa) < len(pb)
	}
	return va < vb
}

// PublishedRows reads assets.jsonl of the published dataset.
func PublishedRows(r io.Reader) ([]Asset, error) {
	var out []Asset
	d := json.NewDecoder(r)
	for d.More() {
		var a Asset
		if err := d.Decode(&a); err != nil {
			return nil, fmt.Errorf("published assets: %w", err)
		}
		out = append(out, a)
	}
	return out, nil
}

// Dataset reads the act and builds the dataset, keeping the published rows.
func Dataset(get Get, published []Asset) (dataset.Dataset, error) {
	page, err := portal.Read(get, Act, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	now, err := ReadCatalogue(page)
	if err != nil {
		return dataset.Dataset{}, err
	}
	rows, err := history.Merge(now, published, Asset.ID, Asset.content,
		func(a *Asset) (**string, **string, **string) { return &a.ValidFrom, &a.ValidTo, &a.ChangeAct }, Changes, less)
	if err != nil {
		return dataset.Dataset{}, err
	}
	var records []any
	for _, a := range rows {
		records = append(records, a)
	}
	return dataset.Dataset{
		Name:  "fixed-assets",
		Title: "Catalogue of fixed assets (HG 2139/2004)",
		Description: "The catalogue of the classification and normal lives of fixed assets approved by HG 2139/2004: groups (1 buildings, " +
			"2 technical installations, transport, animals and plantations, 3 furniture, office equipment and other tangible assets), subgroups, " +
			"classes, subclasses and families, each with the range of its normal life in years, which is the depreciation period of the " +
			"straight-line method; a new asset takes a life within the range of its code. A heading without a range has none. Where the catalogue " +
			"gives one code two lives, as a) for a neutral and b) for a corrosive environment, each is a row with its variant. The catalogue is the " +
			"one in force on 2026-01-01, the first day the dataset covers; a later change gives the day it applies from and the act. Names are as the act writes them.",
		Source: dataset.Source{
			Name:    "Legislative Portal, HG 2139/2004 in force (" + Act + ")",
			URL:     Act,
			Licence: "Legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Monthly,
		Update:    "When a government decision changes the catalogue",
		Files: []dataset.File{{
			Name: "assets.jsonl",
			Fields: []dataset.Field{
				{Name: "code", Description: "The classification code, as 2.1.16.1.2.1. With variant and valid_from it is unique in the file"},
				{Name: "variant", Description: "The letter of a second life the catalogue gives the code, as a or b, or null"},
				{Name: "depth", Description: "1 for a group, 2 for a subgroup, then class, subclass and the levels below"},
				{Name: "parent", Description: "The code above, or for a variant its code; null for a group"},
				{Name: "name", Description: "The name as the act writes it"},
				{Name: "min_years", Description: "The shortest normal life in years, or null for a heading"},
				{Name: "max_years", Description: "The longest normal life in years, or null for a heading"},
				{Name: "valid_from", Description: "The day the row applies from, YYYY-MM-DD, or null for a row in force on 2026-01-01"},
				{Name: "valid_to", Description: "The last day the row applies, or null while it does"},
				{Name: "act", Description: "The act that changed the row, or null for a row in force on 2026-01-01"},
			},
			Records: records,
		}},
	}, nil
}
