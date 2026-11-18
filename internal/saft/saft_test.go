package saft

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/sheet"
)

func book(sheets []sheet.Sheet) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	write := func(name, content string) {
		w, _ := z.Create(name)
		io.WriteString(w, content)
	}
	var list, rels strings.Builder
	for n, s := range sheets {
		fmt.Fprintf(&list, `<sheet name=%q sheetId="%d" r:id="rId%d"/>`, html.EscapeString(s.Name), n+1, n+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Target="worksheets/sheet%d.xml"/>`, n+1, n+1)
		var data strings.Builder
		data.WriteString(`<worksheet><sheetData>`)
		for r, row := range s.Rows {
			fmt.Fprintf(&data, `<row r="%d">`, r+1)
			for c, v := range row {
				if v != "" {
					fmt.Fprintf(&data, `<c r="%c%d" t="inlineStr"><is><t>%s</t></is></c>`, 'A'+c, r+1, html.EscapeString(v))
				}
			}
			data.WriteString(`</row>`)
		}
		data.WriteString(`</sheetData></worksheet>`)
		write(fmt.Sprintf("xl/worksheets/sheet%d.xml", n+1), data.String())
	}
	write("xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+list.String()+`</sheets></workbook>`)
	write("xl/_rels/workbook.xml.rels", `<Relationships>`+rels.String()+`</Relationships>`)
	z.Close()
	return b.Bytes()
}

var purchaseHeader = []string{"Corespondent rand D300 la data de 01.08.2025", "Corespondent rand D300 la data de 31.07.2025", "Sectiune D301",
	"Cod taxă in SAF-T (TaxCode)", "- Activ", "Cod taxa", "Descriere cod (RO)", "Code description (EN)", "Cote TVA", "Scutit TVA", "Cotă TVA", "Comentarii cod de taxa"}

func purchases(name, code string) sheet.Sheet {
	return sheet.Sheet{Name: name, Rows: [][]string{
		{"", "", "", "", "", "NOMENCLATOR"},
		purchaseHeader,
		{"", "", "", "300", "x - Inactiv", "Cod pentru verificare", "", "", "", "0", "0.21"},
		{"5/20", "", "", code, "", "A1_100", "Achiziții 21%", "Acquisitions 21%", "21", "", "x", "Activ cu 01.08.2025"},
		{"", "Legenda procent deducere"},
	}}
}

func workbook() []sheet.Sheet {
	sheets := []sheet.Sheet{
		{Name: "Livrari", Rows: [][]string{
			{"", "", "", "", "Cod taxă in SAF-T (TaxCode)", "- Activ"},
			{"Corespondent rand D300 dupa data de 01.08.2025", "Corespondent rand D300 pana la data de 31.07.2025", "Corespondent rand D311", "Corespondent D398",
				"Cod taxa 310", "x - Inactiv", "Cod taxa", "Descriere cod (RO)", "Code description (EN)", "Start date\n\n(DATE aaaa-ll-zz)", "End date (DATE aaaa-ll-zz)",
				"Facturi cu TVA de corecție (cazuri specifice)", "Facturi de decontare / alta categorie", "Nu intra in sfera tva",
				"Pentru aceste categorii/ coduri, numerotarea nu se schimba", "Cota maxima de TVA", "Cota medie de TVA", "Cota minima de TVA", "Comentarii cod de taxa", "Cota TVA (%)"},
			{"1", "1", "", "", "310301", "", "L1", "Livrări intracomunitare scutite", "Intra-community supplies", "", "", "0", "", "", "x", "", "", "", "", "0"},
			{"9.1", "9", "1", "", "310309", "x - Inactiv", "L9", "Livrări 19%", "", "", "", "", "", "x", "", "x", "", "", "", "19"},
		}},
		{Name: "TVA_NoteContabile", Rows: [][]string{
			{"Corespondent rand D300 la data de 01.08.2025", "Corespondent rand D300 la data de 31.07.2025", "Sectiune D394", "Cod taxă in SAF-T (TaxCode)", "- Activ",
				"Cod taxa", "Descriere cod (RO)", "Code description (EN)", "Temei legal", "Cote TVA (%)"},
			{"16", "", "", "380006", "", "", "Autocolectare TVA 21%", "Self-charging VAT 21%", "", "21"},
		}},
		{Name: "TAX-IMP - Impozite", Rows: [][]string{
			{"", "NOMENCLATOR"},
			{"", "COD_IMP", "DENUMIRE", "Tax Description (in English)", "Categorie BUGET"},
			{"", "100", "Impozit pe profit", "Income tax", "1"},
			{"", "000", "Cod tehnic", "", ""},
		}},
		{Name: "TAX-IMP - Bugete", Rows: [][]string{
			{"", "COD_IMP", "DENUMIRE"},
			{"", "1", "BUGET DE STAT"},
			{"", "1", "BUGET DE STAT"},
			{"", "4", "BUGETUL ASIGURARILOR PENTRU SOMAJ'"},
		}},
		{Name: "WHT - nomenclator", Rows: [][]string{
			{"Nr. Crt.", "TaxCode", "Cod Categorie Impozit (vezi D100) TaxType", "Denumire creanta fiscala denumire utilizată în rapoarte și situații) (RO)",
				"Denumire creanta fiscala (denumire utilizată în rapoarte și situații) (EN)", "Temei legal", "Incadrare Declaratii D207, D100",
				"Cote de impozit cu reținere la sursă", "Tip de venit", "Descriere explicativă - prevederile din Codul fiscal", "Comentarii și explicații suplimentare (informativ)"},
			{"Numărul curent este indicativ", "Codul de taxe"},
			{"1", "150010", "150", "Impozit pe dividende", "<TBD>", "art.43", "D100: art.43", "0.1", "Dividende", "Lung text", ""},
			{"2", "607010", "607 Valabil din 02.2017", "Impozit pe pensii", "", "art.101", "D100", "0.1", "Pensii"},
			{"Note:"},
			{"TaxType", "TaxCode"},
			{"607 Valabil din 02.2017", "607010", "0.1"},
		}},
		{Name: "WHT - D207", Rows: [][]string{
			{"Cod", "Tip Venit"},
			{"", "1. Venituri cu reținere la sursă"},
			{"01", "a. Venituri din dividende"},
			{"", "2. Venituri scutite"},
			{"12", "a. Venituri din dobânzi"},
		}},
		{Name: "Nomenclator stocuri", Rows: [][]string{
			{"Cod_miscari_stoc", "Denumire categorie RO", "Denumire categorie ENG"},
			{"Codurile utilizate pentru completarea câmpului", "Etichetă informativă"},
			{"10", "Achiziție", "Purchase"},
			{"Note:"},
			{"20", "Nu se citește", ""},
		}},
		{Name: "Nomenclator imobilizari", Rows: [][]string{
			{"Cod-miscari-active", "Denumire categorie RO", "Denumire categorie ENG"},
			{"30", "Amortizare", "Depreciation"},
		}},
		{Name: "Nomenclator_Regim_fiscal", Rows: [][]string{
			{"", "Cod TaxType utilizat în raportarea SAF-T", "Descriere cod (RO)", "Code description (EN)"},
			{"", "100010", "Persoană impozabilă", "Taxable person"},
			{"", "100010", "Persoană impozabilă", "Taxable person"},
		}},
		{Name: "SAFT_Nomenclator_StockChar", Rows: [][]string{
			{"", "Cod StockCharacteristicValue", "Descriere cod (RO)", "Code description (EN)"},
			{"", "blue_35", "Colorant", "Dye"},
		}},
		{Name: "Nom_Tipuri_facturi", Rows: [][]string{
			{"", "Pentru completarea câmpului se va selecta una din cele doua coduri: - codul 380 - Factură, - codul 381 - Storno"},
			{"Code", "Name", "EN16931 interpretation", "Nume", "Interpretare EN16931"},
			{"380", "Commercial invoice", "Invoice", "Factură comercială", "Factură"},
			{"381", "Credit note", "Credit Note", "Notă de credit", "Notă de credit"},
			{"82", "Metered services invoice", "Invoice", "Factura servicii contorizate", "Factură"},
		}},
		{Name: "Nom_Mecanisme_plati", Rows: [][]string{
			{"", "Pentru completarea PaymentMethod: - codul 01 - Numerar Pentru completarea câmpului PaymentMechanism se vor selecta, cu prioritate: - codul 10 - numerar"},
			{"Code for Payment Mechanism", "Code used for Payment Method", "Description for Payment Method (not used for reporting)", "Code name", "Usage in EN16931",
				"Metodă de plată", "Numele de cod", "Utilizare în EN16931"},
			{"Codul pentru PaymentMechanism", "Codul pentru PaymentMethod"},
			{"10", "01", "01 - Cash", "In cash", "", "01 - Numerar", "Numerar", ""},
			{"ZZZ", "98", "98 - Mutually defined", "Mutually defined", "", "98 - Definit de comun acord", "Definit de comun acord", ""},
		}},
		{Name: "PlanConturiBalSocCom", Rows: [][]string{
			{"", "NOMENCLATOR"},
			{"Nr. cont", "Descriere [RO]", "Descriere [EN]"},
			{"Account no.", "Romanian description", "English description"},
			{"1", "Clasa 1 – Conturi de capitaluri", "Class 1 – Capital accounts"},
			{"101", "101 Capital", "101 Capital"},
		}},
		{Name: "PlanConturiONG", Rows: [][]string{
			{"", "Nr cont", "Descriere [RO]"},
			{"", "Account no", "Romanian description"},
			{"", "101", "Capital (P)"},
		}},
	}
	for _, name := range VATSheets {
		if name != "Livrari" && name != "TVA_NoteContabile" {
			sheets = append(sheets, purchases(name, fmt.Sprintf("3%02d101", len(sheets))))
		}
	}
	return sheets
}

func TestReadBook(t *testing.T) {
	b, err := ReadBook(workbook())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.VAT) != 11 {
		t.Fatalf("%d VAT codes", len(b.VAT))
	}
	var sales []VATCode
	for _, v := range b.VAT {
		if v.Sheet == "Livrari" {
			sales = append(sales, v)
		}
	}
	if len(sales) != 2 || !sales[0].Active || sales[1].Active || !sales[1].OutOfScope || *sales[1].D300 != "9.1" || *sales[1].D300Previous != "9" ||
		*sales[1].D311 != "1" || *sales[1].Rate != "19" || sales[1].NameEN != nil {
		t.Fatalf("sales %+v", sales)
	}
	if len(b.Taxes) != 2 || *b.Taxes[0].Budget != "1" || b.Taxes[1].Budget != nil {
		t.Fatalf("taxes %+v", b.Taxes)
	}
	if len(b.Budgets) != 2 || b.Budgets[1].Name != "BUGETUL ASIGURARILOR PENTRU SOMAJ" {
		t.Fatalf("budgets %+v", b.Budgets)
	}
	w := b.Withholding
	if len(w) != 2 || w[0].NameEN != nil || w[1].TaxType != "607" || *w[1].TypeNote != "Valabil din 02.2017" || *w[0].Rate != "0.1" {
		t.Fatalf("withholding %+v", w)
	}
	if len(b.IncomeTypes) != 2 || b.IncomeTypes[1].Group != "2. Venituri scutite" {
		t.Fatalf("income types %+v", b.IncomeTypes)
	}
	if len(b.StockMovements) != 1 || len(b.AssetMovements) != 1 || len(b.TaxRegimes) != 1 || len(b.StockCharacteristics) != 1 {
		t.Fatalf("short lists %+v %+v %+v %+v", b.StockMovements, b.AssetMovements, b.TaxRegimes, b.StockCharacteristics)
	}
	if len(b.InvoiceTypes) != 3 || !b.InvoiceTypes[0].SAFT || !b.InvoiceTypes[1].SAFT || b.InvoiceTypes[2].SAFT {
		t.Fatalf("invoice types %+v", b.InvoiceTypes)
	}
	p := b.PaymentMechanisms
	if len(p) != 2 || !p[0].Preferred || p[1].Preferred || p[1].MethodRO != "Definit de comun acord" {
		t.Fatalf("payment mechanisms %+v", p)
	}
	a := b.Accounts
	if len(a) != 3 || a[1].NameRO != "Capital" || *a[1].NameEN != "Capital" || a[2].Chart != "nonprofit" || a[2].NameEN != nil {
		t.Fatalf("accounts %+v", a)
	}
}

func TestReadBookStopsOnWhatItDoesNotKnow(t *testing.T) {
	change := func(sheetName string, row, col int, value string) []sheet.Sheet {
		sheets := workbook()
		for _, s := range sheets {
			if s.Name == sheetName {
				for len(s.Rows[row]) <= col {
					s.Rows[row] = append(s.Rows[row], "")
				}
				s.Rows[row][col] = value
			}
		}
		return sheets
	}
	for name, sheets := range map[string][]sheet.Sheet{
		"a new column":              change("Livrari", 1, 20, "Coloană nouă"),
		"a VAT code twice":          change("TVA_NoteContabile", 1, 3, "310301"),
		"an odd mark of inactive":   change("Livrari", 2, 5, "da"),
		"a tax of no budget":        change("TAX-IMP - Impozite", 2, 4, "7"),
		"an invoice type not there": change("Nom_Tipuri_facturi", 2, 0, "383"),
		"a new sheet of VAT codes":  append(workbook(), purchases("Achizitii noi", "399101")),
		"an odd payment method":     change("Nom_Mecanisme_plati", 3, 5, "02 - Compensare"),
		"an odd rate":               change("WHT - nomenclator", 2, 7, "zece"),
	} {
		_, err := ReadBook(sheets)
		if err == nil {
			t.Errorf("%s: read", name)
		}
		t.Logf("%s: %v", name, err)
	}
}

type site map[string][]byte

func (s site) get(address string) (io.ReadCloser, error) {
	data, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

const bookLink = "https://static.anaf.ro/static/10/Anaf/Declaratii_R/AplicatiiDec/RO_SAFT_SchemaDefCod_05.02.2026.xlsx"

func recorded() site {
	return site{
		Page:     []byte(`<a href="` + bookLink + `">Nomenclatoare</a> <a href="` + bookLink + `">din nou</a>`),
		bookLink: book(workbook()),
	}
}

func TestDataset(t *testing.T) {
	d, err := Dataset(recorded().get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != len(Files()) {
		t.Fatalf("%d files", len(d.Files))
	}
	for _, f := range d.Files {
		if _, ok := keys[f.Name]; !ok {
			t.Errorf("file %s has no key", f.Name)
		}
	}
}

func TestDatasetStopsOnAGoneCode(t *testing.T) {
	published := Published{}
	var err error
	published["accounts.jsonl"], err = ReadPublished("accounts.jsonl", strings.NewReader(`{"chart":"companies","account":"101"}`+"\n"+`{"chart":"companies","account":"102"}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dataset(recorded().get, published); err == nil || !strings.Contains(err.Error(), "companies|102") {
		t.Fatalf("got %v", err)
	}
}

func TestLinkWantsOneWorkbook(t *testing.T) {
	if _, err := Link(`<a href="https://static.anaf.ro/a/RO_SAFT_SchemaDefCod_1.xlsx">a</a><a href="https://static.anaf.ro/a/RO_SAFT_SchemaDefCod_2.xlsx">b</a>`); err == nil {
		t.Error("two workbooks read")
	}
	if _, err := Link(`nothing`); err == nil {
		t.Error("no workbook read")
	}
}
