package chart

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/portal"
)

// annex writes the consolidated annex the way the portal does: each piece of text in its own element, the
// heading of the chart inside a paragraph, notes of the portal between accounts, and chapter 15 after.
func annex(lines ...string) string {
	var b strings.Builder
	b.WriteString(`<html><head><script>var x = "Clasa 9 - no";</script></head><body>`)
	b.WriteString(`<span class="S_PCT_TTL">594.<!----></span> <span class="S_PCT_BDY">- Planul de conturi general este următorul:</span>`)
	for _, l := range lines {
		fmt.Fprintf(&b, "<span class=\"S_PAR\">%s</span>\n", l)
	}
	b.WriteString(`<span class="S_CAP_TTL">Capitolul 15</span><span>101. Capital (P)</span></body></html>`)
	return b.String()
}

var chapter14 = []string{
	"...",
	"Clasa 1 - Conturi de capitaluri, provizioane, împrumuturi și datorii asimilate",
	"10.         Capital și rezerve",
	"101. Capital*4)",
	"1011. Capital subscris nevărsat (P)",
	"103. Alte elemente de capitaluri proprii",
	"1033. Diferențe de curs valutar în relație cu investiția netă într-o",
	"entitate străină*7) (A/P)",
	"12.         Rezultatul exercițiului financiar",
	"121. Profit sau pierdere (A/P)",
	"(la 01-02-2017,",
	"Capitolul 14  a fost completat de",
	"Punctul 14, Articolul 12 din ORDINUL nr. 166 din 25 ianuarie 2017",
	")",
	"14.         Câștiguri sau pierderi",
	"149. Pierderi legate de emiterea instrumentelor de capitaluri proprii",
	"1496  Pierderi rezultate din reorganizări de societăți (A)",
	"Clasa 4 - Conturi de terți",
	"43.         Asigurări sociale",
	"431. Asigurări sociale",
	"4318. Alte contribuții pentru asigurările sociale de sănătate*1)",
	"*1) În acest cont se înregistrează contribuția datorată de alte persoane.",
	"(la 07-02-2018 Planul de conturi a fost completat cu contul 436)",
	"46.         Debitori și creditori diverși",
	"466. Decontări din operațiuni de fiducie ---------- Contul 466 de la grupa 46 a fost introdus de",
	"pct. 35 al art. I din ORDINUL nr. 4.160 din 31 decembrie 2015.",
	"Clasa 6 - Conturi de cheltuieli",
	"69.         Cheltuieli cu impozitul pe profit și alte impozite",
	"695. Cheltuieli cu impozitul specific unor activități^1",
	"Notă",
	"...",
	"^1 Se utilizează pentru evidențierea impozitului specific",
	"unor activități, definit potrivit legii.",
	"(la 18-07-2018,",
	"Contul 695 a fost introdus de ORDINUL nr. 2.531",
	")",
	"Clasa 8 - Conturi speciale",
	"80.         Conturi în afara bilanțului",
	"803. Alte conturi în afara bilanțului",
	"8031. Eliminat",
	"8031. Imobilizări corporale primite cu chirie",
	"8032. Eliminat.",
	"Notă",
	"...",
	"*4) În funcție de forma juridică a entității se înscrie: capital social, patrimoniul regiei etc.",
	"*7) Acest cont apare numai în situațiile financiare anuale consolidate.",
	"Notă",
	"...",
	"Potrivit art. 13 din ORDINUL nr. 2.493 din 8 iulie 2019, prevederile se aplică începând cu 2019.",
}

