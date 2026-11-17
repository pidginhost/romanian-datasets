package statements

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/portal"
)

func table(heading string, rows ...[]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<p>Cod unic de înregistrare ____</p><p>%s la data de ........ - lei -</p><table>", heading)
	b.WriteString(`<tr><td></td><td>Denumirea elementului</td><td>Nr. rd.</td><td>Sold la:</td></tr><tr><td>Începutul exercițiului financiar</td><td>Sfârșitul</td></tr><tr><td></td><td>A</td><td>B</td><td>1</td><td>2</td></tr>`)
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range r {
			fmt.Fprintf(&b, "<td><p>%s</p></td>", c)
		}
		b.WriteString("</tr>")
	}
	return b.String() + "</table>"
}

func annex() string {
	return "<html><script>var BILANȚ = 1;</script>" +
		table("BILANȚ",
			[]string{"A.", "ACTIVE IMOBILIZATE", "", "", ""},
			[]string{"1. Cheltuieli de constituire (ct. 201 - 2801)", "01", "", ""},
			[]string{"TOTAL (rd. 01 la 06)", "07", "", ""},
		) +
		table("BILANȚUL PRESCURTAT",
			[]string{"", "I. STOCURI (ct. 301 + 302 +/- 308 + + 4091 - din ct. 4428 + 267* - 296**)", "05", "", ""},
		) +
		table("CONTUL DE PROFIT ȘI PIERDERE",
			[]string{"1.", "Cifra de afaceri netă (rd. 02 + 03 - 04 + 05 + 06)", "01", "", ""},
			[]string{"2.", "Venituri aferente costului producției în curs de execuție (ct. 711 + 712)", "Sold C", "07", "", ""},
			[]string{"", "Sold D", "08", "", ""},
			[]string{"- Pierdere (rd. 65 + 66 + 67); (rd. 66 + 67 - 64)", "69", "", ""},
		) + "</html>"
}

func TestReadFormats(t *testing.T) {
	lines, err := ReadFormats(annex())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range lines {
		row, side, acc, rows := "-", "-", "-", "-"
		if l.Row != nil {
			row = *l.Row
		}
		if l.Side != nil {
			side = *l.Side
		}
		if l.Accounts != nil {
			acc = *l.Accounts
		}
		if l.Rows != nil {
			rows = *l.Rows
		}
		got = append(got, fmt.Sprintf("%s %d %s %s [%s] acc=%s rows=%s", l.Form, l.Order, row, side, l.Name, acc, rows))
	}
	want := []string{
		"balance-sheet 1 - - [A. ACTIVE IMOBILIZATE] acc=- rows=-",
		"balance-sheet 2 01 - [1. Cheltuieli de constituire] acc=ct. 201 - 2801 rows=-",
		"balance-sheet 3 07 - [TOTAL] acc=- rows=rd. 01 la 06",
		"abridged-balance-sheet 1 05 - [I. STOCURI] acc=ct. 301 + 302 +/- 308 + + 4091 - din ct. 4428 + 267* - 296** rows=-",
		"profit-and-loss 1 01 - [1. Cifra de afaceri netă] acc=- rows=rd. 02 + 03 - 04 + 05 + 06",
		"profit-and-loss 2 07 Sold C [2. Venituri aferente costului producției în curs de execuție] acc=ct. 711 + 712 rows=-",
		"profit-and-loss 3 08 Sold D [2. Venituri aferente costului producției în curs de execuție] acc=ct. 711 + 712 rows=-",
		"profit-and-loss 4 69 - [- Pierdere] acc=- rows=rd. 65 + 66 + 67; rd. 66 + 67 - 64",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var terms []string
	for _, x := range lines[3].Terms {
		mark := ""
		if x.Mark != nil {
			mark = *x.Mark
		}
		terms = append(terms, fmt.Sprintf("%s%s%v%s", x.Sign, x.Account, x.Part, mark))
	}
	if strings.Join(terms, " ") != "+301false +302false +/-308false +4091false -4428true +267false* -296false**" {
		t.Fatalf("terms %v", terms)
	}
}

func TestReadFormatsStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, page := range map[string]string{
		"a form missing": strings.Replace(annex(), "BILANȚUL PRESCURTAT", "ALTCEVA", 1),
		"numbered twice": strings.Replace(annex(), `<td><p>07</p></td><td><p></p></td>`, `<td><p>01</p></td><td><p></p></td>`, 1),
		"odd formula":    strings.Replace(annex(), "ct. 201 - 2801", "ct. 201 - 2801 x 3", 1),
	} {
		if _, err := ReadFormats(page); err == nil {
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

func TestDatasetReadsTheNewestForm(t *testing.T) {
	s := site{
		Order:                     `<a title='Consolidarea din 13.10.2026' style='x' href='~/../../../Public/DetaliiDocument/314288'>13.10.2026</a><a title='Consolidarea din 19.11.2025' style='x' href='~/../../../Public/DetaliiDocument/303278'>19.11.2025</a>`,
		portal.Address + "314288": `<A href="~/../../../Public/DetaliiDocumentAfis/314289">REGLEMENTĂRI CONTABILE</A>`,
		portal.Address + "314289": annex(),
	}
	d, err := Dataset(s.get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != 8 {
		t.Fatalf("%d lines", len(d.Files[0].Records))
	}
}
