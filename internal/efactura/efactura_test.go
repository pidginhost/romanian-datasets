package efactura

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

const sch = `<?xml version="1.0" encoding="UTF-8"?>`

// contains writes a check in the shape the package uses for a code list.
func contains(codes string) string {
	return fmt.Sprintf(`((not(contains(normalize-space(.), ' ')) and contains(' %s ', concat(' ', normalize-space(.), ' '))))`, codes)
}

// codeChecks is the code list file with every check of CodeRules: BR-CL-01 with its two lists, BR-CL-10
// with SEPA for the seller or payee, BR-CL-24 with its comparisons, the others with one list.
func codeChecks(extra string) string {
	var b strings.Builder
	b.WriteString(sch + `<pattern xmlns="http://purl.oclc.org/dsdl/schematron" id="Codesmodel">`)
	for _, r := range CodeRules {
		test := contains("A B")
		switch r.ID {
		case "BR-CL-01":
			test = `(self::cbc:InvoiceTypeCode and ` + contains("380 389 751") + `) or (self::cbc:CreditNoteTypeCode and ` + contains("381 81") + `)`
		case "BR-CL-04":
			test = contains("BGN HRK RON BGN")
		case "BR-CL-10":
			test = contains("0088 0190") + `  or (` + contains("SEPA") + ` and ((ancestor::cac:AccountingSupplierParty) or (ancestor::cac:PayeeParty)))`
		case "BR-CL-24":
			test = `((@mimeCode = 'application/pdf' or @mimeCode = 'image/png'))`
		}
		fmt.Fprintf(&b, `<rule context="x"><assert test=%q flag="fatal" id=%q>[%s]-text</assert></rule>`, test, r.ID, r.ID)
	}
	b.WriteString(extra + `</pattern>`)
	return b.String()
}

const roChecks = sch + `<pattern xmlns="http://purl.oclc.org/dsdl/schematron" id="ROmodel">
	<let name="ISO-3166-RO-CODES" value="('RO-AB','RO-B', 'RO-VS')"/>
	<let name="SECTOR-RO-CODES" value="('SECTOR1', 'SECTOR6')"/>
	<rule context="cbc:InvoiceTypeCode"><assert test="(. and ((not(contains(normalize-space(.), ' ')) and contains(' 380 384 389 751 ', concat(' ', normalize-space(.), ' ')))))" flag="fatal" id="BR-RO-020_1">text</assert></rule>
	<rule context="cbc:CreditNoteTypeCode"><assert test="(. and ((not(contains(normalize-space(.), ' ')) and contains(' 381 ', concat(' ', normalize-space(.), ' ')))))" flag="fatal" id="BR-RO-020_2">text</assert></rule>
	<rule context="cac:InvoicePeriod"><assert test="((not(contains(normalize-space(.), ' ')) and contains(' 3 35 432 ', concat(' ', normalize-space(.), ' '))))" flag="fatal" id="BR-RO-040">text</assert></rule>
	<rule context="/ubl:Invoice"><report test="not(normalize-space(cbc:CountrySubentity) = $ISO-3166-RO-CODES)" flag="fatal" id="BR-RO-110">text</report></rule>
</pattern>`

func pkgZip(codes, ro string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, text := range map[string]string{
		"ro16931-ubl-1.0.9/codelist/EN16931-UBL-codes.sch": codes,
		"ro16931-ubl-1.0.9/cius-ro/RO16931-rules.sch":      ro,
		"ro16931-ubl-1.0.9/UBL/EN16931-UBL-model.sch":      sch + `<x/>`,
	} {
		w, _ := z.Create(name)
		io.WriteString(w, text)
	}
	z.Close()
	return b.Bytes()
}

const recordedPage = `<a href="/static/10/eFactura/ro16931-ubl-1.0.8.zip">1.0.8</a>
<a href="/static/10/eFactura/ro16931-ubl-1.0.10.zip">1.0.10</a>
<a href="/static/10/eFactura/ro16931-ubl-1.0.9.zip">1.0.9</a>`

type site map[string][]byte

