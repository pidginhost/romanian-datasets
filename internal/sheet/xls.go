package sheet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
)

// The legacy Excel format is a Compound File (OLE2) that holds a stream named Workbook, a series of BIFF8
// records. This reader knows the records that carry cell values as text or numbers, and nothing else.

var signature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

const (
	endOfChain = 0xFFFFFFFE
	freeSector = 0xFFFFFFFF
)

type compound struct {
	data       []byte
	sector     int
	miniSector int
	fat        []uint32
	miniFAT    []uint32
	miniStream []byte
	cutoff     uint32
}

func (c *compound) at(sid uint32) ([]byte, error) {
	start := (int(sid) + 1) * c.sector
	if sid >= endOfChain || start+c.sector > len(c.data) {
		return nil, fmt.Errorf("sector %d is out of the file", sid)
	}
	return c.data[start : start+c.sector], nil
}

// chain reads the sectors of a chain from start.
func (c *compound) chain(start uint32, table []uint32, read func(uint32) ([]byte, error)) ([]byte, error) {
	var out []byte
	seen := map[uint32]bool{}
	for sid := start; sid != endOfChain; {
		if seen[sid] || int(sid) >= len(table) {
			return nil, fmt.Errorf("a broken sector chain")
		}
		seen[sid] = true
		b, err := read(sid)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
		sid = table[sid]
	}
	return out, nil
}

func openCompound(data []byte) (*compound, error) {
	if len(data) < 512 || !bytes.Equal(data[:8], signature) {
		return nil, fmt.Errorf("not a compound file")
	}
	le := binary.LittleEndian
	c := &compound{data: data, sector: 1 << le.Uint16(data[0x1E:]), miniSector: 1 << le.Uint16(data[0x20:]), cutoff: le.Uint32(data[0x38:])}
	if c.sector != 512 && c.sector != 4096 {
		return nil, fmt.Errorf("a sector size of %d", c.sector)
	}
	// The sectors of the FAT: 109 in the header, the rest in the DIFAT chain.
	var fatSectors []uint32
	for i := 0; i < 109; i++ {
		if s := le.Uint32(data[0x4C+4*i:]); s != freeSector {
			fatSectors = append(fatSectors, s)
		}
	}
	for sid, n := le.Uint32(data[0x44:]), le.Uint32(data[0x48:]); n > 0 && sid != endOfChain; n-- {
		b, err := c.at(sid)
		if err != nil {
			return nil, err
		}
		for i := 0; i < c.sector/4-1; i++ {
			if s := le.Uint32(b[4*i:]); s != freeSector {
				fatSectors = append(fatSectors, s)
			}
		}
		sid = le.Uint32(b[c.sector-4:])
	}
	for _, s := range fatSectors {
		b, err := c.at(s)
		if err != nil {
			return nil, err
		}
		for i := 0; i < len(b); i += 4 {
			c.fat = append(c.fat, le.Uint32(b[i:]))
		}
	}
	if miniStart := le.Uint32(data[0x3C:]); miniStart != endOfChain {
		b, err := c.chain(miniStart, c.fat, c.at)
		if err != nil {
			return nil, err
		}
		for i := 0; i+4 <= len(b); i += 4 {
			c.miniFAT = append(c.miniFAT, le.Uint32(b[i:]))
		}
	}
	return c, nil
}

// stream gives the stream of a name in the root storage.
func (c *compound) stream(names ...string) ([]byte, error) {
	le := binary.LittleEndian
	dir, err := c.chain(le.Uint32(c.data[0x30:]), c.fat, c.at)
	if err != nil {
		return nil, err
	}
	type entry struct {
		name  string
		kind  byte
		start uint32
		size  uint32
	}
	var entries []entry
	for i := 0; i+128 <= len(dir); i += 128 {
		e := dir[i : i+128]
		n := int(le.Uint16(e[0x40:]))
		if n < 2 || n > 64 {
			continue
		}
		units := make([]uint16, n/2-1)
		for k := range units {
			units[k] = le.Uint16(e[2*k:])
		}
		entries = append(entries, entry{string(utf16.Decode(units)), e[0x42], le.Uint32(e[0x74:]), le.Uint32(e[0x78:])})
	}
	if len(entries) == 0 || entries[0].kind != 5 {
		return nil, fmt.Errorf("the compound file has no root")
	}
	root := entries[0]
	for _, want := range names {
		for _, e := range entries {
			if e.kind != 2 || e.name != want {
				continue
			}
			if e.size >= c.cutoff {
				b, err := c.chain(e.start, c.fat, c.at)
				if err != nil {
					return nil, err
				}
				if int(e.size) > len(b) {
					return nil, fmt.Errorf("stream %s is shorter than its size", want)
				}
				return b[:e.size], nil
			}
			if c.miniStream == nil {
				if c.miniStream, err = c.chain(root.start, c.fat, c.at); err != nil {
					return nil, err
				}
			}
			b, err := c.chain(e.start, c.miniFAT, func(sid uint32) ([]byte, error) {
				start := int(sid) * c.miniSector
				if start+c.miniSector > len(c.miniStream) {
					return nil, fmt.Errorf("mini sector %d is out of the stream", sid)
				}
				return c.miniStream[start : start+c.miniSector], nil
			})
			if err != nil {
				return nil, err
			}
			if int(e.size) > len(b) {
				return nil, fmt.Errorf("stream %s is shorter than its size", want)
			}
			return b[:e.size], nil
		}
	}
	return nil, fmt.Errorf("the compound file has no %v stream", names)
}

