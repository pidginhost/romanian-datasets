// Package efactura builds the list of codes the e-Factura system of ANAF accepts, read from the
// validation package of the Ministry of Finance. Only facts are taken: which codes a rule accepts.
// The names of the codes are in the EN 16931 dataset.
package efactura

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Page is the technical page of e-Factura that links the validation packages.
const Page = "https://mfinante.gov.ro/web/efactura/informatii-tehnice"

const origin = "https://mfinante.gov.ro"

// The limits are many times the size of the page, of a package and of a file in it.
const (
	maxPage    = 4 << 20
	maxPackage = 16 << 20
	maxMember  = 8 << 20
)

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

// Package is one validation package with its version.
type Package struct {
	Version [3]int
	Address string
}

// Label is the version as ANAF writes it: 1.0.9.
func (p Package) Label() string {
	return fmt.Sprintf("%d.%d.%d", p.Version[0], p.Version[1], p.Version[2])
}

var packageLink = regexp.MustCompile(`href="(/static/10/eFactura/ro16931-ubl-(\d+)\.(\d+)\.(\d+)\.zip)"`)

// Newest reads the technical page and gives the package with the highest version.
func Newest(page io.Reader) (Package, error) {
	text, err := io.ReadAll(io.LimitReader(page, maxPage))
	if err != nil {
		return Package{}, err
	}
	var best *Package
	for _, m := range packageLink.FindAllSubmatch(text, -1) {
		var p Package
		for i := range p.Version {
			p.Version[i], _ = strconv.Atoi(string(m[2+i]))
		}
		p.Address = origin + string(m[1])
		if best == nil || later(p.Version, best.Version) {
			best = &p
		}
	}
	if best == nil {
		return Package{}, fmt.Errorf("the e-Factura page links no validation package")
	}
	return *best, nil
}

func later(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// schematron is what the build reads of a schematron file: its variables and its checks, in order.
type schematron struct {
	lets   map[string]string
	checks map[string]string
}

const schNS = "http://purl.oclc.org/dsdl/schematron"

func readSchematron(r io.Reader) (schematron, error) {
	s := schematron{lets: map[string]string{}, checks: map[string]string{}}
	decoder := xml.NewDecoder(io.LimitReader(r, maxMember))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return s, fmt.Errorf("not a schematron file: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Space != schNS {
			continue
		}
		attr := func(name string) string {
			for _, a := range start.Attr {
				if a.Name.Local == name {
					return a.Value
				}
			}
			return ""
		}
		switch start.Name.Local {
		case "let":
			s.lets[attr("name")] = attr("value")
		case "assert", "report":
			if id := attr("id"); id != "" {
				if _, twice := s.checks[id]; twice {
					return s, fmt.Errorf("check %s is there twice", id)
				}
				s.checks[id] = attr("test")
			}
		}
	}
	if len(s.checks) == 0 {
		return s, fmt.Errorf("no checks")
	}
	return s, nil
}

// Rule says what one check of the package accepts and where.
type Rule struct {
	ID string
	// Field is what the check constrains, in our words.
	Field string
	// List is the file of the EN 16931 dataset the codes come from, or empty for a list that has none.
	List string
	// Parts are the conditions under which a list applies, in the order of the lists in the test;
	// empty when the test has one list for every case.
	Parts []string
}

