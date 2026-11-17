package budget

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/sheet"
)

// stateBudget is a revenue sheet in the shape of annex 1: a title, the head of the table, headings without a
// code, codes with a point or stars at the end, one written with a space, a row out of place, a remark in a
// code column, and the note block.
var stateBudget = sheet.Sheet{Name: "Anexa 1 ven.bs. 2026", Rows: [][]string{
	{"Anexa nr.1"},
	{"", "", "", "2026"},
	{"CLASIFICATIA INDICATORILOR PRIVIND BUGETUL DE STAT "},
	{"VENITURI "},
	{"Capitol", "Subcap.", "Paragraf", "DENUMIREA INDICATORILOR"},
	{"TOTAL VENITURI "},
	{"", "", "", "A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL"},
	{"01.01", "", "", "Impozit pe profit"},
	{"", "01.01.01", "", "Impozit pe profit de la agentii economici *)"},
	{"", "01 01.02.", "", "Impozit pe profit de la bancile comerciale"},
	{"03.01", "", "", "Impozit pe venit"},
	{"", "01.01.03 **", "", "Sume redirecționate din impozitul pe profit"},
	{"din 2014", "03.01.02", "", "Impozit pe venituri din salarii", "", "„"},
	{"", "", "45.01.19.02", "Fără cod deasupra"},
	{"N O T Ă :  "},
	{"Prezenta clasificatie :"},
	{"*) se aplica numai bancilor;"},
	{"**) indicatori introduși în anul 2026"},
}}

var economic = sheet.Sheet{Name: "Chelt. clas.ec. 2026", Rows: [][]string{
	{"CHELTUIELI CLASIFICAȚIA ECONOMICĂ "},
	{"Titlu", "Articol", "Alineat", "DENUMIREA INDICATORILOR"},
	{"10", "", "", "CHELTUIELI DE PERSONAL"},
	{"", "10.01", "", "Cheltuieli salariale in bani"},
	{"", "", "10.01.01", "Salarii de baza"},
}}

