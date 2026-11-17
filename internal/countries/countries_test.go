package countries

import (
	"io"
	"strings"
	"testing"
)

// An extract in the shape of the EU file: a current member state, a country written with the old cedilla
// letters, a withdrawn country, Kosovo with no ISO code, Northern Ireland, the European Union itself and a sea.
const recorded = `<?xml version="1.0" encoding="UTF-8"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
         xmlns:skos="http://www.w3.org/2004/02/skos/core#"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns:at="http://publications.europa.eu/ontology/authority/"
         xmlns:owl="http://www.w3.org/2002/07/owl#"
         xml:base="http://publications.europa.eu/resource/authority/country">
   <skos:Concept rdf:about="http://publications.europa.eu/resource/authority/country/ROU" at:deprecated="false">
      <skos:altLabel xml:lang="ro">Romănia</skos:altLabel>
      <skos:inScheme rdf:resource="http://publications.europa.eu/resource/authority/country"/>
      <skos:inScheme rdf:resource="http://publications.europa.eu/resource/authority/country/0005"/>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_ALPHA_3">ROU</skos:notation>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#IBAN_COU">RO</skos:notation>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_ALPHA_2">RO</skos:notation>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_NUM">642</skos:notation>
      <skos:prefLabel xml:lang="fr">Roumanie</skos:prefLabel>
      <skos:prefLabel xml:lang="ro">România</skos:prefLabel>
      <skos:prefLabel xml:lang="en">Romania</skos:prefLabel>
      <at:prefLabel xml:lang="en">Not this one</at:prefLabel>
      <dc:identifier>ROU</dc:identifier>
      <at:start.use>1947-12-30</at:start.use>
   </skos:Concept>
   <skos:Concept rdf:about="http://publications.europa.eu/resource/authority/country/NLD" at:deprecated="false">
      <skos:inScheme rdf:resource="http://publications.europa.eu/resource/authority/country/0005"/>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_ALPHA_3">NLD</skos:notation>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_ALPHA_2">NL</skos:notation>
      <skos:prefLabel xml:lang="ro">Ţările de Jos</skos:prefLabel>
      <skos:prefLabel xml:lang="en">Netherlands</skos:prefLabel>
      <dc:identifier>NLD</dc:identifier>
      <at:start.use>1950-05-09</at:start.use>
   </skos:Concept>
   <skos:Concept rdf:about="http://publications.europa.eu/resource/authority/country/DDR" at:deprecated="true">
      <owl:deprecated rdf:datatype="http://www.w3.org/2001/XMLSchema#boolean">true</owl:deprecated>
      <skos:inScheme rdf:resource="http://publications.europa.eu/resource/authority/country"/>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_ALPHA_3">DDR</skos:notation>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_ALPHA_2">DD</skos:notation>
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_NUM">278</skos:notation>
      <skos:prefLabel xml:lang="ro">Republica Democrată Germană</skos:prefLabel>
      <skos:prefLabel xml:lang="en">German Democratic Republic</skos:prefLabel>
      <dc:identifier>DDR</dc:identifier>
      <at:start.use>1949-05-23</at:start.use>
      <at:end.use>1990-10-02</at:end.use>
   </skos:Concept>
   <skos:Concept rdf:about="http://publications.europa.eu/resource/authority/country/XKX" at:deprecated="false">
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#IBAN_COU">XK</skos:notation>
      <skos:prefLabel xml:lang="en">Kosovo*</skos:prefLabel>
      <skos:prefLabel xml:lang="ro">Kosovo*</skos:prefLabel>
      <dc:identifier>XKX</dc:identifier>
      <at:start.use>2008-02-17</at:start.use>
   </skos:Concept>
   <skos:Concept rdf:about="http://publications.europa.eu/resource/authority/country/EUR" at:deprecated="false">
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#ISO_3166_1_ALPHA_2">EU</skos:notation>
      <skos:prefLabel xml:lang="en">European Union</skos:prefLabel>
      <skos:prefLabel xml:lang="ro">Uniunea Europeană</skos:prefLabel>
      <dc:identifier>EUR</dc:identifier>
      <at:start.use>1993-11-01</at:start.use>
   </skos:Concept>
   <skos:Concept rdf:about="http://publications.europa.eu/resource/authority/country/XXI" at:deprecated="false">
      <skos:notation rdf:datatype="http://publications.europa.eu/ontology/euvoc#NAC">XXI</skos:notation>
      <skos:prefLabel xml:lang="ro">Irlanda de Nord</skos:prefLabel>
      <skos:prefLabel xml:lang="en">Northern Ireland</skos:prefLabel>
      <dc:identifier>XXI</dc:identifier>
      <at:start.use>2021-01-01</at:start.use>
   </skos:Concept>
   <skos:Concept rdf:about="http://publications.europa.eu/resource/authority/country/XME" at:deprecated="false">
      <skos:prefLabel xml:lang="en">Mediterranean Sea</skos:prefLabel>
      <skos:prefLabel xml:lang="ro">Marea Mediterană</skos:prefLabel>
      <dc:identifier>XME</dc:identifier>
      <at:start.use>1950-05-09</at:start.use>
   </skos:Concept>
</rdf:RDF>`

