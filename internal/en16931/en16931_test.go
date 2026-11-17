package en16931

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// gc writes a genericode list in the shape of the Commission files. Each row is code, name, remark;
// an empty name or remark is left out of the row, as the files do.
func gc(columns []string, rows ...[3]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><gc:CodeList xmlns:gc="http://docs.oasis-open.org/codelist/ns/genericode/1.0/">`)
	b.WriteString(`<Identification><ShortName>Test</ShortName><Version>2026-11-15</Version></Identification><ColumnSet>`)
	for _, c := range columns {
		fmt.Fprintf(&b, `<Column Id=%q Use="optional"><ShortName>%s</ShortName></Column>`, c, c)
	}
	b.WriteString(`</ColumnSet><SimpleCodeList>`)
	for _, r := range rows {
		b.WriteString(`<Row>`)
		for i, v := range r {
			if v != "" {
				fmt.Fprintf(&b, `<Value ColumnRef=%q><SimpleValue>%s</SimpleValue></Value>`, []string{"Code", "Name", "Remark"}[i], v)
			}
		}
		b.WriteString(`</Row>`)
	}
	b.WriteString(`</SimpleCodeList></gc:CodeList>`)
	return b.String()
}

var codeName = []string{"Code", "Name"}

// archive writes a zip with every list of Lists and Left: the given ones as given, the others with one code.
func archive(lists map[string]string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	members := []string{"Country.gc"}
	for _, l := range Lists {
		members = append(members, l.Member)
	}
	for _, member := range members {
		text, ok := lists[member]
		if !ok {
			text = gc(codeName, [3]string{"X", "Something", ""})
		}
		w, _ := z.Create(member)
		io.WriteString(w, text)
	}
	z.Close()
	return b.Bytes()
}

func attachmentPage(next string, titles ...string) string {
	var rs []string
	for _, t := range titles {
		rs = append(rs, fmt.Sprintf(`{"title": %q, "_links": {"download": "/download/attachments/467108974/%s?version=1&api=v2"}}`, t, t))
	}
	links := `{}`
	if next != "" {
		links = fmt.Sprintf(`{"next": %q}`, next)
	}
	return `{"results": [` + strings.Join(rs, ",") + `], "_links": ` + links + `}`
}

type site map[string][]byte

func (s site) get(address string) (io.ReadCloser, error) {
	data, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

const (
	older = "EN16931 code lists values - genericodes -used from 2021-05-17.zip"
	newer = "digital-genericodes-2026-11-15.zip"
	page2 = "/rest/api/content/467108974/child/attachment?limit=200&start=200"
)

// recordedSite has two pages of attachments, a spreadsheet that is not read, and two archives: HRK leaves
// the currencies and RON changes its name.
func recordedSite() site {
	return site{
		Attachments:  []byte(attachmentPage(page2, newer, "EN16931 code lists values v18 - used from 2026-11-15.xlsx")),
		Site + page2: []byte(attachmentPage("", older)),
		Site + "/download/attachments/467108974/" + older + "?version=1&api=v2": archive(map[string]string{
			"Currency.gc": gc(codeName, [3]string{"HRK", "Kuna", ""}, [3]string{"RON", "Leu", ""}),
		}),
		Site + "/download/attachments/467108974/" + newer + "?version=1&api=v2": archive(map[string]string{
			"Currency.gc": gc(codeName, [3]string{"RON", "Romanian Leu", ""}, [3]string{"VES", "Bolívar Soberano", ""}),
			"MIME.gc":     gc([]string{"Code"}, [3]string{"application/pdf", "", ""}),
		}),
	}
}

func TestReadAttachmentsFindsTheArchivesOfBothNames(t *testing.T) {
	got, next, err := readAttachments(strings.NewReader(attachmentPage(page2, newer, older, "VATEX - version 9.xlsx")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Used != "2026-11-15" || got[1].Used != "2021-05-17" || next != Site+page2 {
		t.Fatalf("archives %+v next %s", got, next)
	}
	foreign := `{"results": [{"title": "cef-genericodes-2024-05-15.zip", "_links": {"download": "https://example.test/x.zip"}}]}`
	if _, _, err := readAttachments(strings.NewReader(foreign)); err == nil {
		t.Fatal("an archive off the registry page was taken")
	}
}

func TestReadListReadsTheColumnsAndRefusesSurprises(t *testing.T) {
	codes, err := readList(strings.NewReader(gc([]string{"Code", "Name", "Remark"}, [3]string{"S", "Standard  rate", "Standard rate"}, [3]string{"Z", "Zero rated goods", ""})))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 || codes[0] != (Code{"S", "Standard rate", "Standard rate"}) || codes[1].Remark != "" {
		t.Fatalf("codes %+v", codes)
	}
	for name, text := range map[string]string{
		"unknown column": gc([]string{"Code", "Status"}, [3]string{"S", "", ""}),
		"no code column": gc([]string{"Name"}, [3]string{"", "Standard", ""}),
		"twice":          gc(codeName, [3]string{"S", "A", ""}, [3]string{"S", "B", ""}),
		"empty":          gc(codeName),
		"not XML":        "PK",
	} {
		if _, err := readList(strings.NewReader(text)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestReadArchiveWantsExactlyTheKnownLists(t *testing.T) {
	lists, err := readArchive(archive(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := lists["Country.gc"]; kept || len(lists) != len(Lists) {
		t.Fatalf("lists kept: %d, countries kept: %v", len(lists), kept)
	}
	if _, err := readArchive(archive(map[string]string{"Country.gc": "not a list"})); err == nil {
		t.Fatal("a broken list that is left out was not seen")
	}

	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("Currency.gc")
	io.WriteString(w, gc(codeName, [3]string{"RON", "Leu", ""}))
	z.Close()
	if _, err := readArchive(b.Bytes()); err == nil {
		t.Fatal("an archive without the other lists was read")
	}

	b.Reset()
	z = zip.NewWriter(&b)
	for _, name := range append([]string{"Newlist.gc"}, "Currency.gc") {
		w, _ := z.Create(name)
		io.WriteString(w, gc(codeName, [3]string{"A", "B", ""}))
	}
	z.Close()
	if _, err := readArchive(b.Bytes()); err == nil || !strings.Contains(err.Error(), "Newlist.gc") {
		t.Fatalf("an unknown list was not named: %v", err)
	}
}

func TestPeriodsEndACodeTheDayBeforeTheArchiveThatDropsIt(t *testing.T) {
	used := []string{"2021-05-17", "2024-11-15", "2025-05-15"}
	lists := [][]Code{
		{{"HRK", "Kuna", ""}, {"RON", "Leu", ""}, {"CUC", "Peso", ""}},
		{{"RON", "Romanian Leu", ""}, {"CUC", "Peso", ""}},
		{{"RON", "Romanian Leu", ""}, {"HRK", "Kuna", "back"}},
	}
	entries, err := Periods(used, lists)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		to := "open"
		if e.ValidTo != nil {
			to = *e.ValidTo
		}
		remark := "-"
		if e.Remark != nil {
			remark = *e.Remark
		}
		got = append(got, fmt.Sprintf("%s %s %s %s..%s", e.Code, *e.Name, remark, e.ValidFrom, to))
	}
	want := "CUC Peso - 2021-05-17..2025-05-14 | HRK Kuna - 2021-05-17..2024-11-14 | HRK Kuna back 2025-05-15..open | RON Romanian Leu - 2021-05-17..open"
	if strings.Join(got, " | ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, " | "), want)
	}
}

func TestDatasetReadsEveryArchive(t *testing.T) {
	d, err := Dataset(recordedSite().get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != len(Lists) {
		t.Fatalf("%d files", len(d.Files))
	}
	var currencies, mime []any
	for _, f := range d.Files {
		switch f.Name {
		case "currencies.jsonl":
			currencies = f.Records
		case "mime-codes.jsonl":
			mime = f.Records
		}
	}
	lines := func(records []any) string {
		var out []string
		for _, r := range records {
			e := r.(Entry)
			to, name := "open", "-"
			if e.ValidTo != nil {
				to = *e.ValidTo
			}
			if e.Name != nil {
				name = *e.Name
			}
			out = append(out, fmt.Sprintf("%s %s %s..%s", e.Code, name, e.ValidFrom, to))
		}
		return strings.Join(out, " | ")
	}
	if got := lines(currencies); got != "HRK Kuna 2021-05-17..2026-11-14 | RON Romanian Leu 2021-05-17..open | VES Bolívar Soberano 2026-11-15..open" {
		t.Fatalf("currencies %s", got)
	}
	// MIME has no Name column; X was in the first archive only.
	if got := lines(mime); got != "X Something 2021-05-17..2026-11-14 | application/pdf - 2026-11-15..open" {
		t.Fatalf("mime %s", got)
	}
	if !strings.Contains(d.Source.Name, "used from 2021-05-17 to 2026-11-15") {
		t.Fatalf("source %s", d.Source.Name)
	}
}

func TestDatasetStopsWhenAPublishedCodeIsGone(t *testing.T) {
	_, err := Dataset(recordedSite().get, map[string][]string{"currencies.jsonl": {"HRK", "RON", "ZWL"}})
	if err == nil || !strings.Contains(err.Error(), "ZWL") {
		t.Fatalf("error %v", err)
	}
}

func TestPublishedCodesReadsAFileOfTheDataset(t *testing.T) {
	codes, err := PublishedCodes(strings.NewReader("{\"code\":\"HRK\",\"name\":\"Kuna\"}\n{\"code\":\"RON\"}\n"))
	if err != nil || strings.Join(codes, ",") != "HRK,RON" {
		t.Fatalf("%v %v", codes, err)
	}
}