func (s site) get(address string) (io.ReadCloser, error) {
	data, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestNewestComparesVersionsAsNumbers(t *testing.T) {
	p, err := Newest(strings.NewReader(recordedPage))
	if err != nil {
		t.Fatal(err)
	}
	if p.Label() != "1.0.10" || p.Address != "https://mfinante.gov.ro/static/10/eFactura/ro16931-ubl-1.0.10.zip" {
		t.Fatalf("package %+v", p)
	}
	if _, err := Newest(strings.NewReader("<html></html>")); err == nil {
		t.Fatal("a page with no package gave one")
	}
}

func TestReadGivesTheCodesOfEachCheckAndTheSubdivisions(t *testing.T) {
	all, subs, err := Read([]byte(codeChecks("")), []byte(roChecks))
	if err != nil {
		t.Fatal(err)
	}
	pick := func(rule string) string {
		var out []string
		for _, a := range all {
			if a.Rule != rule {
				continue
			}
			s := a.Code
			if a.Part != nil {
				s = *a.Part + ":" + s
			}
			out = append(out, s)
		}
		return strings.Join(out, " ")
	}
	for rule, want := range map[string]string{
		"BR-CL-01":    "credit note:381 credit note:81 invoice:380 invoice:389 invoice:751",
		"BR-CL-04":    "BGN HRK RON",
		"BR-CL-10":    "any party:0088 any party:0190 seller or payee:SEPA",
		"BR-CL-24":    "application/pdf image/png",
		"BR-RO-020_1": "380 384 389 751",
		"BR-RO-020_2": "381",
		"BR-RO-040":   "3 35 432",
	} {
		if got := pick(rule); got != want {
			t.Errorf("%s: %q, want %q", rule, got, want)
		}
	}
	for _, a := range all {
		if a.Rule == "BR-CL-06" && a.List != nil || a.Rule == "BR-CL-23" && (a.List == nil || *a.List != "en16931-codes/units.jsonl") || a.Rule == "BR-CL-14" && (a.List == nil || *a.List != "countries/countries.jsonl") {
			t.Errorf("list of %s: %v", a.Rule, a.List)
		}
	}

	var got []string
	for _, s := range subs {
		switch {
		case s.Mnemonic != nil:
			got = append(got, s.Code+"="+*s.Mnemonic)
		case s.Sector != nil:
			got = append(got, fmt.Sprintf("%s=%d", s.Code, *s.Sector))
		}
	}
	if strings.Join(got, " ") != "RO-AB=AB RO-B=B RO-VS=VS SECTOR1=1 SECTOR6=6" {
		t.Fatalf("subdivisions %v", got)
	}
}

func TestReadStopsOnAPackageItDoesNotKnow(t *testing.T) {
	unknown := `<rule context="y"><assert test="` + strings.ReplaceAll(contains("X"), `'`, `&apos;`) + `" id="BR-CL-27">t</assert></rule>`
	missing := strings.Replace(codeChecks(""), `id="BR-CL-23"`, `id="BR-CL-99-gone"`, 1)
	oneList := strings.Replace(codeChecks(""), `or (self::cbc:CreditNoteTypeCode`, `or (self::cbc:Other`, 1)
	oneList = strings.Replace(oneList, contains("381 81"), `true()`, 1)
	badCounty := strings.Replace(roChecks, `'RO-VS'`, `'Vaslui'`, 1)
	for name, files := range map[string][2]string{
		"unknown code check":  {codeChecks(unknown), roChecks},
		"missing code check":  {missing, roChecks},
		"one list of two":     {oneList, roChecks},
		"a county not a code": {codeChecks(""), badCounty},
		"no sectors":          {codeChecks(""), strings.Replace(roChecks, `name="SECTOR-RO-CODES"`, `name="OTHER"`, 1)},
		"no CIUS-RO rule":     {codeChecks(""), strings.Replace(roChecks, `id="BR-RO-040"`, `id="BR-RO-041"`, 1)},
		"not XML":             {"PK", roChecks},
	} {
		if _, _, err := Read([]byte(files[0]), []byte(files[1])); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestDatasetReadsTheRecordedSite(t *testing.T) {
	s := site{
		Page: []byte(recordedPage),
		"https://mfinante.gov.ro/static/10/eFactura/ro16931-ubl-1.0.10.zip": pkgZip(codeChecks(""), roChecks),
	}
	d, err := Dataset(s.get)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Description, "validation package 1.0.10") || len(d.Files) != 2 || len(d.Files[1].Records) != 5 {
		t.Fatalf("dataset %s, %d files", d.Description, len(d.Files))
	}

	delete(s, "https://mfinante.gov.ro/static/10/eFactura/ro16931-ubl-1.0.10.zip")
	if _, err := Dataset(s.get); err == nil {
		t.Fatal("a missing package was not seen")
	}
}

func TestUnpackWantsBothFiles(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("ro16931-ubl-1.0.9/codelist/EN16931-UBL-codes.sch")
	io.WriteString(w, codeChecks(""))
	z.Close()
	if _, err := unpack(b.Bytes(), codeFile, roFile); err == nil {
		t.Fatal("a package without the CIUS-RO rules was read")
	}
}
