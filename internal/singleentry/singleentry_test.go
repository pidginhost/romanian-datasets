package singleentry

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/portal"
)

// page writes a consolidated form of the code the way the portal does: each piece of text in its own
// element, with the table of contents naming article 68 before the text does.
func page(lines ...string) string {
	var b strings.Builder
	b.WriteString(`<html><body><div class="cuprins"><span>Articolul 68</span><span>Reguli</span></div>`)
	b.WriteString("<span>Articolul 67</span><span>Definirea veniturilor</span><span>(1)</span><span>Text.</span>\n")
	b.WriteString("<span>Articolul 68</span><span>Reguli generale de stabilire a venitului net anual din activități independente, determinat în sistem real, pe baza datelor din contabilitate</span>\n")
	for _, l := range lines {
		fmt.Fprintf(&b, "<span>%s</span>\n", l)
	}
	b.WriteString("<span>Articolul 68^1</span><span>Stabilirea venitului net anual</span><span>(1)</span><span>Alt text.</span></body></html>")
	return b.String()
}

var horizonText = []string{
	"(1)",
	"Venitul net anual din activități independente se determină în sistem real.",
	"(la 01-01-2026,",
	"Alineatul (1), Articolul 68 a fost modificat de Punctul 4. , Articolul XII din LEGEA nr. 239 din 15 decembrie 2025",
	")",
	"Notă",
	"...",
	"Se aplică începând cu veniturile aferente anului 2026.",
	"(2)",
	"Venitul brut cuprinde:",
	"a)",
	"sumele încasate din desfășurarea activității;",
	"...",
	"b)",
	"veniturile sub formă de dobânzi din creanțe comerciale;",
	"(4)",
	"Condițiile generale pe care trebuie să le îndeplinească cheltuielile sunt:",
	"f)",
	"să reprezinte cheltuieli cu prime de asigurare efectuate pentru:",
	"1.",
	"active corporale sau necorporale din patrimoniul afacerii;",
	"...",
	"4.",
	"asigurarea de risc profesional;",
	"(4^1)",
	"Sunt cheltuieli deductibile cheltuielile pentru învățământul dual, potrivit",
	"alin. (4) lit. d)",
	".",
	"(la 26-10-2020,",
	"Articolul 68 din Capitolul II, Titlul IV a fost completat de Punctul 1 din ORDONANȚA DE URGENȚĂ nr. 181",
	")",
	"(5)",
	"Următoarele cheltuieli sunt deductibile limitat:",
	"g)",
	"contribuțiile la fonduri de pensii facultative, în limita a 400 euro anual;",
	"h)",
	"Abrogată.",
	"(la 01-01-2019 Litera h) a fost abrogată de LEGEA nr. 1 din 2018)",
	"(6)",
	"Baza de calcul se determină ca diferență între venitul brut și cheltuielile deductibile.",
	"(7)",
	"Nu sunt cheltuieli deductibile:",
	"a)",
	"sumele sau bunurile utilizate de contribuabil pentru uzul personal;",
}

func TestReadArticleReadsTheCategories(t *testing.T) {
	units, err := ReadArticle(page(horizonText...))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range units {
		got = append(got, fmt.Sprintf("%s %s %v [%s]", u.Code, u.Kind, u.removed, u.Text))
	}
	want := []string{
		"68(2)a income false [sumele încasate din desfășurarea activității;]",
		"68(2)b income false [veniturile sub formă de dobânzi din creanțe comerciale;]",
		"68(4)f deductible false [să reprezinte cheltuieli cu prime de asigurare efectuate pentru: 1. active corporale sau necorporale din patrimoniul afacerii; 4. asigurarea de risc profesional;]",
		"68(4^1) deductible false [Sunt cheltuieli deductibile cheltuielile pentru învățământul dual, potrivit alin. (4) lit. d).]",
		"68(5)g limited false [contribuțiile la fonduri de pensii facultative, în limita a 400 euro anual;]",
		"68(5)h limited true [Abrogată.]",
		"68(7)a non-deductible false [sumele sau bunurile utilizate de contribuabil pentru uzul personal;]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if units[3].changed != "2020-10-26" || !strings.Contains(units[3].act, "ORDONANȚA DE URGENȚĂ nr. 181") {
		t.Fatalf("change of 68(4^1): %q %q", units[3].changed, units[3].act)
	}
	if units[5].changed != "2019-01-01" {
		t.Fatalf("change of 68(5)h: %q", units[5].changed)
	}
}

func TestReadArticleStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, text := range map[string]string{
		"no article":        "<html><span>Articolul 67</span></html>",
		"unknown paragraph": page(append(append([]string{}, horizonText...), "(10)", "Un alineat nou cu categorii.")...),
		"letter twice":      page(append(append([]string{}, horizonText...), "a)", "din nou;")...),
		"unread change":     page(append([]string{"(2)", "a)", "text;", "(la ieri, ceva)"}, horizonText[9:]...)...),
	} {
		if _, err := ReadArticle(text); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

const codePage = `<div id="istoric_fa">
&nbsp;<a title='Consolidarea din 01.03.2026' style='color:#428BCA' href='~/../../../Public/DetaliiDocument/307010'>01.03.2026</a><br/>
&nbsp;<a title='Consolidarea din 25.02.2026' style='color:#428BCA' href='~/../../../Public/DetaliiDocument/306994'>25.02.2026</a><br/>
&nbsp;<a title='Consolidarea din 01.01.2026' style='color:#428BCA' href='~/../../../Public/DetaliiDocument/305351'>01.01.2026</a><br/>
&nbsp;<a title='Consolidarea din 31.12.2025' style='color:#428BCA' href='~/../../../Public/DetaliiDocument/305349'>31.12.2025</a><br/>
</div>`

func replace(lines []string, old string, with ...string) []string {
	var out []string
	for _, l := range lines {
		if l == old {
			out = append(out, with...)
			continue
		}
		out = append(out, l)
	}
	return out
}

// The form of 25.02 announces the change in a note; the form of 01.03 has the new text of g, a new letter
// g^1, each with the portal's dated note.
var announced = replace(horizonText, "contribuțiile la fonduri de pensii facultative, în limita a 400 euro anual;",
	"contribuțiile la fonduri de pensii facultative, în limita a 400 euro anual;", "Notă", "...", "Începând cu data de 01-03-2026 litera g) se modifică.")

var changed = replace(horizonText, "contribuțiile la fonduri de pensii facultative, în limita a 400 euro anual;",
	"contribuțiile la fonduri de pensii facultative și ocupaționale, în limita a 400 euro anual;",
	"(la 01-03-2026,", "Litera g), Alineatul (5), Articolul 68 a fost modificată de Punctul 26., Articolul 6 din ORDONANȚA DE URGENȚĂ nr. 8 din 24 februarie 2026", ")",
	"g^1)", "contribuțiile la produsele paneuropene de pensii personale;",
	"(la 01-03-2026,", "Alineatul (5), Articolul 68 a fost completat de Punctul 27., Articolul 6 din ORDONANȚA DE URGENȚĂ nr. 8 din 24 februarie 2026", ")")

type site map[string]string

func (s site) get(address string) (io.ReadCloser, error) {
	text, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader([]byte(text))), nil
}

func recordedSite(last []string) site {
	return site{
		Code:                      codePage,
		portal.Address + "305351": page(horizonText...),
		portal.Address + "306994": page(announced...),
		portal.Address + "307010": page(last...),
	}
}

func TestDatasetFollowsTheChangesFromTheHorizon(t *testing.T) {
	d, err := Dataset(recordedSite(changed).get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range d.Files[0].Records {
		c := r.(Category)
		from, to := "-", "-"
		if c.ValidFrom != nil {
			from = *c.ValidFrom
		}
		if c.ValidTo != nil {
			to = *c.ValidTo
		}
		got = append(got, fmt.Sprintf("%s %s..%s %d", c.Code, from, to, len(c.Notes)))
	}
	want := []string{
		"68(2)a -..- 0", "68(2)b -..- 0", "68(4)f -..- 0", "68(4^1) -..- 0",
		"68(5)g -..2026-02-28 1", "68(5)g 2026-03-01..- 0", "68(5)g^1 2026-03-01..- 0",
		"68(7)a -..- 0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	g := d.Files[0].Records[5].(Category)
	if !strings.Contains(*g.Act, "ORDONANȚA DE URGENȚĂ nr. 8") || !strings.Contains(g.Text, "ocupaționale") {
		t.Fatalf("%+v", g)
	}
}

func TestDatasetStopsOnAChangeWithoutADatedNote(t *testing.T) {
	undated := replace(horizonText, "contribuțiile la fonduri de pensii facultative, în limita a 400 euro anual;",
		"contribuțiile la fonduri de pensii, în limita a 400 euro anual;")
	if _, err := Dataset(recordedSite(undated).get, nil); err == nil || !strings.Contains(err.Error(), "68(5)g") {
		t.Fatalf("a change without a dated note was taken: %v", err)
	}
	gone := replace(horizonText, "h)")
	gone = replace(gone, "Abrogată.")
	gone = replace(gone, "(la 01-01-2019 Litera h) a fost abrogată de LEGEA nr. 1 din 2018)")
	if _, err := Dataset(recordedSite(gone).get, nil); err == nil || !strings.Contains(err.Error(), "68(5)h") {
		t.Fatalf("a letter that left the text was taken: %v", err)
	}
}

func TestDatasetStopsWhenAPublishedCategoryIsGone(t *testing.T) {
	_, err := Dataset(recordedSite(changed).get, []string{"68(2)a|", "68(2)z|"})
	if err == nil || !strings.Contains(err.Error(), "68(2)z|") {
		t.Fatalf("error %v", err)
	}
}

func TestPartOrdersParagraphsAndLetters(t *testing.T) {
	if !(part("4") < part("4^1") && part("4^1") < part("5") && part("g") < part("g^1") && part("g^2") < part("h") && part("9") < part("10")) {
		t.Fatal("order")
	}
}
