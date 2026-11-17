// Package en16931 builds the code lists of the European standard on e-invoicing, EN 16931, from every
// genericode archive the European Commission publishes, with the period in which each code is in a list.
package en16931

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Page is the registry page of the Commission that holds the archives.
const Page = "https://ec.europa.eu/digital-building-blocks/sites/display/DIGITAL/Registry+of+supporting+artefacts+to+implement+EN16931"

// Site is the root of the attachment addresses.
const Site = "https://ec.europa.eu/digital-building-blocks/sites"

// Attachments lists the files of the registry page.
const Attachments = Site + "/rest/api/content/467108974/child/attachment?limit=200"

// Licence is the terms of the Commission, with the origin of the codes.
const Licence = "European Commission reuse notice (Decision 2011/833/EU, CC BY 4.0 for EU content): reuse allowed with the source acknowledged. " +
	"The codes and their names come from UN/CEFACT, ISO, IANA and the Commission; each file names its origin"

// The limits are many times the size of the answers and of an archive.
const (
	maxList    = 4 << 20
	maxArchive = 16 << 20
	maxMember  = 8 << 20
	maxPages   = 10
)

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

// List is one of the code lists, as the archive names it, with the file it is published in.
type List struct {
	Member string
	File   string
	Origin string
}

// Lists are the lists of the archives that are published, in the order of the files.
var Lists = []List{
	{"Allowance.gc", "allowance-reasons.jsonl", "UNTDID 5189, allowance reason codes (UN/CEFACT)"},
	{"Charge.gc", "charge-reasons.jsonl", "UNTDID 7161, special service description codes (UN/CEFACT)"},
	{"Currency.gc", "currencies.jsonl", "ISO 4217 currency codes"},
	{"1001.gc", "document-types.jsonl", "UNTDID 1001, document name codes (UN/CEFACT)"},
	{"EAS.gc", "electronic-address-schemes.jsonl", "Electronic Address Scheme (EAS), kept by the European Commission"},
	{"ICD.gc", "identifier-schemes.jsonl", "ISO/IEC 6523, International Code Designator (ICD)"},
	{"Item.gc", "item-schemes.jsonl", "UNTDID 7143, item type identification codes (UN/CEFACT)"},
	{"MIME.gc", "mime-codes.jsonl", "MIME media types for attachments (IANA)"},
	{"Payment.gc", "payment-means.jsonl", "UNTDID 4461, payment means codes (UN/CEFACT)"},
	{"1153.gc", "reference-qualifiers.jsonl", "UNTDID 1153, reference code qualifiers (UN/CEFACT)"},
	{"Text.gc", "text-subjects.jsonl", "UNTDID 4451, text subject code qualifiers (UN/CEFACT)"},
	{"Unit.gc", "units.jsonl", "UN/ECE Recommendation 20, codes for units of measure, with the codes of Recommendation 21"},
	{"5305.gc", "vat-categories.jsonl", "UNTDID 5305, duty or tax or fee category codes (UN/CEFACT)"},
	{"VATEX.gc", "vat-exemption-reasons.jsonl", "VAT exemption reason codes (VATEX), kept by the European Commission"},
}

// Left are the lists of the archives that are read but not published, with the dataset that has them.
var Left = map[string]string{
	// The countries are in the countries dataset. The archives write Kosovo 1A and add XI for Northern Ireland.
	"Country.gc": "countries",
}

// Archive is one genericode archive with the day from which it is used.
type Archive struct {
	Used    string
	Address string
}

