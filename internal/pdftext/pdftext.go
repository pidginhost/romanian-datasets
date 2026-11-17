// Package pdftext reads the text of a simple PDF file and where each piece of it stands on the page. It
// knows only what plain report generators write: objects in the open (no object streams), streams packed
// with ASCII85 and Flate, Type0 fonts with a ToUnicode table, and text placed with Td, TD, Tm and shown with
// Tj or TJ. Anything else stops the reading, so that no text is lost without notice.
package pdftext

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Text is one piece of text and the point of the page it starts at, in PDF units from the bottom left.
type Text struct {
	X, Y float64
	S    string
}

// maxStream is many times the largest stream of a report page.
const maxStream = 16 << 20

var (
	objectPattern = regexp.MustCompile(`(?s)(\d+) 0 obj(.*?)endobj`)
	refPattern    = regexp.MustCompile(`(\d+) 0 R`)
	rootPattern   = regexp.MustCompile(`/Root (\d+) 0 R`)
	streamPattern = regexp.MustCompile(`(?s)^(.*?)stream\r?\n(.*?)\r?\n?endstream`)
	fontPattern   = regexp.MustCompile(`/(\w+) (\d+) 0 R`)
)

// document is the objects of a file, by number.
type document map[string][]byte

func read(data []byte) (document, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, fmt.Errorf("not a PDF file")
	}
	if bytes.Contains(data, []byte("/ObjStm")) || bytes.Contains(data, []byte("/Encrypt")) {
		return nil, fmt.Errorf("the file has object streams or encryption, which this reader does not know")
	}
	doc := document{}
	for _, m := range objectPattern.FindAllSubmatch(data, -1) {
		doc[string(m[1])] = m[2]
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("the file has no objects")
	}
	return doc, nil
}

// entry gives the value of a key of a dictionary: an array up to its end, a name, or the text up to the
// next key or the end of the dictionary.
func entry(dict []byte, key string) string {
	loc := regexp.MustCompile(`/` + key + `(?:[\s/\[<(]|$)`).FindIndex(dict)
	if loc == nil {
		return ""
	}
	rest := bytes.TrimLeft(dict[loc[0]+1+len(key):], " \r\n\t")
	switch {
	case bytes.HasPrefix(rest, []byte("[")):
		if end := bytes.IndexByte(rest, ']'); end >= 0 {
			return string(rest[:end+1])
		}
		return string(rest)
	case bytes.HasPrefix(rest, []byte("/")):
		end := bytes.IndexAny(rest[1:], " \r\n\t/[]<>()")
		if end < 0 {
			return string(rest)
		}
		return string(rest[:end+1])
	}
	end := bytes.IndexAny(rest, "/>")
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSpace(string(rest[:end]))
}

func (d document) ref(dict []byte, key string) ([]byte, error) {
	m := refPattern.FindStringSubmatch(entry(dict, key))
	if m == nil {
		return nil, fmt.Errorf("no reference for /%s", key)
	}
	object, ok := d[m[1]]
	if !ok {
		return nil, fmt.Errorf("object %s is missing", m[1])
	}
	return object, nil
}

// stream gives the decoded data of a stream object.
func stream(object []byte) ([]byte, error) {
	m := streamPattern.FindSubmatch(object)
	if m == nil {
		return nil, fmt.Errorf("the object is not a stream")
	}
	dict, data := m[1], m[2]
	filters := regexp.MustCompile(`/(\w+)`).FindAllStringSubmatch(entry(dict, "Filter"), -1)
	for _, f := range filters {
		var r io.Reader
		switch f[1] {
		case "ASCII85Decode":
			end := bytes.Index(data, []byte("~>"))
			if end < 0 {
				return nil, fmt.Errorf("an ASCII85 stream has no end")
			}
			r = ascii85.NewDecoder(bytes.NewReader(bytes.TrimPrefix(bytes.TrimSpace(data[:end]), []byte("<~"))))
		case "FlateDecode":
			z, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			r = z
		default:
			return nil, fmt.Errorf("the filter %s is not known to this reader", f[1])
		}
		out, err := io.ReadAll(io.LimitReader(r, maxStream+1))
		if err != nil {
			return nil, err
		}
		if len(out) > maxStream {
			return nil, fmt.Errorf("a stream is larger than expected")
		}
		data = out
	}
	return data, nil
}

