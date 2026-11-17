// Package vatnode builds the VAT datasets from the vatnode/eu-vat-rates-data file: how the VAT numbers of
// each European jurisdiction look, and which VAT rates it applies. The rates of the member states of the
// European Union come there from the Taxes in Europe Database of the European Commission.
package vatnode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
	"github.com/pidginhost/romanian-datasets/internal/vies"
)

// Address is the current data file.
const Address = "https://raw.githubusercontent.com/vatnode/eu-vat-rates-data/refs/heads/main/data/eu-vat-rates-data.json"

const sourceName = "vatnode/eu-vat-rates-data (https://github.com/vatnode/eu-vat-rates-data), " +
	"from the Taxes in Europe Database of the European Commission for the member states and from national sources for the others; " +
	"VAT prefixes from the VIES service of the European Commission (" + vies.Address + ")"

const sourceLicence = "MIT, copyright (c) 2026 Iurii Rogulia, for the vatnode file; " + vies.Licence + ", for VIES"

// The kinds of rate, in the order they are written.
var categories = []string{"standard", "reduced", "super_reduced", "parking"}

// Jurisdiction is what the file says about one country or territory with a VAT of its own.
type Jurisdiction struct {
	// Code is the ISO 3166-1 alpha-2 code, or XI for Northern Ireland and XK for Kosovo.
	Code         string
	Name         string
	TaxName      string
	Abbreviation string
	Format       string
	Pattern      string
	// Rates holds the rates of each category as written in the file, such as "21" or "5.5".
	Rates map[string][]string
}

var code = regexp.MustCompile(`^[A-Z]{2}$`)

