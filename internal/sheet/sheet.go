// Package sheet reads the cell values of spreadsheet workbooks, in the Office Open XML format (.xlsx) and in
// the legacy Excel format (.xls), with the standard library only. Values come as text; numbers are written
// in their shortest form. Formatting, formulas and everything else are left out.
package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Sheet is one sheet: its name and its rows, each a list of cell values, empty where a cell has none.
type Sheet struct {
	Name string
	Rows [][]string
}

// Cell gives the value at a row and column, counted from 0, or "" outside the sheet.
func (s Sheet) Cell(row, col int) string {
	if row < 0 || row >= len(s.Rows) || col < 0 || col >= len(s.Rows[row]) {
		return ""
	}
	return s.Rows[row][col]
}

// Read reads a workbook in either format, told apart by its first bytes.
func Read(data []byte) ([]Sheet, error) {
	switch {
	case bytes.HasPrefix(data, signature):
		return ReadXLS(data)
	case bytes.HasPrefix(data, []byte("PK")):
		return ReadXLSX(data)
	}
	return nil, fmt.Errorf("not a workbook")
}

// maxPart is many times the largest part of a workbook read.
const maxPart = 64 << 20

func part(z *zip.Reader, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			data, err := io.ReadAll(io.LimitReader(r, maxPart+1))
			if err != nil {
				return nil, err
			}
			if len(data) > maxPart {
				return nil, fmt.Errorf("%s is larger than expected", name)
			}
			return data, nil
		}
	}
	return nil, nil
}

// texts gives the text of every t element under the current element, which ends at its end tag.
func texts(d *xml.Decoder, end string) (string, error) {
	var b strings.Builder
	inText := false
	for {
		t, err := d.Token()
		if err != nil {
			return "", err
		}
		switch e := t.(type) {
		case xml.StartElement:
			// rPh holds a phonetic reading, not the text.
			if e.Name.Local == "rPh" {
				if err := d.Skip(); err != nil {
					return "", err
				}
				continue
			}
			inText = e.Name.Local == "t"
		case xml.CharData:
			if inText {
				b.Write(e)
			}
		case xml.EndElement:
			if e.Name.Local == "t" {
				inText = false
			}
			if e.Name.Local == end {
				return b.String(), nil
			}
		}
	}
}

var refPattern = regexp.MustCompile(`^([A-Z]+)(\d+)$`)

// position turns a cell reference such as C12 into its row and column, counted from 0.
func position(ref string) (int, int, error) {
	m := refPattern.FindStringSubmatch(ref)
	if m == nil {
		return 0, 0, fmt.Errorf("cell reference %q", ref)
	}
	col := 0
	for _, r := range m[1] {
		col = col*26 + int(r-'A'+1)
	}
	row, _ := strconv.Atoi(m[2])
	if row < 1 {
		return 0, 0, fmt.Errorf("cell reference %q", ref)
	}
	return row - 1, col - 1, nil
}

// ReadXLSX reads the sheets of a workbook in the Office Open XML format.
func ReadXLSX(data []byte) ([]Sheet, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a workbook: %w", err)
	}
	var shared []string
	if b, err := part(z, "xl/sharedStrings.xml"); err != nil {
		return nil, err
	} else if b != nil {
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			t, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("shared strings: %w", err)
			}
			if e, ok := t.(xml.StartElement); ok && e.Name.Local == "si" {
				s, err := texts(d, "si")
				if err != nil {
					return nil, err
				}
				shared = append(shared, s)
			}
		}
	}

	book, err := part(z, "xl/workbook.xml")
	if err != nil || book == nil {
		return nil, fmt.Errorf("the workbook has no xl/workbook.xml")
	}
	rels, err := part(z, "xl/_rels/workbook.xml.rels")
	if err != nil || rels == nil {
		return nil, fmt.Errorf("the workbook has no relations")
	}
	var r struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(rels, &r); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	for _, it := range r.Items {
		t := strings.TrimPrefix(it.Target, "/")
		if !strings.HasPrefix(t, "xl/") {
			t = path.Join("xl", t)
		}
		targets[it.ID] = t
	}
	var w struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(book, &w); err != nil {
		return nil, err
	}

	var out []Sheet
	for _, s := range w.Sheets {
		name, ok := targets[s.ID]
		if !ok {
			return nil, fmt.Errorf("sheet %s has no part", s.Name)
		}
		b, err := part(z, name)
		if err != nil || b == nil {
			return nil, fmt.Errorf("sheet %s: no part %s", s.Name, name)
		}
		sheet := Sheet{Name: s.Name}
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			t, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("sheet %s: %w", s.Name, err)
			}
			e, ok := t.(xml.StartElement)
			if !ok || e.Name.Local != "c" {
				continue
			}
			ref, kind := "", ""
			for _, a := range e.Attr {
				switch a.Name.Local {
				case "r":
					ref = a.Value
				case "t":
					kind = a.Value
				}
			}
			row, col, err := position(ref)
			if err != nil {
				return nil, fmt.Errorf("sheet %s: %w", s.Name, err)
			}
			value := ""
			var inline string
			for {
				t, err := d.Token()
				if err != nil {
					return nil, fmt.Errorf("sheet %s: %w", s.Name, err)
				}
				if se, ok := t.(xml.StartElement); ok {
					switch se.Name.Local {
					case "v":
						var v string
						if err := d.DecodeElement(&v, &se); err != nil {
							return nil, err
						}
						value = v
					case "is":
						if inline, err = texts(d, "is"); err != nil {
							return nil, err
						}
					default:
						if err := d.Skip(); err != nil {
							return nil, err
						}
					}
				}
				if ee, ok := t.(xml.EndElement); ok && ee.Name.Local == "c" {
					break
				}
			}
			switch kind {
			case "s":
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 || n >= len(shared) {
					return nil, fmt.Errorf("sheet %s: cell %s refers to shared string %q", s.Name, ref, value)
				}
				value = shared[n]
			case "inlineStr":
				value = inline
			case "", "n":
				if value != "" {
					f, err := strconv.ParseFloat(value, 64)
					if err != nil {
						return nil, fmt.Errorf("sheet %s: cell %s holds %q", s.Name, ref, value)
					}
					value = number(f)
				}
			}
			if value == "" {
				continue
			}
			for len(sheet.Rows) <= row {
				sheet.Rows = append(sheet.Rows, nil)
			}
			for len(sheet.Rows[row]) <= col {
				sheet.Rows[row] = append(sheet.Rows[row], "")
			}
			sheet.Rows[row][col] = value
		}
		out = append(out, sheet)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the workbook has no sheets")
	}
	return out, nil
}
