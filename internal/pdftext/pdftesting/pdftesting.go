// Package pdftesting writes PDF files the way plain report generators do, for tests: one Type0 font with a
// ToUnicode table, streams packed with ASCII85 and Flate, and text placed with TD moves.
package pdftesting

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"fmt"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/pdftext"
)

func pack(data []byte) []byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(data)
	w.Close()
	var a bytes.Buffer
	e := ascii85.NewEncoder(&a)
	e.Write(z.Bytes())
	e.Close()
	return append(a.Bytes(), '~', '>')
}

// Write makes a PDF file of the given pages. Each page shows its texts at their places, in the given order.
func Write(pages [][]pdftext.Text) []byte {
	codes := map[rune]int{}
	var runes []rune
	for _, p := range pages {
		for _, t := range p {
			for _, r := range t.S {
				if _, ok := codes[r]; !ok {
					codes[r] = len(runes)
					runes = append(runes, r)
				}
			}
		}
	}
	var unicode strings.Builder
	fmt.Fprintf(&unicode, "/CIDInit /ProcSet findresource begin\nbegincmap\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n%d beginbfchar\n", len(runes))
	for i, r := range runes {
		fmt.Fprintf(&unicode, "<%04x> <%04x>\n", i, r)
	}
	unicode.WriteString("endbfchar\nendcmap\n")

	var objects []string
	add := func(body string) int {
		objects = append(objects, body)
		return len(objects)
	}
	stream := func(data []byte) int {
		packed := pack(data)
		return add(fmt.Sprintf("<</Length %d\n/Filter [/ASCII85Decode /FlateDecode]\n>>\nstream\n%s\nendstream", len(packed), packed))
	}
	catalog := add("")
	tree := add("")
	cmap := stream([]byte(unicode.String()))
	font := add(fmt.Sprintf("<<\n/Type /Font\n/Subtype /Type0\n/BaseFont /Arial\n/Encoding /Identity-H\n/ToUnicode %d 0 R\n>>", cmap))
	var kids []string
	for _, p := range pages {
		var content strings.Builder
		content.WriteString("1 G\n0.00 w\nBT\n/F0 8.00 Tf\n")
		x, y := 0.0, 0.0
		for _, t := range p {
			fmt.Fprintf(&content, "%.2f %.2f TD\n<", t.X-x, t.Y-y)
			for _, r := range t.S {
				fmt.Fprintf(&content, "%04X", codes[r])
			}
			content.WriteString("> Tj\n")
			x, y = t.X, t.Y
		}
		content.WriteString("ET\n")
		c := stream([]byte(content.String()))
		page := add(fmt.Sprintf("<<\n/Type /Page\n/Parent %d 0 R\n/Resources <</ProcSet [ /PDF /Text ] /Font << /F0 %d 0 R >> >>\n/Contents %d 0 R\n>>", tree, font, c))
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
	}
	objects[catalog-1] = fmt.Sprintf("<<\n/Type /Catalog\n/Pages %d 0 R\n>>", tree)
	objects[tree-1] = fmt.Sprintf("<<\n/Type /Pages\n/Kids [ %s ]\n/Count %d\n>>", strings.Join(kids, " "), len(kids))

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	for i, body := range objects {
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	fmt.Fprintf(&out, "trailer\n<<\n/Size %d\n/Root %d 0 R\n>>\n%%%%EOF\n", len(objects)+1, catalog)
	return out.Bytes()
}
