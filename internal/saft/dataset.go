package saft

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/sheet"
)

// keys gives, by file, the fields that name a record in it.
var keys = map[string][]string{
	"vat-codes.jsonl":             {"tax_code"},
	"taxes.jsonl":                 {"code"},
	"budgets.jsonl":               {"code"},
	"withholding-codes.jsonl":     {"tax_code"},
	"income-types.jsonl":          {"code"},
	"stock-movements.jsonl":       {"code"},
	"asset-movements.jsonl":       {"code"},
	"tax-regimes.jsonl":           {"code"},
	"stock-characteristics.jsonl": {"code"},
	"invoice-types.jsonl":         {"code"},
	"payment-mechanisms.jsonl":    {"code"},
	"accounts.jsonl":              {"chart", "account"},
}

// Files are the names of the files of the dataset.
func Files() []string {
	var out []string
	for name := range keys {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Published gives, by file, the keys of the records published before.
type Published map[string][]string

// ReadPublished reads the keys of one published file.
func ReadPublished(file string, r io.Reader) ([]string, error) {
	fields, ok := keys[file]
	if !ok {
		return nil, fmt.Errorf("the dataset has no file %s", file)
	}
	var out []string
	d := json.NewDecoder(r)
	for d.More() {
		var record map[string]any
		if err := d.Decode(&record); err != nil {
			return nil, fmt.Errorf("published %s: %w", file, err)
		}
		out = append(out, key(record, fields))
	}
	return out, nil
}

func key(record map[string]any, fields []string) string {
	var parts []string
	for _, f := range fields {
		parts = append(parts, fmt.Sprint(record[f]))
	}
	return strings.Join(parts, "|")
}

func records[T any](rows []T) []any {
	var out []any
	for _, r := range rows {
		out = append(out, r)
	}
	return out
}

// check stops the build when a published record is gone from the workbook: a code is never removed, and a
// person decides what a gone code means.
func check(files []dataset.File, published Published) error {
	for _, f := range files {
		have := map[string]bool{}
		for _, r := range f.Records {
			x, _ := json.Marshal(r)
			var record map[string]any
			json.Unmarshal(x, &record)
			have[key(record, keys[f.Name])] = true
		}
		var gone []string
		for _, k := range published[f.Name] {
			if !have[k] {
				gone = append(gone, k)
			}
		}
		if len(gone) > 0 {
			sort.Strings(gone)
			return fmt.Errorf("%s: the workbook no longer has %s; a person decides what this means", f.Name, strings.Join(gone, ", "))
		}
	}
	return nil
}

func read(get Get, address string, limit int64) ([]byte, error) {
	body, err := get(address)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", address)
	}
	return data, nil
}

// Dataset reads the newest workbook and builds the dataset.
func Dataset(get Get, published Published) (dataset.Dataset, error) {
	page, err := read(get, Page, maxPage)
	if err != nil {
		return dataset.Dataset{}, err
	}
	link, err := Link(string(page))
	if err != nil {
		return dataset.Dataset{}, err
	}
	data, err := read(get, link, maxBook)
	if err != nil {
		return dataset.Dataset{}, err
	}
	sheets, err := sheet.Read(data)
	if err != nil {
		return dataset.Dataset{}, fmt.Errorf("%s: %w", link, err)
	}
	b, err := ReadBook(sheets)
	if err != nil {
		return dataset.Dataset{}, fmt.Errorf("%s: %w", link, err)
	}
	named := []dataset.Field{
		{Name: "code", Description: "The code to report"},
		{Name: "name_ro", Description: "The name in Romanian, as the workbook writes it"},
		{Name: "name_en", Description: "The name in English, as the workbook writes it, or null"},
	}
	files := []dataset.File{
		{Name: "accounts.jsonl", Records: records(b.Accounts), Fields: []dataset.Field{
			{Name: "chart", Description: "companies (sheet PlanConturiBalSocCom) or nonprofit (sheet PlanConturiONG)"},
			{Name: "account", Description: "The account, from the class (one digit) to the analytic account (four digits). With chart it is unique in the file"},
			{Name: "name_ro", Description: "The name in Romanian, without the number the workbook writes before it"},
			{Name: "name_en", Description: "The name in English, or null where the chart has none"},
		}},
		{Name: "asset-movements.jsonl", Records: records(b.AssetMovements), Fields: named},
		{Name: "budgets.jsonl", Records: records(b.Budgets), Fields: []dataset.Field{
			{Name: "code", Description: "The category of budgets"},
			{Name: "name", Description: "Its name, as the workbook writes it"},
		}},
		{Name: "income-types.jsonl", Records: records(b.IncomeTypes), Fields: []dataset.Field{
			{Name: "code", Description: "The type of income of declaration D207"},
			{Name: "name", Description: "Its name, as the workbook writes it"},
			{Name: "group", Description: "The group of the type: incomes taxed at source, or exempt"},
		}},
		{Name: "invoice-types.jsonl", Records: records(b.InvoiceTypes), Fields: []dataset.Field{
			{Name: "code", Description: "The type of invoice, from the list of EN 16931"},
			{Name: "name_en", Description: "The name in English"},
			{Name: "en16931_kind_en", Description: "What EN 16931 takes it for, Invoice or Credit Note"},
			{Name: "name_ro", Description: "The name in Romanian"},
			{Name: "en16931_kind_ro", Description: "What EN 16931 takes it for, in Romanian"},
			{Name: "saft", Description: "True for the types the instructions of the sheet tell to report in InvoiceType"},
		}},
		{Name: "payment-mechanisms.jsonl", Records: records(b.PaymentMechanisms), Fields: []dataset.Field{
			{Name: "code", Description: "The code of PaymentMechanism"},
			{Name: "payment_method", Description: "The code of PaymentMethod the mechanism falls under: 01 cash, 02 offset, 03 non-cash, 98 mutually defined, 99 not defined"},
			{Name: "payment_method_en", Description: "The name of the payment method in English"},
			{Name: "payment_method_ro", Description: "The name of the payment method in Romanian"},
			{Name: "name_en", Description: "The name of the mechanism in English"},
			{Name: "name_ro", Description: "The name of the mechanism in Romanian"},
			{Name: "en16931_use_en", Description: "How EN 16931 uses it, in English, or null"},
			{Name: "en16931_use_ro", Description: "How EN 16931 uses it, in Romanian, or null"},
			{Name: "preferred", Description: "True for the mechanisms the instructions of the sheet tell to use first"},
		}},
		{Name: "stock-characteristics.jsonl", Records: records(b.StockCharacteristics), Fields: named},
		{Name: "stock-movements.jsonl", Records: records(b.StockMovements), Fields: named},
		{Name: "tax-regimes.jsonl", Records: records(b.TaxRegimes), Fields: named},
		{Name: "taxes.jsonl", Records: records(b.Taxes), Fields: []dataset.Field{
			{Name: "code", Description: "The tax, three digits, which TaxType takes"},
			{Name: "name_ro", Description: "The name in Romanian, as the workbook writes it"},
			{Name: "name_en", Description: "The name in English, or null"},
			{Name: "budget", Description: "The category of budgets in budgets.jsonl, or null"},
		}},
		{Name: "vat-codes.jsonl", Records: records(b.VAT), Fields: []dataset.Field{
			{Name: "tax_code", Description: "The VAT tax code to report in TaxCode, six digits"},
			{Name: "sheet", Description: "The sheet of the workbook, which says the kind of operation: Livrari for sales, Achizitii and the share of VAT deducted for purchases, TVA_NoteContabile for ledger entries without a source document"},
			{Name: "label", Description: "The short label of the workbook, as L1 or A1_100, or null"},
			{Name: "name_ro", Description: "The name in Romanian"},
			{Name: "name_en", Description: "The name in English, or null"},
			{Name: "rate", Description: "The VAT rate in percent as the workbook writes it, as 21 or 0, or its words where it depends on another state; null where it gives none"},
			{Name: "active", Description: "False for a code the workbook marks inactive"},
			{Name: "out_of_scope", Description: "True for an operation the workbook marks as outside the scope of VAT"},
			{Name: "d300_row", Description: "The rows of the D300 form in force from 2025-08-01 the code goes to, as the workbook writes them, or null"},
			{Name: "d300_row_previous", Description: "The rows of the D300 form in force until 2025-07-31, or null"},
			{Name: "d301_section", Description: "The section of D301, or null"},
			{Name: "d394_section", Description: "The section of D394, or null"},
			{Name: "d311_row", Description: "The row of D311, or null"},
			{Name: "d398_row", Description: "The row of D398, or null"},
			{Name: "legal_basis", Description: "The legal basis the workbook gives, or null"},
			{Name: "start_date", Description: "The start date the workbook gives, or null"},
			{Name: "end_date", Description: "The end date the workbook gives, or null"},
			{Name: "comments", Description: "The comment of the workbook, as \"Activ cu 01.08.2025\", or null"},
		}},
		{Name: "withholding-codes.jsonl", Records: records(b.Withholding), Fields: []dataset.Field{
			{Name: "tax_code", Description: "The tax code to report in TaxCode for a tax withheld at source"},
			{Name: "tax_type", Description: "The tax it belongs to, in taxes.jsonl"},
			{Name: "tax_type_note", Description: "What the workbook writes after the tax type, as \"Valabil din 02.2017\", or null"},
			{Name: "name_ro", Description: "The name in Romanian"},
			{Name: "name_en", Description: "The name in English, or null"},
			{Name: "legal_basis", Description: "The legal basis, or null"},
			{Name: "declarations", Description: "Where the tax is declared, D100 or D207, as the workbook writes it, or null"},
			{Name: "rate", Description: "The rate as a fraction, as 0.1 for 10 percent, or null"},
			{Name: "income_type", Description: "The kind of income, or null"},
		}},
	}
	if err := check(files, published); err != nil {
		return dataset.Dataset{}, err
	}
	return dataset.Dataset{
		Name:  "saft",
		Title: "Nomenclatures of the SAF-T file (D406)",
		Description: "The nomenclatures ANAF publishes in its workbook of schema definitions and codes for D406, the SAF-T file: the VAT tax codes " +
			"with the rows of D300, D301, D394, D311 and D398 they go to, the taxes and the categories of budgets, the tax codes of taxes withheld " +
			"at source and the types of income of D207, the movement codes of stock and of assets, the tax regimes, the characteristics of " +
			"excise goods, the types of invoices, the payment mechanisms, and the charts of accounts of companies and of non-profit entities as " +
			"SAF-T takes them. The newest workbook the page of D406 links is read. Names are as the workbook writes them. Lists the dataset " +
			"already has elsewhere (countries, currencies, units, the catalogue of fixed assets) and the combined nomenclature are left out.",
		Source: dataset.Source{
			Name:    "ANAF, workbook of SAF-T schema definitions and codes (" + link + ")",
			URL:     link,
			Licence: "Official administrative text of ANAF, the nomenclatures of declaration D406, not subject to copyright (Law 8/1996, article 9). ANAF states no licence",
		},
		Frequency: dataset.Monthly,
		Update:    "When ANAF publishes a new workbook of codes",
		Files:     files,
	}, nil
}
