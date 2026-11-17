package mfp

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func code(c int) *int { return &c }

const countiesXML = `<?xml version="1.0" encoding="UTF-8" standalone="no"?><NOM_JUDETE><rand><COD>1</COD><DENUMIRE>ALBA</DENUMIRE><AUTO>AB</AUTO><ORDINE>1</ORDINE></rand><rand><COD>40</COD><DENUMIRE>BUCUREŞTI</DENUMIRE><AUTO>B</AUTO><ORDINE>42</ORDINE></rand></NOM_JUDETE>`

const typesXML = `<?xml version="1.0" encoding="UTF-8" standalone="no"?><NOM_TIP_LOCALITATI><rand><COD>?</COD><DENUMIRE>Tip necunoscut</DENUMIRE></rand><rand><COD>Mun</COD><DENUMIRE>MUNICIPIU</DENUMIRE></rand><rand><COD>MRJ</COD><DENUMIRE>Municipiu Resedinta de Judet</DENUMIRE></rand><rand><COD>Sat</COD><DENUMIRE>SAT</DENUMIRE></rand><rand><COD>Com</COD><DENUMIRE>COMUNĂ</DENUMIRE></rand><rand><COD>Sec</COD><DENUMIRE>SECTOR</DENUMIRE></rand></NOM_TIP_LOCALITATI>`

func locality(county, code int, name, kind string, parent, postal, siruta int) string {
	return localityWith(county, code, name, kind, parent, postal, siruta, "0")
}

func localityWith(county, code int, name, kind string, parent, postal, siruta int, townHall string) string {
	return fmt.Sprintf(`<rand><JUD_COD>%d</JUD_COD><COD>%d</COD><Cod_POLITIE>1</Cod_POLITIE><DENUMIRE>%s</DENUMIRE><TPL_COD>%s</TPL_COD><COD_POSTAL>%d</COD_POSTAL><LOC_COD>%d</LOC_COD><COD_FISCAL_PRIMARIE>%s</COD_FISCAL_PRIMARIE><COD_SIRUTA>%d</COD_SIRUTA></rand>`,
		county, code, name, kind, postal, parent, townHall, siruta)
}

