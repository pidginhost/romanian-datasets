package siruta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

// dbfTable writes a dBase table in the shape INS uses: character fields of fixed width, values padded
// with spaces. deleted marks the rows written as deleted.
func dbfTable(fields []string, widths []int, rows [][]string, deleted ...int) []byte {
	recordLen := 1
	for _, w := range widths {
		recordLen += w
	}
	headerLen := 32 + 32*len(fields) + 1

	var b bytes.Buffer
	head := make([]byte, 32)
	head[0] = 0x03
	binary.LittleEndian.PutUint32(head[4:8], uint32(len(rows)))
	binary.LittleEndian.PutUint16(head[8:10], uint16(headerLen))
	binary.LittleEndian.PutUint16(head[10:12], uint16(recordLen))
	b.Write(head)
	for i, f := range fields {
		d := make([]byte, 32)
		copy(d, f)
		d[11] = 'C'
		d[16] = byte(widths[i])
		b.Write(d)
	}
	b.WriteByte(0x0D)

	gone := map[int]bool{}
	for _, d := range deleted {
		gone[d] = true
	}
	for r, row := range rows {
		if gone[r] {
			b.WriteByte('*')
		} else {
			b.WriteByte(' ')
		}
		for i, v := range row {
			b.WriteString(fmt.Sprintf("%-*s", widths[i], v))
		}
	}
	b.WriteByte(0x1A)
	return b.Bytes()
}

var (
	unitFields = []string{"SIRUTA", "NIV", "SIRSUP", "TIP", "DENLOC", "ULT", "MED", "JUD", "PREFIX", "REGIUNE", "CODP", "FSJ", "FS2", "FS3", "FSL", "FICTIV", "NUTS3", "LAU"}
	unitWidths = []int{6, 1, 6, 2, 33, 1, 1, 2, 1, 1, 6, 2, 5, 7, 5, 1, 6, 10}
)

// unit is one row of siruta.dbf with the fields the build reads; the others are filled as INS fills them.
func unit(code, level, parent, kind int, name, med string, county, region, postal int, nuts3, lau string) []string {
	return []string{
		fmt.Sprint(code), fmt.Sprint(level), fmt.Sprint(parent), fmt.Sprint(kind), name, "0", med,
		fmt.Sprint(county), "0", fmt.Sprint(region), fmt.Sprint(postal), "1", "", "", "1", "0", nuts3, lau,
	}
}

// An extract of the S1 2026 edition: Alba with Alba Iulia and Bărăbanț, a commune whose two copies spell
// the name differently, and Bucharest with a sector.
func extract() (units, counties, regions []byte) {
	units = dbfTable(unitFields, unitWidths, [][]string{
		unit(10, 1, 1, 40, "JUDETUL ALBA", "0", 1, 7, 0, "RO121", ""),
		unit(1017, 2, 10, 1, "MUNICIPIUL ALBA IULIA", "1", 1, 7, 0, "RO121", "RO_1017"),
		unit(1026, 3, 1017, 9, "ALBA IULIA", "1", 1, 7, 510005, "RO121", ""),
		unit(1035, 3, 1017, 10, "BARABANT", "1", 1, 7, 510001, "RO121", ""),
		unit(6592, 2, 10, 3, "RAMETEA", "3", 1, 7, 0, "RO121", "RO_6592"),
		unit(13524, 2, 10, 3, "VALEA MARE-PRAVAT", "3", 1, 7, 0, "RO121", "RO_13524"),
		unit(403, 1, 1, 40, "MUNICIPIUL BUCURESTI", "0", 40, 8, 0, "RO321", ""),
		unit(179132, 2, 403, 9, "MUNICIPIUL BUCURESTI", "1", 40, 8, 0, "RO321", "RO_179132"),
		unit(179141, 3, 179132, 6, "BUCURESTI SECTORUL 1", "1", 40, 8, 10013, "RO321", ""),
	})
	counties = dbfTable([]string{"JUD", "DENJ", "FSJ", "MNEMONIC", "ZONA"}, []int{2, 20, 2, 2, 1}, [][]string{
		{"1", "ALBA", "1", "AB", "7"},
		{"40", "BUCURESTI", "42", "B", "8"},
	})
	regions = dbfTable([]string{"ZONA", "SIRUTA", "DENZONA"}, []int{1, 6, 30}, [][]string{
		{"7", "877", "CENTRU"},
		{"8", "886", "BUCURESTI  - ILFOV"},
	})
	return units, counties, regions
}

// The data.gov.ro copy of the same extract, with the letters, the cedilla and its own spellings.
const markedCSV = "\xef\xbb\xbfSIRUTA;DENLOC;CODP;JUD;SIRSUP;TIP;NIV;MED;REGIUNE;FSJ;NUTS;LAU\r\n" +
	"10;JUDEŢUL ALBA;0;1;1;40;1;0;7;1;RO121;\r\n" +
	"1017;MUNICIPIUL ALBA IULIA;0;1;10;1;2;1;7;1;RO121;RO_1017\r\n" +
	"1026;ALBA IULIA;510005;1;1017;9;3;1;7;1;RO121;\r\n" +
	"1035;BĂRĂBANŢ;510001;1;1017;10;3;1;7;1;RO121;\r\n" +
	"6592;RIMETEA;0;1;10;3;2;3;7;1;RO121;RO_6592\r\n" +
	"13524;VALEA MARE PRAVĂŢ;0;1;10;3;2;3;7;1;RO121;RO_13524\r\n" +
	"403;MUNICIPIUL BUCUREŞTI;0;40;1;40;1;0;8;42;RO321;\r\n" +
	"179132;MUNICIPIUL BUCUREŞTI;0;40;403;9;2;1;8;42;RO321;RO_179132\r\n" +
	"179141;BUCUREŞTI SECTORUL 1;10013;40;179132;6;3;1;8;42;RO321;\r\n"

