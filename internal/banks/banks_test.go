package banks

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// The tables are written as BNR writes them: CRLF lines, upper case tags, cells and rows that are not
// closed, a misspelt closing tag, and headers that span two rows.
const institutionHeader = "<THEAD>\r\n<TR>\r\n<TH colSpan=2>Înscrierea în Registru</TH>\r\n<TH rowSpan=2>Denumirea instituţiei de credit</TH>\r\n" +
	"<TH rowSpan=2>Adresa sediului social</TH>\r\n<TH colSpan=3>Elemente de identificare</TH>\r\n" +
	"<TH rowSpan=2>Data de la care poate începe desfășurarea activităţii</TH>\r\n<TH rowSpan=2>Observaţii</TH></TR>\r\n<TR>\r\n" +
	"<TH>Număr</TH>\r\n<TH>Data</TH>\r\n<TH>Cod de identificare fiscală</TH>\r\n<TH>Numărul de ordine în registrul comerţului</TH>\r\n" +
	"<TH>Identificatorul entității juridice (LEI)</TH>\r\n<TR class=num_row>\r\n<TH colSpan=2>1</TH>\r\n<TH>2</TH>\r\n<TH>3</TH>\r\n" +
	"<TH colSpan=3>4</TH>\r\n<TH>5</TH>\r\n<TH>6</TH></TR></THEAD>\r\n"

const branchHeader = "<THEAD>\r\n<TR>\r\n<TH colSpan=2>Înscrierea în Registru</TH>\r\n<TH rowSpan=2>Denumirea sucursalei</TH>\r\n" +
	"<TH rowSpan=2>Adresa sediului sucursalei</TH>\r\n<TH rowSpan=2>Identificatorul entității juridice (LEI)</TH>\r\n" +
	"<TH rowSpan=2>Data de la care poate începe desfășurarea activităţii</TH>\r\n<TH rowSpan=2>Observaţii</TH></TR>\r\n<TR>\r\n" +
	"<TH>Număr</TH>\r\n<TH>Data</TH>\r\n<TR class=num_row>\r\n<TH colSpan=2>1</TH>\r\n<TH>2</TH>\r\n<TH>3</TH>\r\n<TH>4</TH>\r\n<TH>5</TH>\r\n<TH>6</TH></TR></THEAD>\r\n"

const removedHeader = "<THEAD>\r\n<TR>\r\n<TH colSpan=2>Înscrierea în Registru</TH>\r\n<TH rowSpan=2>Denumire entitate</TH>\r\n" +
	"<TH rowSpan=2>Tip entitate*</TH>\r\n<TH colSpan=3>Elemente de identificare</TH>\r\n<TH rowSpan=2>Data radierii</TH>\r\n<TR>\r\n" +
	"<TH>Număr</TH>\r\n<TH>Data</TH>\r\n<TH>Cod de identificare fiscală</TH>\r\n<TH>Numărul de ordine în registrul comerţului</TH>\r\n" +
	"<TH>Identificatorul entității juridice (LEI)</TH>\r\n<TR class=num_row>\r\n<TH colSpan=2>1</TH>\r\n<TH>2</TH>\r\n<TH>3</TH>\r\n" +
	"<TH colSpan=3>4</TH>\r\n<TH>5</TH></TR></THEAD>\r\n"

// table writes one table under its headings. Each row is the list of its cells.
func table(headings []string, header string, rows ...[]string) string {
	var b strings.Builder
	b.WriteString("<div class=\"block-stats \">\r\n<DIV id=IC class=registru>\r\n")
	for _, h := range headings {
		fmt.Fprintf(&b, "<H3>%s</H3>\r\n", h)
	}
	b.WriteString("<TABLE class=\"center max-h\" border=1 cellPadding=5>\r\n<COLGROUP>\r\n<COL class=regno></COLGROUP>\r\n")
	b.WriteString(header)
	b.WriteString("<TBODY>\r\n \r\n")
	for _, r := range rows {
		b.WriteString("<TR>\r\n")
		for _, c := range r {
			fmt.Fprintf(&b, "<TD>%s</TD>\r\n", c)
		}
		b.WriteString("</TR> ")
	}
	b.WriteString("</TBOBY></TBODY></TABLE></div>\r\n<p>* În temeiul art.417. Total: 1. Data generării: 12.11.2026</p>")
	return b.String()
}

