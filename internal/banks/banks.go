// Package banks builds the credit institutions of Romania from the register of the National Bank of Romania,
// with the bank code a Romanian IBAN carries and the BIC, from the BIC-to-LEI mapping table of SWIFT that
// GLEIF publishes.
package banks

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Mappings lists the monthly BIC-to-LEI mapping tables.
const Mappings = "https://mapping.gleif.org/api/v2/bic-lei"

// MappingPage is the page of GLEIF that publishes the tables and their licence.
const MappingPage = "https://www.gleif.org/en/lei-data/lei-mapping/download-bic-to-lei-relationship-files"

// maxMapping is many times the size of a table.
const maxMapping = 64 << 20

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) {
		accept := ""
		if address == Mappings {
			accept = "application/vnd.api+json"
		}
		return fetch.Get(client, address, accept)
	}
}

// HeadOffices gives, for a branch the register gives no LEI for, the LEI of the institution it belongs to,
// as the register names it. SWIFT gives the BIC of such a branch under that LEI.
var HeadOffices = map[string]string{
	"RB-PJS-40-024": "3TK20IVIUJ8J3ZU0QE75", // ING Bank N.V.
	"RB-PJS-40-065": "N1FBEDJ5J41VKZLO2475", // Citibank Europe plc
	"RB-PJS-40-074": "969500VHT1ZNPVWOYF63", // BNP Paribas Personal Finance
	"RB-PJS-40-075": "529900L6M79Q98YVU182", // Bank of China (Central and Eastern Europe)
	"RB-PJS-40-077": "P4GTT6GF1W40CVIMFR43", // PKO Bank Polski
	"RB-PJS-40-078": "485100NUOK3CEDCUTW40", // Revolut Bank UAB
}

// Code is a bank code of a Romanian IBAN.
type Code struct {
	// Code is the four letters after the check digits of the IBAN.
	Code string  `json:"code"`
	BIC  *string `json:"bic"`
	// Number is the register number of the institution, or null for a code outside the register.
	Number *string `json:"number"`
	// Name is the holder of a code outside the register.
	Name   *string `json:"name"`
	Source string  `json:"source"`
}

// FromMapping is the source of a code read from the SWIFT table.
const FromMapping = "SWIFT BIC-to-LEI mapping table"

// Others are codes of IBANs held outside the credit institutions.
var Others = []Code{{
	Code:   "TREZ",
	Name:   ptr("Trezoreria Statului"),
	Source: "Order MFP 1650/2014: TREZ is the code of the State Treasury in the IBAN of its accounts",
}}

func ptr(s string) *string { return &s }

// Key identifies a code in its file.
func (c Code) Key() string {
	value := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return c.Code + "|" + value(c.BIC) + "|" + value(c.Number)
}

var (
	mappingName = regexp.MustCompile(`^LEI-BIC-(\d{8})\.zip$`)
	bicPattern  = regexp.MustCompile(`^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$`)
)

// Mapping is one monthly table: its date and, for each LEI, the Romanian BICs.
type Mapping struct {
	Date string
	BICs map[string][]string
}

