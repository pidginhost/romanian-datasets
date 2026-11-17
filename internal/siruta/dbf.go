package siruta

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// readDBF reads a dBase table as INS writes it: a header, one 32-byte descriptor for each field ended by
// 0x0D, then fixed-length records, each starting with a byte that is '*' when the record is deleted.
// Values are trimmed of spaces. INS writes only ASCII; anything else is refused, so a change in the
// files is seen instead of read wrong.
func readDBF(b []byte) ([]map[string]string, error) {
	if len(b) < 33 {
		return nil, fmt.Errorf("too short for a dBase table")
	}
	count := int(binary.LittleEndian.Uint32(b[4:8]))
	headerLen := int(binary.LittleEndian.Uint16(b[8:10]))
	recordLen := int(binary.LittleEndian.Uint16(b[10:12]))

	type field struct {
		name   string
		length int
	}
	var fields []field
	width := 1
	for at := 32; ; at += 32 {
		if at >= len(b) || at >= headerLen {
			return nil, fmt.Errorf("the field descriptors have no end")
		}
		if b[at] == 0x0D {
			break
		}
		if at+32 > len(b) {
			return nil, fmt.Errorf("a field descriptor is cut short")
		}
		name, _, _ := strings.Cut(string(b[at:at+11]), "\x00")
		fields = append(fields, field{strings.TrimSpace(name), int(b[at+16])})
		width += int(b[at+16])
	}
	switch {
	case len(fields) == 0:
		return nil, fmt.Errorf("the table has no fields")
	case width != recordLen:
		return nil, fmt.Errorf("the fields take %d bytes, the records %d", width, recordLen)
	case headerLen+count*recordLen > len(b):
		return nil, fmt.Errorf("%d records of %d bytes do not fit in the file", count, recordLen)
	}

	var rows []map[string]string
	for i := 0; i < count; i++ {
		record := b[headerLen+i*recordLen : headerLen+(i+1)*recordLen]
		if record[0] == '*' {
			continue
		}
		row := map[string]string{}
		at := 1
		for _, f := range fields {
			value := record[at : at+f.length]
			for _, c := range value {
				if c >= 0x80 {
					return nil, fmt.Errorf("record %d, field %s: not ASCII", i+1, f.name)
				}
			}
			row[f.name] = strings.TrimSpace(string(value))
			at += f.length
		}
		rows = append(rows, row)
	}
	return rows, nil
}
