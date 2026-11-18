package statements

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/portal"
)

func formTable(rows ...[]string) string {
	var b strings.Builder
	b.WriteString("<table>")
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range r {
			fmt.Fprintf(&b, "<td><span>%s</span></td>", c)
		}
		b.WriteString("</tr>")
	}
	return b.String() + "</table>"
}

const (
	large = "„Date informative“ (cod 30) pentru entitățile prevăzute la pct. 2.1 din anexa nr. 1 la ordin"
	fixed = "„Situația activelor imobilizate“ (cod 40) pentru entitățile prevăzute la pct. 2.1 , 2.2 și 2.3 din anexa nr. 1 la ordin și pentru entitățile prevăzute la pct. 2.1 din anexa nr. 2 la ordin"
	small = "„Date informative“ (cod 30) pentru entitățile prevăzute la pct. 2.2 și 2.3 din anexa nr. 1 la ordin și pentru entitățile prevăzute la pct. 2.1 din anexa nr. 2 la ordin"
)

func signatures() string {
	return formTable([]string{"REPREZENTANTUL LEGAL", "ÎNTOCMIT"}, []string{"Numele și prenumele"})
}

func orderPage() string {
	structure := func(entities string) string {
		return "<p>6. Structura formularului*) " + entities + " este următoarea:</p><p>*) Formularul este reprodus în facsimil.</p>"
	}
	return "<html><p>Anexa nr. 4</p><p>Structura formularelor „Date informative“ (cod 30) și „Situația activelor imobilizate“ (cod 40)</p>" +
		structure(large) + "<p>DATE INFORMATIVE la data de 31 decembrie 2025</p>" +
		formTable(
			[]string{"Cod 30", "- lei -"},
			[]string{"I. Date privind rezultatul înregistrat", "Nr. rd.", "Nr. unități", "Sume (lei)"},
			[]string{"A", "B", "1", "2"},
			[]string{"Unități care au înregistrat profit", "01", "", ""},
			[]string{"", "Nr. rd.", "Sume (lei)"},
			[]string{"31 decembrie 2024", "31 decembrie 2025"},
			[]string{"XIII. Dividende cuvenite bugetului (rd. 03 + 04)", "02", "", ""},
			[]string{"Capital social vărsat (ct. 1012)*6) (rd. 05 + 06), din care:", "03", "", ""},
			[]string{"Dobânzi de plătit către nerezidenți (din ct. 4518 + din ct. 4538)", "04", "", ""},
			[]string{"Variația stocurilor (ct. 711)", "Sold C", "05", "", ""},
			[]string{"", "Sold D", "06", "", ""},
			[]string{"PROFITUL SAU PIERDEREA FINANCIAR(Ă):", "07", "", ""},
			[]string{"", "- Profit (rd. 01 - 02)"},
		) + signatures() +
		structure(small) + "<p>DATE INFORMATIVE la data de 31 decembrie 2025</p>" +
		formTable(
			[]string{"Cod 30", "- lei -"},
			[]string{"I. Date privind rezultatul înregistrat", "Nr. rd.", "Nr. unități", "Sume (lei)"},
			[]string{"Unități care au înregistrat profit", "01", "", ""},
		) + signatures() +
		structure(fixed) + "<p>SITUAȚIA ACTIVELOR IMOBILIZATE la data de 31 decembrie 2025</p>" +
		formTable(
			[]string{"cod 40", "-lei-"},
			[]string{"Elemente de imobilizări", "Nr. rd.", "Valori brute"},
			[]string{"A", "B", "1", "2"},
			[]string{"I. Imobilizări necorporale"},
			[]string{"1. Cheltuieli de constituire", "01", "", "X"},
		) + "<p>*1) Se vor evidenția distinct creșterile.</p><p>SITUAȚIA AMORTIZĂRII ACTIVELOR IMOBILIZATE</p>" +
		formTable(
			[]string{"cod 40", "-lei-"},
			[]string{"Elemente de imobilizări", "Nr. rd.", "Sold inițial"},
			[]string{"A", "B", "6", "7"},
			[]string{"Amortizare - TOTAL (rd. 01)", "02", ""},
		) + signatures() + "</html>"
}

