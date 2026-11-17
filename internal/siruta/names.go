package siruta

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Catalogue asks data.gov.ro for the SIRUTA datasets INS publishes there.
const Catalogue = "https://data.gov.ro/api/3/action/package_search?q=siruta&fq=organization%3Ainstitutul-national-de-statistica&rows=100"

// maxCatalogue and maxCSV are many times the size of the answer and of one edition.
const (
	maxCatalogue = 4 << 20
	maxCSV       = 16 << 20
)

// plain gives, for each Romanian letter, the letter INS writes in its place.
// Both the comma below and the older cedilla forms are taken.
var plain = map[rune]rune{
	'Ă': 'A', 'Â': 'A', 'Î': 'I', 'Ș': 'S', 'Ş': 'S', 'Ț': 'T', 'Ţ': 'T',
	'ă': 'a', 'â': 'a', 'î': 'i', 'ș': 's', 'ş': 's', 'ț': 't', 'ţ': 't',
}

var commaBelow = map[rune]rune{'Ş': 'Ș', 'Ţ': 'Ț', 'ş': 'ș', 'ţ': 'ț'}

func isASCIIAlnum(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}

// withLetters gives the INS name with the Romanian letters of the marked name, when the letters of the
// two names are the same once the marks are taken off. Spaces, dashes and dots stay as INS writes them,
// and the cedilla becomes the comma below. When the letters differ the INS name is given, and false.
func withLetters(ins, marked string) (string, bool) {
	var letters []rune
	for _, r := range marked {
		switch {
		case isASCIIAlnum(r):
			letters = append(letters, r)
		case plain[r] != 0:
			letters = append(letters, r)
		case r >= utf8.RuneSelf:
			// A letter that is neither ASCII nor Romanian: the names are not the same.
			return ins, false
		}
	}

	var out strings.Builder
	next := 0
	for _, r := range ins {
		if !isASCIIAlnum(r) {
			out.WriteRune(r)
			continue
		}
		if next == len(letters) {
			return ins, false
		}
		letter := letters[next]
		base := letter
		if p := plain[letter]; p != 0 {
			base = p
		}
		if base != r {
			return ins, false
		}
		if c := commaBelow[letter]; c != 0 {
			letter = c
		}
		out.WriteRune(letter)
		next++
	}
	if next != len(letters) {
		return ins, false
	}
	return out.String(), true
}

type catalogue struct {
	Success bool `json:"success"`
	Result  struct {
		Results []struct {
			Name      string `json:"name"`
			Created   string `json:"metadata_created"`
			Resources []struct {
				Format string `json:"format"`
				URL    string `json:"url"`
			} `json:"resources"`
		} `json:"results"`
	} `json:"result"`
}

// csvAddresses reads the catalogue answer and gives the addresses of the CSV files, newest dataset first.
func csvAddresses(r io.Reader) ([]string, error) {
	var c catalogue
	if err := json.NewDecoder(io.LimitReader(r, maxCatalogue)).Decode(&c); err != nil {
		return nil, fmt.Errorf("not a data.gov.ro catalogue answer: %w", err)
	}
	if !c.Success {
		return nil, fmt.Errorf("data.gov.ro answered without success")
	}
	packages := c.Result.Results
	sort.SliceStable(packages, func(i, j int) bool {
		if packages[i].Created != packages[j].Created {
			return packages[i].Created > packages[j].Created
		}
		return packages[i].Name < packages[j].Name
	})
	var out []string
	for _, p := range packages {
		for _, res := range p.Resources {
			if strings.EqualFold(strings.TrimSpace(res.Format), "csv") {
				out = append(out, res.URL)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("data.gov.ro lists no CSV of SIRUTA")
	}
	return out, nil
}

// markedNames reads a SIRUTA CSV of data.gov.ro and gives the name of each code. Some files of data.gov.ro
// named .csv are spreadsheets or are not UTF-8; those are refused, and the caller tries an older one.
func markedNames(r io.Reader) (map[int]string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxCSV+1))
	switch {
	case err != nil:
		return nil, err
	case len(data) > maxCSV:
		return nil, fmt.Errorf("larger than a SIRUTA edition")
	case !utf8.Valid(data):
		return nil, fmt.Errorf("not UTF-8 text")
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	header, _, _ := bytes.Cut(data, []byte("\n"))
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	if bytes.Contains(header, []byte(";")) {
		reader.Comma = ';'
	}
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("not CSV: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("no rows")
	}

	codeAt, nameAt := -1, -1
	for i, h := range rows[0] {
		switch strings.ToUpper(strings.TrimSpace(h)) {
		case "SIRUTA":
			codeAt = i
		case "DENLOC":
			nameAt = i
		}
	}
	if codeAt < 0 || nameAt < 0 {
		return nil, fmt.Errorf("no SIRUTA and DENLOC columns")
	}

	names := map[int]string{}
	for i, row := range rows[1:] {
		if len(row) <= codeAt || len(row) <= nameAt {
			return nil, fmt.Errorf("row %d is short", i+2)
		}
		code, err := strconv.Atoi(strings.TrimSpace(row[codeAt]))
		if err != nil {
			return nil, fmt.Errorf("row %d: code %q", i+2, row[codeAt])
		}
		names[code] = strings.Join(strings.Fields(row[nameAt]), " ")
	}
	return names, nil
}