// CodeRules are the checks of the code list file, as the European standard numbers them.
var CodeRules = []Rule{
	{"BR-CL-01", "Invoice type code and credit note type code", "document-types.jsonl", []string{"invoice", "credit note"}},
	{"BR-CL-03", "Currency of an amount", "currencies.jsonl", nil},
	{"BR-CL-04", "Invoice currency", "currencies.jsonl", nil},
	{"BR-CL-05", "VAT accounting currency", "currencies.jsonl", nil},
	{"BR-CL-06", "VAT point date code of the invoice period", "", nil},
	{"BR-CL-07", "Scheme of an invoiced object identifier", "reference-qualifiers.jsonl", nil},
	{"BR-CL-10", "Scheme of a party identifier", "identifier-schemes.jsonl", []string{"any party", "seller or payee"}},
	{"BR-CL-11", "Scheme of a legal registration identifier", "identifier-schemes.jsonl", nil},
	{"BR-CL-13", "Scheme of an item classification", "item-schemes.jsonl", nil},
	{"BR-CL-14", "Country of an address", "countries.jsonl", nil},
	{"BR-CL-15", "Country of origin of an item", "countries.jsonl", nil},
	{"BR-CL-16", "Payment means", "payment-means.jsonl", nil},
	{"BR-CL-17", "VAT category", "vat-categories.jsonl", nil},
	{"BR-CL-18", "VAT category of an item", "vat-categories.jsonl", nil},
	{"BR-CL-19", "Allowance reason", "allowance-reasons.jsonl", nil},
	{"BR-CL-20", "Charge reason", "charge-reasons.jsonl", nil},
	{"BR-CL-21", "Scheme of an item standard identifier", "identifier-schemes.jsonl", nil},
	{"BR-CL-22", "VAT exemption reason", "vat-exemption-reasons.jsonl", nil},
	{"BR-CL-23", "Unit of measure", "units.jsonl", nil},
	{"BR-CL-24", "Type of an attached document", "mime-codes.jsonl", nil},
	{"BR-CL-25", "Scheme of an electronic address", "electronic-address-schemes.jsonl", nil},
	{"BR-CL-26", "Scheme of a delivery location identifier", "identifier-schemes.jsonl", nil},
}

// RORules are the checks of CIUS-RO that narrow a list.
var RORules = []Rule{
	{"BR-RO-020_1", "Invoice type code", "document-types.jsonl", nil},
	{"BR-RO-020_2", "Credit note type code", "document-types.jsonl", nil},
	{"BR-RO-040", "VAT point date code", "", nil},
}

var (
	spaceList = regexp.MustCompile(`contains\(\s*'([^']*)'\s*,\s*concat\(`)
	mimeEqual = regexp.MustCompile(`@mimeCode\s*=\s*'([^']+)'`)
	quoted    = regexp.MustCompile(`'([^']+)'`)
)

// lists gives the code lists of one test, in order. A test lists its codes between spaces in a contains(),
// except the one on attachments, which compares the type with each value.
func lists(test string) [][]string {
	var out [][]string
	for _, m := range spaceList.FindAllStringSubmatch(test, -1) {
		out = append(out, strings.Fields(m[1]))
	}
	if len(out) == 0 {
		var codes []string
		for _, m := range mimeEqual.FindAllStringSubmatch(test, -1) {
			codes = append(codes, m[1])
		}
		if len(codes) > 0 {
			out = append(out, codes)
		}
	}
	return out
}

// Accepted is one code a rule accepts.
type Accepted struct {
	Rule string `json:"rule"`
	// Part names when the code applies, for a rule with more than one list; null otherwise.
	Part  *string `json:"part"`
	Field string  `json:"field"`
	// List is the file of en16931-codes with the name of the code, or null.
	List *string `json:"list"`
	Code string  `json:"code"`
}

// accepted reads the codes of each rule. A rule the package does not have, or one whose lists are not
// the ones expected, stops the build.
func accepted(s schematron, rules []Rule) ([]Accepted, error) {
	var out []Accepted
	for _, r := range rules {
		test, ok := s.checks[r.ID]
		if !ok {
			return nil, fmt.Errorf("the package has no check %s", r.ID)
		}
		found := lists(test)
		want := len(r.Parts)
		if want == 0 {
			want = 1
		}
		if len(found) != want {
			return nil, fmt.Errorf("check %s has %d lists of codes, %d expected", r.ID, len(found), want)
		}
		var list *string
		if r.List != "" {
			l := r.List
			list = &l
		}
		for i, codes := range found {
			var part *string
			if len(r.Parts) > 0 {
				p := r.Parts[i]
				part = &p
			}
			seen := map[string]bool{}
			for _, code := range codes {
				if seen[code] {
					continue
				}
				seen[code] = true
				out = append(out, Accepted{Rule: r.ID, Part: part, Field: r.Field, List: list, Code: code})
			}
			if len(seen) == 0 {
				return nil, fmt.Errorf("check %s accepts no code", r.ID)
			}
		}
	}
	// A code rule of the package the build does not know stops it.
	known := map[string]bool{}
	for _, r := range rules {
		known[r.ID] = true
	}
	for id := range s.checks {
		if strings.HasPrefix(id, "BR-CL-") && !known[id] {
			return nil, fmt.Errorf("check %s is a code list the build does not know", id)
		}
	}
	return out, nil
}