func TestReadIndicatorsReadsTheSheetsOfAnAnnex(t *testing.T) {
	got, err := ReadIndicators(2026, "Anexa 1", []sheet.Sheet{stateBudget})
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, i := range got {
		p, sec, r := "-", "-", "-"
		if i.Parent != nil {
			p = *i.Parent
		}
		if i.Section != nil {
			sec = *i.Section
		}
		if i.Remark != nil {
			r = *i.Remark
		}
		lines = append(lines, fmt.Sprintf("%s %s %s<%s [%s] sec=%s notes=%v remark=%s", i.Part, i.Level, i.Code, p, i.Name, sec, i.Notes, r))
	}
	want := []string{
		"revenue chapter 01.01<- [Impozit pe profit] sec=A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL notes=[] remark=-",
		"revenue subchapter 01.01.01<01.01 [Impozit pe profit de la agentii economici *)] sec=A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL notes=[se aplica numai bancilor;] remark=-",
		"revenue subchapter 01.01.02<01.01 [Impozit pe profit de la bancile comerciale] sec=A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL notes=[] remark=-",
		"revenue chapter 03.01<- [Impozit pe venit] sec=A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL notes=[] remark=-",
		"revenue subchapter 01.01.03<01.01 [Sume redirecționate din impozitul pe profit] sec=A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL notes=[indicatori introduși în anul 2026] remark=-",
		"revenue subchapter 03.01.02<03.01 [Impozit pe venituri din salarii] sec=A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL notes=[] remark=din 2014",
		"revenue paragraph 45.01.19.02<- [Fără cod deasupra] sec=A11. IMPOZIT PE VENIT, PROFIT SI CASTIGURI DIN CAPITAL notes=[] remark=-",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	eco, err := ReadIndicators(2026, "Anexa I (economica)", []sheet.Sheet{economic})
	if err != nil {
		t.Fatal(err)
	}
	if len(eco) != 3 || eco[2].View != "economic" || eco[2].Level != "paragraph" || eco[0].Level != "title" || eco[2].Part != "expense" {
		t.Fatalf("%+v", eco)
	}
}

func TestReadIndicatorsStopsOnWhatItDoesNotKnow(t *testing.T) {
	with := func(extra ...[]string) sheet.Sheet {
		s := sheet.Sheet{Name: "x", Rows: append([][]string{}, stateBudget.Rows[:14]...)}
		s.Rows = append(s.Rows, extra...)
		return s
	}
	for name, s := range map[string]sheet.Sheet{
		"two codes":   with([]string{"02.01", "02.01.01", "", "Doua coduri"}),
		"no name":     with([]string{"", "02.01.01", "", ""}),
		"twice":       with([]string{"01.01", "", "", "Impozit pe profit"}),
		"no head":     {Name: "x", Rows: [][]string{{"01.01", "", "", "Impozit"}}},
		"no revenue":  {Name: "x", Rows: [][]string{{"Capitol", "Subcap.", "Paragraf", "DENUMIREA INDICATORILOR"}, {"01.01", "", "", "Impozit"}}},
		"empty sheet": {Name: "x", Rows: stateBudget.Rows[:6]},
	} {
		if _, err := ReadIndicators(2026, "Anexa 1", []sheet.Sheet{s}); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

var departmental = sheet.Sheet{Name: "Anexa II 2026", Rows: [][]string{
	{"ANEXA II"},
	{"Cod", "Denumirea ordonatorului principal de credite"},
	{"AUTORITĂŢI PUBLICE, MINISTERE"},
	{"1", "Administratia Prezidentiala"},
	{"2", "Senatul României", "LG 1/2000"},
	{"JUDEŢE ŞI MUNICIPIUL BUCUREŞTI"},
	{"1", "Judeţul Alba"},
}}

func TestReadDepartmentsKeepsTheSections(t *testing.T) {
	got, err := ReadDepartments(2026, "Anexa II", []sheet.Sheet{departmental})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || *got[0].Section != "AUTORITĂŢI PUBLICE, MINISTERE" || *got[2].Section != "JUDEŢE ŞI MUNICIPIUL BUCUREŞTI" || *got[1].Remark != "LG 1/2000" {
		t.Fatalf("%+v", got)
	}
}

func book(sheets ...sheet.Sheet) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	write := func(name, content string) {
		w, _ := z.Create(name)
		io.WriteString(w, content)
	}
	var list, rels strings.Builder
	for n, s := range sheets {
		fmt.Fprintf(&list, `<sheet name=%q sheetId="%d" r:id="rId%d"/>`, s.Name, n+1, n+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Target="worksheets/sheet%d.xml"/>`, n+1, n+1)
		var data strings.Builder
		data.WriteString(`<worksheet><sheetData>`)
		for r, row := range s.Rows {
			fmt.Fprintf(&data, `<row r="%d">`, r+1)
			for c, v := range row {
				if v != "" {
					fmt.Fprintf(&data, `<c r="%c%d" t="inlineStr"><is><t>%s</t></is></c>`, 'A'+c, r+1, v)
				}
			}
			data.WriteString(`</row>`)
		}
		data.WriteString(`</sheetData></worksheet>`)
		write(fmt.Sprintf("xl/worksheets/sheet%d.xml", n+1), data.String())
	}
	write("xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+list.String()+`</sheets></workbook>`)
	write("xl/_rels/workbook.xml.rels", `<Relationships>`+rels.String()+`</Relationships>`)
	z.Close()
	return b.Bytes()
}

func page(year int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<html><p>Clasificația indicatorilor privind finanțele publice în vigoare pentru anul %d</p>`, year)
	b.WriteString(`<a href="/ro/buget-2026">Buget 2026</a>`)
	for label := range Annexes {
		fmt.Fprintf(&b, `<a href="/static/c/%s.xlsx">%s</a>`, strings.ReplaceAll(label, " ", "_"), label)
	}
	for label := range Departmental {
		fmt.Fprintf(&b, `<a href="/static/c/%s.xlsx">%s</a>`, strings.ReplaceAll(label, " ", "_"), label)
	}
	return b.String() + `</html>`
}

type site map[string][]byte

func (s site) get(address string) (io.ReadCloser, error) {
	data, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func recordedSite() site {
	s := site{Page: []byte(page(2026))}
	for label := range Annexes {
		sh := stateBudget
		if label == "Anexa I (economica)" {
			sh = economic
		}
		s[Site+"/static/c/"+strings.ReplaceAll(label, " ", "_")+".xlsx"] = book(sh)
	}
	for label := range Departmental {
		s[Site+"/static/c/"+strings.ReplaceAll(label, " ", "_")+".xlsx"] = book(departmental)
	}
	return s
}

func TestDatasetKeepsTheRowsOfOtherYears(t *testing.T) {
	old := Indicator{Year: 2025, Annex: "Anexa 1", Budget: "state budget", Part: "revenue", View: "functional", Code: "01.01", Level: "chapter", Name: "Impozit pe profit"}
	d, err := Dataset(recordedSite().get, Published{Indicators: []Indicator{old}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	rows := d.Files[0].Records
	if first := rows[0].(Indicator); first.Year != 2025 {
		t.Fatalf("the row of 2025 was not kept first: %+v", first)
	}
	if len(rows) != 1+11*7+3 || len(d.Files[1].Records) != 6 {
		t.Fatalf("%d indicators, %d departments", len(rows), len(d.Files[1].Records))
	}
}

func TestDatasetStopsOnAGoneCodeOrAnUnknownAnnex(t *testing.T) {
	gone := Indicator{Year: 2026, Annex: "Anexa 1", Part: "revenue", View: "functional", Code: "99.01"}
	if _, err := Dataset(recordedSite().get, Published{Indicators: []Indicator{gone}}); err == nil || !strings.Contains(err.Error(), "99.01") {
		t.Fatalf("a gone code was taken: %v", err)
	}
	s := recordedSite()
	s[Page] = []byte(strings.Replace(page(2026), "</html>", `<a href="/static/c/x.xlsx">Anexa 11</a></html>`, 1))
	if _, err := Dataset(s.get, Published{}); err == nil || !strings.Contains(err.Error(), "Anexa 11") {
		t.Fatalf("an unknown annex was taken: %v", err)
	}
}

func TestCodeReadsTheWaysTheMinistryWritesOne(t *testing.T) {
	for cell, want := range map[string]string{"01.01": "01.01|", "61.00.": "61.00|", "39.00.16.07**": "39.00.16.07|**", "45.03.01  *)": "45.03.01|*", "18 10.50": "18.10.50|"} {
		c, mark, ok := code(cell)
		if !ok || c+"|"+mark != want {
			t.Errorf("%q: %s|%s %v, want %s", cell, c, mark, ok, want)
		}
	}
	if _, _, ok := code("din 2014"); ok {
		t.Error("a remark was read as a code")
	}
}
