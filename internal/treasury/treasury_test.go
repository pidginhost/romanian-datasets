package treasury

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/pdftext"
	"github.com/pidginhost/romanian-datasets/internal/pdftext/pdftesting"
)

// iban gives the IBAN of an account at a unit, with its check digits.
func iban(unit, suffix string) string {
	body := "TREZ" + unit + suffix + "RO00"
	var digits strings.Builder
	for _, r := range body {
		if r >= 'A' && r <= 'Z' {
			fmt.Fprintf(&digits, "%d", r-'A'+10)
		} else {
			digits.WriteRune(r)
		}
	}
	n, _ := new(big.Int).SetString(digits.String(), 10)
	check := 98 - new(big.Int).Mod(n, big.NewInt(97)).Int64()
	return fmt.Sprintf("RO%02dTREZ%s%s", check, unit, suffix)
}

// row is one line of a list: a funding source (letter and name, or two empty strings for a section without
// one), an account (date, suffix, code, name), or the rest of the name above.
type row []string

// listPDF writes the list of a unit as ANAF does: a heading on each page, the funding source repeated at the
// top of a page, the column names, and one line per account, a long name going on below. A cell can stand
// a little off its line.
func listPDF(unit, name string, pages ...[]row) []byte {
	var out [][]pdftext.Text
	for n, rows := range pages {
		page := []pdftext.Text{
			{X: 441, Y: 5.24, S: "Pag."}, {X: 469.28, Y: 5.24, S: fmt.Sprintf(" %d ", n+1)},
			{X: 241.88, Y: 782.24, S: "Coduri IBAN"},
			{X: 538.94, Y: 741.76, S: "13/11/2026"},
			{X: 167.98, Y: 764.24, S: name},
		}
		y := 728.24
		for _, r := range rows {
			switch len(r) {
			case 2:
				page = append(page, pdftext.Text{X: 16.70, Y: y, S: "Sursa de finantare:"})
				if r[0] != "" {
					page = append(page, pdftext.Text{X: 157.50, Y: y, S: r[0]}, pdftext.Text{X: 184.42, Y: y, S: r[1]})
				}
				y -= 16.48
				page = append(page,
					pdftext.Text{X: 16.70, Y: y, S: "Data publicarii"}, pdftext.Text{X: 116.98, Y: y, S: "Cont IBAN "},
					pdftext.Text{X: 247.50, Y: y, S: "Denumire"}, pdftext.Text{X: 197.98, Y: y - 1.52, S: "Cont"})
				y -= 17.24
			case 4:
				page = append(page,
					pdftext.Text{X: 16.70, Y: y, S: r[0]}, pdftext.Text{X: 67.50, Y: y, S: iban(unit, r[1])},
					pdftext.Text{X: 196.70, Y: y + 1.5, S: r[2]}, pdftext.Text{X: 247.50, Y: y, S: r[3] + " "})
				y -= 13.52
			case 1:
				y += 4.52
				page = append(page, pdftext.Text{X: 247.50, Y: y, S: r[0]})
				y -= 13.52
			}
		}
		out = append(out, page)
	}
	return pdftesting.Write(out)
}

var (
	profit  = row{"21-11-05", "20010101XXXXX", "20A010100", "Impozit pe profit de la agentii economici"}
	salary  = row{"13-01-23", "20030102XXXXX", "20A030200", "Impozit pe venituri din salarii"}
	long    = row{"07-02-25", "20020107XXXXX", "20A020700", "Impozit pe veniturile microintreprinderilor realizate de persoana fizica dintr-o asociere cu o"}
	general = row{"21-11-05", "4201XXXXXXXXX", "4201", "Disponibilitati ale persoanelor fizice"}
)

func albaIulia() []byte {
	return listPDF("002", "Trezorerie operativa Municipiul  Alba Iulia",
		[]row{{"A", "Integral de la buget"}, profit, long},
		[]row{{"A", "Integral de la buget"}, {"persoana juridica, care nu genereaza o persoana juridica"}, salary, {"", ""}, general})
}

func TestReadListReadsTheAccounts(t *testing.T) {
	list, err := ReadList(albaIulia())
	if err != nil {
		t.Fatal(err)
	}
	if list.Name != "Trezorerie operativa Municipiul Alba Iulia" || list.Sources["A"] != "Integral de la buget" || len(list.Accounts) != 4 {
		t.Fatalf("%+v", list)
	}
	got := list.Accounts[1]
	if got.Code != "20A020700" || got.Published != "2025-02-07" || got.IBAN != iban("002", "20020107XXXXX") ||
		got.Name != "Impozit pe veniturile microintreprinderilor realizate de persoana fizica dintr-o asociere cu o persoana juridica, care nu genereaza o persoana juridica" {
		t.Fatalf("%+v", got)
	}
	if last := list.Accounts[3]; last.Source != "" || last.Code != "4201" {
		t.Fatalf("%+v", last)
	}
}