// newestMapping reads the list of tables and gives the address and date of the newest.
func newestMapping(r io.Reader) (string, string, error) {
	var list struct {
		Data []struct {
			Attributes struct {
				FileName     string `json:"fileName"`
				DownloadLink string `json:"downloadLink"`
				Valid        bool   `json:"valid"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&list); err != nil {
		return "", "", fmt.Errorf("not the list of mapping tables: %w", err)
	}
	address, newest := "", ""
	for _, d := range list.Data {
		m := mappingName.FindStringSubmatch(d.Attributes.FileName)
		if m == nil || !d.Attributes.Valid || m[1] <= newest {
			continue
		}
		if !strings.HasPrefix(d.Attributes.DownloadLink, "https://mapping.gleif.org/") {
			return "", "", fmt.Errorf("the table %s is not on mapping.gleif.org", d.Attributes.FileName)
		}
		address, newest = d.Attributes.DownloadLink, m[1]
	}
	if address == "" {
		return "", "", fmt.Errorf("the list has no mapping table")
	}
	day, err := time.Parse("20060102", newest)
	if err != nil {
		return "", "", err
	}
	return address, day.Format(time.DateOnly), nil
}

// ReadMapping reads a mapping table, a zip with one CSV of LEI and BIC, and keeps the Romanian BICs.
func ReadMapping(data []byte) (map[string][]string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("the mapping table is not a zip: %w", err)
	}
	if len(z.File) != 1 || !strings.HasSuffix(strings.ToLower(z.File[0].Name), ".csv") {
		return nil, fmt.Errorf("the mapping table should hold one CSV file")
	}
	f, err := z.File[0].Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := csv.NewReader(io.LimitReader(f, maxMapping))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("mapping table: %w", err)
	}
	if len(header) != 2 || strings.TrimPrefix(header[0], "\xef\xbb\xbf") != "LEI" || header[1] != "BIC" {
		return nil, fmt.Errorf("the mapping table has the columns %v, not LEI and BIC", header)
	}
	out := map[string][]string{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("mapping table: %w", err)
		}
		lei, bic := row[0], row[1]
		if !leiPattern.MatchString(lei) || !bicPattern.MatchString(bic) {
			return nil, fmt.Errorf("the mapping table has the row %q, %q", lei, bic)
		}
		if bic[4:6] == "RO" {
			out[lei] = append(out[lei], bic)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the mapping table has no Romanian BIC")
	}
	return out, nil
}

// readMapping finds and reads the newest mapping table.
func readMapping(get Get) (Mapping, error) {
	body, err := get(Mappings)
	if err != nil {
		return Mapping{}, err
	}
	address, date, err := newestMapping(body)
	body.Close()
	if err != nil {
		return Mapping{}, err
	}
	body, err = get(address)
	if err != nil {
		return Mapping{}, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxMapping+1))
	if err != nil {
		return Mapping{}, err
	}
	if len(data) > maxMapping {
		return Mapping{}, fmt.Errorf("%s is larger than expected", address)
	}
	bics, err := ReadMapping(data)
	if err != nil {
		return Mapping{}, fmt.Errorf("%s: %w", address, err)
	}
	return Mapping{Date: date, BICs: bics}, nil
}

// needsCode are the kinds of institution that hold accounts under a code of their own.
var needsCode = map[string]bool{"bank": true, "housing-bank": true, "mortgage-bank": true, "cooperative-central": true}

// Codes joins the register and the mapping table by LEI, and a branch by the LEI heads gives for it. An
// institution of a kind that needs a code and has none stops the build, as does a head office given for a
// branch the register now gives a LEI for.
func Codes(institutions []Institution, bics map[string][]string, heads map[string]string) ([]Institution, []Code, error) {
	var codes []Code
	found := map[string]bool{}
	for n := range heads {
		found[n] = false
	}
	for i := range institutions {
		in := &institutions[i]
		lei := in.LEI
		if head, ok := heads[in.Number]; ok {
			if in.LEI != nil {
				return nil, nil, fmt.Errorf("the register now gives %s the LEI %s: remove it from HeadOffices", in.Number, *in.LEI)
			}
			found[in.Number] = true
			in.HeadOffice = ptr(head)
			lei = in.HeadOffice
		}
		if lei == nil {
			continue
		}
		for _, bic := range bics[*lei] {
			codes = append(codes, Code{Code: bic[:4], BIC: ptr(bic), Number: ptr(in.Number), Source: FromMapping})
		}
	}
	for n, ok := range found {
		if !ok {
			return nil, nil, fmt.Errorf("HeadOffices names %s, which the register does not have", n)
		}
	}

	has := map[string]bool{}
	for _, c := range codes {
		has[*c.Number] = true
	}
	var missing []string
	for _, in := range institutions {
		if in.Removed == nil && (needsCode[in.Kind] || in.HeadOffice != nil) && !has[in.Number] {
			missing = append(missing, in.Number)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, nil, fmt.Errorf("no BIC for %s: a person decides", strings.Join(missing, ", "))
	}

	codes = append(codes, Others...)
	sort.Slice(codes, func(i, j int) bool { return codes[i].Key() < codes[j].Key() })
	return institutions, codes, nil
}

// Published are the keys of the dataset as published before.
type Published struct {
	Numbers []string
	Codes   []string
}

// PublishedNumbers reads institutions.jsonl of the published dataset.
func PublishedNumbers(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for {
		var in Institution
		err := decoder.Decode(&in)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("published institutions: %w", err)
		}
		out = append(out, in.Number)
	}
}

// PublishedCodes reads codes.jsonl of the published dataset.
func PublishedCodes(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for {
		var c Code
		err := decoder.Decode(&c)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("published codes: %w", err)
		}
		out = append(out, c.Key())
	}
}

func month(date string) string {
	d, _ := time.Parse(time.DateOnly, date)
	return d.Format("January 2006")
}

// Dataset reads the register and the newest mapping table and builds the dataset. An institution or code
// published before and missing now stops the build.
func Dataset(get Get, published Published) (dataset.Dataset, error) {
	institutions, err := Register(get)
	if err != nil {
		return dataset.Dataset{}, err
	}
	mapping, err := readMapping(get)
	if err != nil {
		return dataset.Dataset{}, err
	}
	institutions, codes, err := Codes(institutions, mapping.BICs, HeadOffices)
	if err != nil {
		return dataset.Dataset{}, err
	}

	var missing []string
	numbers := map[string]bool{}
	for _, in := range institutions {
		numbers[in.Number] = true
	}
	for _, n := range published.Numbers {
		if !numbers[n] {
			missing = append(missing, "institution "+n)
		}
	}
	keys := map[string]bool{}
	for _, c := range codes {
		keys[c.Key()] = true
	}
	for _, k := range published.Codes {
		if !keys[k] {
			missing = append(missing, "code "+k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("published entries are gone: %s: a person decides", strings.Join(missing, ", "))
	}

	var institutionRecords, codeRecords []any
	for _, in := range institutions {
		institutionRecords = append(institutionRecords, in)
	}
	for _, c := range codes {
		codeRecords = append(codeRecords, c)
	}

	notice := "SWIFT © and database rights " + month(mapping.Date) + ". All rights reserved. " +
		"This Mapping Table has been developed by SWIFT. Any use of the Mapping Table, in whole or in part, is subject to the " +
		"BIC/LEI Mapping Table License Agreement as published with the Mapping Table available on GLEIF's website. " +
		"The Mapping Table is updated monthly. For the latest BIC information and updates, always refer to www.swift.com/bic ."

	return dataset.Dataset{
		Name:  "banks",
		Title: "Credit institutions and IBAN bank codes",
		Description: "The credit institutions in the register of the National Bank of Romania: banks, housing banks, mortgage banks, " +
			"credit cooperatives and their central bank, branches of foreign credit institutions, and those removed from the register, with the day of removal. " +
			"codes.jsonl gives the bank code of a Romanian IBAN, the four letters after the check digits, with the BIC: " +
			"the Romanian BICs of the SWIFT BIC-to-LEI mapping table, joined to the register by LEI. " +
			"A branch the register gives no LEI for is joined by the LEI of its head office, stated in head_office_lei. " +
			"Credit cooperatives have no code of their own: their accounts carry the code of their central bank. " +
			"TREZ, the State Treasury, is added from Order MFP 1650/2014. " +
			"A code can name an institution that was removed: the BIC of an absorbed bank may stay with the bank that absorbed it. " +
			"Names and addresses are as BNR writes them; BNR shows some Romanian letters as a question mark.",
		Source: dataset.Source{
			Name: "National Bank of Romania, register of credit institutions (" + RegisterPage + "); " +
				"SWIFT BIC-to-LEI mapping table of " + mapping.Date + ", published by GLEIF (" + MappingPage + ")",
			URL: RegisterPage,
			Licence: "Register: facts taken from the public register BNR keeps under Regulation 1/2007; BNR's own terms for its content: " +
				"https://www.bnr.ro/2281-termeni-si-conditii. BICs: royalty-free licence of SWIFT for any use, with this notice: " + notice,
		},
		Frequency: dataset.Monthly,
		Update:    "When BNR changes the register or SWIFT publishes a new mapping table, which it does every month",
		Files: []dataset.File{
			{
				Name: "institutions.jsonl",
				Fields: []dataset.Field{
					{Name: "number", Description: "The number of the entry in the register, unique in the file"},
					{Name: "kind", Description: "bank, housing-bank, mortgage-bank, cooperative-central, cooperative, branch-third-country or branch-eu"},
					{Name: "name", Description: "The name as the register writes it"},
					{Name: "address", Description: "The registered office, or null for a removed institution"},
					{Name: "registered", Description: "The day of entry in the register, YYYY-MM-DD"},
					{Name: "cui", Description: "The tax identification code (CUI), or null"},
					{Name: "trade_register", Description: "The number in the trade register, or null"},
					{Name: "lei", Description: "The Legal Entity Identifier the register gives, or null"},
					{Name: "head_office_lei", Description: "For a branch the register gives no LEI for, the LEI of the institution it belongs to; null otherwise"},
					{Name: "start", Description: "The day from which the institution may operate, YYYY-MM-DD, or null"},
					{Name: "removed", Description: "The day of removal from the register, YYYY-MM-DD, or null for an institution in the register"},
					{Name: "remarks", Description: "The remarks of the register: former names, mergers, or null"},
				},
				Records: institutionRecords,
			},
			{
				Name: "codes.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The bank code of a Romanian IBAN: characters 5 to 8"},
					{Name: "bic", Description: "The BIC, or null for a code that has none here"},
					{Name: "number", Description: "The number of the institution in institutions.jsonl, or null for a code outside the register"},
					{Name: "name", Description: "The holder of a code outside the register, or null"},
					{Name: "source", Description: "Where the code comes from"},
				},
				Records: codeRecords,
			},
		},
	}, nil
}
