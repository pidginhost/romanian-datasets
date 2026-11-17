package taxoffices

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// The page in the shape ANAF writes it: directorates as accordion buttons, a chapter for each county, a
// table of units with their contact and the localities they serve.
const directorate = `<button class="accordion"%s><h3 style="margin-top: 0; padding-top: 0;">Direcția Generală Regională a Finanțelor Publice %s</h3>%s<br>Asistență contribuabili: Tel. 031.403.91.60; <a href="https://www.anaf.ro/asistpublic/" target="_blank">Formular de contact</a></button>
<div class="panel" style="margin-top: 10px;">
%s
</div>
`

const chapter = `<button class="accordion1"><p class="stilCapitol">%s</p></button>
<div class="panel">
	<table class="tabel">
		<tr>
			<th style="width:40%%">Unitatea fiscală</th>
    		<th style="width:60%%">Localități arondate</th>
		</tr>
%s
	</table>
</div>
`

const unitRow = `
		<tr>
			<td><strong>%s</strong><br>%s<hr>Asistență în domeniul fiscal:<br>
			- prin e-mail: <a href="https://www.anaf.ro/asistpublic/" target="_blank">Formular de contact</a></td>
    		<td style="width:60%%">%s</td>
		</tr>
`

func page() string {
	alba := fmt.Sprintf(chapter, "Administrația Județeană a Finanțelor Publice ALBA",
		fmt.Sprintf(unitRow, "Administrația Județeană a Finanțelor Publice ALBA", "Alba Iulia, Str. Primăverii nr. 10, C.P. 510110<br>Tel. 0258.813.002", "Alba Iulia, Ciugud")+
			fmt.Sprintf(unitRow, "Unitatea Fiscală Municipală Aiud", "Str. Simion Bărnuțiu nr. 8, C.P. 51520<br>Tel. 0258.861.324", "Aiud, Rimetea, Vînători"))
	bucharest := fmt.Sprintf(chapter, "Direcția Generală Regională a Finanțelor Publice BUCUREȘTI",
		fmt.Sprintf(unitRow, "Administrația Fiscală pentru Contribuabili Nerezidenți", "Piața Presei Libere, nr.1, sector 1", "")+
			fmt.Sprintf(unitRow, "Administrația Sectorului 1 a Finanțelor Publice", "Strada Caransebeș nr. 1, sector 1, București", "Sectorul 1"))
	dolj := fmt.Sprintf(chapter, "Administrația Județeană a Finanțelor Publice DOLJ",
		fmt.Sprintf(unitRow, "Administrația Județeană a Finanțelor Publice DOLJ", "Craiova, Str. Mihail Kogălniceanu nr. 3", "Mischii Mârșani, Sãtmãrel"))
	return "<html><div>search</div>\n" +
		fmt.Sprintf(directorate, "", "Brașov", "Str. Mihail Kogălniceanu nr. 7, Brașov, Tel. 0268.308.421", alba) +
		fmt.Sprintf(directorate, ` style="margin-top: 10px;"`, "București", "Str. Dimitrie Gerota nr. 13, sector 2", bucharest) +
		fmt.Sprintf(directorate, ` style="margin-top: 10px;"`, "Craiova", "Str. Mihail Kogălniceanu nr. 3, Craiova", dolj) + "</html>"
}

func code(c int) *int { return &c }

var (
	places = []Place{
		{Code: 1017, Name: "MUNICIPIUL ALBA IULIA", Parent: code(10), Type: 1, County: 1, Level: 2},
		{Code: 1071, Name: "CIUGUD", Parent: code(10), Type: 3, County: 1, Level: 2},
		{Code: 1213, Name: "MUNICIPIUL AIUD", Parent: code(10), Type: 4, County: 1, Level: 2},
		{Code: 6592, Name: "RAMETEA", Parent: code(10), Type: 3, County: 1, Level: 2},
		{Code: 9999, Name: "VÂNĂTORI", Parent: code(1213), Type: 11, County: 1, Level: 3},
		{Code: 179132, Name: "MUNICIPIUL BUCUREȘTI", Parent: code(403), Type: 9, County: 40, Level: 2},
		{Code: 179141, Name: "BUCUREȘTI SECTORUL 1", Parent: code(179132), Type: 6, County: 40, Level: 3},
		{Code: 73246, Name: "MISCHII", Parent: code(1608), Type: 3, County: 16, Level: 2},
		{Code: 73317, Name: "MÂRȘANI", Parent: code(1608), Type: 3, County: 16, Level: 2},
		{Code: 77777, Name: "SĂTMĂREL", Parent: code(73246), Type: 23, County: 16, Level: 3},
	}
	counties = []County{{Code: 1, Name: "JUDEȚUL ALBA"}, {Code: 16, Name: "JUDEȚUL DOLJ"}, {Code: 40, Name: "MUNICIPIUL BUCUREȘTI"}}
)

