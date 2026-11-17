package pdftext_test

import (
	"bytes"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/pdftext"
	"github.com/pidginhost/romanian-datasets/internal/pdftext/pdftesting"
)

func TestPagesReadsTheTextAndWhereItStands(t *testing.T) {
	want := [][]pdftext.Text{
		{{X: 241.88, Y: 782.24, S: "Coduri IBAN"}, {X: 16.70, Y: 694.52, S: "21-11-05"}, {X: 67.50, Y: 694.52, S: "RO24TREZ00220010101XXXXX"}},
		{{X: 247.50, Y: 631.52, S: "Bistrița-Năsăud (sector 1)"}},
	}
	got, err := pdftext.Pages(pdftesting.Write(want))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d pages", len(got))
	}
	for p := range want {
		if len(got[p]) != len(want[p]) {
			t.Fatalf("page %d: %+v", p+1, got[p])
		}
		for i, w := range want[p] {
			g := got[p][i]
			if g.S != w.S || g.X-w.X > 0.001 || w.X-g.X > 0.001 || g.Y-w.Y > 0.001 || w.Y-g.Y > 0.001 {
				t.Errorf("page %d text %d: %+v, want %+v", p+1, i, g, w)
			}
		}
	}
}

func TestPagesStopsOnWhatItDoesNotKnow(t *testing.T) {
	good := pdftesting.Write([][]pdftext.Text{{{X: 10, Y: 10, S: "A"}}})
	for name, data := range map[string][]byte{
		"not a PDF":      []byte("<html>"),
		"object streams": append(append([]byte{}, good...), []byte("/ObjStm")...),
		"other filter":   bytes.Replace(good, []byte("/ASCII85Decode /FlateDecode"), []byte("/LZWDecode"), 1),
		"other encoding": bytes.Replace(good, []byte("/Identity-H"), []byte("/WinAnsiEncoding"), 1),
		"no pages":       bytes.Replace(good, []byte("/Type /Page\n"), []byte("/Type /Other\n"), 1),
	} {
		if _, err := pdftext.Pages(data); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}