func parse(t *testing.T, text string) map[string]Country {
	t.Helper()
	all, err := Parse(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]Country{}
	for i, c := range all {
		if i > 0 && key(all[i-1]) >= key(c) {
			t.Fatalf("not sorted by alpha-3, invoice-only entries last: %s before %s", key(all[i-1]), key(c))
		}
		byCode[key(c)] = c
	}
	return byCode
}

// key is the alpha-3 code, or for an entry only for invoices its alpha-2 code after every alpha-3 code.
func key(c Country) string {
	if c.Alpha3 == nil {
		return "~" + c.Alpha2
	}
	return *c.Alpha3
}

func TestACountryHasItsCodesNamesAndUnionMembership(t *testing.T) {
	ro := parse(t, recorded)["ROU"]
	if ro.Alpha2 != "RO" || *ro.Numeric != "642" || ro.NameRO != "România" || ro.NameEN != "Romania" {
		t.Fatalf("Romania is %+v", ro)
	}
	if !ro.EUMember || !ro.ISO || ro.ValidFrom != "1947-12-30" || ro.ValidTo != nil {
		t.Fatalf("Romania is %+v", ro)
	}
}

func TestRomanianNamesAreWrittenWithTheCommaBelowAndAMissingNumericCodeIsNull(t *testing.T) {
	nl := parse(t, recorded)["NLD"]
	if nl.NameRO != "Țările de Jos" || nl.Numeric != nil {
		t.Fatalf("Netherlands is %+v", nl)
	}
}

func TestAWithdrawnCountryStaysWithItsLastDay(t *testing.T) {
	ddr := parse(t, recorded)["DDR"]
	if ddr.ValidTo == nil || *ddr.ValidTo != "1990-10-02" || ddr.EUMember {
		t.Fatalf("East Germany is %+v", ddr)
	}
}

func TestKosovoIsListedWithItsUserAssignedCodesAndNoAsterisk(t *testing.T) {
	xk := parse(t, recorded)["XKX"]
	if xk.Alpha2 != "XK" || xk.ISO || xk.NameRO != "Kosovo" || xk.NameEN != "Kosovo" {
		t.Fatalf("Kosovo is %+v", xk)
	}
}

func TestTheUnionItselfAndEntriesWithNoCodeAreLeftOut(t *testing.T) {
	all := parse(t, recorded)
	if len(all) != 5 {
		t.Fatalf("%d entries, want 5: %v", len(all), all)
	}
}

func TestATableThatCannotBeTrustedIsRefused(t *testing.T) {
	for name, text := range map[string]string{
		"a web page":   "<html><body>Not found</body></html>",
		"no countries": strings.Replace(recorded, "ISO_3166_1_ALPHA_3", "OTHER", -1),
		"no name":      strings.Replace(recorded, `<skos:prefLabel xml:lang="ro">România</skos:prefLabel>`, "", 1),
		"a bad day":    strings.Replace(recorded, "1947-12-30", "long ago", 1),
		"withdrawn with no last day": strings.Replace(recorded,
			`country/ROU" at:deprecated="false"`, `country/ROU" at:deprecated="true"`, 1),
		"twice": strings.Replace(recorded, "</rdf:RDF>", recorded[strings.Index(recorded, "<skos:Concept"):strings.Index(recorded, "</skos:Concept>")+len("</skos:Concept>")]+"</rdf:RDF>", 1),
	} {
		if name == "no countries" {
			// Kosovo has no ISO code and would still be read; take it out as well.
			text = strings.Replace(text, "IBAN_COU", "OTHER", -1)
		}
		if _, err := Parse(strings.NewReader(text)); err == nil {
			t.Errorf("%s was read as the country table", name)
		}
	}
}