const (
	bankLEI   = "549300RG3H390KEL8896"
	alphaLEI  = "529900TKT32Z5LP7XF90"
	ingLEI    = "3TK20IVIUJ8J3ZU0QE75"
	centralLE = "3157006K4C7PJT790450"
)

func tables() map[int]string {
	empty := table([]string{"Secțiunea a III-a - Bănci de credit ipotecar "}, institutionHeader)
	return map[int]string{
		27302: table([]string{"Secțiunea I - Bănci \r\n"}, institutionHeader,
			[]string{"RB-PJR-12-019", "18.02.1999", "BANCA TRANSILVANIA S.A.", "Cluj-Napoca, Calea Dorobantilor, nr.30-36.", "5022670", "J1993004155124", bankLEI, "28.01.1994", "- la data de 28.02.2025 banca a fuzionat prin absorbtie cu OTP Bank Romania S.A."},
			[]string{"RB-PJR-40-011", "18.02.1999", "UNICREDIT BANK S.A.", "Bucuresti, Bdul. Expozitiei nr.1F, sector 1", "361536", "J1991007706408", "5493003BDYD5VPGUQS04", "20.03.1991", ""}),
		27303: table([]string{"Secțiunea a II-a - Bănci de economisire şi creditare în domeniul locativ"}, institutionHeader),
		27304: empty,
		27305: table([]string{"Secțiunea a IV-a - Organizații cooperatiste de credit", "Subsecţiunea I - Case centrale"}, institutionHeader,
			[]string{"ROCC-CC-01-40-0001", "11.11.2002", "BANCA CENTRALA COOPERATISTA CREDITCOOP", "Sector 6, Bucuresti, Calea Plevnei, Nr.200", "361323", "j/40/8830/1996", centralLE, "26.09.2002", ""}) +
			table([]string{"", "Subsecţiunea a II-a - Cooperative de credit"}, institutionHeader,
				[]string{"ROCC-CO-01-08-0135", "11.11.2002", "Banca Cooperatista Unirea Brasov", "Judet Brasov, Brasov, Str. Agriselor, Nr. 1", "R4164626", "J/08/798/1993", "", "26.09.2002", "Pana la data de 25.07.2007 a functionat sub denumirea UNIREA &quot;COOPERATIVA&quot;."}),
		27307: table([]string{"Secțiunea I - Sucursale ale instituţiilor de credit din state terțe"}, branchHeader),
		27308: table([]string{"\r\nSecțiunea II - Sucursale ale instituţiilor de credit din alte state membre\r\n"}, branchHeader,
			[]string{"RB-PJS-40-024", "18.02.1999", "ING BANK N.V. AMSTERDAM - SUCURSALA BUCURESTI", "Bucure?ti, str. Aviator Popisteanu, nr. 54A", "", "22.06.1994", ""}),
		27311: table(nil, removedHeader,
			[]string{"RB-PJR-40-022", "18.02.1999", "ALPHA BANK ROMANIA SA", "1", "5062063", "J40/28415/1993", alphaLEI, "14.10.2025"}),
	}
}

func mappingZip(rows string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("lei-bic-20261030T000000.csv")
	io.WriteString(w, "\xef\xbb\xbfLEI,BIC\r\n"+rows)
	z.Close()
	return b.Bytes()
}

const mappingRows = bankLEI + ",BTRLRO22XXX\r\n" + bankLEI + ",BTRLITR1XXX\r\n" +
	"5493003BDYD5VPGUQS04,BACXROBUXXX\r\n" + alphaLEI + ",BACXROBUXXX\r\n" +
	centralLE + ",CRCOROBUXXX\r\n" + ingLEI + ",INGBROBUXXX\r\n" + ingLEI + ",INGBNL2AXXX\r\n" +
	"2549004BOCU15LMNDJ92,XBSEROB1XXX\r\n"

const mappingList = `{"data": [
 {"type": "bic-lei", "attributes": {"fileName": "LEI-BIC-20260930.zip", "downloadLink": "https://mapping.gleif.org/api/v2/bic-lei/old/download", "valid": true}},
 {"type": "bic-lei", "attributes": {"fileName": "LEI-BIC-20261030.zip", "downloadLink": "https://mapping.gleif.org/api/v2/bic-lei/new/download", "valid": true}},
 {"type": "bic-lei", "attributes": {"fileName": "LEI-BIC-20261130.zip", "downloadLink": "https://mapping.gleif.org/api/v2/bic-lei/broken/download", "valid": false}}
]}`

