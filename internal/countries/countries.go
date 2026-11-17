// Package countries builds the list of countries and territories from the authority table of the
// Publications Office of the European Union, which carries the ISO 3166-1 codes and the names in Romanian.
package countries

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
	"github.com/pidginhost/romanian-datasets/internal/vies"
)

// Address is the whole table in one file.
const Address = "https://publications.europa.eu/resource/distribution/country/rdf/skos_core/countries-skos.rdf"

// maxFile is several times the size of the table. A longer answer is not the table.
const maxFile = 32 << 20

const (
	skos         = "http://www.w3.org/2004/02/skos/core#"
	euvoc        = "http://publications.europa.eu/ontology/euvoc#"
	euMembers    = "http://publications.europa.eu/resource/authority/country/0005"
	userAssigned = 'X'
)

// Country is one country or territory.
type Country struct {
	// Alpha2 and Alpha3 are the ISO 3166-1 codes, or the user-assigned ones when ISO is false.
	Alpha2 string `json:"alpha2"`
	Alpha3 string `json:"alpha3"`
	// Numeric is the ISO 3166-1 numeric code as three digits, or null when the source gives none.
	Numeric *string `json:"numeric"`
	NameRO  string  `json:"name_ro"`
	NameEN  string  `json:"name_en"`
	// EUMember is true for a current member state of the European Union.
	EUMember bool `json:"eu_member"`
	// EUVAT is true for a country whose VAT numbers the VIES service of the European Commission answers for.
	EUVAT bool `json:"eu_vat"`
	// VATPrefix is the prefix of its VAT numbers when EUVAT is true, otherwise null. Greece is EL.
	VATPrefix *string `json:"vat_prefix"`
	// ISO is false for an entry that ISO 3166-1 does not list, which is Kosovo.
	ISO       bool   `json:"iso"`
	ValidFrom string `json:"valid_from"`
	// ValidTo is the last day of use of a withdrawn entry, or null for a current one.
	ValidTo *string `json:"valid_to"`
}

type table struct {
	Concepts []concept `xml:"http://www.w3.org/2004/02/skos/core# Concept"`
}

type concept struct {
	Deprecated string `xml:"deprecated,attr"`
	Labels     []struct {
		Lang string `xml:"lang,attr"`
		Text string `xml:",chardata"`
	} `xml:"http://www.w3.org/2004/02/skos/core# prefLabel"`
	Notations []struct {
		Type string `xml:"datatype,attr"`
		Text string `xml:",chardata"`
	} `xml:"http://www.w3.org/2004/02/skos/core# notation"`
	Schemes []struct {
		Resource string `xml:"resource,attr"`
	} `xml:"http://www.w3.org/2004/02/skos/core# inScheme"`
	Identifier string `xml:"identifier"`
	Start      string `xml:"start.use"`
	End        string `xml:"end.use"`
}

func (c concept) notation(kind string) string {
	for _, n := range c.Notations {
		if n.Type == euvoc+kind {
			return strings.TrimSpace(n.Text)
		}
	}
	return ""
}

func (c concept) label(lang string) string {
	for _, l := range c.Labels {
		if l.Lang == lang {
			return strings.TrimSpace(l.Text)
		}
	}
	return ""
}

// The table still writes some Romanian names with the cedilla letters of older fonts.
var commaBelow = strings.NewReplacer("ş", "ș", "Ş", "Ș", "ţ", "ț", "Ţ", "Ț")

// name drops the asterisk with which the table points to a footnote on the status of Kosovo.
func name(label string) string { return strings.TrimSpace(strings.TrimSuffix(label, "*")) }

// Parse reads the table and keeps the entries that are countries or territories with codes, sorted by alpha-3.
func Parse(r io.Reader) ([]Country, error) {
	var t table
	if err := xml.NewDecoder(io.LimitReader(r, maxFile)).Decode(&t); err != nil {
		return nil, fmt.Errorf("not the EU country table: %w", err)
	}

	var out []Country
	seen := map[string]bool{}
	for _, c := range t.Concepts {
		country := Country{
			Alpha2: c.notation("ISO_3166_1_ALPHA_2"),
			Alpha3: c.notation("ISO_3166_1_ALPHA_3"),
			NameRO: name(commaBelow.Replace(c.label("ro"))),
			NameEN: name(c.label("en")),
			ISO:    true,
		}
		id := strings.TrimSpace(c.Identifier)
		switch {
		case country.Alpha2 != "" && country.Alpha3 != "":
		case country.Alpha2 == "" && country.Alpha3 == "" && len(id) == 3 && id[0] == userAssigned && len(c.notation("IBAN_COU")) == 2:
			// No ISO code, but banks use a code for it: Kosovo, XK and XKX.
			country.Alpha2, country.Alpha3, country.ISO = c.notation("IBAN_COU"), id, false
		default:
			// A sea, a disputed area, a grouping, or the European Union itself.
			continue
		}

		if numeric := c.notation("ISO_3166_1_NUM"); numeric != "" {
			country.Numeric = &numeric
		}
		for _, s := range c.Schemes {
			country.EUMember = country.EUMember || s.Resource == euMembers
		}
		country.ValidFrom = strings.TrimSpace(c.Start)
		if end := strings.TrimSpace(c.End); end != "" {
			country.ValidTo = &end
		}

		switch {
		case country.NameRO == "" || country.NameEN == "":
			return nil, fmt.Errorf("%s has no name in Romanian or in English", country.Alpha3)
		case !isDate(country.ValidFrom) || (country.ValidTo != nil && !isDate(*country.ValidTo)):
			return nil, fmt.Errorf("%s: its days of use are not dates", country.Alpha3)
		case (c.Deprecated == "true") != (country.ValidTo != nil):
			return nil, fmt.Errorf("%s: withdrawn and last day of use do not agree", country.Alpha3)
		case seen[country.Alpha3]:
			return nil, fmt.Errorf("%s is in the table twice", country.Alpha3)
		}
		seen[country.Alpha3] = true
		out = append(out, country)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the table has no countries")
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Alpha3 < out[j].Alpha3 })
	return out, nil
}

func isDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// Fetch gives one source file.
type Fetch func() (io.ReadCloser, error)

// FromSite reads the table from the Publications Office.
func FromSite(client *http.Client) Fetch {
	return func() (io.ReadCloser, error) { return fetch.Get(client, Address, "") }
}

// VIESFromSite reads the list of VAT prefixes from the VIES service.
func VIESFromSite(client *http.Client) Fetch {
	return func() (io.ReadCloser, error) { return fetch.Get(client, vies.Address, "application/json") }
}

// WithVAT marks the current countries VIES answers for. A withdrawn country is never marked.
func WithVAT(all []Country, prefixes []string) []Country {
	out := append([]Country(nil), all...)
	for i := range out {
		if out[i].ValidTo != nil {
			continue
		}
		if prefix, in := vies.PrefixOf(out[i].Alpha2, prefixes); in {
			out[i].EUVAT, out[i].VATPrefix = true, &prefix
		}
	}
	return out
}

// Dataset reads the table and the VIES list and builds the dataset.
func Dataset(table, viesList Fetch) (dataset.Dataset, error) {
	body, err := table()
	if err != nil {
		return dataset.Dataset{}, err
	}
	defer body.Close()
	all, err := Parse(body)
	if err != nil {
		return dataset.Dataset{}, err
	}

	list, err := viesList()
	if err != nil {
		return dataset.Dataset{}, err
	}
	defer list.Close()
	prefixes, err := vies.Prefixes(list)
	if err != nil {
		return dataset.Dataset{}, err
	}
	all = WithVAT(all, prefixes)

	var records []any
	for _, c := range all {
		records = append(records, c)
	}

	return dataset.Dataset{
		Name:  "countries",
		Title: "Countries and territories",
		Description: "Countries and territories with their ISO 3166-1 codes, their names in Romanian and English, " +
			"whether they are a member state of the European Union today, and whether they are in the European Union for VAT, with their VAT prefix. " +
			"Northern Ireland has the VAT prefix XI but is not a country, so it is not here. " +
			"Withdrawn entries stay, with their last day of use, so an alpha-2 code can appear on a withdrawn entry and on a current one; the alpha-3 code is unique. " +
			"Kosovo is not in ISO 3166-1 and is listed with the user-assigned codes XK and XKX. " +
			"Names are the short names the European Union institutions use, with Romanian letters written with the comma below " +
			"and without the asterisk the source puts on Kosovo. A name does not state a position on the status of a territory.",
		Source: dataset.Source{
			Name:    "Publications Office of the European Union, Countries and territories authority table; VAT prefixes from the VIES service of the European Commission (" + vies.Address + ")",
			URL:     Address,
			Licence: vies.Licence,
		},
		Frequency: dataset.Manual,
		Update:    "When the Publications Office changes the table: a new or withdrawn country, a changed name, a change in the European Union",
		Files: []dataset.File{{
			Name: "countries.jsonl",
			Fields: []dataset.Field{
				{Name: "alpha2", Description: "ISO 3166-1 alpha-2 code; for Kosovo the user-assigned XK"},
				{Name: "alpha3", Description: "ISO 3166-1 alpha-3 code, unique in the file; for Kosovo the user-assigned XKX"},
				{Name: "numeric", Description: "ISO 3166-1 numeric code, three digits, or null when the source gives none"},
				{Name: "name_ro", Description: "Short name in Romanian"},
				{Name: "name_en", Description: "Short name in English"},
				{Name: "eu_member", Description: "True for a member state of the European Union at the time of the build"},
				{Name: "eu_vat", Description: "True for a country whose VAT numbers the VIES service of the European Commission answers for"},
				{Name: "vat_prefix", Description: "The prefix of its VAT numbers when eu_vat is true, otherwise null. Greece is EL, not GR"},
				{Name: "iso", Description: "False for an entry ISO 3166-1 does not list"},
				{Name: "valid_from", Description: "The first day of use the source gives, as YYYY-MM-DD. Many old entries carry 1950-05-09, the first day of the table"},
				{Name: "valid_to", Description: "The last day of use of a withdrawn entry, as YYYY-MM-DD, or null for a current one"},
			},
			Records: records,
		}},
	}, nil
}
