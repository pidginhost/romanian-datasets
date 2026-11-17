package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"unicode/utf16"
)

func rec(kind uint16, data []byte) []byte {
	b := make([]byte, 4, 4+len(data))
	binary.LittleEndian.PutUint16(b, kind)
	binary.LittleEndian.PutUint16(b[2:], uint16(len(data)))
	return append(b, data...)
}

func u16(v ...int) []byte {
	var b []byte
	for _, x := range v {
		b = binary.LittleEndian.AppendUint16(b, uint16(x))
	}
	return b
}

func wide(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

// workbookStream writes a BIFF8 stream with one sheet, named Anexa 3. The shared strings are "Capitol" and
// "Bistrița", the second split between the SST record and a CONTINUE record. The sheet holds a shared
// string, a wide label, a number, an RK integer, an RK with the /100 flag, a MULRK run and a formula with a
// string result. pad adds an unknown record after the sheet to make the stream larger.
func workbookStream(pad int) []byte {
	var globals []byte
	globals = append(globals, rec(recBOF, append(u16(0x0600, 0x0005), make([]byte, 12)...))...)
	sheetName := []byte("Anexa 3")
	boundAt := len(globals)
	bound := append(make([]byte, 6), byte(len(sheetName)), 0)
	globals = append(globals, rec(recBoundSheet, append(bound, sheetName...))...)
	// SST: total 2, unique 2; "Capitol" compressed; "Bistrița" wide, 3 characters, then the rest.
	sst := binary.LittleEndian.AppendUint32(nil, 2)
	sst = binary.LittleEndian.AppendUint32(sst, 2)
	sst = append(sst, u16(7)...)
	sst = append(sst, 0)
	sst = append(sst, []byte("Capitol")...)
	sst = append(sst, u16(8)...)
	sst = append(sst, 1)
	sst = append(sst, wide("Bis")...)
	globals = append(globals, rec(recSST, sst)...)
	globals = append(globals, rec(recContinue, append([]byte{1}, wide("trița")...))...)
	globals = append(globals, rec(0x000A, nil)...)

	sheetAt := len(globals)
	binary.LittleEndian.PutUint32(globals[boundAt+4:], uint32(sheetAt))
	var s []byte
	s = append(s, rec(recBOF, append(u16(0x0600, 0x0010), make([]byte, 12)...))...)
	s = append(s, rec(recLabelSST, append(u16(4, 0, 15), binary.LittleEndian.AppendUint32(nil, 0)...))...)
	s = append(s, rec(recLabelSST, append(u16(5, 3, 15), binary.LittleEndian.AppendUint32(nil, 1)...))...)
	label := append(u16(6, 3, 15, 4), 1)
	s = append(s, rec(recLabel, append(label, wide("Șase")...))...)
	s = append(s, rec(recNumber, append(u16(7, 0, 15), binary.LittleEndian.AppendUint64(nil, math.Float64bits(2026))...))...)
	s = append(s, rec(recRK, append(u16(7, 1, 15), binary.LittleEndian.AppendUint32(nil, 7<<2|2)...))...)
	s = append(s, rec(recRK, append(u16(7, 2, 15), binary.LittleEndian.AppendUint32(nil, 1250<<2|3)...))...)
	mul := u16(8, 0)
	for _, v := range []int{1, 2} {
		mul = append(mul, u16(15)...)
		mul = binary.LittleEndian.AppendUint32(mul, uint32(v<<2|2))
	}
	s = append(s, rec(recMulRK, append(mul, u16(1)...))...)
	formula := append(u16(9, 0, 15), 0, 0, 0, 0, 0, 0, 0xFF, 0xFF)
	s = append(s, rec(recFormula, append(formula, make([]byte, 8)...))...)
	s = append(s, rec(recString, append(append(u16(5), 0), []byte("01.01")...))...)
	s = append(s, rec(0x000A, nil)...)
	s = append(s, rec(0x00E5, make([]byte, pad))...)
	return append(globals, s...)
}

// compoundFile wraps a stream as the Workbook of a compound file with 512-byte sectors: in its own sectors
// when it is 4096 bytes or more, in the mini stream otherwise.
func compoundFile(stream []byte) []byte {
	le := binary.LittleEndian
	const end, free, fatSect = 0xFFFFFFFE, 0xFFFFFFFF, 0xFFFFFFFD
	sector := func(b []byte) []byte {
		out := make([]byte, ((len(b)+511)/512)*512)
		copy(out, b)
		if len(out) == 0 {
			out = make([]byte, 512)
		}
		return out
	}
	fat := []uint32{fatSect, end}
	var body []byte
	rootStart, rootSize := uint32(end), uint32(0)
	bookStart := uint32(2)
	miniFATStart, miniFATCount := uint32(end), uint32(0)
	if len(stream) >= 4096 {
		data := sector(stream)
		n := len(data) / 512
		for i := 0; i < n; i++ {
			if i == n-1 {
				fat = append(fat, end)
			} else {
				fat = append(fat, uint32(3+i))
			}
		}
		body = data
	} else {
		mini := make([]byte, ((len(stream)+63)/64)*64)
		copy(mini, stream)
		container := sector(mini)
		n := len(container) / 512
		for i := 0; i < n; i++ {
			if i == n-1 {
				fat = append(fat, end)
			} else {
				fat = append(fat, uint32(3+i))
			}
		}
		var miniFAT []byte
		for i := 0; i < len(mini)/64; i++ {
			next := uint32(i + 1)
			if i == len(mini)/64-1 {
				next = end
			}
			miniFAT = le.AppendUint32(miniFAT, next)
		}
		miniFATStart, miniFATCount = uint32(2+n), 1
		fat = append(fat, end)
		body = append(container, sector(miniFAT)...)
		rootStart, rootSize = 2, uint32(len(mini))
		bookStart = 0
	}
	for len(fat) < 128 {
		fat = append(fat, free)
	}
	var fatSector []byte
	for _, v := range fat {
		fatSector = le.AppendUint32(fatSector, v)
	}
	entry := func(name string, kind byte, start, size uint32, child uint32) []byte {
		e := make([]byte, 128)
		n := wide(name)
		copy(e, n)
		le.PutUint16(e[0x40:], uint16(len(n)+2))
		e[0x42] = kind
		le.PutUint32(e[0x44:], free)
		le.PutUint32(e[0x48:], free)
		le.PutUint32(e[0x4C:], child)
		le.PutUint32(e[0x74:], start)
		le.PutUint32(e[0x78:], size)
		return e
	}
	dir := append(entry("Root Entry", 5, rootStart, rootSize, 1), entry("Workbook", 2, bookStart, uint32(len(stream)), free)...)
	dir = append(dir, make([]byte, 256)...)

	header := make([]byte, 512)
	copy(header, signature)
	le.PutUint16(header[0x18:], 0x3E)
	le.PutUint16(header[0x1A:], 3)
	le.PutUint16(header[0x1C:], 0xFFFE)
	le.PutUint16(header[0x1E:], 9)
	le.PutUint16(header[0x20:], 6)
	le.PutUint32(header[0x2C:], 1)
	le.PutUint32(header[0x30:], 1)
	le.PutUint32(header[0x38:], 4096)
	le.PutUint32(header[0x3C:], miniFATStart)
	le.PutUint32(header[0x40:], miniFATCount)
	le.PutUint32(header[0x44:], end)
	le.PutUint32(header[0x4C:], 0)
	for i := 1; i < 109; i++ {
		le.PutUint32(header[0x4C+4*i:], free)
	}
	out := append(header, fatSector...)
	out = append(out, dir...)
	return append(out, body...)
}

func checkSheet(t *testing.T, sheets []Sheet) {
	t.Helper()
	if len(sheets) != 1 || sheets[0].Name != "Anexa 3" {
		t.Fatalf("%+v", sheets)
	}
	s := sheets[0]
	for _, c := range []struct {
		row, col int
		want     string
	}{
		{4, 0, "Capitol"}, {5, 3, "Bistrița"}, {6, 3, "Șase"}, {7, 0, "2026"}, {7, 1, "7"}, {7, 2, "12.5"},
		{8, 0, "1"}, {8, 1, "2"}, {9, 0, "01.01"}, {0, 0, ""}, {99, 9, ""},
	} {
		if got := s.Cell(c.row, c.col); got != c.want {
			t.Errorf("cell %d,%d: %q, want %q", c.row, c.col, got, c.want)
		}
	}
}

func TestReadXLSFromItsOwnSectorsAndFromTheMiniStream(t *testing.T) {
	for name, pad := range map[string]int{"own sectors": 4096, "mini stream": 0} {
		sheets, err := Read(compoundFile(workbookStream(pad)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkSheet(t, sheets)
	}
}

func TestReadXLSStopsOnABrokenFile(t *testing.T) {
	good := compoundFile(workbookStream(4096))
	for name, data := range map[string][]byte{
		"not a compound file": []byte("not a workbook at all"),
		"cut short":           good[:1500],
	} {
		if _, err := Read(data); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func xlsx(files map[string]string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, content := range files {
		w, _ := z.Create(name)
		w.Write([]byte(content))
	}
	z.Close()
	return b.Bytes()
}

var book = map[string]string{
	"xl/workbook.xml": `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<sheets><sheet name="Anexa 3" sheetId="1" r:id="rId1"/></sheets></workbook>`,
	"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
	"xl/sharedStrings.xml": `<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">` +
		`<si><t>Capitol</t></si><si><r><t>Bis</t></r><r><rPr><b/></rPr><t>trița</t></r><rPh><t>ignored</t></rPh></si></sst>`,
	"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="5"><c r="A5" t="s"><v>0</v></c></row>` +
		`<row r="6"><c r="D6" t="s"><v>1</v></c></row>` +
		`<row r="7"><c r="D7" t="inlineStr"><is><t>Șase</t></is></c></row>` +
		`<row r="8"><c r="A8"><v>2026</v></c><c r="B8" t="n"><v>7</v></c><c r="C8"><v>12.50</v></c></row>` +
		`<row r="9"><c r="A9"><v>1</v></c><c r="B9"><v>2</v></c><c r="C9" s="3"/></row>` +
		`<row r="10"><c r="A10" t="str"><f>A1</f><v>01.01</v></c></row>` +
		`</sheetData></worksheet>`,
}

func TestReadXLSX(t *testing.T) {
	sheets, err := Read(xlsx(book))
	if err != nil {
		t.Fatal(err)
	}
	checkSheet(t, sheets)
}

func TestReadXLSXStopsOnABrokenBook(t *testing.T) {
	broken := map[string]string{}
	for k, v := range book {
		broken[k] = v
	}
	broken["xl/worksheets/sheet1.xml"] = strings.Replace(book["xl/worksheets/sheet1.xml"], `<v>1</v></c></row>`, `<v>9</v></c></row>`, 1)
	if _, err := Read(xlsx(broken)); err == nil {
		t.Error("a cell referring to a missing shared string was read")
	}
	delete(broken, "xl/workbook.xml")
	if _, err := Read(xlsx(broken)); err == nil {
		t.Error("a book without a workbook part was read")
	}
}