// Subdivision is a code of a Romanian address that CIUS-RO asks for.
type Subdivision struct {
	Code string `json:"code"`
	// Mnemonic is the county letters, the mnemonic of the SIRUTA counties; null for a sector.
	Mnemonic *string `json:"mnemonic"`
	// Sector is the number of the Bucharest sector; null for a county.
	Sector *int `json:"sector"`
}

var (
	countyCode = regexp.MustCompile(`^RO-([A-Z]{1,2})$`)
	sectorCode = regexp.MustCompile(`^SECTOR([1-6])$`)
)

func subdivisions(s schematron) ([]Subdivision, error) {
	var out []Subdivision
	for _, name := range []string{"ISO-3166-RO-CODES", "SECTOR-RO-CODES"} {
		value, ok := s.lets[name]
		if !ok {
			return nil, fmt.Errorf("the CIUS-RO rules have no %s", name)
		}
		for _, m := range quoted.FindAllStringSubmatch(value, -1) {
			code := strings.TrimSpace(m[1])
			var sub Subdivision
			sub.Code = code
			if c := countyCode.FindStringSubmatch(code); c != nil && name == "ISO-3166-RO-CODES" {
				sub.Mnemonic = &c[1]
			} else if c := sectorCode.FindStringSubmatch(code); c != nil && name == "SECTOR-RO-CODES" {
				n, _ := strconv.Atoi(c[1])
				sub.Sector = &n
			} else {
				return nil, fmt.Errorf("%s has %q, which is not a county or sector code", name, code)
			}
			out = append(out, sub)
		}
	}
	return out, nil
}

// unpack gives the files of the package whose path ends with each name.
func unpack(data []byte, names ...string) (map[string][]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a zip: %w", err)
	}
	out := map[string][]byte{}
	for _, f := range z.File {
		for _, name := range names {
			if f.Name != name && !strings.HasSuffix(f.Name, "/"+name) {
				continue
			}
			if out[name] != nil {
				return nil, fmt.Errorf("the package has %s twice", name)
			}
			body, err := f.Open()
			if err != nil {
				return nil, err
			}
			content, err := io.ReadAll(io.LimitReader(body, maxMember+1))
			body.Close()
			if err != nil {
				return nil, err
			}
			if len(content) > maxMember {
				return nil, fmt.Errorf("%s is larger than expected", name)
			}
			out[name] = content
		}
	}
	for _, name := range names {
		if out[name] == nil {
			return nil, fmt.Errorf("the package has no %s", name)
		}
	}
	return out, nil
}

// Read reads the two schematron files of a package.
func Read(codes, ro []byte) ([]Accepted, []Subdivision, error) {
	codeChecks, err := readSchematron(bytes.NewReader(codes))
	if err != nil {
		return nil, nil, fmt.Errorf("code lists: %w", err)
	}
	roChecks, err := readSchematron(bytes.NewReader(ro))
	if err != nil {
		return nil, nil, fmt.Errorf("CIUS-RO rules: %w", err)
	}
	fromCodes, err := accepted(codeChecks, CodeRules)
	if err != nil {
		return nil, nil, err
	}
	fromRO, err := accepted(roChecks, RORules)
	if err != nil {
		return nil, nil, err
	}
	all := append(fromCodes, fromRO...)
	part := func(a Accepted) string {
		if a.Part == nil {
			return ""
		}
		return *a.Part
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Rule != all[j].Rule {
			return all[i].Rule < all[j].Rule
		}
		if part(all[i]) != part(all[j]) {
			return part(all[i]) < part(all[j])
		}
		return all[i].Code < all[j].Code
	})
	subs, err := subdivisions(roChecks)
	if err != nil {
		return nil, nil, err
	}
	return all, subs, nil
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