// rar5 writes a RAR 5 archive that stores the files without compression, as the RAR 5.0 technical note
// describes it: signature, main header, one file header and data for each file, end header.
func rar5(files map[string][]byte, order ...string) []byte {
	vint := func(b *bytes.Buffer, v uint64) {
		for {
			c := byte(v & 0x7F)
			v >>= 7
			if v != 0 {
				b.WriteByte(c | 0x80)
				continue
			}
			b.WriteByte(c)
			return
		}
	}
	block := func(out *bytes.Buffer, body []byte) {
		// The header size counts from the header type; it fits one byte for these small headers.
		var sized bytes.Buffer
		vint(&sized, uint64(len(body)))
		sized.Write(body)
		crc := make([]byte, 4)
		binary.LittleEndian.PutUint32(crc, crc32.ChecksumIEEE(sized.Bytes()))
		out.Write(crc)
		out.Write(sized.Bytes())
	}

	var out bytes.Buffer
	out.Write([]byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00})

	var main bytes.Buffer
	vint(&main, 1) // main archive header
	vint(&main, 0) // header flags
	vint(&main, 0) // archive flags
	block(&out, main.Bytes())

	for _, name := range order {
		data := files[name]
		var h bytes.Buffer
		vint(&h, 2)                 // file header
		vint(&h, 0x0002)            // a data area follows
		vint(&h, uint64(len(data))) // data size
		vint(&h, 0x0004)            // file flags: data CRC32 present
		vint(&h, uint64(len(data))) // unpacked size
		vint(&h, 0x20)              // attributes
		crc := make([]byte, 4)
		binary.LittleEndian.PutUint32(crc, crc32.ChecksumIEEE(data))
		h.Write(crc)
		vint(&h, 0) // compression: version 0, method 0, stored
		vint(&h, 0) // host OS: Windows
		vint(&h, uint64(len(name)))
		h.WriteString(name)
		block(&out, h.Bytes())
		out.Write(data)
	}

	var end bytes.Buffer
	vint(&end, 5) // end of archive
	vint(&end, 0)
	vint(&end, 0)
	block(&out, end.Bytes())
	return out.Bytes()
}

// site serves recorded files by address and counts the requests.
type site map[string][]byte

func (s site) get(address string) (io.ReadCloser, error) {
	data, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

const recordedPage = `<html><body>
<a href="https://insse.ro/cms/files/siruta/2025/SIRUTA_AN_2025.rar">Descarca SIRUTA AN</a>
<a href="https://insse.ro/cms/files/siruta/2026/SIRUTA_S1_2026.rar">Descarca SIRUTA SEMESTRU</a>
<a href="https://insse.ro/cms/files/siruta/2026/Metodologie.doc">Metodologie</a>
</body></html>`

func catalogueAnswer(packages ...string) []byte {
	return []byte(`{"success": true, "result": {"count": 2, "results": [` + strings.Join(packages, ",") + `]}}`)
}

func pkg(name, created string, resources ...string) string {
	var rs []string
	for i := 0; i+1 < len(resources); i += 2 {
		rs = append(rs, fmt.Sprintf(`{"format": %q, "url": %q}`, resources[i], resources[i+1]))
	}
	return fmt.Sprintf(`{"name": %q, "metadata_created": %q, "resources": [%s]}`, name, created, strings.Join(rs, ","))
}

// recordedSite is the INS page and archive, the data.gov.ro catalogue, an older CSV that is a spreadsheet,
// and the CSV of the newest edition.
func recordedSite() site {
	units, counties, regions := extract()
	archive := rar5(map[string][]byte{
		"siruta.dbf": units, "JUD.DBF": counties, "ZONE.DBF": regions, "Metodologie.doc": []byte("not read"),
	}, "ZONE.DBF", "JUD.DBF", "Metodologie.doc", "siruta.dbf")
	return site{
		Page: []byte(recordedPage),
		"https://insse.ro/cms/files/siruta/2026/SIRUTA_S1_2026.rar": archive,
		Catalogue: catalogueAnswer(
			pkg("siruta-2025", "2025-07-01T07:00:00", "XML", "https://data.gov.ro/legenda.xml", "CSV", "https://data.gov.ro/siruta_s1_2025.csv"),
			pkg("siruta_s1-2026", "2026-07-24T07:00:00", "CSV", "https://data.gov.ro/siruta_s1_2026.csv"),
		),
		"https://data.gov.ro/siruta_s1_2025.csv": []byte("PK\x03\x04 a spreadsheet named .csv"),
		"https://data.gov.ro/siruta_s1_2026.csv": []byte(markedCSV),
	}
}