func TestReadListStopsOnWhatItDoesNotKnow(t *testing.T) {
	broken := strings.Replace(iban("002", "20010101XXXXX"), "RO", "RO9", 1)[:24]
	for name, data := range map[string][]byte{
		"account before a source": listPDF("002", "Trezorerie operativa Blaj", []row{profit}),
		"stray name":              listPDF("002", "Trezorerie operativa Blaj", []row{{"A", "Integral de la buget"}, {"no account above"}, profit}),
	} {
		if _, err := ReadList(data); err == nil {
			t.Errorf("%s: read", name)
		}
	}
	pages := [][]pdftext.Text{{
		{X: 167.98, Y: 764.24, S: "Trezorerie operativa Blaj"},
		{X: 16.70, Y: 728.24, S: "Sursa de finantare:"}, {X: 157.50, Y: 728.24, S: "A"}, {X: 184.42, Y: 728.24, S: "Integral de la buget"},
		{X: 16.70, Y: 694.52, S: "21-11-05"}, {X: 67.50, Y: 694.52, S: broken}, {X: 196.70, Y: 694.52, S: "20A010100"}, {X: 247.50, Y: 694.52, S: "Impozit"},
	}}
	if _, err := ReadList(pdftesting.Write(pages)); err == nil {
		t.Error("a wrong IBAN was read")
	}
	pages[0][0].S = "Trezorerie operativa Aiud"
	pages = append(pages, []pdftext.Text{{X: 167.98, Y: 764.24, S: "Trezorerie operativa Blaj"}})
	if _, err := ReadList(pdftesting.Write(pages)); err == nil {
		t.Error("a list with two treasury names was read")
	}
}

func TestValidIBAN(t *testing.T) {
	if iban("002", "20010101XXXXX") != "RO24TREZ00220010101XXXXX" || !ValidIBAN("RO24TREZ00220010101XXXXX") || ValidIBAN("RO25TREZ00220010101XXXXX") || ValidIBAN("RO") {
		t.Fatal("check digits")
	}
}

var (
	places = []Place{
		{Code: 179132, Name: "MUNICIPIUL BUCUREȘTI", County: 40, Level: 2},
		{Code: 179141, Name: "BUCUREȘTI SECTORUL 1", County: 40, Level: 3},
		{Code: 1017, Name: "MUNICIPIUL ALBA IULIA", County: 1, Level: 2},
		{Code: 1348, Name: "MUNICIPIUL BLAJ", County: 1, Level: 2},
		{Code: 77812, Name: "MUNICIPIUL TÂRGU JIU", County: 18, Level: 2},
	}
	counties = []County{{Code: 1, Name: "JUDEȚUL ALBA"}, {Code: 18, Name: "JUDEȚUL GORJ"}, {Code: 40, Name: "MUNICIPIUL BUCUREȘTI"}}
)

func TestLocateMatchesByNameOrByTheTable(t *testing.T) {
	for _, c := range []struct {
		code, name, county string
		wantCounty         int
		wantSIRUTA         int
	}{
		{"002", "Trezorerie operativa Municipiul Alba Iulia", "Alba", 1, 1017},
		{"700", "Trezorerie operativa Municipiul Bucuresti", "", 40, 179132},
		{"701", "Trezorerie operativa Sector 1", "", 40, 179141},
		{"336", "Trezorerie operativa Municipiul Tg.Jiu", "Gorj", 18, 77812},
		{"421", "Trezorerie operativa Ilfov", "", 40, 0},
	} {
		county, siruta, err := Locate(c.code, c.name, c.county, places, counties)
		if err != nil {
			t.Fatalf("%s: %v", c.code, err)
		}
		got := 0
		if siruta != nil {
			got = *siruta
		}
		if county != c.wantCounty || got != c.wantSIRUTA {
			t.Errorf("%s: county %d siruta %d", c.code, county, got)
		}
	}
	for name, c := range map[string][3]string{
		"no match":         {"003", "Trezorerie operativa Cugir", "Alba"},
		"unknown county":   {"003", "Trezorerie operativa Blaj", "Atlantis"},
		"stale table":      {"336", "Trezorerie operativa Municipiul Alba Iulia", "Alba"},
		"table off county": {"336", "Trezorerie operativa Municipiul Tg.Jiu", "Alba"},
	} {
		if _, _, err := Locate(c[0], c[1], c[2], places, counties); err == nil {
			t.Errorf("%s: located", name)
		}
	}
}