// Parse reads the file and gives the jurisdictions sorted by code.
func Parse(r io.Reader) ([]Jurisdiction, error) {
	var file struct {
		Rates map[string]struct {
			Country      string        `json:"country"`
			TaxName      string        `json:"vat_name"`
			Abbreviation string        `json:"vat_abbr"`
			Format       string        `json:"format"`
			Pattern      string        `json:"pattern"`
			Standard     *json.Number  `json:"standard"`
			Reduced      []json.Number `json:"reduced"`
			SuperReduced *json.Number  `json:"super_reduced"`
			Parking      *json.Number  `json:"parking"`
		} `json:"rates"`
	}
	decoder := json.NewDecoder(io.LimitReader(r, 16<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("not the vatnode file: %w", err)
	}
	if len(file.Rates) == 0 {
		return nil, fmt.Errorf("the vatnode file has no jurisdictions")
	}

	var out []Jurisdiction
	for key, entry := range file.Rates {
		j := Jurisdiction{
			Code: key, Name: strings.TrimSpace(entry.Country), TaxName: strings.TrimSpace(entry.TaxName),
			Abbreviation: strings.TrimSpace(entry.Abbreviation), Format: strings.TrimSpace(entry.Format),
			Pattern: entry.Pattern, Rates: map[string][]string{},
		}
		switch {
		case !code.MatchString(key):
			return nil, fmt.Errorf("jurisdiction %q is not a two-letter code", key)
		case j.Name == "" || j.TaxName == "" || j.Abbreviation == "" || j.Format == "" || j.Pattern == "":
			return nil, fmt.Errorf("%s: a name, the format or the pattern is missing", key)
		case entry.Standard == nil:
			return nil, fmt.Errorf("%s has no standard rate", key)
		}
		if _, err := regexp.Compile(j.Pattern); err != nil {
			return nil, fmt.Errorf("%s: the pattern is not a regular expression: %w", key, err)
		}

		given := map[string][]json.Number{"standard": {*entry.Standard}, "reduced": entry.Reduced}
		if entry.SuperReduced != nil {
			given["super_reduced"] = []json.Number{*entry.SuperReduced}
		}
		if entry.Parking != nil {
			given["parking"] = []json.Number{*entry.Parking}
		}
		for category, numbers := range given {
			for _, n := range numbers {
				rate, err := percent(n)
				if err != nil {
					return nil, fmt.Errorf("%s, %s rate: %w", key, category, err)
				}
				j.Rates[category] = append(j.Rates[category], rate)
			}
			// By value, so that the order of the file does not decide the order here.
			rates := j.Rates[category]
			sort.Slice(rates, func(a, b int) bool {
				x, _ := strconv.ParseFloat(rates[a], 64)
				y, _ := strconv.ParseFloat(rates[b], 64)
				return x < y
			})
		}
		out = append(out, j)
	}

	sort.Slice(out, func(i, k int) bool { return out[i].Code < out[k].Code })
	return out, nil
}

var decimal = regexp.MustCompile(`^\d{1,2}(\.\d{1,4})?$`)

// percent writes a rate as a decimal with no trailing zeros: 21.0 becomes "21" and 5.50 becomes "5.5".
// It stays text, so that no reader meets it as a binary fraction.
func percent(n json.Number) (string, error) {
	text := n.String()
	if !decimal.MatchString(text) {
		return "", fmt.Errorf("%q is not a percentage below 100", text)
	}
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text, nil
}

// Fetch gives one source file.
type Fetch func() (io.ReadCloser, error)

// FromSite reads the vatnode file.
func FromSite(client *http.Client) Fetch {
	return func() (io.ReadCloser, error) { return fetch.Get(client, Address, "") }
}

// VIESFromSite reads the list of VAT prefixes from the VIES service.
func VIESFromSite(client *http.Client) Fetch {
	return func() (io.ReadCloser, error) { return fetch.Get(client, vies.Address, "application/json") }
}

func read(file, viesList Fetch) ([]Jurisdiction, []string, error) {
	body, err := file()
	if err != nil {
		return nil, nil, err
	}
	defer body.Close()
	all, err := Parse(body)
	if err != nil {
		return nil, nil, err
	}

	list, err := viesList()
	if err != nil {
		return nil, nil, err
	}
	defer list.Close()
	prefixes, err := vies.Prefixes(list)
	return all, prefixes, err
}

// Number is how the VAT numbers of one jurisdiction look.
type Number struct {
	Code string `json:"code"`
	Name string `json:"name_en"`
	// EUVAT is true when the VIES service answers for the jurisdiction.
	EUVAT bool `json:"eu_vat"`
	// VATPrefix is the prefix VIES uses, or null outside the European Union for VAT.
	VATPrefix    *string `json:"vat_prefix"`
	TaxName      string  `json:"tax_name"`
	Abbreviation string  `json:"tax_abbreviation"`
	Format       string  `json:"format"`
	Pattern      string  `json:"pattern"`
}

// Rate is one VAT rate in force in one jurisdiction.
type Rate struct {
	Code     string `json:"code"`
	EUVAT    bool   `json:"eu_vat"`
	Category string `json:"category"`
	Rate     string `json:"rate"`
}

const codeField = "ISO 3166-1 alpha-2 code of the jurisdiction; XI for Northern Ireland and XK for Kosovo, which have none"
const euField = "True when the VIES service of the European Commission answers for the VAT numbers of the jurisdiction"

// Numbers builds the dataset of VAT number formats.
func Numbers(file, viesList Fetch) (dataset.Dataset, error) {
	all, prefixes, err := read(file, viesList)
	if err != nil {
		return dataset.Dataset{}, err
	}

	var records []any
	for _, j := range all {
		n := Number{
			Code: j.Code, Name: j.Name, TaxName: j.TaxName, Abbreviation: j.Abbreviation,
			Format: j.Format, Pattern: j.Pattern,
		}
		if prefix, in := vies.PrefixOf(j.Code, prefixes); in {
			n.EUVAT, n.VATPrefix = true, &prefix
		}
		records = append(records, n)
	}

	return dataset.Dataset{
		Name:  "vat-numbers",
		Title: "VAT numbers of European jurisdictions",
		Description: "How a VAT number looks in each European jurisdiction: the prefix, the format in words and as a regular expression, " +
			"and the name of the tax in the local language. It says which jurisdictions are in the European Union for VAT. " +
			"A number that fits the pattern is well formed; whether it is registered is a question for VIES or the national register.",
		Source:    dataset.Source{Name: sourceName, URL: Address, Licence: sourceLicence},
		Frequency: dataset.Manual,
		Update:    "When a jurisdiction changes the form of its VAT numbers, or joins or leaves the European Union for VAT",
		Files: []dataset.File{{
			Name: "numbers.jsonl",
			Fields: []dataset.Field{
				{Name: "code", Description: codeField},
				{Name: "name_en", Description: "Name of the jurisdiction in English"},
				{Name: "eu_vat", Description: euField},
				{Name: "vat_prefix", Description: "The prefix of the VAT numbers in VIES when eu_vat is true, otherwise null. Greece is EL, not GR"},
				{Name: "tax_name", Description: "The name of the tax in the local language"},
				{Name: "tax_abbreviation", Description: "The short name of the tax in the local language"},
				{Name: "format", Description: "The form of the VAT number, in words"},
				{Name: "pattern", Description: "The form of the VAT number as a regular expression, with the prefix where the jurisdiction uses one"},
			},
			Records: records,
		}},
	}, nil
}

// Rates builds the dataset of VAT rates.
func Rates(file, viesList Fetch) (dataset.Dataset, error) {
	all, prefixes, err := read(file, viesList)
	if err != nil {
		return dataset.Dataset{}, err
	}

	var records []any
	for _, j := range all {
		_, in := vies.PrefixOf(j.Code, prefixes)
		for _, category := range categories {
			for _, rate := range j.Rates[category] {
				records = append(records, Rate{Code: j.Code, EUVAT: in, Category: category, Rate: rate})
			}
		}
	}

	return dataset.Dataset{
		Name:  "vat-rates",
		Title: "VAT rates of European jurisdictions",
		Description: "The VAT rates in force in each European jurisdiction: the standard rate, the reduced rates, " +
			"and the super-reduced and parking rates where there are any. It does not say which goods a reduced rate applies to, " +
			"and it holds no earlier rates: the day a dataset changed is in the index.",
		Source:    dataset.Source{Name: sourceName, URL: Address, Licence: sourceLicence},
		Frequency: dataset.Weekly,
		Update:    "When a jurisdiction changes a rate; the source checks the European Commission database daily",
		Files: []dataset.File{{
			Name: "rates.jsonl",
			Fields: []dataset.Field{
				{Name: "code", Description: codeField},
				{Name: "eu_vat", Description: euField},
				{Name: "category", Description: "standard, reduced, super_reduced or parking"},
				{Name: "rate", Description: "The rate in percent, as decimal text with no trailing zeros: \"21\", \"5.5\""},
			},
			Records: records,
		}},
	}, nil
}
