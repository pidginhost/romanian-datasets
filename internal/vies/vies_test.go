package vies

import (
	"strings"
	"testing"
)

// The shape of the VIES answer, cut to a few states.
const recorded = `{ "vow" : { "available" : true }, "countries" : [
 { "countryCode" : "RO", "availability" : "Available" },
 { "countryCode" : "EL", "availability" : "Unavailable" },
 { "countryCode" : "DE", "availability" : "Available" },
 { "countryCode" : "FR", "availability" : "Available" },
 { "countryCode" : "IT", "availability" : "Available" },
 { "countryCode" : "XI", "availability" : "Available" } ] }`

func TestThePrefixesAreReadWhateverTheServiceStatus(t *testing.T) {
	got, err := Prefixes(strings.NewReader(recorded))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "DE EL FR IT RO XI" {
		t.Fatalf("prefixes are %v", got)
	}
}

func TestAnAnswerThatIsNotTheListIsRefused(t *testing.T) {
	for name, text := range map[string]string{
		"a web page":   "<html>Service unavailable</html>",
		"too few":      `{"countries":[{"countryCode":"RO"}]}`,
		"a bad code":   strings.Replace(recorded, `"XI"`, `"XIX"`, 1),
		"a code twice": strings.Replace(recorded, `"XI"`, `"RO"`, 1),
	} {
		if _, err := Prefixes(strings.NewReader(text)); err == nil {
			t.Errorf("%s was read as the VIES list", name)
		}
	}
}

func TestGreeceHasItsOwnVATPrefixAndACountryOutsideHasNone(t *testing.T) {
	prefixes, _ := Prefixes(strings.NewReader(recorded))
	for alpha2, want := range map[string]string{"RO": "RO", "GR": "EL"} {
		if got, in := PrefixOf(alpha2, prefixes); got != want || !in {
			t.Errorf("PrefixOf(%s) = %s, %v", alpha2, got, in)
		}
	}
	if _, in := PrefixOf("GB", prefixes); in {
		t.Error("the United Kingdom is given a VIES prefix")
	}
	if _, in := PrefixOf("XI", prefixes); !in {
		t.Error("XI itself is not found")
	}
}
