// Package anafforms builds the versions of the ANAF declarations of the first slice of Conta: for each, the
// reporting period it applies from, the order that set it, and the validator and schema ANAF publishes for
// it, with the SHA-256 of each, so that a reader can check what it downloads from ANAF.
package anafforms

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Folder is where ANAF keeps the page of each declaration.
const Folder = "https://static.anaf.ro/static/10/Anaf/Declaratii_R/"

// ToolPage is the page of ANAF that links the DUKIntegrator package.
const ToolPage = "https://static.anaf.ro/static/DUKIntegrator/DUKIntegrator.htm"

// Forms are the declarations the dataset covers.
var Forms = []string{"100", "112", "300", "390", "394"}

// Horizon is the first reporting period the dataset covers: the version in force for it and every later one.
var Horizon = "2026-01"

// maxPage and maxFile are many times the largest page and file.
const (
	maxPage = 4 << 20
	maxFile = 256 << 20
)

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

// Version is one version of a declaration.
type Version struct {
	Form string `json:"form"`
	// Title is the text ANAF writes for the version: the name, the orders and when it applies.
	Title string `json:"title"`
	// PeriodFrom is the first reporting period, YYYY-MM, the version applies to.
	PeriodFrom string  `json:"period_from"`
	Published  *string `json:"published"`
	// Order is the place of the version on the page of ANAF, 1 for the first: for two versions of one period,
	// the earlier on the page is the newer.
	Order       int     `json:"order"`
	FormPDF     *string `json:"form_pdf"`
	Validator   string  `json:"validator"`
	ValidatorSH string  `json:"validator_sha256"`
	ValidatorSz int64   `json:"validator_bytes"`
	Schema      *string `json:"schema"`
	SchemaSHA   *string `json:"schema_sha256"`
	SchemaBytes *int64  `json:"schema_bytes"`
	Annex       *string `json:"validation_annex"`
}

// Key identifies a version in its file.
func (v Version) Key() string { return v.Form + "|" + v.Validator }

// Tool is a program ANAF publishes for the declarations.
type Tool struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

