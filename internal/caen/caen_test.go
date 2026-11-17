package caen

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// An extract of N_CAEN.CSV in its shape: a byte order mark, '^' between fields, padded codes, every version.
// Version 2 writes a division and a group with their own code again as class; version 1 has a subsection
// and is not read.
const recordedCAEN = "\xef\xbb\xbfSECTIUNEA^SUBSECTIUNEA^DIVIZIUNEA^GRUPA^CLASA^DENUMIRE^VERSIUNE_CAEN\r\n" +
	"A^^01^011^0111^Cultura cerealelor^0\r\n" +
	"D^DA^15^151^1511^Producția, prelucrarea și conservarea cărnii^1\r\n" +
	"J^  ^  ^   ^    ^Informații și  comunicații^2\r\n" +
	"J^  ^62^   ^62  ^Activități de servicii în tehnologia informației^2\r\n" +
	"J^  ^62^620^620 ^Activități de servicii în tehnologia informației^2\r\n" +
	"J^  ^62^620^6201^Activități de realizare a soft-ului la comandă (software orientat client)^2\r\n" +
	"J^  ^62^620^6202^Activități de consultanță în tehnologia informației^2\r\n" +
	"K^  ^62^621^6210^Activități de realizare a soft-ului la comandă (software orientat client)^3\r\n" +
	"K^  ^  ^   ^    ^Telecomunicații; activități de programare^3\r\n" +
	"K^  ^62^   ^    ^Activități de programare, consultanță în tehnologia informației^3\r\n" +
	"K^  ^62^621^    ^Activități de programare a calculatoarelor^3\r\n" +
	"K^  ^62^622^    ^Activități de consultanță în tehnologia informației^3\r\n" +
	"K^  ^62^622^6220^Activități de consultanță în tehnologia informației^3\r\n"

const recordedVersions = "\xef\xbb\xbfCOD^DESCRIERE\r\n0^Versiunea 1998\r\n1^Versiunea 2003\r\n2^Versiunea 2008\r\n3^Versiunea 2025\r\n"

func binding(rev21, rev2 string) string {
	return fmt.Sprintf(`{"rev21": {"type": "literal", "value": %q}, "rev2": {"type": "literal", "value": %q}}`, rev21, rev2)
}

// A SPARQL answer with a division link, which is left out, and the class links of the extract.
var recordedAnswer = `{"head": {"vars": ["rev21", "rev2"]}, "results": {"bindings": [` + strings.Join([]string{
	binding("62", "62"), binding("62.10", "62.01"), binding("62.20", "62.02"),
}, ",") + `]}}`

func catalogueAnswer(packages ...string) string {
	return `{"success": true, "result": {"results": [` + strings.Join(packages, ",") + `]}}`
}

func pkg(name, created string, resources ...string) string {
	var rs []string
	for i := 0; i+1 < len(resources); i += 2 {
		rs = append(rs, fmt.Sprintf(`{"name": %q, "url": %q}`, resources[i], resources[i+1]))
	}
	return fmt.Sprintf(`{"name": %q, "metadata_created": %q, "resources": [%s]}`, name, created, strings.Join(rs, ","))
}

type site map[string]string

