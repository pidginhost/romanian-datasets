package siruta

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
)

func TestReadDBFSkipsDeletedRowsAndTrimsValues(t *testing.T) {
	table := dbfTable([]string{"JUD", "MNEMONIC"}, []int{2, 2}, [][]string{{"1", "AB"}, {"2", "AR"}, {"40", "B"}}, 1)
	rows, err := readDBF(table)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["JUD"] != "1" || rows[1]["JUD"] != "40" || rows[1]["MNEMONIC"] != "B" {
		t.Fatalf("rows %v", rows)
	}
}

func TestReadDBFRefusesWhatIsNotAnINSTable(t *testing.T) {
	good := dbfTable([]string{"DENLOC"}, []int{10}, [][]string{{"ALBA"}})
	notASCII := dbfTable([]string{"DENLOC"}, []int{10}, [][]string{{"BĂRĂBANȚ"}})
	cut := good[:len(good)-6]
	for name, b := range map[string][]byte{"not ASCII": notASCII, "cut short": cut, "empty": nil} {
		if _, err := readDBF(b); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestWithLettersPutsTheLettersOnTheINSName(t *testing.T) {
	for _, c := range []struct {
		ins, marked, want string
		same              bool
	}{
		{"BARABANT", "BĂRĂBANŢ", "BĂRĂBANȚ", true},
		{"JUDETUL ARGES", "JUDEȚUL ARGEŞ", "JUDEȚUL ARGEȘ", true},
		{"VALEA MARE-PRAVAT", "VALEA MARE PRAVĂŢ", "VALEA MARE-PRAVĂȚ", true},
		{"ALEXANDRU I. CUZA", "ALEXANDRU I.CUZA", "ALEXANDRU I. CUZA", true},
		{"MUNICIPIUL MARGHITA", "MUNICIPIUL  MARGHITA", "MUNICIPIUL MARGHITA", true},
		{"RAMETEA", "RIMETEA", "RAMETEA", false},
		{"ROSIA DE SECAS", "ROŞA DE SECAŞ", "ROSIA DE SECAS", false},
		{"SOROGARI", "ŞOROGANI", "SOROGARI", false},
		{"ALBA", "ALBÉ", "ALBA", false},
	} {
		got, same := withLetters(c.ins, c.marked)
		if got != c.want || same != c.same {
			t.Errorf("%s with %s = %s %v, want %s %v", c.ins, c.marked, got, same, c.want, c.same)
		}
	}
}

func TestMarkedNamesReadsBothShapesOfCSVAndRefusesSpreadsheets(t *testing.T) {
	names, err := markedNames(strings.NewReader(markedCSV))
	if err != nil {
		t.Fatal(err)
	}
	if names[1035] != "BĂRĂBANŢ" || len(names) != 9 {
		t.Fatalf("names %v", names)
	}

	older := "siruta,niv,sirsup,tip,denloc\r\n10,1,1,40,\"JUDEŢUL ALBA\"\r\n"
	if names, err := markedNames(strings.NewReader(older)); err != nil || names[10] != "JUDEŢUL ALBA" {
		t.Fatalf("comma CSV: %v %v", names, err)
	}

	for name, text := range map[string]string{
		"spreadsheet":    "PK\x03\x04\x14\x00\x06\x00\x08\x00\x00\x00!\x00\xa17n\xcf",
		"no code column": "DENLOC\nALBA\n",
		"decimal codes":  "\"SIRUTA\";\"DENLOC\"\n10,00;\"JUDEUL ALBA\"\n",
	} {
		if _, err := markedNames(strings.NewReader(text)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestCSVAddressesPutsTheNewestDatasetFirst(t *testing.T) {
	answer := catalogueAnswer(
		pkg("siruta-2024", "2025-01-14T00:00:00", "CSV", "https://data.gov.ro/2024.csv"),
		pkg("siruta_s1-2026", "2026-07-24T00:00:00", "DOC", "https://data.gov.ro/m.doc", "csv", "https://data.gov.ro/2026.csv"),
	)
	got, err := csvAddresses(bytes.NewReader(answer))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "https://data.gov.ro/2026.csv https://data.gov.ro/2024.csv" {
		t.Fatalf("addresses %v", got)
	}
	if _, err := csvAddresses(strings.NewReader(`{"success": false}`)); err == nil {
		t.Fatal("an answer without success was read")
	}
}

func TestNewestTakesTheLatestEdition(t *testing.T) {
	e, err := Newest(strings.NewReader(recordedPage))
	if err != nil {
		t.Fatal(err)
	}
	if e.Label() != "S1 2026" || e.Address != "https://insse.ro/cms/files/siruta/2026/SIRUTA_S1_2026.rar" {
		t.Fatalf("edition %+v", e)
	}

	// The annual edition is the state at the end of the year, after the first half.
	page := `<a href="/cms/files/siruta/2026/SIRUTA_S1_2026.rar">S1</a><a href="/cms/files/siruta/2026/SIRUTA_AN_2026.rar">AN</a>`
	if e, err := Newest(strings.NewReader(page)); err != nil || e.Label() != "AN 2026" || e.Address != "https://insse.ro/cms/files/siruta/2026/SIRUTA_AN_2026.rar" {
		t.Fatalf("edition %+v %v", e, err)
	}
	if _, err := Newest(strings.NewReader("<html>no archive</html>")); err == nil {
		t.Fatal("a page with no archive gave an edition")
	}
}

func TestUnpackGivesTheNamedFilesOfTheArchive(t *testing.T) {
	archive := rar5(map[string][]byte{"SIRUTA/siruta.dbf": []byte("units"), "siruta_t.DBF": []byte("other"), "JUD.DBF": []byte("counties")},
		"siruta_t.DBF", "SIRUTA/siruta.dbf", "JUD.DBF")
	files, err := unpack(bytes.NewReader(archive), "siruta.dbf", "jud.dbf")
	if err != nil {
		t.Fatal(err)
	}
	if string(files["siruta.dbf"]) != "units" || string(files["jud.dbf"]) != "counties" {
		t.Fatalf("files %q", files)
	}
	if _, err := unpack(bytes.NewReader(archive), "ZONE.DBF"); err == nil {
		t.Fatal("a missing file was not seen")
	}
	if _, err := unpack(strings.NewReader("PK not a rar"), "siruta.dbf"); err == nil {
		t.Fatal("a file that is not RAR was read")
	}
}

func TestReadBuildsTheRegister(t *testing.T) {
	units, counties, regions := extract()
	marked, err := markedNames(strings.NewReader(markedCSV))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := Read(units, counties, regions, marked)
	if err != nil {
		t.Fatal(err)
	}

	byCode := map[int]Unit{}
	for _, u := range reg.Units {
		byCode[u.Code] = u
	}
	alba, iulia, sector := byCode[10], byCode[1026], byCode[179141]
	switch {
	case alba.Name != "JUDEȚUL ALBA" || alba.Parent != nil || alba.Urban != nil || alba.PostalCode != nil:
		t.Errorf("Alba %+v", alba)
	case iulia.Parent == nil || *iulia.Parent != 1017 || !*iulia.Urban || *iulia.PostalCode != "510005" || iulia.LAU != nil:
		t.Errorf("Alba Iulia %+v", iulia)
	case *byCode[1017].LAU != "RO_1017":
		t.Errorf("municipality %+v", byCode[1017])
	case sector.Name != "BUCUREȘTI SECTORUL 1" || *sector.PostalCode != "010013" || sector.Level != 3:
		t.Errorf("sector %+v", sector)
	case byCode[13524].Name != "VALEA MARE-PRAVĂȚ" || byCode[6592].Name != "RAMETEA" || *byCode[6592].Urban:
		t.Errorf("names %s %s", byCode[13524].Name, byCode[6592].Name)
	}
	if len(reg.Unmarked) != 1 || reg.Unmarked[0] != 6592 {
		t.Errorf("unmarked %v", reg.Unmarked)
	}
	if len(reg.Counties) != 2 || reg.Counties[1] != (County{Code: 40, Mnemonic: "B", Name: "MUNICIPIUL BUCUREȘTI", Unit: 403, Region: 8}) {
		t.Errorf("counties %+v", reg.Counties)
	}
	if reg.Regions[1] != (Region{Code: 8, Unit: 886, Name: "BUCURESTI - ILFOV"}) {
		t.Errorf("regions %+v", reg.Regions)
	}
}

func TestReadRefusesARegisterThatDoesNotHoldTogether(t *testing.T) {
	_, counties, regions := extract()
	county := unit(10, 1, 1, 40, "JUDETUL ALBA", "0", 1, 7, 0, "RO121", "")
	for name, rows := range map[string][][]string{
		"unknown parent":  {county, unit(1026, 3, 1017, 9, "ALBA IULIA", "1", 1, 7, 510005, "RO121", "")},
		"unknown type":    {county, unit(1017, 2, 10, 7, "MUNICIPIUL ALBA IULIA", "1", 1, 7, 0, "RO121", "")},
		"unknown county":  {county, unit(1017, 2, 10, 1, "MUNICIPIUL ALBA IULIA", "1", 2, 7, 0, "RO121", "")},
		"twice":           {county, county},
		"no environment":  {county, unit(1017, 2, 10, 1, "MUNICIPIUL ALBA IULIA", "0", 1, 7, 0, "RO121", "")},
		"county no unit":  {unit(1017, 2, 10, 1, "MUNICIPIUL ALBA IULIA", "1", 1, 7, 0, "RO121", "")},
		"bad NUTS 3 code": {unit(10, 1, 1, 40, "JUDETUL ALBA", "0", 1, 7, 0, "121", "")},
	} {
		units := dbfTable(unitFields, unitWidths, rows)
		if _, err := Read(units, counties, regions, nil); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestDatasetReadsTheRecordedSites(t *testing.T) {
	d, err := Dataset(recordedSite().get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.Frequency != dataset.Monthly || !strings.Contains(d.Description, "edition S1 2026") || !strings.Contains(d.Description, "1 names keep") {
		t.Fatalf("dataset %s %s", d.Frequency, d.Description)
	}
	lines, err := dataset.Lines(d.Files[0].Records)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := bytes.Cut(lines, []byte("\n"))
	want := `{"code":10,"name":"JUDEȚUL ALBA","parent":null,"type":40,"level":1,"urban":null,"county":1,"region":7,"postal_code":null,"nuts3":"RO121","lau":null}`
	if string(first) != want {
		t.Fatalf("first unit\n%s\nwant\n%s", first, want)
	}

	codes, err := PublishedCodes(bytes.NewReader(lines))
	if err != nil || len(codes) != 9 {
		t.Fatalf("published codes %v %v", codes, err)
	}
}

func TestDatasetStopsWhenAPublishedCodeIsGone(t *testing.T) {
	_, err := Dataset(recordedSite().get, []int{10, 1017, 99999})
	if err == nil || !strings.Contains(err.Error(), "99999") {
		t.Fatalf("error %v", err)
	}
}

func TestDatasetTriesAnOlderCSVWhenTheNewestCannotBeRead(t *testing.T) {
	s := recordedSite()
	s[Catalogue] = catalogueAnswer(
		pkg("siruta-new", "2026-12-01T00:00:00", "CSV", "https://data.gov.ro/broken.csv"),
		pkg("siruta_s1-2026", "2026-07-24T00:00:00", "CSV", "https://data.gov.ro/siruta_s1_2026.csv"),
	)
	s["https://data.gov.ro/broken.csv"] = []byte("PK\x03\x04")
	d, err := Dataset(s.get, nil)
	if err != nil {
		t.Fatal(err)
	}
	var first Unit
	line, _ := json.Marshal(d.Files[0].Records[0])
	if err := json.Unmarshal(line, &first); err != nil || first.Name != "JUDEȚUL ALBA" {
		t.Fatalf("first %+v %v", first, err)
	}
}