type site map[string][]byte

func (s site) get(address string) (io.ReadCloser, error) {
	data, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func recordedSite() site {
	s := site{
		Mappings: []byte(mappingList),
		"https://mapping.gleif.org/api/v2/bic-lei/new/download": mappingZip(mappingRows),
	}
	for bid, text := range tables() {
		s[fmt.Sprintf("%s%d", RegisterAddress, bid)] = []byte(text)
	}
	return s
}

func withHeadOffices(t *testing.T, heads map[string]string) {
	t.Helper()
	saved := HeadOffices
	HeadOffices = heads
	t.Cleanup(func() { HeadOffices = saved })
}

func TestReadTableReadsTheTablesOfBNR(t *testing.T) {
	got, err := ReadTable(strings.NewReader(tables()[27305]))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != "cooperative-central" || got[1].Kind != "cooperative" {
		t.Fatalf("%+v", got)
	}
	coop := got[1]
	if *coop.CUI != "R4164626" || coop.LEI != nil || coop.Registered != "2002-11-11" || *coop.Start != "2002-09-26" ||
		*coop.Remarks != `Pana la data de 25.07.2007 a functionat sub denumirea UNIREA "COOPERATIVA".` {
		t.Fatalf("%+v", coop)
	}

	branches, err := ReadTable(strings.NewReader(tables()[27308]))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Kind != "branch-eu" || branches[0].CUI != nil || *branches[0].Address != "Bucure?ti, str. Aviator Popisteanu, nr. 54A" {
		t.Fatalf("%+v", branches)
	}

	removed, err := ReadTable(strings.NewReader(tables()[27311]))
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].Kind != "bank" || *removed[0].Removed != "2025-10-14" || removed[0].Address != nil {
		t.Fatalf("%+v", removed)
	}

	if none, err := ReadTable(strings.NewReader(tables()[27304])); err != nil || len(none) != 0 {
		t.Fatalf("%v %v", none, err)
	}
}