func localities(rows ...string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="no"?><NOM_LOCALITATI>` + strings.Join(rows, "") + `</NOM_LOCALITATI>`
}

var (
	alba = localities(
		locality(1, 7, "Alba Iulia", "MRJ", 0, 0, 1026),
		localityWith(1, 794, "Alba Iulia", "Mun", 0, 0, 1008, "4562583"),
		locality(1, 55, "Bonceşti", "Sat", 177, 515501, 1473),
		locality(1, 177, "Rîmetea", "Com", 0, 0, 0),
		locality(1, 300, "Fără domiciliu în România (AB)", "?", 0, 0, 319000),
		locality(1, 301, "Ada-Kaleh", "Sat", 0, 0, 114113),
	)
	bucharest = localities(
		locality(40, 1, "1", "Sec", 7, 0, 179141),
		locality(40, 8, "7", "Sec", 7, 0, 179203),
	)
	places = []Place{
		{Code: 10, Name: "JUDEȚUL ALBA", Type: 40, County: 1, Level: 1},
		{Code: 1017, Name: "MUNICIPIUL ALBA IULIA", Parent: code(10), Type: 1, County: 1, Level: 2},
		{Code: 1026, Name: "ALBA IULIA", Parent: code(1017), Type: 9, County: 1, Level: 3},
		{Code: 1464, Name: "BONCEȘTI COMUNA", Parent: code(10), Type: 3, County: 1, Level: 2},
		{Code: 1473, Name: "BONCEȘTI", Parent: code(1464), Type: 22, County: 1, Level: 3},
		{Code: 6592, Name: "RAMETEA", Parent: code(10), Type: 3, County: 1, Level: 2},
		{Code: 179132, Name: "MUNICIPIUL BUCUREȘTI", Parent: code(403), Type: 9, County: 40, Level: 2},
		{Code: 179141, Name: "BUCUREȘTI SECTORUL 1", Parent: code(179132), Type: 6, County: 40, Level: 3},
	}
	counties = []SIRUTACounty{{Code: 1, Name: "JUDEȚUL ALBA"}, {Code: 40, Name: "MUNICIPIUL BUCUREȘTI"}}
	day      = time.Date(2026, 11, 17, 2, 52, 53, 0, time.UTC)
)

type site map[string]string

func (s site) get(address string) (io.ReadCloser, error) {
	text, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader([]byte(text))), nil
}

func (s site) modified(address string) (time.Time, error) {
	if address != Counties {
		return time.Time{}, fmt.Errorf("no date for %s", address)
	}
	return day, nil
}

func recordedSite() site {
	return site{
		Counties: countiesXML,
		Types:    typesXML,
		Folder + "nomLocalitati_1_17.11.2026.xml":  alba,
		Folder + "nomLocalitati_40_17.11.2026.xml": bucharest,
	}
}

func TestDatasetMatchesEveryLocalityItCan(t *testing.T) {
	s := recordedSite()
	d, err := Dataset(s.get, s.modified, places, counties, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range d.Files[0].Records {
		l := r.(Locality)
		value := func(p *int) string {
			if p == nil {
				return "-"
			}
			return fmt.Sprint(*p)
		}
		how := "-"
		if l.Match != nil {
			how = *l.Match
		}
		got = append(got, fmt.Sprintf("%s %s %s/%s %s", l.Key(), l.Name, value(l.SIRUTA), value(l.UAT), how))
	}
	want := []string{
		"1|7 Alba Iulia 1026/1017 code",
		"1|55 Bonceşti 1473/1464 code",
		"1|177 Rîmetea 6592/6592 name",
		"1|300 Fără domiciliu în România (AB) -/- -",
		"1|301 Ada-Kaleh -/- -",
		"1|794 Alba Iulia 1017/1017 name",
		"40|1 1 179141/179141 code",
		"40|8 7 -/- -",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if alba := d.Files[0].Records[5].(Locality); alba.Code != 794 || alba.TownHall == nil || *alba.TownHall != "4562583" {
		t.Fatalf("%+v", alba)
	}
	first := d.Files[0].Records[1].(Locality)
	if *first.Parent != 177 || *first.Postal != "515501" || *first.MFPSIRUTA != 1473 {
		t.Fatalf("%+v", first)
	}
	if len(d.Files[1].Records) != 6 || !strings.Contains(d.Description, "Of 8 localities, 3 match by code, 2 by name, and 3 not") {
		t.Fatalf("%d types; %s", len(d.Files[1].Records), d.Description)
	}
}

func TestDatasetStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, change := range map[string]func(site){
		"county of another name": func(s site) { s[Counties] = strings.Replace(countiesXML, "ALBA", "ARAD", 1) },
		"unknown type": func(s site) {
			s[Folder+"nomLocalitati_1_17.11.2026.xml"] = localities(locality(1, 7, "Alba Iulia", "Cart", 0, 0, 1026))
		},
		"row of another county": func(s site) {
			s[Folder+"nomLocalitati_1_17.11.2026.xml"] = localities(locality(2, 7, "Arad", "Mun", 0, 0, 9262))
		},
		"bad town hall code": func(s site) {
			s[Folder+"nomLocalitati_1_17.11.2026.xml"] = localities(localityWith(1, 794, "Alba Iulia", "Mun", 0, 0, 1008, "4562584"))
		},
		"listed twice": func(s site) {
			s[Folder+"nomLocalitati_1_17.11.2026.xml"] = localities(locality(1, 7, "Alba Iulia", "MRJ", 0, 0, 1026), locality(1, 7, "Alba Iulia", "MRJ", 0, 0, 1026))
		},
		"no file of the day": func(s site) { delete(s, Folder+"nomLocalitati_40_17.11.2026.xml") },
		"not a nomenclature": func(s site) { s[Types] = "<html>" },
	} {
		s := recordedSite()
		change(s)
		if _, err := Dataset(s.get, s.modified, places, counties, nil); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

func TestDatasetStopsWhenAPublishedLocalityIsGone(t *testing.T) {
	s := recordedSite()
	_, err := Dataset(s.get, s.modified, places, counties, Published{"1|7", "1|999"})
	if err == nil || !strings.Contains(err.Error(), "1|999") {
		t.Fatalf("error %v", err)
	}
}

func TestPublishedKeysReadTheFile(t *testing.T) {
	keys, err := PublishedKeys(strings.NewReader("{\"county\":1,\"code\":7}\n{\"county\":40,\"code\":1}\n"))
	if err != nil || strings.Join(keys, " ") != "1|7 40|1" {
		t.Fatalf("%v %v", keys, err)
	}
}

func TestValidCUI(t *testing.T) {
	for code, want := range map[string]bool{"4562583": true, "4562584": false, "18547290": true, "1": false, "12a": false} {
		if ValidCUI(code) != want {
			t.Errorf("%s: %v", code, !want)
		}
	}
}
