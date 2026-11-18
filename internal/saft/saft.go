// Package saft builds the nomenclatures of the SAF-T file (declaration D406) that ANAF publishes in its
// workbook of schema definitions and codes: the VAT tax codes with their rows in D300, the taxes and their
// budgets, the withholding tax codes, the movement codes of stock and assets, the tax regimes, the invoice
// types, the payment mechanisms and the charts of accounts.
package saft

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/fetch"
	"github.com/pidginhost/romanian-datasets/internal/sheet"
)

// Page is the page of declaration D406, which links the newest workbook.
const Page = "https://static.anaf.ro/static/10/Anaf/Declaratii_R/406.html"

const (
	maxPage = 1 << 20
	// maxBook is many times the size of the workbook.
	maxBook = 64 << 20
)

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files of ANAF over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

var linkPattern = regexp.MustCompile(`href="(https://static\.anaf\.ro/[^"]*/RO_SAFT_SchemaDefCod_[^"/]*\.xlsx)"`)

// Link gives the address of the workbook the page links.
func Link(page string) (string, error) {
	found := map[string]bool{}
	var out string
	for _, m := range linkPattern.FindAllStringSubmatch(page, -1) {
		if !found[m[1]] {
			found[m[1]] = true
			out = m[1]
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("the page of D406 links %d workbooks of codes, not one", len(found))
	}
	return out, nil
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func optional(s string) *string {
	if s = clean(s); s == "" {
		return nil
	}
	return &s
}

// table is one sheet with the column of each header.
type table struct {
	sheet  sheet.Sheet
	header int
	cols   map[string]int
}

func (t table) get(row int, field string) string {
	c, ok := t.cols[field]
	if !ok {
		return ""
	}
	return clean(t.sheet.Cell(row, c))
}

func find(sheets []sheet.Sheet, name string) (sheet.Sheet, error) {
	for _, s := range sheets {
		if s.Name == name {
			return s, nil
		}
	}
	return sheet.Sheet{}, fmt.Errorf("the workbook has no sheet %q", name)
}

// open finds the header row of a sheet, the first row that holds marker, and names its columns by headers.
// A header the build does not know stops it, so that a new column is read by a person first; "" leaves a
// known column out.
func open(sheets []sheet.Sheet, name, marker string, headers map[string]string) (table, error) {
	s, err := find(sheets, name)
	if err != nil {
		return table{}, err
	}
	for r, row := range s.Rows {
		has := false
		for _, c := range row {
			if clean(c) == marker {
				has = true
			}
		}
		if !has {
			continue
		}
		t := table{sheet: s, header: r, cols: map[string]int{}}
		for c, cell := range row {
			h := clean(cell)
			if h == "" {
				continue
			}
			field, ok := headers[h]
			if !ok {
				for prefix, f := range headers {
					if strings.HasSuffix(prefix, "...") && strings.HasPrefix(h, strings.TrimSuffix(prefix, "...")) {
						field, ok = f, true
					}
				}
			}
			if !ok {
				return table{}, fmt.Errorf("%s: the column %q is not known to the build", name, h)
			}
			if field == "" {
				continue
			}
			if _, twice := t.cols[field]; twice {
				return table{}, fmt.Errorf("%s: two columns give %s", name, field)
			}
			t.cols[field] = c
		}
		return t, nil
	}
	return table{}, fmt.Errorf("%s: no row of headers with %q", name, marker)
}

// need checks that a table has the columns the build reads.
func (t table) need(fields ...string) error {
	for _, f := range fields {
		if _, ok := t.cols[f]; !ok {
			return fmt.Errorf("%s: no column gives %s", t.sheet.Name, f)
		}
	}
	return nil
}

// VATSheets are the sheets of VAT tax codes. A sheet of VAT tax codes not here stops the build.
var VATSheets = []string{
	"Livrari", "Achizitii ded 100%", "Achizitii ded 50%", "Achizitii ded 50%_baserate", "Achizitii ded 50%_not_known",
	"Achizitii neded 50%", "Achizitii neded", "Achizitii baserate", "Achizitii not known", "TVA_NoteContabile",
}

// vatHeaders names the columns of the VAT sheets. The rows of D300 are named by the days of the forms they
// belong to: a workbook for a new form of D300 stops the build until a person reads it.
var vatHeaders = map[string]string{
	"Corespondent rand D300 dupa data de 01.08.2025":    "d300",
	"Corespondent rand D300 la data de 01.08.2025":      "d300",
	"Corespondent rand D300 pana la data de 31.07.2025": "d300_previous",
	"Corespondent rand D300 la data de 31.07.2025":      "d300_previous",
	"Corespondent rand D311":                            "d311",
	"Corespondent D398":                                 "d398",
	"Sectiune D301":                                     "d301",
	"Sectiune D394":                                     "d394",
	"Cod taxa 310":                                      "code",
	"Cod taxă in SAF-T (TaxCode)":                       "code",
	"x - Inactiv":                                       "inactive",
	"- Activ":                                           "inactive",
	"Cod taxa":                                          "label",
	"Descriere cod (RO)":                                "name_ro",
	"Code description (EN)":                             "name_en",
	"Start date (DATE aaaa-ll-zz)":                      "start",
	"End date (DATE aaaa-ll-zz)":                        "end",
	"Temei legal":                                       "legal",
	"Cota TVA (%)":                                      "rate",
	"Cote TVA (%)":                                      "rate",
	"Cote TVA":                                          "rate",
	"Comentarii cod de taxa":                            "comments",
	"Nu intra in sfera tva":                             "out_of_scope",
	// Marks of the rate and of kinds of invoice, which the rate and the name already say.
	"Cotă TVA":           "",
	"Scutit TVA":         "",
	"Cota maxima de TVA": "",
	"Cota medie de TVA":  "",
	"Cota minima de TVA": "",
	"Facturi cu TVA de corecție (cazuri specifice)": "",
	"Facturi de decontare / alta categorie":         "",
	"Pentru aceste categorii/ coduri...":            "",
}

var (
	sixDigits   = regexp.MustCompile(`^\d{6}$`)
	threeDigits = regexp.MustCompile(`^\d{3}$`)
	digits      = regexp.MustCompile(`^\d+$`)
	codeList    = regexp.MustCompile(`codul (\d+)`)
	typePattern = regexp.MustCompile(`^(\d{3})(?: (.+))?$`)
)

// VATCode is one VAT tax code of SAF-T.
type VATCode struct {
	Code string `json:"tax_code"`
	// Sheet is the sheet of the workbook the code is on, which says the kind of operation.
	Sheet        string  `json:"sheet"`
	Label        *string `json:"label"`
	NameRO       string  `json:"name_ro"`
	NameEN       *string `json:"name_en"`
	Rate         *string `json:"rate"`
	Active       bool    `json:"active"`
	OutOfScope   bool    `json:"out_of_scope"`
	D300         *string `json:"d300_row"`
	D300Previous *string `json:"d300_row_previous"`
	D301         *string `json:"d301_section"`
	D394         *string `json:"d394_section"`
	D311         *string `json:"d311_row"`
	D398         *string `json:"d398_row"`
	Legal        *string `json:"legal_basis"`
	Start        *string `json:"start_date"`
	End          *string `json:"end_date"`
	Comments     *string `json:"comments"`
}

// ReadVAT reads the VAT tax codes of every VAT sheet.
func ReadVAT(sheets []sheet.Sheet) ([]VATCode, error) {
	known := map[string]bool{}
	for _, name := range VATSheets {
		known[name] = true
	}
	for _, s := range sheets {
		for _, row := range s.Rows {
			for _, c := range row {
				if h := clean(c); (h == "Cod taxă in SAF-T (TaxCode)" || h == "Cod taxa 310") && !known[s.Name] {
					return nil, fmt.Errorf("the sheet %q has VAT tax codes the build does not read: a person adds it to VATSheets", s.Name)
				}
			}
		}
	}
	var out []VATCode
	seen := map[string]bool{}
	for _, name := range VATSheets {
		t, err := open(sheets, name, "Descriere cod (RO)", vatHeaders)
		if err != nil {
			return nil, err
		}
		if err := t.need("code", "name_ro", "rate", "d300", "inactive"); err != nil {
			return nil, err
		}
		n := 0
		for r := t.header + 1; r < len(t.sheet.Rows); r++ {
			code := t.get(r, "code")
			switch {
			case code == "" || threeDigits.MatchString(code):
				// A row of notes, or the row that gives the rate of each mark.
				continue
			case !sixDigits.MatchString(code):
				return nil, fmt.Errorf("%s: the tax code %q", name, code)
			case seen[code]:
				return nil, fmt.Errorf("the VAT tax code %s is written twice", code)
			}
			seen[code] = true
			n++
			v := VATCode{Code: code, Sheet: name, NameRO: t.get(r, "name_ro")}
			if v.NameRO == "" {
				return nil, fmt.Errorf("%s: the tax code %s has no name", name, code)
			}
			switch t.get(r, "inactive") {
			case "":
				v.Active = true
			case "x - Inactiv", "x":
			default:
				return nil, fmt.Errorf("%s, %s: the mark of an inactive code %q", name, code, t.get(r, "inactive"))
			}
			switch t.get(r, "out_of_scope") {
			case "":
			case "x":
				v.OutOfScope = true
			default:
				return nil, fmt.Errorf("%s, %s: the mark out of the scope of VAT %q", name, code, t.get(r, "out_of_scope"))
			}
			v.Label, v.NameEN, v.Rate = optional(t.get(r, "label")), optional(t.get(r, "name_en")), optional(t.get(r, "rate"))
			v.D300, v.D300Previous = optional(t.get(r, "d300")), optional(t.get(r, "d300_previous"))
			v.D301, v.D394, v.D311, v.D398 = optional(t.get(r, "d301")), optional(t.get(r, "d394")), optional(t.get(r, "d311")), optional(t.get(r, "d398"))
			v.Legal, v.Start, v.End, v.Comments = optional(t.get(r, "legal")), optional(t.get(r, "start")), optional(t.get(r, "end")), optional(t.get(r, "comments"))
			out = append(out, v)
		}
		if n == 0 {
			return nil, fmt.Errorf("%s: no tax codes", name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

// Tax is one tax of the nomenclature of taxes, which TaxType takes.
type Tax struct {
	Code   string  `json:"code"`
	NameRO string  `json:"name_ro"`
	NameEN *string `json:"name_en"`
	Budget *string `json:"budget"`
}

// Budget is one category of budgets.
type Budget struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ReadTaxes reads the taxes and the categories of budgets.
func ReadTaxes(sheets []sheet.Sheet) ([]Tax, []Budget, error) {
	t, err := open(sheets, "TAX-IMP - Impozite", "COD_IMP", map[string]string{
		"COD_IMP": "code", "DENUMIRE": "name_ro", "Tax Description (in English)": "name_en", "Categorie BUGET": "budget"})
	if err != nil {
		return nil, nil, err
	}
	if err := t.need("code", "name_ro", "name_en", "budget"); err != nil {
		return nil, nil, err
	}
	var taxes []Tax
	seen := map[string]bool{}
	for r := t.header + 1; r < len(t.sheet.Rows); r++ {
		code := t.get(r, "code")
		if code == "" {
			continue
		}
		if !threeDigits.MatchString(code) || seen[code] {
			return nil, nil, fmt.Errorf("taxes: the code %q", code)
		}
		seen[code] = true
		taxes = append(taxes, Tax{Code: code, NameRO: t.get(r, "name_ro"), NameEN: optional(t.get(r, "name_en")), Budget: optional(t.get(r, "budget"))})
	}
	b, err := open(sheets, "TAX-IMP - Bugete", "COD_IMP", map[string]string{"COD_IMP": "code", "DENUMIRE": "name"})
	if err != nil {
		return nil, nil, err
	}
	names := map[string]string{}
	for r := b.header + 1; r < len(b.sheet.Rows); r++ {
		code, name := b.get(r, "code"), b.get(r, "name")
		if code == "" {
			continue
		}
		// The sheet writes some categories twice, and some names end in a stray quote.
		name = strings.TrimSuffix(name, "'")
		if !digits.MatchString(code) || (names[code] != "" && names[code] != name) {
			return nil, nil, fmt.Errorf("budgets: the code %q", code)
		}
		names[code] = name
	}
	var budgets []Budget
	for code, name := range names {
		budgets = append(budgets, Budget{Code: code, Name: name})
	}
	sort.Slice(budgets, func(i, j int) bool { return budgets[i].Code < budgets[j].Code })
	for _, x := range taxes {
		if x.Budget != nil && names[*x.Budget] == "" {
			return nil, nil, fmt.Errorf("the tax %s is of the budget %s, which the budgets do not have", x.Code, *x.Budget)
		}
	}
	return taxes, budgets, nil
}

// Withholding is one tax code of a tax withheld at source.
type Withholding struct {
	Code    string `json:"tax_code"`
	TaxType string `json:"tax_type"`
	// TypeNote is what the sheet writes after the tax type, as "Valabil din 02.2017".
	TypeNote *string `json:"tax_type_note"`
	NameRO   string  `json:"name_ro"`
	NameEN   *string `json:"name_en"`
	Legal    *string `json:"legal_basis"`
	// Declarations is where the tax is declared, as D100 or D207 and the article.
	Declarations *string `json:"declarations"`
	// Rate is the rate as a fraction, as 0.1.
	Rate       *string `json:"rate"`
	IncomeType *string `json:"income_type"`
}

// ReadWithholding reads the withholding tax codes. The sheet ends with notes and an example.
func ReadWithholding(sheets []sheet.Sheet) ([]Withholding, error) {
	t, err := open(sheets, "WHT - nomenclator", "TaxCode", map[string]string{
		"Nr. Crt.": "", "TaxCode": "code", "Cod Categorie Impozit (vezi D100) TaxType": "type",
		"Denumire creanta fiscala denumire utilizată în rapoarte și situații) (RO)":  "name_ro",
		"Denumire creanta fiscala (denumire utilizată în rapoarte și situații) (EN)": "name_en",
		"Temei legal": "legal", "Incadrare Declaratii D207, D100": "declarations",
		"Cote de impozit cu reținere la sursă": "rate", "Tip de venit": "income",
		"Descriere explicativă - prevederile din...": "",
		"Comentarii și explicații suplimentare...":   "",
	})
	if err != nil {
		return nil, err
	}
	if err := t.need("code", "type", "name_ro", "rate"); err != nil {
		return nil, err
	}
	var out []Withholding
	seen := map[string]bool{}
	for r := t.header + 1; r < len(t.sheet.Rows); r++ {
		if clean(t.sheet.Cell(r, 0)) == "Note:" {
			break
		}
		code := t.get(r, "code")
		if !sixDigits.MatchString(code) {
			continue
		}
		if seen[code] {
			return nil, fmt.Errorf("the withholding tax code %s is written twice", code)
		}
		seen[code] = true
		w := Withholding{Code: code, TaxType: t.get(r, "type"), NameRO: t.get(r, "name_ro"), Legal: optional(t.get(r, "legal")),
			Declarations: optional(t.get(r, "declarations")), Rate: optional(t.get(r, "rate")), IncomeType: optional(t.get(r, "income"))}
		if en := t.get(r, "name_en"); en != "<TBD>" {
			w.NameEN = optional(en)
		}
		m := typePattern.FindStringSubmatch(w.TaxType)
		if m == nil || w.NameRO == "" {
			return nil, fmt.Errorf("the withholding tax code %s: tax type %q, name %q", code, w.TaxType, w.NameRO)
		}
		w.TaxType, w.TypeNote = m[1], optional(m[2])
		if w.Rate != nil {
			if _, err := strconv.ParseFloat(*w.Rate, 64); err != nil {
				return nil, fmt.Errorf("the withholding tax code %s: the rate %q", code, *w.Rate)
			}
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no withholding tax codes")
	}
	return out, nil
}

// IncomeType is one type of income of declaration D207.
type IncomeType struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// ReadIncomeTypes reads the types of income of D207, each under the group above it.
func ReadIncomeTypes(sheets []sheet.Sheet) ([]IncomeType, error) {
	t, err := open(sheets, "WHT - D207", "Tip Venit", map[string]string{"Cod": "code", "Tip Venit": "name"})
	if err != nil {
		return nil, err
	}
	var out []IncomeType
	group := ""
	seen := map[string]bool{}
	for r := t.header + 1; r < len(t.sheet.Rows); r++ {
		code, name := t.get(r, "code"), t.get(r, "name")
		switch {
		case code == "" && name != "":
			group = name
		case code != "":
			if !digits.MatchString(code) || seen[code] || name == "" || group == "" {
				return nil, fmt.Errorf("D207 income types: the row %q %q", code, name)
			}
			seen[code] = true
			out = append(out, IncomeType{Code: code, Name: name, Group: group})
		}
	}
	return out, nil
}

// Code is a code of a short nomenclature, with its names.
type Code struct {
	Code   string  `json:"code"`
	NameRO string  `json:"name_ro"`
	NameEN *string `json:"name_en"`
}

// readCodes reads a short nomenclature: a code column and its names, until the notes. A code written twice
// must be written the same.
func readCodes(sheets []sheet.Sheet, name, marker string, headers map[string]string, pattern *regexp.Regexp) ([]Code, error) {
	t, err := open(sheets, name, marker, headers)
	if err != nil {
		return nil, err
	}
	if err := t.need("code", "name_ro", "name_en"); err != nil {
		return nil, err
	}
	var out []Code
	seen := map[string]Code{}
	for r := t.header + 1; r < len(t.sheet.Rows); r++ {
		if strings.HasPrefix(clean(t.sheet.Cell(r, 0)), "Note") {
			break
		}
		c := Code{Code: t.get(r, "code"), NameRO: t.get(r, "name_ro"), NameEN: optional(t.get(r, "name_en"))}
		if !pattern.MatchString(c.Code) {
			continue
		}
		if c.NameRO == "" {
			return nil, fmt.Errorf("%s: the code %s has no name", name, c.Code)
		}
		if before, twice := seen[c.Code]; twice {
			a, _ := json.Marshal(before)
			b, _ := json.Marshal(c)
			if string(a) != string(b) {
				return nil, fmt.Errorf("%s: the code %s is written twice, not the same", name, c.Code)
			}
			continue
		}
		seen[c.Code] = c
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no codes", name)
	}
	return out, nil
}

var codeOfStock = regexp.MustCompile(`^[0-9a-z_]+$`)

// InvoiceType is one type of invoice. SAFT is true for the types the instructions of the sheet tell to use.
type InvoiceType struct {
	Code   string `json:"code"`
	NameEN string `json:"name_en"`
	KindEN string `json:"en16931_kind_en"`
	NameRO string `json:"name_ro"`
	KindRO string `json:"en16931_kind_ro"`
	SAFT   bool   `json:"saft"`
}

// listed gives the codes an instruction row of a sheet names as "codul 380".
func listed(s sheet.Sheet, marker string) (map[string]bool, error) {
	for _, row := range s.Rows {
		for _, c := range row {
			if text := clean(c); strings.Contains(text, marker) {
				out := map[string]bool{}
				for _, m := range codeList.FindAllStringSubmatch(text, -1) {
					out[m[1]] = true
				}
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("%s: no instruction %q", s.Name, marker)
}

// ReadInvoiceTypes reads the types of invoices.
func ReadInvoiceTypes(sheets []sheet.Sheet) ([]InvoiceType, error) {
	t, err := open(sheets, "Nom_Tipuri_facturi", "EN16931 interpretation", map[string]string{
		"Code": "code", "Name": "name_en", "EN16931 interpretation": "kind_en", "Nume": "name_ro", "Interpretare EN16931": "kind_ro"})
	if err != nil {
		return nil, err
	}
	use, err := listed(t.sheet, "se va selecta una din")
	if err != nil {
		return nil, err
	}
	var out []InvoiceType
	seen := map[string]bool{}
	for r := t.header + 1; r < len(t.sheet.Rows); r++ {
		code := t.get(r, "code")
		if !digits.MatchString(code) {
			continue
		}
		if seen[code] {
			return nil, fmt.Errorf("the invoice type %s is written twice", code)
		}
		seen[code] = true
		out = append(out, InvoiceType{Code: code, NameEN: t.get(r, "name_en"), KindEN: t.get(r, "kind_en"), NameRO: t.get(r, "name_ro"), KindRO: t.get(r, "kind_ro"), SAFT: use[code]})
	}
	for code := range use {
		if !seen[code] {
			return nil, fmt.Errorf("the instructions name the invoice type %s, which the list does not have", code)
		}
	}
	return out, nil
}

// PaymentMechanism is one payment mechanism and the payment method it falls under.
type PaymentMechanism struct {
	Code      string  `json:"code"`
	Method    string  `json:"payment_method"`
	MethodEN  string  `json:"payment_method_en"`
	MethodRO  string  `json:"payment_method_ro"`
	NameEN    string  `json:"name_en"`
	NameRO    string  `json:"name_ro"`
	EN16931EN *string `json:"en16931_use_en"`
	EN16931RO *string `json:"en16931_use_ro"`
	Preferred bool    `json:"preferred"`
}

var methodName = regexp.MustCompile(`^(\d{2}) - (.+)$`)

// ReadPaymentMechanisms reads the payment mechanisms. Preferred marks those the instructions tell to use
// first.
func ReadPaymentMechanisms(sheets []sheet.Sheet) ([]PaymentMechanism, error) {
	t, err := open(sheets, "Nom_Mecanisme_plati", "Code for Payment Mechanism", map[string]string{
		"Code for Payment Mechanism": "code", "Code used for Payment Method": "method",
		"Description for Payment Method (not used for reporting)": "method_en", "Code name": "name_en",
		"Usage in EN16931": "use_en", "Metodă de plată": "method_ro", "Numele de cod": "name_ro", "Utilizare în EN16931": "use_ro"})
	if err != nil {
		return nil, err
	}
	instruction, err := listed(t.sheet, "PaymentMechanism se vor selecta")
	if err != nil {
		return nil, err
	}
	// The instruction lists the payment methods first, then the mechanisms to use first.
	preferred := map[string]bool{}
	for _, row := range t.sheet.Rows {
		for _, c := range row {
			if text := clean(c); strings.Contains(text, "PaymentMechanism se vor selecta") {
				_, after, _ := strings.Cut(text, "PaymentMechanism se vor selecta")
				for _, m := range codeList.FindAllStringSubmatch(after, -1) {
					preferred[m[1]] = true
				}
			}
		}
	}
	if len(instruction) == 0 || len(preferred) == 0 {
		return nil, fmt.Errorf("the instructions of the payment mechanisms name no codes")
	}
	var out []PaymentMechanism
	seen := map[string]bool{}
	for r := t.header + 1; r < len(t.sheet.Rows); r++ {
		code, method := t.get(r, "code"), t.get(r, "method")
		if !regexp.MustCompile(`^\d{2}$`).MatchString(method) {
			continue
		}
		if code == "" || seen[code] {
			return nil, fmt.Errorf("payment mechanisms: the code %q", code)
		}
		seen[code] = true
		p := PaymentMechanism{Code: code, Method: method, NameEN: t.get(r, "name_en"), NameRO: t.get(r, "name_ro"),
			EN16931EN: optional(t.get(r, "use_en")), EN16931RO: optional(t.get(r, "use_ro")), Preferred: preferred[code]}
		en, ro := methodName.FindStringSubmatch(t.get(r, "method_en")), methodName.FindStringSubmatch(t.get(r, "method_ro"))
		if en == nil || ro == nil || en[1] != method || ro[1] != method {
			return nil, fmt.Errorf("payment mechanism %s: the payment method %q, %q, %q", code, method, t.get(r, "method_en"), t.get(r, "method_ro"))
		}
		p.MethodEN, p.MethodRO = en[2], ro[2]
		out = append(out, p)
	}
	for code := range preferred {
		if !seen[code] {
			return nil, fmt.Errorf("the instructions name the payment mechanism %s, which the list does not have", code)
		}
	}
	return out, nil
}

// Charts are the sheets of charts of accounts read, by the name of the chart in the dataset.
var Charts = []struct{ Chart, Sheet string }{
	{"companies", "PlanConturiBalSocCom"},
	{"nonprofit", "PlanConturiONG"},
}

// Account is one account of a chart of accounts of SAF-T.
type Account struct {
	Chart   string  `json:"chart"`
	Account string  `json:"account"`
	NameRO  string  `json:"name_ro"`
	NameEN  *string `json:"name_en"`
}

var accountNumber = regexp.MustCompile(`^\d{1,4}$`)

// ReadAccounts reads the charts of accounts. A name that starts with its own number loses it.
func ReadAccounts(sheets []sheet.Sheet) ([]Account, error) {
	var out []Account
	for _, c := range Charts {
		headers := map[string]string{"Nr. cont": "account", "Nr cont": "account", "Descriere [RO]": "name_ro", "Descriere [EN]": "name_en"}
		marker := "Nr. cont"
		if c.Sheet == "PlanConturiONG" {
			marker = "Nr cont"
		}
		t, err := open(sheets, c.Sheet, marker, headers)
		if err != nil {
			return nil, err
		}
		if err := t.need("account", "name_ro"); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for r := t.header + 1; r < len(t.sheet.Rows); r++ {
			account := t.get(r, "account")
			if !accountNumber.MatchString(account) {
				continue
			}
			if seen[account] {
				return nil, fmt.Errorf("%s: the account %s is written twice", c.Sheet, account)
			}
			seen[account] = true
			a := Account{Chart: c.Chart, Account: account, NameRO: strings.TrimPrefix(t.get(r, "name_ro"), account+" "), NameEN: optional(strings.TrimPrefix(t.get(r, "name_en"), account+" "))}
			if a.NameRO == "" {
				return nil, fmt.Errorf("%s: the account %s has no name", c.Sheet, account)
			}
			out = append(out, a)
		}
		if len(seen) == 0 {
			return nil, fmt.Errorf("%s: no accounts", c.Sheet)
		}
	}
	return out, nil
}

// Book is everything the dataset reads from the workbook.
type Book struct {
	VAT                  []VATCode
	Taxes                []Tax
	Budgets              []Budget
	Withholding          []Withholding
	IncomeTypes          []IncomeType
	StockMovements       []Code
	AssetMovements       []Code
	TaxRegimes           []Code
	StockCharacteristics []Code
	InvoiceTypes         []InvoiceType
	PaymentMechanisms    []PaymentMechanism
	Accounts             []Account
}

// ReadBook reads the nomenclatures out of the sheets of the workbook.
func ReadBook(sheets []sheet.Sheet) (Book, error) {
	var b Book
	var err error
	if b.VAT, err = ReadVAT(sheets); err != nil {
		return Book{}, err
	}
	if b.Taxes, b.Budgets, err = ReadTaxes(sheets); err != nil {
		return Book{}, err
	}
	if b.Withholding, err = ReadWithholding(sheets); err != nil {
		return Book{}, err
	}
	if b.IncomeTypes, err = ReadIncomeTypes(sheets); err != nil {
		return Book{}, err
	}
	names := map[string]string{"Denumire categorie RO": "name_ro", "Denumire categorie ENG": "name_en"}
	stock := map[string]string{"Cod_miscari_stoc": "code"}
	assets := map[string]string{"Cod-miscari-active": "code"}
	for k, v := range names {
		stock[k], assets[k] = v, v
	}
	if b.StockMovements, err = readCodes(sheets, "Nomenclator stocuri", "Cod_miscari_stoc", stock, digits); err != nil {
		return Book{}, err
	}
	if b.AssetMovements, err = readCodes(sheets, "Nomenclator imobilizari", "Cod-miscari-active", assets, digits); err != nil {
		return Book{}, err
	}
	described := func(code string) map[string]string {
		return map[string]string{code: "code", "Descriere cod (RO)": "name_ro", "Code description (EN)": "name_en"}
	}
	if b.TaxRegimes, err = readCodes(sheets, "Nomenclator_Regim_fiscal", "Cod TaxType utilizat în raportarea SAF-T", described("Cod TaxType utilizat în raportarea SAF-T"), sixDigits); err != nil {
		return Book{}, err
	}
	if b.StockCharacteristics, err = readCodes(sheets, "SAFT_Nomenclator_StockChar", "Cod StockCharacteristicValue", described("Cod StockCharacteristicValue"), codeOfStock); err != nil {
		return Book{}, err
	}
	if b.InvoiceTypes, err = ReadInvoiceTypes(sheets); err != nil {
		return Book{}, err
	}
	if b.PaymentMechanisms, err = ReadPaymentMechanisms(sheets); err != nil {
		return Book{}, err
	}
	if b.Accounts, err = ReadAccounts(sheets); err != nil {
		return Book{}, err
	}
	return b, nil
}
