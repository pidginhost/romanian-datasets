package travel

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func page(rows ...[]string) string {
	var b strings.Builder
	b.WriteString(`<html><table><tr><td>Nr. crt.</td><td>Țara</td><td>Valuta</td><td>Indemnizație de deplasare</td></tr>`)
	b.WriteString(`<tr><td>0</td><td>1</td><td>2</td><td>3</td><td>4</td><td>5</td><td>6</td></tr>`)
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range r {
			fmt.Fprintf(&b, "<td><p>%s</p></td>", c)
		}
		b.WriteString("</tr>")
	}
	return b.String() + `</table></html>`
}

var table = [][]string{
	{"1", "Austria", "euro", "35", "150", "56", "300"},
	{"2", "Zair", "USD", "32", "100", "51", "200"},
	{"3", "Antile", "USD", "32", "100", "51", "200"},
	{"4", "Sao Tome și Principe", "USD", "32", "100", "51", "200"},
}

func code(c string) *string { return &c }

var countries = []Country{
	{Alpha2: "AT", NameRO: "Austria"},
	{Alpha2: "CD", NameRO: "Republica Democratică Congo"},
	{Alpha2: "ST", NameRO: "Sao Tomé și Príncipe"},
	{Alpha2: "YU", NameRO: "Iugoslavia", ValidTo: code("2003-07-14")},
}

func TestReadTableLinksTheCountries(t *testing.T) {
	got, err := ReadTable(page(table...), countries)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, a := range got {
		c := "-"
		if a.Alpha2 != nil {
			c = *a.Alpha2
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %d/%d %d/%d", a.Country, c, a.Currency, a.DailyI, a.AccommodationI, a.DailyII, a.AccommodationII))
	}
	want := "Austria AT EUR 35/150 56/300 | Zair CD USD 32/100 51/200 | Antile - USD 32/100 51/200 | Sao Tome și Principe ST USD 32/100 51/200"
	if strings.Join(lines, " | ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(lines, " | "), want)
	}
}

func TestReadTableStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, rows := range map[string][][]string{
		"unknown country":  {{"1", "Atlantida", "USD", "1", "2", "3", "4"}},
		"unknown currency": {{"1", "Austria", "lei", "1", "2", "3", "4"}},
		"bad amount":       {{"1", "Austria", "euro", "x", "2", "3", "4"}},
		"twice":            {table[0], table[0]},
		"empty":            {},
	} {
		if _, err := ReadTable(page(rows...), countries); err == nil {
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

func TestDatasetNeedsAPersonForAChange(t *testing.T) {
	d, err := Dataset(site{Act: page(table...)}.get, countries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	var first []Allowance
	for _, r := range d.Files[0].Records {
		first = append(first, r.(Allowance))
	}
	changed := append([][]string{}, table...)
	changed[0] = []string{"1", "Austria", "euro", "40", "150", "56", "300"}
	if _, err := Dataset(site{Act: page(changed...)}.get, countries, first); err == nil || !strings.Contains(err.Error(), "Austria") {
		t.Fatalf("a change nobody recorded was taken: %v", err)
	}
}