type attachments struct {
	Results []struct {
		Title string `json:"title"`
		Links struct {
			Download string `json:"download"`
		} `json:"_links"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

var archiveName = regexp.MustCompile(`(?i)genericodes.*?(\d{4}-\d{2}-\d{2})\.zip$`)

// readAttachments reads one page of the attachment list: the archives on it, and the address of the next page.
func readAttachments(r io.Reader) ([]Archive, string, error) {
	var a attachments
	if err := json.NewDecoder(io.LimitReader(r, maxList)).Decode(&a); err != nil {
		return nil, "", fmt.Errorf("not an attachment list: %w", err)
	}
	var out []Archive
	for _, res := range a.Results {
		m := archiveName.FindStringSubmatch(strings.TrimSpace(res.Title))
		if m == nil {
			continue
		}
		if _, err := time.Parse(time.DateOnly, m[1]); err != nil {
			return nil, "", fmt.Errorf("archive %s: %q is not a day", res.Title, m[1])
		}
		if !strings.HasPrefix(res.Links.Download, "/download/attachments/") {
			return nil, "", fmt.Errorf("archive %s: download address %q is not on the registry page", res.Title, res.Links.Download)
		}
		out = append(out, Archive{Used: m[1], Address: Site + res.Links.Download})
	}
	next := ""
	if a.Links.Next != "" {
		if !strings.HasPrefix(a.Links.Next, "/rest/api/content/467108974/child/attachment") {
			return nil, "", fmt.Errorf("next page %q is not of the registry page", a.Links.Next)
		}
		next = Site + a.Links.Next
	}
	return out, next, nil
}

// Code is one row of a list in one archive.
type Code struct {
	Code   string
	Name   string
	Remark string
}

type genericode struct {
	Columns []struct {
		ID string `xml:"Id,attr"`
	} `xml:"ColumnSet>Column"`
	Rows []struct {
		Values []struct {
			Column string `xml:"ColumnRef,attr"`
			Value  string `xml:"SimpleValue"`
		} `xml:"Value"`
	} `xml:"SimpleCodeList>Row"`
}

var knownColumns = map[string]bool{"Code": true, "Name": true, "Remark": true}

// readList reads one genericode list. Codes are unique; the columns are Code, and Name and Remark when there.
func readList(r io.Reader) ([]Code, error) {
	var g genericode
	if err := xml.NewDecoder(io.LimitReader(r, maxMember)).Decode(&g); err != nil {
		return nil, fmt.Errorf("not a genericode list: %w", err)
	}
	hasCode := false
	for _, c := range g.Columns {
		if !knownColumns[c.ID] {
			return nil, fmt.Errorf("column %q is not Code, Name or Remark", c.ID)
		}
		hasCode = hasCode || c.ID == "Code"
	}
	if !hasCode {
		return nil, fmt.Errorf("no Code column")
	}
	seen := map[string]bool{}
	var out []Code
	for i, row := range g.Rows {
		var c Code
		for _, v := range row.Values {
			value := strings.Join(strings.Fields(v.Value), " ")
			switch v.Column {
			case "Code":
				c.Code = value
			case "Name":
				c.Name = value
			case "Remark":
				c.Remark = value
			}
		}
		switch {
		case c.Code == "":
			return nil, fmt.Errorf("row %d has no code", i+1)
		case seen[c.Code]:
			return nil, fmt.Errorf("code %s is in the list twice", c.Code)
		}
		seen[c.Code] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the list has no codes")
	}
	return out, nil
}

// readArchive reads the published lists of one archive. A list the build does not know stops it; a list
// left out is checked to be a list and not kept.
func readArchive(data []byte) (map[string][]Code, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a zip: %w", err)
	}
	known := map[string]bool{}
	for _, l := range Lists {
		known[l.Member] = true
	}
	for member := range Left {
		known[member] = true
	}
	out := map[string][]Code{}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Base(f.Name)
		if !known[name] {
			return nil, fmt.Errorf("list %s is not one the build knows", f.Name)
		}
		if _, twice := out[name]; twice {
			return nil, fmt.Errorf("list %s is in the archive twice", name)
		}
		body, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		codes, err := readList(body)
		body.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if _, left := Left[name]; left {
			continue
		}
		out[name] = codes
	}
	for _, l := range Lists {
		if out[l.Member] == nil {
			return nil, fmt.Errorf("the archive has no %s", l.Member)
		}
	}
	return out, nil
}

// Entry is one code over one unbroken stretch of archives.
type Entry struct {
	Code string `json:"code"`
	// Name and Remark are those of the last archive of the stretch; null when that archive gives none.
	Name   *string `json:"name"`
	Remark *string `json:"remark"`
	// ValidFrom is the day from which the first archive of the stretch is used.
	ValidFrom string `json:"valid_from"`
	// ValidTo is the day before the archive that dropped the code, or null while it is in the newest one.
	ValidTo *string `json:"valid_to"`
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Periods turns one list in every archive, sorted by day, into entries sorted by code and first day.
// A code missing from an archive ends its stretch on the day before; if it comes back a new stretch starts.
func Periods(used []string, lists [][]Code) ([]Entry, error) {
	type open struct {
		Entry
		last Code
	}
	current := map[string]*open{}
	var done []Entry
	closeIt := func(o *open, before string) error {
		day, err := time.Parse(time.DateOnly, before)
		if err != nil {
			return err
		}
		end := day.AddDate(0, 0, -1).Format(time.DateOnly)
		o.Name, o.Remark, o.ValidTo = text(o.last.Name), text(o.last.Remark), &end
		done = append(done, o.Entry)
		return nil
	}

	for i, list := range lists {
		here := map[string]Code{}
		for _, c := range list {
			here[c.Code] = c
		}
		for code, o := range current {
			if _, still := here[code]; !still {
				if err := closeIt(o, used[i]); err != nil {
					return nil, err
				}
				delete(current, code)
			}
		}
		for code, c := range here {
			if o, there := current[code]; there {
				o.last = c
			} else {
				current[code] = &open{Entry{Code: code, ValidFrom: used[i]}, c}
			}
		}
	}
	for _, o := range current {
		o.Name, o.Remark = text(o.last.Name), text(o.last.Remark)
		done = append(done, o.Entry)
	}

	sort.Slice(done, func(i, j int) bool {
		if done[i].Code != done[j].Code {
			return done[i].Code < done[j].Code
		}
		return done[i].ValidFrom < done[j].ValidFrom
	})
	return done, nil
}

func read(get Get, address string, limit int64) ([]byte, error) {
	body, err := get(address)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", address)
	}
	return data, nil
}

// archives reads every page of the attachment list and gives the archives sorted by the day they are used from.
func archives(get Get) ([]Archive, error) {
	var all []Archive
	address := Attachments
	for page := 0; address != ""; page++ {
		if page == maxPages {
			return nil, fmt.Errorf("the attachment list has more than %d pages", maxPages)
		}
		data, err := read(get, address, maxList)
		if err != nil {
			return nil, err
		}
		more, next, err := readAttachments(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		all = append(all, more...)
		address = next
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("the registry page has no genericode archive")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Used < all[j].Used })
	for i := 1; i < len(all); i++ {
		if all[i].Used == all[i-1].Used {
			return nil, fmt.Errorf("two archives are used from %s", all[i].Used)
		}
	}
	return all, nil
}

// PublishedCodes reads one file of the published dataset and gives its codes.
func PublishedCodes(r io.Reader) ([]string, error) {
	var codes []string
	decoder := json.NewDecoder(r)
	for {
		var e struct {
			Code string `json:"code"`
		}
		err := decoder.Decode(&e)
		if err == io.EOF {
			return codes, nil
		}
		if err != nil {
			return nil, fmt.Errorf("published codes: %w", err)
		}
		codes = append(codes, e.Code)
	}
}

// Dataset reads every archive and builds the dataset. published gives, for each file, the codes of the
// dataset as published before; one missing now stops the build, since a code is never removed. That
// happens only if the Commission takes an old archive off its page.
func Dataset(get Get, published map[string][]string) (dataset.Dataset, error) {
	found, err := archives(get)
	if err != nil {
		return dataset.Dataset{}, err
	}
	var used []string
	byList := map[string][][]Code{}
	for _, a := range found {
		data, err := read(get, a.Address, maxArchive)
		if err != nil {
			return dataset.Dataset{}, err
		}
		lists, err := readArchive(data)
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("archive used from %s: %w", a.Used, err)
		}
		used = append(used, a.Used)
		for name, codes := range lists {
			byList[name] = append(byList[name], codes)
		}
	}

	first, newest := used[0], used[len(used)-1]
	var files []dataset.File
	for _, l := range Lists {
		entries, err := Periods(used, byList[l.Member])
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("%s: %w", l.Member, err)
		}
		have := map[string]bool{}
		var records []any
		for _, e := range entries {
			have[e.Code] = true
			records = append(records, e)
		}
		var missing []string
		for _, code := range published[l.File] {
			if !have[code] {
				missing = append(missing, code)
			}
		}
		if len(missing) > 0 {
			return dataset.Dataset{}, fmt.Errorf("%s no longer has the published codes %s: an archive left the registry page; a person decides", l.File, strings.Join(missing, ", "))
		}
		files = append(files, dataset.File{
			Name: l.File,
			Fields: []dataset.Field{
				{Name: "code", Description: "The code: " + l.Origin},
				{Name: "name", Description: "The name in English, or null when the list gives none"},
				{Name: "remark", Description: "The remark of the list, or null"},
				{Name: "valid_from", Description: "The day from which the first archive with the code is used, as YYYY-MM-DD; not before " + first + ", the first archive"},
				{Name: "valid_to", Description: "The day before the archive that dropped the code is used, as YYYY-MM-DD, or null while the code is in the newest archive"},
			},
			Records: records,
		})
	}

	return dataset.Dataset{
		Name:  "en16931-codes",
		Title: "Code lists of the European standard on e-invoicing, EN 16931",
		Description: "The code lists an invoice under EN 16931 uses, as the European Commission publishes them for the standard: " +
			"units of measure, currencies, VAT categories and exemption reasons, payment means, document types, " +
			"electronic address and identifier schemes, allowance and charge reasons, item schemes, reference and text qualifiers, attachment types. " +
			"They are European, not Romanian; the e-Factura dataset says which codes ANAF accepts. " +
			"Every archive the Commission still publishes is read, from the one used from " + first + " to the one used from " + newest + ", " +
			"so a code that left a list stays with its last day. A code can come back later, as a new row. " +
			"Names are in English as in the lists. The currencies with a BNR rate are in bnr-currencies, by the same code. " +
			"The country list of the archives is not here: the countries are in the countries dataset. " +
			"An invoice writes Kosovo 1A, where countries has XK, and may write XI for Northern Ireland, which countries leaves out.",
		Source: dataset.Source{
			Name:    "European Commission, Registry of supporting artefacts to implement EN16931, genericode archives used from " + first + " to " + newest,
			URL:     Page,
			Licence: Licence,
		},
		Frequency: dataset.Monthly,
		Update:    "The Commission publishes a new archive twice a year, used from 15 May and 15 November",
		Files:     files,
	}, nil
}