func TestReadGivesTheUnitsAndTheLocalitiesTheyServe(t *testing.T) {
	units, areas, err := Read([]byte(page()), places, counties)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 8 {
		t.Fatalf("%d units: %+v", len(units), units)
	}
	byID := map[string]Unit{}
	for _, u := range units {
		byID[u.ID] = u
	}
	aiud := byID["unitatea-fiscala-municipala-aiud"]
	if aiud.Kind != "municipal" || *aiud.County != 1 || *aiud.Regional != "directia-generala-regionala-a-finantelor-publice-brasov" ||
		aiud.Contact != "Str. Simion Bărnuțiu nr. 8, C.P. 51520; Tel. 0258.861.324" {
		t.Fatalf("%+v", aiud)
	}
	if r := byID["directia-generala-regionala-a-finantelor-publice-bucuresti"]; r.Kind != "regional" || r.Regional != nil || r.County != nil {
		t.Fatalf("%+v", r)
	}
	if n := byID["administratia-fiscala-pentru-contribuabili-nerezidenti"]; n.Kind != "non-residents" || *n.County != 40 {
		t.Fatalf("%+v", n)
	}

	var got []string
	for _, a := range areas {
		got = append(got, fmt.Sprintf("%d/%d %s %s", a.SIRUTA, a.UAT, a.Name, a.Unit))
	}
	want := []string{
		"1017/1017 Alba Iulia administratia-judeteana-a-finantelor-publice-alba",
		"1071/1071 Ciugud administratia-judeteana-a-finantelor-publice-alba",
		"1213/1213 Aiud unitatea-fiscala-municipala-aiud",
		"6592/6592 Rimetea unitatea-fiscala-municipala-aiud",
		"9999/1213 Vînători unitatea-fiscala-municipala-aiud",
		"73246/73246 Mischii Mârșani administratia-judeteana-a-finantelor-publice-dolj",
		"73317/73317 Mischii Mârșani administratia-judeteana-a-finantelor-publice-dolj",
		"77777/73246 Sãtmãrel administratia-judeteana-a-finantelor-publice-dolj",
		"179141/179141 Sectorul 1 administratia-sectorului-1-a-finantelor-publice",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReadStopsOnWhatItCannotPlace(t *testing.T) {
	for name, text := range map[string]string{
		"unknown locality": strings.Replace(page(), "Ciugud", "Ciugud, Atlantis", 1),
		"served twice":     strings.Replace(page(), "Aiud, Rimetea", "Aiud, Ciugud, Rimetea", 1),
		"unknown county":   strings.Replace(page(), "Publice DOLJ</p>", "Publice ATLANTIS</p>", 1),
		"unknown kind":     strings.Replace(page(), "<strong>Unitatea Fiscală Municipală Aiud", "<strong>Biroul Aiud", 1),
		"unit twice":       strings.Replace(page(), "Administrația Sectorului 1 a Finanțelor Publice", "Unitatea Fiscală Municipală Aiud", 1),
		"markup changed":   strings.Replace(page(), `<button class="accordion" style="margin-top: 10px;"><h3`, `<button class="accordion" style="margin-top: 10px;"><h2`, 1),
		"not the page":     "<html></html>",
	} {
		if _, _, err := Read([]byte(text), places, counties); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestIDWritesTheNameInPlainLetters(t *testing.T) {
	if got := ID("Administrația Fiscală pentru Contribuabili Mijlocii - D.G.R.F.P. București"); got != "administratia-fiscala-pentru-contribuabili-mijlocii-d-g-r-f-p-bucuresti" {
		t.Fatal(got)
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

func TestDatasetReadsTheRecordedPage(t *testing.T) {
	d, err := Dataset(site{Page: page()}.get, places, counties, Published{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != 8 || len(d.Files[1].Records) != 9 {
		t.Fatalf("%d units, %d areas", len(d.Files[0].Records), len(d.Files[1].Records))
	}
	_, err = Dataset(site{Page: page()}.get, places, counties, Published{Units: []string{"unitatea-fiscala-oraseneasca-cugir"}, Areas: []int{1017, 1348}})
	if err == nil || !strings.Contains(err.Error(), "unit unitatea-fiscala-oraseneasca-cugir") || !strings.Contains(err.Error(), "area 1348") {
		t.Fatalf("error %v", err)
	}
}

func TestPublishedKeysReadTheFilesOfTheDataset(t *testing.T) {
	units, err := PublishedUnits(strings.NewReader("{\"id\":\"a\"}\n{\"id\":\"b\"}\n"))
	if err != nil || strings.Join(units, " ") != "a b" {
		t.Fatalf("%v %v", units, err)
	}
	areas, err := PublishedAreas(strings.NewReader("{\"siruta\":1017}\n{\"siruta\":179141}\n"))
	if err != nil || fmt.Sprint(areas) != "[1017 179141]" {
		t.Fatalf("%v %v", areas, err)
	}
}