const index = `<html><a href="https://static.anaf.ro/static/10/Anaf/AsistentaContribuabili_r/iban2014/bugetstat.htm">buget</a>
<a href="https://static.anaf.ro/static/10/Anaf/AsistentaContribuabili_r/iban2014/Alba.htm">Alba</a>
<a href="https://static.anaf.ro/static/10/Anaf/AsistentaContribuabili_r/iban2014/iban_TREZ000_TREZ700.pdf">Bucuresti</a></html>`

const alba = `<HTML><HEAD><TITLE>Trezoreria
      jude&#355;ean&#259; Alba</TITLE></HEAD>
<a href="http://static.anaf.ro/static/10/Anaf/AsistentaContribuabili_r/iban2014/iban_TREZ001_TREZ002.pdf">Alba Iulia</a></HTML>`

type site map[string][]byte

func (s site) get(address string) (io.ReadCloser, error) {
	data, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func recordedSite() site {
	return site{
		Index:                               []byte(index),
		Folder + "Alba.htm":                 []byte(alba),
		Folder + "iban_TREZ001_TREZ002.pdf": albaIulia(),
		Folder + "iban_TREZ000_TREZ700.pdf": listPDF("700", "Trezorerie operativa Municipiul Bucuresti", []row{{"A", "Integral de la buget"}, profit}),
	}
}

func TestDatasetJoinsTheAccountsOfEveryUnit(t *testing.T) {
	d, err := Dataset(recordedSite().get, places, counties, Published{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	units, sources, accounts := d.Files[0].Records, d.Files[1].Records, d.Files[2].Records
	if len(units) != 2 || len(sources) != 1 || len(accounts) != 4 {
		t.Fatalf("%d units, %d sources, %d accounts", len(units), len(sources), len(accounts))
	}
	if u := units[0].(Unit); u.Code != "002" || u.Parent != "001" || u.County != 1 || *u.SIRUTA != 1017 {
		t.Fatalf("%+v", u)
	}
	first := accounts[3].(Entry)
	if first.Key() != "|4201" || first.Source != nil {
		t.Fatalf("%+v", first)
	}
	p := accounts[0].(Entry)
	if p.Key() != "A|20A010100" || strings.Join(p.Units, ",") != "002,700" || p.Suffix != "20010101XXXXX" || p.Published != "2005-11-21" {
		t.Fatalf("%+v", p)
	}
	for _, unit := range p.Units {
		if !ValidIBAN(iban(unit, p.Suffix)) {
			t.Fatalf("the IBAN of %s does not rebuild", unit)
		}
	}
}

func TestDatasetStopsWhenAnAccountDiffersOrIsGone(t *testing.T) {
	s := recordedSite()
	renamed := row{profit[0], profit[1], profit[2], "Impozit pe profit"}
	s[Folder+"iban_TREZ000_TREZ700.pdf"] = listPDF("700", "Trezorerie operativa Municipiul Bucuresti", []row{{"A", "Integral de la buget"}, renamed})
	if _, err := Dataset(s.get, places, counties, Published{}); err == nil || !strings.Contains(err.Error(), "A|20A010100") {
		t.Fatalf("a renamed account was taken: %v", err)
	}

	_, err := Dataset(recordedSite().get, places, counties, Published{Units: []string{"002", "003"}, Accounts: []string{"A|20A010100", "A|20A999900"}})
	if err == nil || !strings.Contains(err.Error(), "unit 003") || !strings.Contains(err.Error(), "account A|20A999900") {
		t.Fatalf("error %v", err)
	}
}

func TestPublishedKeysReadTheFilesOfTheDataset(t *testing.T) {
	units, err := PublishedUnits(strings.NewReader("{\"code\":\"002\"}\n{\"code\":\"700\"}\n"))
	if err != nil || strings.Join(units, " ") != "002 700" {
		t.Fatalf("%v %v", units, err)
	}
	accounts, err := PublishedAccounts(strings.NewReader("{\"source\":\"A\",\"account\":\"20A010100\"}\n{\"source\":null,\"account\":\"4201\"}\n"))
	if err != nil || strings.Join(accounts, " ") != "A|20A010100 |4201" {
		t.Fatalf("%v %v", accounts, err)
	}
}