// BIFF8 record types this reader uses.
const (
	recBOF        = 0x0809
	recBoundSheet = 0x0085
	recSST        = 0x00FC
	recContinue   = 0x003C
	recLabelSST   = 0x00FD
	recLabel      = 0x0204
	recNumber     = 0x0203
	recRK         = 0x027E
	recMulRK      = 0x00BD
	recFormula    = 0x0006
	recString     = 0x0207
)

type record struct {
	kind   uint16
	offset int
	data   []byte
}

func records(stream []byte) ([]record, error) {
	var out []record
	for i := 0; i+4 <= len(stream); {
		kind := binary.LittleEndian.Uint16(stream[i:])
		size := int(binary.LittleEndian.Uint16(stream[i+2:]))
		if i+4+size > len(stream) {
			return nil, fmt.Errorf("a record runs past the stream")
		}
		out = append(out, record{kind, i, stream[i+4 : i+4+size]})
		i += 4 + size
	}
	return out, nil
}

// pieces reads data that may run over CONTINUE records: a string that goes on in the next piece starts
// that piece with a new flags byte.
type pieces struct {
	parts [][]byte
	part  int
	pos   int
}

func (p *pieces) need(n int) error {
	for p.part < len(p.parts) && p.pos == len(p.parts[p.part]) {
		p.part++
		p.pos = 0
	}
	if p.part >= len(p.parts) || p.pos+n > len(p.parts[p.part]) {
		return fmt.Errorf("the shared strings end early")
	}
	return nil
}

func (p *pieces) bytes(n int) ([]byte, error) {
	if err := p.need(n); err != nil {
		return nil, err
	}
	b := p.parts[p.part][p.pos : p.pos+n]
	p.pos += n
	return b, nil
}

func (p *pieces) skip(n int) error {
	for n > 0 {
		if err := p.need(1); err != nil {
			return err
		}
		k := len(p.parts[p.part]) - p.pos
		if k > n {
			k = n
		}
		p.pos += k
		n -= k
	}
	return nil
}

// chars reads count characters, which may go on in the next piece with a new flags byte.
func (p *pieces) chars(count int, wide bool) (string, error) {
	var units []uint16
	for count > 0 {
		if p.part < len(p.parts) && p.pos == len(p.parts[p.part]) {
			p.part++
			p.pos = 0
			flags, err := p.bytes(1)
			if err != nil {
				return "", err
			}
			wide = flags[0]&1 == 1
		}
		if err := p.need(1); err != nil {
			return "", err
		}
		size := 1
		if wide {
			size = 2
		}
		left := (len(p.parts[p.part]) - p.pos) / size
		if left == 0 {
			return "", fmt.Errorf("a character is split between two records")
		}
		if left > count {
			left = count
		}
		b, _ := p.bytes(left * size)
		for i := 0; i < left; i++ {
			if wide {
				units = append(units, binary.LittleEndian.Uint16(b[2*i:]))
			} else {
				units = append(units, uint16(b[i]))
			}
		}
		count -= left
	}
	return string(utf16.Decode(units)), nil
}

// unicodeString reads an XLUnicodeRichExtendedString.
func (p *pieces) unicodeString() (string, error) {
	head, err := p.bytes(3)
	if err != nil {
		return "", err
	}
	count := int(binary.LittleEndian.Uint16(head))
	flags := head[2]
	runs, ext := 0, 0
	if flags&0x08 != 0 {
		b, err := p.bytes(2)
		if err != nil {
			return "", err
		}
		runs = int(binary.LittleEndian.Uint16(b))
	}
	if flags&0x04 != 0 {
		b, err := p.bytes(4)
		if err != nil {
			return "", err
		}
		ext = int(binary.LittleEndian.Uint32(b))
	}
	s, err := p.chars(count, flags&1 == 1)
	if err != nil {
		return "", err
	}
	if err := p.skip(4*runs + ext); err != nil {
		return "", err
	}
	return s, nil
}

func rk(v uint32) float64 {
	var f float64
	if v&2 != 0 {
		f = float64(int32(v) >> 2)
	} else {
		f = math.Float64frombits(uint64(v&0xFFFFFFFC) << 32)
	}
	if v&1 != 0 {
		f /= 100
	}
	return f
}