func (s site) get(address string) (io.ReadCloser, error) {
	text, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

func recordedSite() site {
	return site{
		Catalogue: catalogueAnswer(
			pkg("firme-04-11-2026", "2026-11-06T05:13:41", "OD_FIRME.CSV", "https://data.gov.ro/od_firme.csv"),
			pkg("nomenclatoare-19-10-2026", "2026-10-20T09:00:00", "N_CAEN.CSV", "https://data.gov.ro/old/n_caen.csv", "N_VERSIUNE_CAEN.CSV", "https://data.gov.ro/old/n_versiune_caen.csv"),
			// A newer dataset whose versions file has no name: it is found by its address.
			pkg("nomenclatoare-04-11-2026", "2026-11-06T05:11:04", "N_CAEN.CSV", "https://data.gov.ro/new/n_caen.csv", "", "https://data.gov.ro/new/n_versiune_caen.csv"),
		),
		"https://data.gov.ro/new/n_caen.csv":          recordedCAEN,
		"https://data.gov.ro/new/n_versiune_caen.csv": recordedVersions,
		CorrespondenceAddress():                       recordedAnswer,
	}
}

func TestNewestTakesTheNewestNomenclatureWithBothFiles(t *testing.T) {
	files, err := Newest(strings.NewReader(recordedSite()[Catalogue]))
	if err != nil {
		t.Fatal(err)
	}
	if files.CAEN != "https://data.gov.ro/new/n_caen.csv" || files.Versions != "https://data.gov.ro/new/n_versiune_caen.csv" {
		t.Fatalf("files %+v", files)
	}

	partial := catalogueAnswer(
		pkg("nomenclatoare-04-11-2026", "2026-11-06T00:00:00", "N_CAEN.CSV", "https://data.gov.ro/new/n_caen.csv"),
		pkg("nomenclatoare-19-10-2026", "2026-10-20T00:00:00", "N_CAEN.CSV", "https://data.gov.ro/old/n_caen.csv", "N_VERSIUNE_CAEN.CSV", "https://data.gov.ro/old/v.csv"),
	)
	if files, err := Newest(strings.NewReader(partial)); err != nil || files.CAEN != "https://data.gov.ro/old/n_caen.csv" {
		t.Fatalf("an incomplete dataset was taken: %+v %v", files, err)
	}
	if _, err := Newest(strings.NewReader(catalogueAnswer(pkg("firme", "2026-11-06T00:00:00")))); err == nil {
		t.Fatal("a catalogue with no nomenclature gave files")
	}
}

func TestReadKeepsRev2AndRev3AsATree(t *testing.T) {
	all, err := Read(strings.NewReader(recordedCAEN))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range all {
		parent := "-"
		if a.Parent != nil {
			parent = *a.Parent
		}
		got = append(got, fmt.Sprintf("%d %s %s<%s %s", a.Version, a.Level, a.Code, parent, a.Name))
	}
	want := []string{
		"2 section J<- Informații și comunicații",
		"2 division 62<J Activități de servicii în tehnologia informației",
		"2 group 620<62 Activități de servicii în tehnologia informației",
		"2 class 6201<620 Activități de realizare a soft-ului la comandă (software orientat client)",
		"2 class 6202<620 Activități de consultanță în tehnologia informației",
		"3 section K<- Telecomunicații; activități de programare",
		"3 division 62<K Activități de programare, consultanță în tehnologia informației",
		"3 group 621<62 Activități de programare a calculatoarelor",
		"3 class 6210<621 Activități de realizare a soft-ului la comandă (software orientat client)",
		"3 group 622<62 Activități de consultanță în tehnologia informației",
		"3 class 6220<622 Activități de consultanță în tehnologia informației",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReadRefusesAVersionThatDoesNotHoldTogether(t *testing.T) {
	head := "SECTIUNEA^SUBSECTIUNEA^DIVIZIUNEA^GRUPA^CLASA^DENUMIRE^VERSIUNE_CAEN\n"
	rev3 := "K^^^^^Telecomunicații^3\nK^^62^^^Programare^3\nK^^62^621^^Programare^3\nK^^62^621^6210^Programare^3\n"
	for name, rows := range map[string]string{
		"no group":         "J^^^^^Informații^2\nJ^^62^^^IT^2\nJ^^62^620^6201^Soft^2\n",
		"other section":    "J^^^^^Informații^2\nK^^^^^Financiar^2\nJ^^62^^^IT^2\nK^^62^620^^IT^2\nK^^62^620^6201^Soft^2\n",
		"twice":            "J^^^^^Informații^2\nJ^^^^^Informații^2\n",
		"no level":         "J^^62^621^6201^Soft^2\n",
		"a subsection":     "J^JA^^^^Informații^2\n",
		"no name":          "J^^^^^ ^2\n",
		"version 3 absent": "",
	} {
		text := head + rows
		if name != "version 3 absent" {
			text += rev3
		}
		if _, err := Read(strings.NewReader(text)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestReadCorrespondenceKeepsClassesAndNamesTheKind(t *testing.T) {
	answer := `{"results": {"bindings": [` + strings.Join([]string{
		binding("01", "01"),
		binding("01.11", "01.11"),
		binding("46.18", "45.11"), binding("47.81", "45.11"), binding("46.18", "46.18"),
		binding("62.10", "62.01"), binding("62.10", "62.01"),
		binding("47.92", "45.19"), binding("47.92", "47.91"),
	}, ",") + `]}}`
	links, err := ReadCorrespondence(strings.NewReader(answer))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range links {
		got = append(got, l.Rev2+">"+l.Rev3+" "+l.Kind)
	}
	want := "0111>0111 1:1 | 4511>4618 n:m | 4511>4781 1:n | 4519>4792 n:1 | 4618>4618 n:1 | 4791>4792 n:1 | 6201>6210 1:1"
	if strings.Join(got, " | ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, " | "), want)
	}

	for name, text := range map[string]string{
		"class to group": `{"results": {"bindings": [` + binding("62.1", "62.01") + `]}}`,
		"no classes":     `{"results": {"bindings": [` + binding("62", "62") + `]}}`,
		"not JSON":       `<html>`,
	} {
		if _, err := ReadCorrespondence(strings.NewReader(text)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestCheckLinksWantsEveryRev2ClassAndKnownCodes(t *testing.T) {
	all, err := Read(strings.NewReader(recordedCAEN))
	if err != nil {
		t.Fatal(err)
	}
	links := []Link{{"6201", "6210", "1:1"}, {"6202", "6220", "1:1"}}
	if fresh, err := CheckLinks(links, all); err != nil || len(fresh) != 0 {
		t.Fatalf("%v %v", fresh, err)
	}
	if fresh, err := CheckLinks(links[:1], all); err == nil {
		t.Fatalf("a Rev.2 class without a link was let through: %v", fresh)
	}
	if _, err := CheckLinks(append(links, Link{"6203", "6210", "n:1"}), all); err == nil {
		t.Fatal("an unknown Rev.2 class was let through")
	}
	if fresh, err := CheckLinks([]Link{{"6201", "6210", "n:1"}, {"6202", "6210", "n:1"}}, all); err != nil || strings.Join(fresh, ",") != "6220" {
		t.Fatalf("new in Rev.3: %v %v", fresh, err)
	}
}

func TestDatasetReadsTheRecordedSites(t *testing.T) {
	d, err := Dataset(recordedSite().get)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 3 || len(d.Files[0].Records) != 11 || len(d.Files[1].Records) != 2 || len(d.Files[2].Records) != 2 {
		t.Fatalf("files %d", len(d.Files))
	}
	if d.Files[1].Records[1] != (Version{Code: 3, Name: "Versiunea 2025"}) {
		t.Fatalf("versions %v", d.Files[1].Records)
	}
	if d.Files[2].Records[0] != (Link{Rev2: "6201", Rev3: "6210", Kind: "1:1"}) {
		t.Fatalf("links %v", d.Files[2].Records)
	}
	if strings.Contains(d.Source.Name, "04-11-2026") {
		t.Fatalf("the source names the day of the ONRC dataset, so every new ONRC dataset would change it: %s", d.Source.Name)
	}
}

func TestDatasetStopsWhenAVersionIsMissing(t *testing.T) {
	s := recordedSite()
	s["https://data.gov.ro/new/n_versiune_caen.csv"] = "COD^DESCRIERE\n2^Versiunea 2008\n"
	if _, err := Dataset(s.get); err == nil || !bytes.Contains([]byte(err.Error()), []byte("version 3")) {
		t.Fatalf("error %v", err)
	}
}
