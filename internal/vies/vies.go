// Package vies reads which VAT prefixes the VIES service of the European Commission answers for.
// These are the member states of the European Union for VAT, and Northern Ireland (XI).
package vies

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
)

// Address lists the prefixes VIES knows, with whether each national service answers right now.
const Address = "https://ec.europa.eu/taxation_customs/vies/rest-api/check-status"

// Licence is the terms the European Commission publishes its content under.
const Licence = "European Commission reuse notice (Decision 2011/833/EU): reuse allowed with the source acknowledged"

var prefix = regexp.MustCompile(`^[A-Z]{2}$`)

// Prefixes reads the answer of the status service and gives the prefixes, sorted.
// Whether a national service is up at the moment does not matter and is not kept.
func Prefixes(r io.Reader) ([]string, error) {
	var answer struct {
		Countries []struct {
			Code string `json:"countryCode"`
		} `json:"countries"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&answer); err != nil {
		return nil, fmt.Errorf("not the VIES status answer: %w", err)
	}

	seen := map[string]bool{}
	var out []string
	for _, c := range answer.Countries {
		switch {
		case !prefix.MatchString(c.Code):
			return nil, fmt.Errorf("VIES prefix %q is not two letters", c.Code)
		case seen[c.Code]:
			return nil, fmt.Errorf("VIES lists %s twice", c.Code)
		}
		seen[c.Code] = true
		out = append(out, c.Code)
	}
	// Fewer than the six founding states is not the list.
	if len(out) < 6 {
		return nil, fmt.Errorf("VIES lists %d prefixes; that is not the whole list", len(out))
	}
	sort.Strings(out)
	return out, nil
}

// Greece is GR in ISO 3166-1 and EL in VAT numbers. Every other member state uses its ISO code.
const (
	greeceISO = "GR"
	greeceVAT = "EL"
)

// PrefixOf gives the VAT prefix of a country by its ISO alpha-2 code, and whether VIES answers for it.
func PrefixOf(alpha2 string, prefixes []string) (string, bool) {
	want := alpha2
	if alpha2 == greeceISO {
		want = greeceVAT
	}
	i := sort.SearchStrings(prefixes, want)
	return want, i < len(prefixes) && prefixes[i] == want
}