func TestReadTableStopsOnWhatItDoesNotKnow(t *testing.T) {
	row := []string{"RB-PJR-12-019", "18.02.1999", "BANCA", "Cluj", "5022670", "J1", bankLEI, "28.01.1994", ""}
	for name, text := range map[string]string{
		"unknown heading": table([]string{"Secțiunea V - Altele"}, institutionHeader, row),
		"unknown column":  table([]string{"Secțiunea I - Bănci"}, strings.Replace(institutionHeader, "Observaţii", "Capital", 1), row),
		"no kind":         table(nil, institutionHeader, row),
		"unknown kind":    table(nil, removedHeader, []string{"RB-PJR-40-022", "18.02.1999", "ALPHA", "9", "1", "J", alphaLEI, "14.10.2025"}),
		"bad date":        table([]string{"Secțiunea I - Bănci"}, institutionHeader, append(append([]string{}, row[:7]...), "1994-01-28", "")),
		"bad LEI":         table([]string{"Secțiunea I - Bănci"}, institutionHeader, append(append([]string{}, row[:6]...), "X", "28.01.1994", "")),
		"short row":       table([]string{"Secțiunea I - Bănci"}, institutionHeader, row[:8]),
		"not a table":     "<html>",
	} {
		if _, err := ReadTable(strings.NewReader(text)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestReadMappingKeepsTheRomanianBICs(t *testing.T) {
	bics, err := ReadMapping(mappingZip(mappingRows))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(bics[bankLEI], ",") != "BTRLRO22XXX" || strings.Join(bics[ingLEI], ",") != "INGBROBUXXX" || len(bics) != 6 {
		t.Fatalf("%v", bics)
	}
	for name, data := range map[string][]byte{
		"not a zip": []byte("PK"),
		"bad BIC":   mappingZip(bankLEI + ",BT\r\n"),
		"none":      mappingZip(ingLEI + ",INGBNL2AXXX\r\n"),
	} {
		if _, err := ReadMapping(data); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestNewestMappingTakesTheNewestValidTable(t *testing.T) {
	address, date, err := newestMapping(strings.NewReader(mappingList))
	if err != nil || address != "https://mapping.gleif.org/api/v2/bic-lei/new/download" || date != "2026-10-30" {
		t.Fatalf("%s %s %v", address, date, err)
	}
	foreign := strings.Replace(mappingList, "https://mapping.gleif.org/api/v2/bic-lei/new", "https://example.test/new", 1)
	if _, _, err := newestMapping(strings.NewReader(foreign)); err == nil {
		t.Fatal("a table off mapping.gleif.org was taken")
	}
}

func TestCodesJoinsByLEIAndByHeadOffice(t *testing.T) {
	register, err := Register(recordedSite().get)
	if err != nil {
		t.Fatal(err)
	}
	bics, _ := ReadMapping(mappingZip(mappingRows))
	institutions, codes, err := Codes(register, bics, map[string]string{"RB-PJS-40-024": ingLEI})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range codes {
		got = append(got, c.Key())
	}
	want := "BACX|BACXROBUXXX|RB-PJR-40-011 BACX|BACXROBUXXX|RB-PJR-40-022 BTRL|BTRLRO22XXX|RB-PJR-12-019 " +
		"CRCO|CRCOROBUXXX|ROCC-CC-01-40-0001 INGB|INGBROBUXXX|RB-PJS-40-024 TREZ||"
	if strings.Join(got, " ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, " "), want)
	}
	for _, in := range institutions {
		if in.Number == "RB-PJS-40-024" && (in.HeadOffice == nil || *in.HeadOffice != ingLEI) {
			t.Fatalf("head office %+v", in)
		}
	}

	if _, _, err := Codes(register, bics, map[string]string{"RB-PJS-40-099": ingLEI}); err == nil {
		t.Fatal("a head office for a branch not in the register was taken")
	}
	if _, _, err := Codes(register, bics, map[string]string{"RB-PJR-12-019": ingLEI}); err == nil {
		t.Fatal("a head office for a row with a LEI was taken")
	}
	delete(bics, bankLEI)
	if _, _, err := Codes(register, bics, nil); err == nil || !strings.Contains(err.Error(), "RB-PJR-12-019") {
		t.Fatalf("a bank without a code was taken: %v", err)
	}
}

func TestDatasetReadsTheRecordedSources(t *testing.T) {
	withHeadOffices(t, map[string]string{"RB-PJS-40-024": ingLEI})
	d, err := Dataset(recordedSite().get, Published{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != 6 || len(d.Files[1].Records) != 6 {
		t.Fatalf("%d institutions, %d codes", len(d.Files[0].Records), len(d.Files[1].Records))
	}
	if !strings.Contains(d.Source.Licence, "SWIFT © and database rights October 2026.") || !strings.Contains(d.Source.Name, "2026-10-30") {
		t.Fatalf("source %+v", d.Source)
	}
}

func TestDatasetStopsWhenAPublishedEntryIsGone(t *testing.T) {
	withHeadOffices(t, map[string]string{"RB-PJS-40-024": ingLEI})
	_, err := Dataset(recordedSite().get, Published{
		Numbers: []string{"RB-PJR-12-019", "RB-PJR-40-026"},
		Codes:   []string{"BTRL|BTRLRO22XXX|RB-PJR-12-019", "OTPV|OTPVROBUXXX|RB-PJR-40-028"},
	})
	if err == nil || !strings.Contains(err.Error(), "institution RB-PJR-40-026") || !strings.Contains(err.Error(), "code OTPV") {
		t.Fatalf("error %v", err)
	}
}

func TestPublishedKeysReadTheFilesOfTheDataset(t *testing.T) {
	numbers, err := PublishedNumbers(strings.NewReader("{\"number\":\"RB-PJR-12-019\"}\n{\"number\":\"RB-PJR-40-022\"}\n"))
	if err != nil || strings.Join(numbers, " ") != "RB-PJR-12-019 RB-PJR-40-022" {
		t.Fatalf("%v %v", numbers, err)
	}
	codes, err := PublishedCodes(strings.NewReader("{\"code\":\"BTRL\",\"bic\":\"BTRLRO22XXX\",\"number\":\"RB-PJR-12-019\"}\n{\"code\":\"TREZ\",\"bic\":null,\"number\":null}\n"))
	if err != nil || strings.Join(codes, " ") != "BTRL|BTRLRO22XXX|RB-PJR-12-019 TREZ||" {
		t.Fatalf("%v %v", codes, err)
	}
}
