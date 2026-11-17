package vatnode

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// An extract in the shape of the vatnode file: a member state, Greece with its own prefix,
// Northern Ireland, and a country outside the European Union.
const recorded = `{
 "version": "2026-11-16", "source": "European Commission TEDB",
 "rates": {
  "RO": {"country": "Romania", "currency": "RON", "eu_member": true, "vat_name": "Taxa pe valoarea adăugată", "vat_abbr": "TVA",
         "standard": 21.0, "reduced": [11.0], "super_reduced": null, "parking": null,
         "format": "RO + 2–10 digits", "pattern": "^RO\\d{2,10}$"},
  "GR": {"country": "Greece", "currency": "EUR", "eu_member": true, "vat_name": "Φόρος Προστιθέμενης Αξίας", "vat_abbr": "ΦΠΑ",
         "standard": 24.0, "reduced": [13.0, 6.0], "super_reduced": 4.0, "parking": 13.0,
         "format": "EL + 9 digits", "pattern": "^EL\\d{9}$"},
  "XI": {"country": "Northern Ireland", "currency": "GBP", "eu_member": false, "vat_name": "Value Added Tax", "vat_abbr": "VAT",
         "standard": 20.0, "reduced": [5.0], "super_reduced": null, "parking": null,
         "format": "XI + 9 digits", "pattern": "^XI\\d{9}$"},
  "CH": {"country": "Switzerland", "currency": "CHF", "eu_member": false, "vat_name": "Mehrwertsteuer", "vat_abbr": "MWST",
         "standard": 8.1, "reduced": [2.6, 3.8], "super_reduced": null, "parking": null,
         "format": "CHE-NNN.NNN.NNN", "pattern": "^CHE-?\\d{9}$"}
 }
}`

const viesList = `{"countries":[{"countryCode":"RO"},{"countryCode":"EL"},{"countryCode":"XI"},{"countryCode":"DE"},{"countryCode":"FR"},{"countryCode":"IT"}]}`

func from(text string) Fetch {
	return func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(text)), nil }
}

func TestRatesAreDecimalTextWithNoTrailingZeros(t *testing.T) {
	all, err := Parse(strings.NewReader(recorded))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].Code != "CH" || all[3].Code != "XI" {
		t.Fatalf("jurisdictions are %+v", all)
	}
	ch, gr := all[0], all[1]
	if got := fmt.Sprint(ch.Rates["standard"], ch.Rates["reduced"]); got != "[8.1] [2.6 3.8]" {
		t.Fatalf("Swiss rates are %s", got)
	}
	if got := fmt.Sprint(gr.Rates["standard"], gr.Rates["super_reduced"], gr.Rates["parking"]); got != "[24] [4] [13]" {
		t.Fatalf("Greek rates are %s", got)
	}
}

func TestAFileThatCannotBeTrustedIsRefused(t *testing.T) {
	for name, text := range map[string]string{
		"a web page":       "<html>Not found</html>",
		"no jurisdictions": `{"rates": {}}`,
		"a bad code":       strings.Replace(recorded, `"CH":`, `"CHE":`, 1),
		"no standard rate": strings.Replace(recorded, `"standard": 8.1`, `"standard": null`, 1),
		"no pattern":       strings.Replace(recorded, `"pattern": "^CHE-?\\d{9}$"`, `"pattern": ""`, 1),
		"a bad pattern":    strings.Replace(recorded, `"pattern": "^CHE-?\\d{9}$"`, `"pattern": "^CHE(\\d{9}$"`, 1),
		"a rate over 100":  strings.Replace(recorded, `"standard": 8.1`, `"standard": 810`, 1),
		"a negative rate":  strings.Replace(recorded, `"standard": 8.1`, `"standard": -8.1`, 1),
	} {
		if _, err := Parse(strings.NewReader(text)); err == nil {
			t.Errorf("%s was read as the vatnode file", name)
		}
	}
}

func TestVATNumbersSayWhoIsInTheUnionForVATAndWithWhichPrefix(t *testing.T) {
	d, err := Numbers(from(recorded), from(viesList))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range d.Files[0].Records {
		n := r.(Number)
		prefix := "none"
		if n.VATPrefix != nil {
			prefix = *n.VATPrefix
		}
		got[n.Code] = fmt.Sprint(n.EUVAT, " ", prefix)
	}
	want := map[string]string{"RO": "true RO", "GR": "true EL", "XI": "true XI", "CH": "false none"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if d.Frequency != "manual" {
		t.Fatalf("frequency is %s", d.Frequency)
	}
}

func TestVATRatesAreOneRecordForEachRateInAFixedOrder(t *testing.T) {
	d, err := Rates(from(recorded), from(viesList))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, r := range d.Files[0].Records {
		rate := r.(Rate)
		lines = append(lines, fmt.Sprint(rate.Code, " ", rate.EUVAT, " ", rate.Category, " ", rate.Rate))
	}
	want := []string{
		"CH false standard 8.1", "CH false reduced 2.6", "CH false reduced 3.8",
		"GR true standard 24", "GR true reduced 6", "GR true reduced 13", "GR true super_reduced 4", "GR true parking 13",
		"RO true standard 21", "RO true reduced 11",
		"XI true standard 20", "XI true reduced 5",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rates are\n%s", strings.Join(lines, "\n"))
	}
	if d.Frequency != "weekly" {
		t.Fatalf("frequency is %s", d.Frequency)
	}
}

func TestAVIESListThatCannotBeReadStopsTheBuild(t *testing.T) {
	if _, err := Rates(from(recorded), from("<html>down</html>")); err == nil {
		t.Fatal("the dataset was built without the VIES list")
	}
}
