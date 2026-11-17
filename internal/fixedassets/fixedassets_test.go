package fixedassets

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/history"
)

func page(rows ...[]string) string {
	var b strings.Builder
	b.WriteString(`<html><table><tr><td>EMITENT</td><td>GUVERNUL</td></tr>`)
	b.WriteString(`<tr><td>CODUL DE CLASIFICARE</td><td>DENUMIREA ACTIVELOR FIXE</td><td>DURATE NORMALE DE FUNCȚIONARE - ANI -</td></tr><tr><td>0</td><td>1</td><td>2</td></tr>`)
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range r {
			fmt.Fprintf(&b, "<td><p>%s</p></td>", c)
		}
		b.WriteString("</tr>")
	}
	b.WriteString(`<tr><td>Clasificarea și duratele normale de funcționare a mijloacelor fixe conform HG 964/1998</td><td>Noua clasificare</td></tr>`)
	b.WriteString(`<tr><td>1.1.1.</td><td>8</td><td>1.1.1.</td><td>40-60</td></tr></table></html>`)
	return b.String()
}

var catalogue = [][]string{
	{"Grupa 1. CONSTRUCȚII"},
	{"1.1.", "Construcții industriale", ""},
	{"1.1.1.", "Clădiri industriale în afară de clădirile din:", "40-60"},
	{"1.1.1.1.", "- industria alimentară;", "28-42"},
	{"1.1.10.", "Rampe de încărcare-descărcare.", "25 - 35"},
	{"GRUPA 2 INSTALAȚII TEHNICE"},
	{"2.1. ECHIPAMENTE TEHNOLOGICE"},
	{"2.1.6.", "Cuptoare", "12"},
	{"", "a) mediu neutru sau ușor coroziv;", "9-15"},
	{"", "b) mediu puternic coroziv", "6-10"},
	{"2.5. Alte instalații tehnice, mijloace de transport, animale 20-30 și plantații neregăsite în cadrul grupei 2."},
}

func TestReadCatalogue(t *testing.T) {
	got, err := ReadCatalogue(page(catalogue...))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, a := range got {
		v, p, lo, hi := "-", "-", "-", "-"
		if a.Variant != nil {
			v = *a.Variant
		}
		if a.Parent != nil {
			p = *a.Parent
		}
		if a.MinYears != nil {
			lo, hi = fmt.Sprint(*a.MinYears), fmt.Sprint(*a.MaxYears)
		}
		lines = append(lines, fmt.Sprintf("%s/%s %d<%s %s-%s [%s]", a.Code, v, a.Depth, p, lo, hi, a.Name))
	}
	want := []string{
		"1/- 1<- --- [CONSTRUCȚII]",
		"1.1/- 2<1 --- [Construcții industriale]",
		"1.1.1/- 3<1.1 40-60 [Clădiri industriale în afară de clădirile din:]",
		"1.1.1.1/- 4<1.1.1 28-42 [- industria alimentară;]",
		"1.1.10/- 3<1.1 25-35 [Rampe de încărcare-descărcare.]",
		"2/- 1<- --- [INSTALAȚII TEHNICE]",
		"2.1/- 2<2 --- [ECHIPAMENTE TEHNOLOGICE]",
		"2.1.6/- 3<2.1 12-12 [Cuptoare]",
		"2.1.6/a 3<2.1.6 9-15 [mediu neutru sau ușor coroziv;]",
		"2.1.6/b 3<2.1.6 6-10 [mediu puternic coroziv]",
		"2.5/- 2<2 20-30 [Alte instalații tehnice, mijloace de transport, animale și plantații neregăsite în cadrul grupei 2.]",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if !less("1.1.2", "1.1.10") || !less("2.1.6", "2.1.6|a") || less("2.1.6|b", "2.1.6|a") {
		t.Fatal("order")
	}
}

func TestReadCatalogueStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, rows := range map[string][][]string{
		"bad life":    {{"Grupa 1. CONSTRUCȚII"}, {"1.1.", "Construcții", "patruzeci"}},
		"no parent":   {{"Grupa 1. CONSTRUCȚII"}, {"2.1.", "Altceva", "10-12"}},
		"twice":       {{"Grupa 1. CONSTRUCȚII"}, {"1.1.", "A", ""}, {"1.1.", "B", ""}},
		"odd variant": {{"Grupa 1. CONSTRUCȚII"}, {"1.1.", "A", ""}, {"", "fara litera", "5-6"}},
		"odd heading": {{"Ceva fara cod"}},
		"backwards":   {{"Grupa 1. CONSTRUCȚII"}, {"1.1.", "A", "12-8"}},
	} {
		if _, err := ReadCatalogue(page(rows...)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
	if _, err := ReadCatalogue("<html></html>"); err == nil {
		t.Error("a page without the catalogue was read")
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

func TestDatasetKeepsThePublishedRows(t *testing.T) {
	d, err := Dataset(site{Act: page(catalogue...)}.get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	var first []Asset
	for _, r := range d.Files[0].Records {
		first = append(first, r.(Asset))
	}
	changed := append([][]string{}, catalogue...)
	changed[3] = []string{"1.1.1.1.", "- industria alimentară;", "20-30"}
	if _, err := Dataset(site{Act: page(changed...)}.get, first); err == nil || !strings.Contains(err.Error(), "1.1.1.1") {
		t.Fatalf("a change nobody recorded was taken: %v", err)
	}
	Changes["1.1.1.1"] = historyChange("2026-07-01", "HG 9/2026")
	defer delete(Changes, "1.1.1.1")
	d, err = Dataset(site{Act: page(changed...)}.get, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != len(first)+1 {
		t.Fatalf("%d rows", len(d.Files[0].Records))
	}
}

func historyChange(applies, act string) history.Change {
	return history.Change{Applies: applies, Act: act}
}