func TestTheDatasetIsValidAndManual(t *testing.T) {
	d, err := Dataset(
		func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(recorded)), nil },
		func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(viesList)), nil },
		invoiceCodes,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != 5 || d.Frequency != "manual" {
		t.Fatalf("dataset is %+v", d)
	}
}

// invoiceCodes is the country list of an EN 16931 archive for the extract: the Netherlands is left out to
// show a current country an invoice cannot name.
var invoiceCodes = []string{"RO", "1A", "XI"}

const viesList = `{"countries":[{"countryCode":"RO"},{"countryCode":"NL"},{"countryCode":"EL"},{"countryCode":"DD"},{"countryCode":"FR"},{"countryCode":"XI"}]}`

func TestACountryInTheUnionForVATHasItsPrefixAndTheOthersHaveNone(t *testing.T) {
	all, _ := Parse(strings.NewReader(recorded))
	byCode := map[string]Country{}
	for _, c := range WithVAT(all, []string{"DD", "EL", "FR", "NL", "RO", "XI"}) {
		byCode[key(c)] = c
	}

	if ro := byCode["ROU"]; !ro.EUVAT || ro.VATPrefix == nil || *ro.VATPrefix != "RO" {
		t.Fatalf("Romania is %+v", ro)
	}
	if xk := byCode["XKX"]; xk.EUVAT || xk.VATPrefix != nil {
		t.Fatalf("Kosovo is %+v", xk)
	}
	// A withdrawn country keeps its code but is not in the Union for VAT.
	if ddr := byCode["DDR"]; ddr.EUVAT || ddr.VATPrefix != nil {
		t.Fatalf("East Germany is %+v", ddr)
	}
}

func TestTheDatasetCarriesTheVATMarks(t *testing.T) {
	d, err := Dataset(
		func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(recorded)), nil },
		func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(viesList)), nil },
		invoiceCodes,
	)
	if err != nil {
		t.Fatal(err)
	}
	marked := 0
	for _, r := range d.Files[0].Records {
		if r.(Country).EUVAT {
			marked++
		}
	}
	if marked != 3 {
		t.Fatalf("%d countries marked, want Romania, the Netherlands and Northern Ireland", marked)
	}
}

func TestNorthernIrelandIsThereOnlyForInvoices(t *testing.T) {
	ni := parse(t, recorded)["~XI"]
	if ni.Alpha2 != "XI" || ni.Alpha3 != nil || ni.Numeric != nil || ni.ISO || !ni.InvoiceOnly || ni.NameRO != "Irlanda de Nord" || ni.EUMember {
		t.Fatalf("Northern Ireland is %+v", ni)
	}
}

func TestEachCurrentCountryGetsTheCodeOfTheInvoiceList(t *testing.T) {
	all, err := Parse(strings.NewReader(recorded))
	if err != nil {
		t.Fatal(err)
	}
	with, err := WithInvoiceCodes(all, invoiceCodes)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range with {
		code := "-"
		if c.InvoiceCode != nil {
			code = *c.InvoiceCode
		}
		got[key(c)] = code
	}
	want := map[string]string{"ROU": "RO", "NLD": "-", "DDR": "-", "XKX": "1A", "~XI": "XI"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: invoice code %s, want %s", k, got[k], v)
		}
	}

	for name, codes := range map[string][]string{
		"a code of no country":       {"RO", "1A", "XI", "ZZ"},
		"a withdrawn country":        {"RO", "1A", "XI", "DD"},
		"Kosovo twice":               {"RO", "1A", "XK", "XI"},
		"Northern Ireland not there": {"RO", "1A"},
	} {
		if _, err := WithInvoiceCodes(all, codes); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