func TestReadChapterReadsTheChart(t *testing.T) {
	accounts, err := ReadChapter(annex(chapter14...), Companies)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range accounts {
		f, r := "-", "-"
		if a.Function != nil {
			f = *a.Function
		}
		if a.Role != nil {
			r = *a.Role
		}
		got = append(got, fmt.Sprintf("%s %s [%s] %s %s %d", a.Code, a.Level, *a.Name, f, r, len(a.Notes)))
	}
	want := []string{
		"1 class [Conturi de capitaluri, provizioane, împrumuturi și datorii asimilate] - - 0",
		"10 group [Capital și rezerve] - - 0",
		"101 account [Capital] - - 1",
		"1011 subaccount [Capital subscris nevărsat] P - 0",
		"103 account [Alte elemente de capitaluri proprii] - - 0",
		"1033 subaccount [Diferențe de curs valutar în relație cu investiția netă într-o entitate străină] A/P - 1",
		"12 group [Rezultatul exercițiului financiar] - - 0",
		"121 account [Profit sau pierdere] A/P result 0",
		"14 group [Câștiguri sau pierderi] - - 0",
		"149 account [Pierderi legate de emiterea instrumentelor de capitaluri proprii] - - 0",
		"1496 subaccount [Pierderi rezultate din reorganizări de societăți] A - 0",
		"4 class [Conturi de terți] - - 0",
		"43 group [Asigurări sociale] - - 0",
		"431 account [Asigurări sociale] - - 0",
		"4318 subaccount [Alte contribuții pentru asigurările sociale de sănătate] - - 1",
		"46 group [Debitori și creditori diverși] - - 0",
		"466 account [Decontări din operațiuni de fiducie] - - 0",
		"6 class [Conturi de cheltuieli] - - 0",
		"69 group [Cheltuieli cu impozitul pe profit și alte impozite] - - 0",
		"695 account [Cheltuieli cu impozitul specific unor activități] - expense 1",
		"8 class [Conturi speciale] - - 0",
		"80 group [Conturi în afara bilanțului] - - 0",
		"803 account [Alte conturi în afara bilanțului] - - 0",
		"8031 subaccount [Imobilizări corporale primite cu chirie] - - 0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	byCode := map[string]Account{}
	for _, a := range accounts {
		byCode[a.Code] = a
	}
	if n := byCode["695"].Notes[0]; n != "Se utilizează pentru evidențierea impozitului specific unor activități, definit potrivit legii." {
		t.Fatalf("note of 695: %q", n)
	}
	if n := byCode["4318"].Notes[0]; n != "În acest cont se înregistrează contribuția datorată de alte persoane." {
		t.Fatalf("note of 4318: %q", n)
	}
	if *byCode["1496"].Parent != "149" {
		t.Fatal("parent of 1496")
	}
}

func TestReadChapterStopsOnWhatItDoesNotKnow(t *testing.T) {
	without := func(drop string) []string {
		var out []string
		for _, l := range chapter14 {
			if l != drop {
				out = append(out, l)
			}
		}
		return out
	}
	for name, page := range map[string]string{
		"no chart":         "<html><span>Capitolul 15</span></html>",
		"missing note":     annex(without("*4) În funcție de forma juridică a entității se înscrie: capital social, patrimoniul regiei etc.")...),
		"missing parent":   annex(without("149. Pierderi legate de emiterea instrumentelor de capitaluri proprii")...),
		"listed twice":     annex(append(append([]string{}, chapter14[:5]...), append([]string{"1011. Capital subscris nevărsat (P)"}, chapter14[5:]...)...)...),
		"two functions":    annex(append(append([]string{}, chapter14[:4]...), append([]string{"1012. Capital subscris vărsat (P) (A)"}, chapter14[4:]...)...)...),
		"missing own note": annex(without("^1 Se utilizează pentru evidențierea impozitului specific")...),
	} {
		if _, err := ReadChapter(page, Companies); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

const orderPage = `<div id="istoric_fa">
&nbsp;<a title='Consolidarea din 13.10.2026' style='color:#428BCA' href='~/../../../Public/DetaliiDocument/314288'>13.10.2026</a><br/>
&nbsp;<a title='Consolidarea din 19.11.2025' style='color:#428BCA' href='~/../../../Public/DetaliiDocument/303278'>19.11.2025</a><br/>
&nbsp;<a title='Consolidarea din 22.08.2025' style='color:#428BCA' href='~/../../../Public/DetaliiDocument/300253'>22.08.2025</a><br/>
</div>`

func formPage(annexID string) string {
	return `<span class="S_ANX_TTL">ANEXĂ</span><span class="S_REF"><A href="~/../../../Public/DetaliiDocumentAfis/` + annexID + `">REGLEMENTĂRI CONTABILE</A></span>`
}

type site map[string]string

func (s site) get(address string) (io.ReadCloser, error) {
	text, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader([]byte(text))), nil
}

func recordedSite(later []string) site {
	return site{
		Order:             orderPage,
		Portal + "303278": formPage("303279"),
		Portal + "303279": annex(chapter14...),
		Portal + "314288": formPage("314289"),
		Portal + "314289": annex(later...),
	}
}

func TestFormsFromTheHorizon(t *testing.T) {
	forms, err := portal.Forms(orderPage)
	if err != nil {
		t.Fatal(err)
	}
	from, err := portal.FromHorizon(forms, Horizon)
	if err != nil {
		t.Fatal(err)
	}
	if len(from) != 2 || from[0].ID != "303278" || from[1].ID != "314288" {
		t.Fatalf("%+v", from)
	}
	// The newest form alone came after the horizon, so no form is in force on it.
	if _, err := portal.FromHorizon(forms[2:], Horizon); err == nil {
		t.Fatal("a form after the horizon was taken as in force on it")
	}
}

func TestDatasetTakesTheChartInForceOnTheHorizon(t *testing.T) {
	d, err := Dataset(recordedSite(chapter14).get, Companies, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != 24 {
		t.Fatalf("%d accounts", len(d.Files[0].Records))
	}
	for _, r := range d.Files[0].Records {
		if a := r.(Account); a.ValidFrom != nil || a.ValidTo != nil || a.Act != nil {
			t.Fatalf("%+v", a)
		}
	}
}

func TestDatasetNeedsAPersonForAChangeOfTheChart(t *testing.T) {
	later := append([]string{}, chapter14...)
	for i, l := range later {
		if l == "1011. Capital subscris nevărsat (P)" {
			later[i] = "1011. Capital subscris și nevărsat (P)"
		}
	}
	later = append(later[:len(later)-6], append([]string{"8033. Valori materiale primite în păstrare"}, later[len(later)-6:]...)...)
	if _, err := Dataset(recordedSite(later).get, Companies, nil); err == nil || !strings.Contains(err.Error(), "2026-10-13") {
		t.Fatalf("a change without a person was taken: %v", err)
	}

	Companies.Changes["2026-10-13"] = Change{Applies: "2026-11-01", Act: "OMF 1281/2026"}
	defer delete(Companies.Changes, "2026-10-13")
	d, err := Dataset(recordedSite(later).get, Companies, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range d.Files[0].Records {
		a := r.(Account)
		if a.Code != "1011" && a.Code != "8033" {
			continue
		}
		from, to := "-", "-"
		if a.ValidFrom != nil {
			from = *a.ValidFrom
		}
		if a.ValidTo != nil {
			to = *a.ValidTo
		}
		got = append(got, fmt.Sprintf("%s %s %s..%s", a.Code, *a.Name, from, to))
	}
	want := "1011 Capital subscris nevărsat -..2026-10-31 | 1011 Capital subscris și nevărsat 2026-11-01..- | 8033 Valori materiale primite în păstrare 2026-11-01..-"
	if strings.Join(got, " | ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, " | "), want)
	}
}

func TestDatasetStopsWhenAPublishedAccountIsGone(t *testing.T) {
	_, err := Dataset(recordedSite(chapter14).get, Companies, []string{"101|", "5126|"})
	if err == nil || !strings.Contains(err.Error(), "5126|") {
		t.Fatalf("error %v", err)
	}
}

func TestPublishedKeysReadTheFile(t *testing.T) {
	keys, err := PublishedKeys(strings.NewReader("{\"code\":\"101\",\"valid_from\":null}\n{\"code\":\"1011\",\"valid_from\":\"2026-11-01\"}\n"))
	if err != nil || strings.Join(keys, " ") != "101| 1011|2026-11-01" {
		t.Fatalf("%v %v", keys, err)
	}
}

// table writes a chart the way OMFP 3103/2017 and OMFP 1917/2005 do: each code on a line of its own and its
// name on the lines after it.
func table(start, end string, lines ...string) string {
	var b strings.Builder
	b.WriteString(`<html><body><span>` + start + `</span>`)
	for _, l := range lines {
		fmt.Fprintf(&b, "<span>%s</span>\n", l)
	}
	b.WriteString(`<span>` + end + `</span><span>101</span><span>Nu e în plan</span></body></html>`)
	return b.String()
}

var nonprofitChart = []string{
	"1", "CONTURI DE CAPITALURI", "10", "CAPITAL ȘI REZERVE", "101", "Capital (P)",
	"105", "Rezerve din reevaluare", "105.01", "Rezerve din reevaluare privind activitățile", "fără scop patrimonial (P)",
	"107", "Patrimoniul privat^2 (P)",
	"12", "REZULTATUL EXERCIȚIULUI FINANCIAR", "121", "Excedent/profit sau deficit/pierdere",
	"1211", "Excedent sau deficit privind activitățile fără scop patrimonial (A/P)",
	"Notă", "...", "^2 Acest cont apare doar la entitățile care dețin bunuri de natura patrimoniului privat.",
	"(la 21-01-2021,", "Planul de conturi de la punctul 344 a fost modificat de", "Articolul 15 din ORDINUL nr. 58", ")",
	"Notă", "...", "Potrivit art. 13 din ORDINUL nr. 2.493, prevederile se aplică începând cu anul 2019.",
	"6", "CONTURI DE CHELTUIELI", "60", "CHELTUIELI PRIVIND STOCURILE", "601", "Cheltuieli cu materiile prime",
}

func TestReadChapterReadsAChartWrittenAsATable(t *testing.T) {
	accounts, err := ReadChapter(table("344.", "Capitolul 9", append([]string{"Planul de conturi general este următorul:"}, nonprofitChart...)...), Nonprofit)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range accounts {
		f, r, p := "-", "-", "-"
		if a.Function != nil {
			f = *a.Function
		}
		if a.Role != nil {
			r = *a.Role
		}
		if a.Parent != nil {
			p = *a.Parent
		}
		got = append(got, fmt.Sprintf("%s<%s %s [%s] %s %s %d", a.Code, p, a.Level, *a.Name, f, r, len(a.Notes)))
	}
	want := []string{
		"1<- class [CONTURI DE CAPITALURI] - - 0",
		"10<1 group [CAPITAL ȘI REZERVE] - - 0",
		"101<10 account [Capital] P - 0",
		"105<10 account [Rezerve din reevaluare] - - 0",
		"105.01<105 detail [Rezerve din reevaluare privind activitățile fără scop patrimonial] P - 0",
		"107<10 account [Patrimoniul privat] P - 1",
		"12<1 group [REZULTATUL EXERCIȚIULUI FINANCIAR] - - 0",
		"121<12 account [Excedent/profit sau deficit/pierdere] - result 0",
		"1211<121 subaccount [Excedent sau deficit privind activitățile fără scop patrimonial] A/P result 0",
		"6<- class [CONTURI DE CHELTUIELI] - - 0",
		"60<6 group [CHELTUIELI PRIVIND STOCURILE] - - 0",
		"601<60 account [Cheltuieli cu materiile prime] - expense 0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

var publicChart = []string{
	"C", "L", "A", "S", "A", "GRUPA", "CONT SINTETIC gr. I, gr. II și gr. III", "DENUMIRE CONT NOU",
	"1", "CONTURI DE CAPITALURI", "10", "CAPITAL, REZERVE, FONDURI",
	"100", "Fondul activelor fixe necorporale", "100 00", "Abrogat.", "100 01", "Fondul activelor fixe necorporale ale statului",
	"100 00 00", "Abrogat.", "100 01 01", "Fondul activelor fixe necorporale ale statului",
	"139", "Eliminat.", "139 01", "Eliminat.", "139 01 00", "Elimimnat.",
	"8", "CONTURI SPECIALE", "80", "CONTURI ÎN AFARA BILANȚULUI",
	"803 00 01", "Active fixe și obiecte de inventar primite în folosință",
	"89", "891", "Bilanț de deschidere",
}

func TestReadChapterReadsThePublicChart(t *testing.T) {
	accounts, err := ReadChapter(table("Capitolul VI", "Capitolul VII*)", append([]string{"PLANUL DE CONTURI GENERAL PENTRU INSTITUȚIILE PUBLICE"}, publicChart...)...), Public)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range accounts {
		name, p := "-", "-"
		if a.Name != nil {
			name = *a.Name
		}
		if a.Parent != nil {
			p = *a.Parent
		}
		got = append(got, fmt.Sprintf("%s<%s %s [%s]", a.Code, p, a.Level, name))
	}
	want := []string{
		"1<- class [CONTURI DE CAPITALURI]",
		"10<1 group [CAPITAL, REZERVE, FONDURI]",
		"100<10 account [Fondul activelor fixe necorporale]",
		"100 01<100 subaccount [Fondul activelor fixe necorporale ale statului]",
		"100 01 01<100 01 detail [Fondul activelor fixe necorporale ale statului]",
		"8<- class [CONTURI SPECIALE]",
		"80<8 group [CONTURI ÎN AFARA BILANȚULUI]",
		"803 00 01<80 detail [Active fixe și obiecte de inventar primite în folosință]",
		"89<8 group [-]",
		"891<89 account [Bilanț de deschidere]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	odd := append([]string{"PLANUL DE CONTURI GENERAL PENTRU INSTITUȚIILE PUBLICE"}, publicChart...)
	odd = append(odd, "892", "Elimnat.")
	if _, err := ReadChapter(table("Capitolul VI", "Capitolul VII*)", odd...), Public); err == nil || !strings.Contains(err.Error(), "892") {
		t.Fatalf("an unknown mark was taken as a name: %v", err)
	}
}

func TestAboveAndLevel(t *testing.T) {
	for code, want := range map[string]string{"100 01 01": "100 01|detail", "100 01": "100|subaccount", "105.01": "105|detail", "1011": "101|subaccount", "101": "10|account", "10": "1|group", "1": "|class"} {
		if got := above(code) + "|" + level(code); got != want {
			t.Errorf("%s: %s, want %s", code, got, want)
		}
	}
}