const (
	codeFile = "codelist/EN16931-UBL-codes.sch"
	roFile   = "cius-ro/RO16931-rules.sch"
)

// Dataset reads the newest validation package and builds the dataset.
func Dataset(get Get) (dataset.Dataset, error) {
	page, err := read(get, Page, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	pkg, err := Newest(bytes.NewReader(page))
	if err != nil {
		return dataset.Dataset{}, err
	}
	data, err := read(get, pkg.Address, maxPackage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	files, err := unpack(data, codeFile, roFile)
	if err != nil {
		return dataset.Dataset{}, fmt.Errorf("%s: %w", pkg.Address, err)
	}
	all, subs, err := Read(files[codeFile], files[roFile])
	if err != nil {
		return dataset.Dataset{}, fmt.Errorf("package %s: %w", pkg.Label(), err)
	}

	var codes, subdivisions []any
	for _, a := range all {
		codes = append(codes, a)
	}
	for _, s := range subs {
		subdivisions = append(subdivisions, s)
	}

	return dataset.Dataset{
		Name:  "efactura",
		Title: "Codes accepted by e-Factura",
		Description: "The codes the e-Factura system of ANAF accepts in an invoice, as its validation package " + pkg.Label() + " checks them: " +
			"for each check of a code list (BR-CL-*) and each check of CIUS-RO that narrows one (BR-RO-*), every code it accepts. " +
			"The package carries its own copy of the European lists, which can be older than en16931-codes: a code new in en16931-codes can be refused by ANAF, " +
			"and a code that left en16931-codes can still be accepted. Names are not here: read them from the file of en16931-codes the list field names. " +
			"Also the subdivision codes CIUS-RO asks for in a Romanian address: RO-AB to RO-VS for the county, and SECTOR1 to SECTOR6 as the city in Bucharest. " +
			"Only facts are taken from the package; its rules decide, and they are not here.",
		Source: dataset.Source{
			Name:    "Ministry of Finance, e-Factura validation package ro16931-ubl-" + pkg.Label() + " (" + pkg.Address + "), files " + codeFile + " and " + roFile,
			URL:     Page,
			Licence: "Facts taken from the package: which codes each check accepts. The package states no licence and no text of it is copied",
		},
		Frequency: dataset.Monthly,
		Update:    "When the Ministry of Finance publishes a new validation package",
		Files: []dataset.File{
			{
				Name: "accepted-codes.jsonl",
				Fields: []dataset.Field{
					{Name: "rule", Description: "The check of the package: BR-CL-* for a code list of EN 16931, BR-RO-* for a rule of CIUS-RO"},
					{Name: "part", Description: "For a check with more than one list, when this one applies: invoice or credit note (BR-CL-01); any party, or seller or payee only (BR-CL-10). Null otherwise"},
					{Name: "field", Description: "What the check constrains, in our words"},
					{Name: "list", Description: "The file of en16931-codes with the name of the code, or null for the VAT point date codes, which are not there (UNTDID 2005: 3, 35, 432)"},
					{Name: "code", Description: "A code the check accepts. A code outside the list of its check is refused"},
				},
				Records: codes,
			},
			{
				Name: "subdivisions.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The code CIUS-RO asks for: RO-AB to RO-VS (ISO 3166-2:RO) for the county of a Romanian address, SECTOR1 to SECTOR6 for the city in Bucharest, whose county is RO-B"},
					{Name: "mnemonic", Description: "For a county, the county letters, the mnemonic of counties.jsonl in siruta; null for a sector"},
					{Name: "sector", Description: "For a sector, its number, 1 to 6; null for a county"},
				},
				Records: subdivisions,
			},
		},
	}, nil
}
