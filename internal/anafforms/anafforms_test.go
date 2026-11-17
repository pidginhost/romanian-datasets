package anafforms

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"
)

// row writes a version the way the pages of ANAF do: the first cell not closed, the links in the cells after
// it, and the addresses over plain http.
func row(title, softA, softJ, annex, schema string) string {
	return `<tr>
		<td style="height: 83px"><b> ` + title + `
	<td align="center"><p><a href="http://static.anaf.ro/x/` + softA + `" target="_blank">soft A</a><br>actualizat &#238;n data de 19.10.2026</td>
	<td><p align="center"><a href="http://static.anaf.ro/x/` + softJ + `" target="_blank">soft J*</a><br>actualizat &#238;n data de 14.09.2026</td>
	<td><a href="http://static.anaf.ro/x/` + annex + `" target="_blank">Anexa</a> valid&#259;ri<br>
			<a href="http://static.anaf.ro/x/` + schema + `" target="_blank">
			Schema</a> XSD <br></td></tr>`
}

func page(rows ...string) string {
	return `<html><table><tr><td><a href="100.html">100</a><a href="112.html">112</a></td></tr><tr><td>Denumire formular</td></tr>` +
		strings.Join(rows, "\n") + `</table></html>`
}

var d300 = page(
	row("300 - Decont de tax&#259; pe valoarea ad&#259;ugat&#259; conform OPANAF nr. 9/2026, utilizat &#238;ncep&#226;nd cu declararea obliga&#355;iilor fiscale aferente lunii iulie 2026 - publicat &#238;n data 11.08.2026", "a3.pdf", "j3.zip", "x3.pdf", "s3.xml"),
	row("300 - Decont de tax&#259; pe valoarea ad&#259;ugat&#259; conform OPANAF nr. 174/2026, utilizat &#238;ncep&#226;nd cu declararea obliga&#355;iilor fiscale aferente lunii ianuarie 2026 - publicat &#238;n data 11.02.2026", "a2.pdf", "j2.zip", "x2.pdf", "s2.xml"),
	row("300 - Decont de tax&#259; pe valoarea ad&#259;ugat&#259; conform OPANAF nr. 2131/02.09.2025, utilizat &#238;ncep&#226;nd cu declararea obliga&#355;iilor fiscale aferente lunii august 2025 - publicat &#238;n data 03.09.2025", "a1.pdf", "j1.zip", "x1.pdf", "s1.xml"),
	row("300 - Decont conform OPANAF 3781/ 22.12.2017, - publicat &#238;n data de 01.02.2018", "a0.pdf", "j0.zip", "x0.pdf", "s0.xsd"),
)

func TestReadPageReadsTheVersionsFromTheHorizon(t *testing.T) {
	versions, err := ReadPage("300", d300)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range versions {
		got = append(got, fmt.Sprintf("%d %s %s %s %s", v.Order, v.PeriodFrom, *v.Published, v.Validator, *v.Schema))
	}
	want := "1 2026-07 2026-08-11 https://static.anaf.ro/x/j3.zip https://static.anaf.ro/x/s3.xml | 2 2026-01 2026-02-11 https://static.anaf.ro/x/j2.zip https://static.anaf.ro/x/s2.xml"
	if strings.Join(got, " | ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, " | "), want)
	}
}

func TestPeriodReadsEveryWayANAFWritesIt(t *testing.T) {
	for title, want := range map[string]string{
		"100 - Declarație conform OPANAF nr. 57/19.01.2026, valabil începand cu 01/2024 - publicat în data de 09.02.2024":       "2024-01",
		"112 - Declarația conform Ordinului comun nr. 605/95/928/2314 din 2026 , valabil începând cu luna de raportare 07/2026": "2026-07",
		"390 - Declarația recapitulativă - conform OPANAF nr. 705/11.03.2020 - incepand cu perioada de raportare 02/2020":       "2020-02",
		"390 - Declarația recapitulativă - incepand cu anul de raportare 2017 - publicat în data de 01.02.2017":                 "2017-01",
		"394 Declaraţie informativă - utilizat începând cu declararea obligaţiilor fiscale aferente lunii august 2025":          "2025-08",
	} {
		if got, err := period(title); err != nil || got != want {
			t.Errorf("%q: %s %v, want %s", title, got, err, want)
		}
	}
	if _, err := period("300 - Decont conform OPANAF 3781/ 22.12.2017"); err == nil {
		t.Error("a title without a period was read")
	}
}

func TestReadPageStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, text := range map[string]string{
		"no period above the horizon": page(row("300 - Decont nou, publicat în data 11.08.2026", "a.pdf", "j.zip", "x.pdf", "s.xml")),
		"another form":                page(row("301 - Decont special valabil începand cu 01/2026", "a.pdf", "j.zip", "x.pdf", "s.xml")),
		"nothing in force":            page(row("300 - Decont valabil începand cu 07/2026", "a.pdf", "j.zip", "x.pdf", "s.xml")),
		"no versions":                 page(),
	} {
		if _, err := ReadPage("300", text); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

type site map[string]string

func (s site) get(address string) (io.ReadCloser, error) {
	text, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader([]byte(text))), nil
}

func recordedSite() site {
	s := site{ToolPage: `<a href="https://static.anaf.ro/static/DUKIntegrator/dist.zip">DUKIntegrator</a>`, "https://static.anaf.ro/static/DUKIntegrator/dist.zip": "duk"}
	for _, f := range Forms {
		s[Folder+f+".html"] = strings.ReplaceAll(d300, "300 - ", f+" - ")
	}
	for _, name := range []string{"j3.zip", "j2.zip", "s3.xml", "s2.xml"} {
		s["https://static.anaf.ro/x/"+name] = "file " + name
	}
	return s
}

func TestDatasetHashesTheValidatorsAndSchemas(t *testing.T) {
	d, err := Dataset(recordedSite().get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != 2*len(Forms) || len(d.Files[1].Records) != 1 {
		t.Fatalf("%d versions, %d tools", len(d.Files[0].Records), len(d.Files[1].Records))
	}
	sum := sha256.Sum256([]byte("file j3.zip"))
	if v := d.Files[0].Records[0].(Version); v.ValidatorSH != hex.EncodeToString(sum[:]) || v.ValidatorSz != int64(len("file j3.zip")) {
		t.Fatalf("%+v", v)
	}
	_, err = Dataset(recordedSite().get, []string{"100|https://static.anaf.ro/x/j9.zip"})
	if err == nil || !strings.Contains(err.Error(), "j9.zip") {
		t.Fatalf("a version gone from the page was taken: %v", err)
	}
}