var year2025 = YearOrder{Year: 2025, Act: "OMF 2036/2025", Page: "306521"}

func TestReadYearForms(t *testing.T) {
	lines, err := ReadYearForms(orderPage(), year2025)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range lines {
		show := func(p *string) string {
			if p == nil {
				return "-"
			}
			return *p
		}
		got = append(got, fmt.Sprintf("%s %s %d %s [%s] %s [%s] acc=%s rows=%s", l.Form, l.Code, l.Order, show(l.Row), show(l.Section), show(l.Side), l.Name, show(l.Accounts), show(l.Rows)))
	}
	want := []string{
		"informative-data 30 1 01 [I. Date privind rezultatul înregistrat] - [Unități care au înregistrat profit] acc=- rows=-",
		"informative-data 30 2 02 [XIII. Dividende cuvenite bugetului] - [XIII. Dividende cuvenite bugetului] acc=- rows=rd. 03 + 04",
		"informative-data 30 3 03 [XIII. Dividende cuvenite bugetului] - [Capital social vărsat *6), din care:] acc=ct. 1012 rows=rd. 05 + 06",
		"informative-data 30 4 04 [XIII. Dividende cuvenite bugetului] - [Dobânzi de plătit către nerezidenți] acc=din ct. 4518 + din ct. 4538 rows=-",
		"informative-data 30 5 05 [XIII. Dividende cuvenite bugetului] Sold C [Variația stocurilor] acc=ct. 711 rows=-",
		"informative-data 30 6 06 [XIII. Dividende cuvenite bugetului] Sold D [Variația stocurilor] acc=ct. 711 rows=-",
		"informative-data 30 7 07 [XIII. Dividende cuvenite bugetului] - [PROFITUL SAU PIERDEREA FINANCIAR(Ă):] acc=- rows=-",
		"informative-data 30 8 - [XIII. Dividende cuvenite bugetului] - [- Profit] acc=- rows=rd. 01 - 02",
		"informative-data-small 30 1 01 [I. Date privind rezultatul înregistrat] - [Unități care au înregistrat profit] acc=- rows=-",
		"fixed-assets 40 1 - [SITUAȚIA ACTIVELOR IMOBILIZATE] - [I. Imobilizări necorporale] acc=- rows=-",
		"fixed-assets 40 2 01 [SITUAȚIA ACTIVELOR IMOBILIZATE] - [1. Cheltuieli de constituire] acc=- rows=-",
		"fixed-assets 40 3 02 [SITUAȚIA AMORTIZĂRII ACTIVELOR IMOBILIZATE] - [Amortizare - TOTAL] acc=- rows=rd. 01",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if lines[3].Terms[1].Account != "4538" || !lines[3].Terms[1].Part {
		t.Fatalf("terms %+v", lines[3].Terms)
	}
}

func TestReadYearFormsStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, page := range map[string]string{
		"an unknown form":      strings.Replace(orderPage(), "pct. 2.1 din anexa nr. 1 la ordin este", "pct. 2.9 din anexa nr. 1 la ordin este", 1),
		"a row number missing": strings.Replace(orderPage(), "<td><span>04</span></td>", "<td><span>14</span></td>", 1),
		"a form missing":       strings.Replace(orderPage(), "<td><span>Cod 30</span></td>", "<td><span>Altceva</span></td>", 2),
		"an odd formula":       strings.Replace(orderPage(), "ct. 1012", "ct. 1012 x 2", 1),
		"no annex":             strings.Replace(orderPage(), "Structura formularelor", "Altceva", 1),
	} {
		if _, err := ReadYearForms(page, year2025); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestYearDatasetReadsTheNewestFormOfTheOrder(t *testing.T) {
	s := site{
		portal.Address + "306521": `<a title='Consolidarea din 20.12.2025' style='x' href='~/../../../Public/DetaliiDocument/306600'>20.12.2025</a>`,
		portal.Address + "306600": orderPage(),
	}
	d, err := YearDataset(s.get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if n := len(d.Files[0].Records); n != 12 {
		t.Fatalf("%d lines", n)
	}
	if first := d.Files[0].Records[0].(FormLine); first.Form != "informative-data" {
		t.Fatalf("first line %+v", first)
	}
}