// cmap reads a ToUnicode table: two-byte codes to text.
func cmap(data []byte) (map[uint16]string, error) {
	out := map[uint16]string{}
	tokens := tokenize(data)
	for i := 0; i < len(tokens); i++ {
		switch tokens[i].op {
		case "beginbfchar":
			for i++; i+1 < len(tokens) && tokens[i].op != "endbfchar"; i += 2 {
				code, text, err := pair(tokens[i], tokens[i+1])
				if err != nil {
					return nil, err
				}
				out[code] = text
			}
		case "beginbfrange":
			for i++; i+2 < len(tokens) && tokens[i].op != "endbfrange"; i += 3 {
				first, base, err := pair(tokens[i], tokens[i+2])
				if err != nil {
					return nil, err
				}
				last, _, err := pair(tokens[i+1], tokens[i+2])
				if err != nil {
					return nil, err
				}
				r := []rune(base)
				if len(r) != 1 || last < first {
					return nil, fmt.Errorf("a bfrange this reader does not know")
				}
				for c := first; ; c++ {
					out[c] = string(r[0] + rune(c-first))
					if c == last {
						break
					}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the ToUnicode table is empty")
	}
	return out, nil
}

func pair(code, text token) (uint16, string, error) {
	if code.kind != hexString || len(code.bytes) != 2 || text.kind != hexString || len(text.bytes)%2 != 0 {
		return 0, "", fmt.Errorf("a ToUnicode entry this reader does not know")
	}
	units := make([]uint16, len(text.bytes)/2)
	for i := range units {
		units[i] = uint16(text.bytes[2*i])<<8 | uint16(text.bytes[2*i+1])
	}
	return uint16(code.bytes[0])<<8 | uint16(code.bytes[1]), string(utf16.Decode(units)), nil
}

type kind int

const (
	operator kind = iota
	number
	name
	hexString
	literal
	arrayStart
	arrayEnd
	other
)

type token struct {
	kind  kind
	op    string
	num   float64
	bytes []byte
}

// tokenize splits a content stream or a CMap into its tokens.
func tokenize(data []byte) []token {
	var out []token
	for i := 0; i < len(data); {
		c := data[i]
		switch {
		case c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0:
			i++
		case c == '%':
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
		case c == '[':
			out = append(out, token{kind: arrayStart})
			i++
		case c == ']':
			out = append(out, token{kind: arrayEnd})
			i++
		case c == '<' && i+1 < len(data) && data[i+1] == '<', c == '>' && i+1 < len(data) && data[i+1] == '>':
			out = append(out, token{kind: other})
			i += 2
		case c == '<':
			end := bytes.IndexByte(data[i:], '>')
			if end < 0 {
				return append(out, token{kind: other})
			}
			digits := bytes.Map(func(r rune) rune {
				if strings.ContainsRune(" \n\r\t", r) {
					return -1
				}
				return r
			}, data[i+1:i+end])
			if len(digits)%2 == 1 {
				digits = append(digits, '0')
			}
			b, err := hex.DecodeString(string(digits))
			if err != nil {
				out = append(out, token{kind: other})
			} else {
				out = append(out, token{kind: hexString, bytes: b})
			}
			i += end + 1
		case c == '(':
			var b []byte
			depth := 0
			for i++; i < len(data); i++ {
				ch := data[i]
				if ch == '\\' && i+1 < len(data) {
					i++
					switch e := data[i]; e {
					case 'n':
						b = append(b, '\n')
					case 'r':
						b = append(b, '\r')
					case 't':
						b = append(b, '\t')
					case 'b':
						b = append(b, '\b')
					case 'f':
						b = append(b, '\f')
					case '0', '1', '2', '3', '4', '5', '6', '7':
						j := i
						for j < len(data) && j < i+3 && data[j] >= '0' && data[j] <= '7' {
							j++
						}
						v, _ := strconv.ParseUint(string(data[i:j]), 8, 8)
						b = append(b, byte(v))
						i = j - 1
					default:
						b = append(b, e)
					}
					continue
				}
				if ch == '(' {
					depth++
				}
				if ch == ')' {
					if depth == 0 {
						break
					}
					depth--
				}
				b = append(b, ch)
			}
			out = append(out, token{kind: literal, bytes: b})
			i++
		case c == '/':
			j := i + 1
			for j < len(data) && !strings.ContainsRune(" \n\r\t\f/[]<>()%", rune(data[j])) {
				j++
			}
			out = append(out, token{kind: name, op: string(data[i+1 : j])})
			i = j
		default:
			j := i
			for j < len(data) && !strings.ContainsRune(" \n\r\t\f/[]<>()%", rune(data[j])) {
				j++
			}
			if j == i {
				j++
			}
			word := string(data[i:j])
			if v, err := strconv.ParseFloat(word, 64); err == nil {
				out = append(out, token{kind: number, num: v})
			} else {
				out = append(out, token{kind: operator, op: word})
			}
			i = j
		}
	}
	return out
}

// Pages reads the text of every page, in page order.
func Pages(data []byte) ([][]Text, error) {
	doc, err := read(data)
	if err != nil {
		return nil, err
	}
	m := rootPattern.FindSubmatch(data[bytes.LastIndex(data, []byte("trailer")):])
	if m == nil {
		return nil, fmt.Errorf("the file has no /Root")
	}
	catalog, ok := doc[string(m[1])]
	if !ok {
		return nil, fmt.Errorf("the catalog is missing")
	}
	tree, err := doc.ref(catalog, "Pages")
	if err != nil {
		return nil, err
	}
	var pages [][]byte
	var walk func(node []byte, depth int) error
	walk = func(node []byte, depth int) error {
		if depth > 16 {
			return fmt.Errorf("the page tree is too deep")
		}
		if entry(node, "Type") == "/Page" {
			pages = append(pages, node)
			return nil
		}
		for _, k := range refPattern.FindAllStringSubmatch(entry(node, "Kids"), -1) {
			kid, ok := doc[k[1]]
			if !ok {
				return fmt.Errorf("page object %s is missing", k[1])
			}
			if err := walk(kid, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(tree, 0); err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("the file has no pages")
	}

	tables := map[string]map[uint16]string{}
	var out [][]Text
	for n, page := range pages {
		fonts := map[string]map[uint16]string{}
		start := bytes.Index(page, []byte("/Font"))
		if start < 0 {
			return nil, fmt.Errorf("page %d has no fonts", n+1)
		}
		block := page[start:]
		if end := bytes.Index(block, []byte(">>")); end >= 0 {
			block = block[:end]
		}
		for _, f := range fontPattern.FindAllSubmatch(block, -1) {
			number := string(f[2])
			if _, ok := tables[number]; !ok {
				font, ok := doc[number]
				if !ok {
					return nil, fmt.Errorf("font object %s is missing", number)
				}
				if entry(font, "Encoding") != "/Identity-H" {
					return nil, fmt.Errorf("font %s has an encoding this reader does not know", number)
				}
				unicode, err := doc.ref(font, "ToUnicode")
				if err != nil {
					return nil, fmt.Errorf("font %s: %w", number, err)
				}
				data, err := stream(unicode)
				if err != nil {
					return nil, fmt.Errorf("font %s: %w", number, err)
				}
				table, err := cmap(data)
				if err != nil {
					return nil, fmt.Errorf("font %s: %w", number, err)
				}
				tables[number] = table
			}
			fonts[string(f[1])] = tables[number]
		}
		contents, err := doc.ref(page, "Contents")
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", n+1, err)
		}
		content, err := stream(contents)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", n+1, err)
		}
		texts, err := show(tokenize(content), fonts)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", n+1, err)
		}
		out = append(out, texts)
	}
	return out, nil
}

// show runs the text operators of a content stream.
func show(tokens []token, fonts map[string]map[uint16]string) ([]Text, error) {
	var out []Text
	var operands []token
	var font map[uint16]string
	var lineX, lineY float64
	inText := false
	text := func(b []byte) (string, error) {
		if font == nil {
			return "", fmt.Errorf("text shown before a font is chosen")
		}
		if len(b)%2 != 0 {
			return "", fmt.Errorf("text of an odd number of bytes in a two-byte font")
		}
		var s strings.Builder
		for i := 0; i < len(b); i += 2 {
			r, ok := font[uint16(b[i])<<8|uint16(b[i+1])]
			if !ok {
				return "", fmt.Errorf("the code %02X%02X is not in the ToUnicode table", b[i], b[i+1])
			}
			s.WriteString(r)
		}
		return s.String(), nil
	}
	numbers := func(n int) ([]float64, error) {
		if len(operands) != n {
			return nil, fmt.Errorf("an operator with %d operands, %d expected", len(operands), n)
		}
		var v []float64
		for _, o := range operands {
			if o.kind != number {
				return nil, fmt.Errorf("an operand that is not a number")
			}
			v = append(v, o.num)
		}
		return v, nil
	}

	for _, t := range tokens {
		if t.kind != operator {
			operands = append(operands, t)
			continue
		}
		switch t.op {
		case "BT":
			inText, lineX, lineY = true, 0, 0
		case "ET":
			inText = false
		case "Td", "TD":
			v, err := numbers(2)
			if err != nil {
				return nil, err
			}
			lineX += v[0]
			lineY += v[1]
		case "Tm":
			v, err := numbers(6)
			if err != nil {
				return nil, err
			}
			lineX, lineY = v[4], v[5]
		case "Tf":
			if len(operands) != 2 || operands[0].kind != name {
				return nil, fmt.Errorf("Tf without a font name")
			}
			f, ok := fonts[operands[0].op]
			if !ok {
				return nil, fmt.Errorf("the font %s is not on the page", operands[0].op)
			}
			font = f
		case "Tj":
			if !inText || len(operands) != 1 || (operands[0].kind != hexString && operands[0].kind != literal) {
				return nil, fmt.Errorf("Tj outside a text object or without a string")
			}
			s, err := text(operands[0].bytes)
			if err != nil {
				return nil, err
			}
			out = append(out, Text{X: lineX, Y: lineY, S: s})
		case "TJ":
			if !inText {
				return nil, fmt.Errorf("TJ outside a text object")
			}
			var s strings.Builder
			for _, o := range operands {
				if o.kind == hexString || o.kind == literal {
					part, err := text(o.bytes)
					if err != nil {
						return nil, err
					}
					s.WriteString(part)
				}
			}
			out = append(out, Text{X: lineX, Y: lineY, S: s.String()})
		case "'", "\"", "T*", "cm":
			// These move the text in ways this reader does not follow.
			return nil, fmt.Errorf("the operator %s is not known to this reader", t.op)
		}
		operands = operands[:0]
	}
	return out, nil
}