func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// ReadXLS reads the sheets of a workbook in the legacy Excel format.
func ReadXLS(data []byte) ([]Sheet, error) {
	c, err := openCompound(data)
	if err != nil {
		return nil, err
	}
	stream, err := c.stream("Workbook", "Book")
	if err != nil {
		return nil, err
	}
	recs, err := records(stream)
	if err != nil {
		return nil, err
	}
	le := binary.LittleEndian

	var sheets []Sheet
	starts := map[int]int{}
	var strings []string
	for i, r := range recs {
		switch r.kind {
		case recBoundSheet:
			if len(r.data) < 8 {
				return nil, fmt.Errorf("a short sheet record")
			}
			p := &pieces{parts: [][]byte{r.data[6:]}}
			head, err := p.bytes(2)
			if err != nil {
				return nil, err
			}
			name, err := p.chars(int(head[0]), head[1]&1 == 1)
			if err != nil {
				return nil, err
			}
			starts[int(le.Uint32(r.data))] = len(sheets)
			sheets = append(sheets, Sheet{Name: name})
		case recSST:
			if len(r.data) < 8 {
				return nil, fmt.Errorf("a short shared string record")
			}
			p := &pieces{parts: [][]byte{r.data[8:]}}
			for k := i + 1; k < len(recs) && recs[k].kind == recContinue; k++ {
				p.parts = append(p.parts, recs[k].data)
			}
			total := int(le.Uint32(r.data[4:]))
			for n := 0; n < total; n++ {
				s, err := p.unicodeString()
				if err != nil {
					return nil, fmt.Errorf("shared string %d: %w", n, err)
				}
				strings = append(strings, s)
			}
		}
	}

	current := -1
	set := func(row, col int, value string) {
		if current < 0 {
			return
		}
		s := &sheets[current]
		for len(s.Rows) <= row {
			s.Rows = append(s.Rows, nil)
		}
		for len(s.Rows[row]) <= col {
			s.Rows[row] = append(s.Rows[row], "")
		}
		s.Rows[row][col] = value
	}
	pendingRow, pendingCol := -1, -1
	for _, r := range recs {
		if r.kind == recBOF {
			if at, ok := starts[r.offset]; ok {
				current = at
			}
			continue
		}
		d := r.data
		switch r.kind {
		case recLabelSST:
			if len(d) < 10 {
				return nil, fmt.Errorf("a short cell record")
			}
			n := int(le.Uint32(d[6:]))
			if n >= len(strings) {
				return nil, fmt.Errorf("a cell refers to shared string %d of %d", n, len(strings))
			}
			set(int(le.Uint16(d)), int(le.Uint16(d[2:])), strings[n])
		case recLabel:
			p := &pieces{parts: [][]byte{d[6:]}}
			head, err := p.bytes(3)
			if err != nil {
				return nil, err
			}
			s, err := p.chars(int(le.Uint16(head)), head[2]&1 == 1)
			if err != nil {
				return nil, err
			}
			set(int(le.Uint16(d)), int(le.Uint16(d[2:])), s)
		case recNumber:
			if len(d) < 14 {
				return nil, fmt.Errorf("a short number record")
			}
			set(int(le.Uint16(d)), int(le.Uint16(d[2:])), number(math.Float64frombits(le.Uint64(d[6:]))))
		case recRK:
			if len(d) < 10 {
				return nil, fmt.Errorf("a short number record")
			}
			set(int(le.Uint16(d)), int(le.Uint16(d[2:])), number(rk(le.Uint32(d[6:]))))
		case recMulRK:
			row, first := int(le.Uint16(d)), int(le.Uint16(d[2:]))
			for k := 0; 4+6*k+6 <= len(d)-2; k++ {
				set(row, first+k, number(rk(le.Uint32(d[4+6*k+2:]))))
			}
		case recFormula:
			if len(d) < 14 {
				return nil, fmt.Errorf("a short formula record")
			}
			row, col := int(le.Uint16(d)), int(le.Uint16(d[2:]))
			if d[12] == 0xFF && d[13] == 0xFF {
				if d[6] == 0 {
					pendingRow, pendingCol = row, col
				}
				continue
			}
			set(row, col, number(math.Float64frombits(le.Uint64(d[6:]))))
		case recString:
			if pendingRow >= 0 {
				p := &pieces{parts: [][]byte{d}}
				head, err := p.bytes(3)
				if err != nil {
					return nil, err
				}
				s, err := p.chars(int(le.Uint16(head)), head[2]&1 == 1)
				if err != nil {
					return nil, err
				}
				set(pendingRow, pendingCol, s)
				pendingRow, pendingCol = -1, -1
			}
		}
	}
	if len(sheets) == 0 {
		return nil, fmt.Errorf("the workbook has no sheets")
	}
	return sheets, nil
}