var (
	rowPattern       = regexp.MustCompile(`(?i)<tr`)
	cellPattern      = regexp.MustCompile(`(?i)<td`)
	linkPattern      = regexp.MustCompile(`(?is)<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	tagPattern       = regexp.MustCompile(`<[^>]*>`)
	numberPattern    = regexp.MustCompile(`(?:valabil|[iî]ncep[aâ]nd cu)[^0-9]{0,60}?(\d{2})/(\d{4})`)
	monthPattern     = regexp.MustCompile(`(?i)aferente lunii (\pL+) (\d{4})`)
	yearPattern      = regexp.MustCompile(`(?i)anul de raportare (\d{4})`)
	publishedPattern = regexp.MustCompile(`publicat [iî]n (?:data(?: de)? )?(\d{2})\.(\d{2})\.(\d{4})`)
)

var months = map[string]int{
	"ianuarie": 1, "februarie": 2, "martie": 3, "aprilie": 4, "mai": 5, "iunie": 6,
	"iulie": 7, "august": 8, "septembrie": 9, "octombrie": 10, "noiembrie": 11, "decembrie": 12,
}

func text(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))), " ")
}

// https writes an address of ANAF over HTTPS, as ANAF serves every file both ways.
func https(address string) string {
	return "https://" + strings.TrimPrefix(strings.TrimPrefix(address, "http://"), "https://")
}

// period reads the first reporting period from the text of a version.
func period(title string) (string, error) {
	if m := numberPattern.FindStringSubmatch(title); m != nil {
		month, _ := strconv.Atoi(m[1])
		if month >= 1 && month <= 12 {
			return m[2] + "-" + m[1], nil
		}
	}
	if m := monthPattern.FindStringSubmatch(title); m != nil {
		if month, ok := months[strings.ToLower(m[1])]; ok {
			return fmt.Sprintf("%s-%02d", m[2], month), nil
		}
	}
	if m := yearPattern.FindStringSubmatch(title); m != nil {
		return m[1] + "-01", nil
	}
	return "", fmt.Errorf("no reporting period the build reads in %q", title)
}

// ReadPage reads the page of a declaration and gives its versions from the horizon, in the order of the
// page. ANAF lists the newest version first, so the first version of a period not after the horizon is the one
// in force for it, and the reading stops there: older versions do not always say their period.
func ReadPage(form, page string) ([]Version, error) {
	var out []Version
	rows := rowPattern.Split(page, -1)
	for _, r := range rows[1:] {
		cells := cellPattern.Split(r, -1)
		if len(cells) < 3 {
			continue
		}
		v := Version{Form: form, Title: text("<td" + cells[1])}
		for _, l := range linkPattern.FindAllStringSubmatch(r, -1) {
			address := https(l[1])
			label := text(l[2])
			switch {
			case strings.HasPrefix(label, "soft J"):
				v.Validator = address
			case strings.HasPrefix(label, "soft A"):
				v.FormPDF = &address
			case strings.HasPrefix(label, "Anexa"):
				v.Annex = &address
			case strings.HasPrefix(label, "Schema"):
				v.Schema = &address
			}
		}
		if v.Validator == "" {
			continue
		}
		if !strings.HasPrefix(v.Title, form) {
			return nil, fmt.Errorf("a row of %s is about another form: %q", form, v.Title)
		}
		p, err := period(v.Title)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", form, err)
		}
		v.PeriodFrom = p
		if m := publishedPattern.FindStringSubmatch(v.Title); m != nil {
			d := m[3] + "-" + m[2] + "-" + m[1]
			if _, err := time.Parse(time.DateOnly, d); err != nil {
				return nil, fmt.Errorf("%s: publication date %q", form, d)
			}
			v.Published = &d
		}
		v.Order = len(out) + 1
		out = append(out, v)
		if v.PeriodFrom <= Horizon {
			return out, nil
		}
	}
	return nil, fmt.Errorf("the page of %s lists no version in force for %s", form, Horizon)
}

// hash reads a file and gives its SHA-256 and size.
func hash(get Get, address string) (string, int64, error) {
	body, err := get(address)
	if err != nil {
		return "", 0, err
	}
	defer body.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(body, maxFile+1))
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", address, err)
	}
	if n > maxFile {
		return "", 0, fmt.Errorf("%s is larger than expected", address)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func read(get Get, address string) (string, error) {
	body, err := get(address)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxPage+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxPage {
		return "", fmt.Errorf("%s is larger than expected", address)
	}
	return string(data), nil
}

// PublishedKeys reads versions.jsonl of the published dataset.
func PublishedKeys(r io.Reader) ([]string, error) {
	var out []string
	decoder := json.NewDecoder(r)
	for decoder.More() {
		var v Version
		if err := decoder.Decode(&v); err != nil {
			return nil, fmt.Errorf("published versions: %w", err)
		}
		out = append(out, v.Key())
	}
	return out, nil
}

// Dataset reads the page of each declaration and the tool page, and hashes the validators and schemas. A
// version published before and missing now stops the build.
func Dataset(get Get, published []string) (dataset.Dataset, error) {
	var versions []Version
	for _, form := range Forms {
		page, err := read(get, Folder+form+".html")
		if err != nil {
			return dataset.Dataset{}, err
		}
		all, err := ReadPage(form, page)
		if err != nil {
			return dataset.Dataset{}, err
		}
		for _, v := range all {
			if v.ValidatorSH, v.ValidatorSz, err = hash(get, v.Validator); err != nil {
				return dataset.Dataset{}, err
			}
			if v.Schema != nil {
				sum, size, err := hash(get, *v.Schema)
				if err != nil {
					return dataset.Dataset{}, err
				}
				v.SchemaSHA, v.SchemaBytes = &sum, &size
			}
			versions = append(versions, v)
		}
	}

	have := map[string]bool{}
	for _, v := range versions {
		have[v.Key()] = true
	}
	var missing []string
	for _, k := range published {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return dataset.Dataset{}, fmt.Errorf("published versions are gone from the pages of ANAF: %s: a person decides", strings.Join(missing, ", "))
	}

	toolPage, err := read(get, ToolPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	var tools []any
	for _, l := range linkPattern.FindAllStringSubmatch(toolPage, -1) {
		if !strings.HasSuffix(strings.ToLower(l[1]), ".zip") {
			continue
		}
		address := https(l[1])
		sum, size, err := hash(get, address)
		if err != nil {
			return dataset.Dataset{}, err
		}
		tools = append(tools, Tool{Name: "DUKIntegrator", URL: address, SHA256: sum, Bytes: size})
	}
	if len(tools) == 0 {
		return dataset.Dataset{}, fmt.Errorf("%s links no DUKIntegrator package", ToolPage)
	}

	var records []any
	for _, v := range versions {
		records = append(records, v)
	}
	return dataset.Dataset{
		Name:  "anaf-forms",
		Title: "ANAF declaration versions and validators",
		Description: "The versions of the declarations " + strings.Join(Forms, ", ") + " as ANAF publishes them: for each, the first reporting period it " +
			"applies to, the orders that set it as ANAF writes them, and the DUKIntegrator validator (soft J), the XSD schema, the PDF form (soft A) " +
			"and the validation annex ANAF links, with the SHA-256 and size of the validator and the schema as downloaded. " +
			"It holds the version in force for the reporting period " + Horizon + " and every later one. For a period, use the version with the latest " +
			"period_from not after it; of two with the same period_from, the one with the lower order, which ANAF lists first. " +
			"tools.jsonl gives the DUKIntegrator package ANAF links, with its SHA-256. A file ANAF replaces under the same address changes its hash here.",
		Source: dataset.Source{
			Name:    "ANAF, pages of the electronic declarations (" + Folder + "<form>.html) and of DUKIntegrator (" + ToolPage + ")",
			URL:     Folder + "descarcare_declaratii.htm",
			Licence: "Facts taken from the pages of ANAF and from its files: addresses, dates, order numbers and hashes. ANAF states no licence for them",
		},
		Frequency: dataset.Monthly,
		Update:    "When ANAF publishes a new version or validator of a declaration, which it does several times a year",
		Files: []dataset.File{
			{
				Name: "versions.jsonl",
				Fields: []dataset.Field{
					{Name: "form", Description: "The declaration, as 100 or 394. With validator it is unique in the file"},
					{Name: "title", Description: "The text ANAF writes for the version"},
					{Name: "period_from", Description: "The first reporting period the version applies to, YYYY-MM"},
					{Name: "published", Description: "The day ANAF published the version, YYYY-MM-DD, or null"},
					{Name: "order", Description: "The place of the version on the page of ANAF, 1 for the first"},
					{Name: "form_pdf", Description: "The address of the PDF form (soft A), or null"},
					{Name: "validator", Description: "The address of the validator package (soft J)"},
					{Name: "validator_sha256", Description: "The SHA-256 of the validator package"},
					{Name: "validator_bytes", Description: "The size of the validator package in bytes"},
					{Name: "schema", Description: "The address of the XSD schema, or null"},
					{Name: "schema_sha256", Description: "The SHA-256 of the schema, or null"},
					{Name: "schema_bytes", Description: "The size of the schema in bytes, or null"},
					{Name: "validation_annex", Description: "The address of the annex with the validations, or null"},
				},
				Records: records,
			},
			{
				Name: "tools.jsonl",
				Fields: []dataset.Field{
					{Name: "name", Description: "The program"},
					{Name: "url", Description: "The address of the package"},
					{Name: "sha256", Description: "The SHA-256 of the package"},
					{Name: "bytes", Description: "The size of the package in bytes"},
				},
				Records: tools,
			},
		},
	}, nil
}
