// Package reges builds the nomenclatures of REGES-Online, the employee register of the Labour Inspectorate,
// from its public nomenclature service, with the REGES identifier of every entry.
package reges

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Address is the service that gives every list at once. It needs no key.
const Address = "https://api.inspectiamuncii.ro/api/Nomenclator?tip=toate"

// Repository documents the service.
const Repository = "https://github.com/reges-ro/integrare"

// maxAnswer is several times the size of the answer.
const maxAnswer = 64 << 20

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the service over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "application/json") }
}

// Fields describes every field the lists have. A field not here stops the build.
var Fields = map[string]string{
	"id":                   "The REGES identifier of the entry: a number, or a UUID in the longer lists. With cod it is unique in the file",
	"nume":                 "The name as REGES writes it",
	"cod":                  "The code of the entry in REGES messages",
	"versiune":             "The version of the classification the entry belongs to",
	"versiuneCurenta":      "True for an entry of the current version",
	"ocupatie":             "True for an occupation, false for a group above the occupations",
	"corId":                "The corId REGES gives the entry, or null",
	"domeniuActivitateId":  "The domeniuActivitateId REGES gives the entry, or null",
	"isDomeniuActivitate":  "True for a CAEN class, false for a section, division or group",
	"codSiruta":            "The SIRUTA code of the locality, a code of units.jsonl in siruta",
	"judetId":              "The id of its county in judet.jsonl",
	"cetatenie":            "The citizenship, an id of cetatenie.jsonl, alone or in an object",
	"capitole":             "The chapters of the annex, with their letters and functions, as REGES gives them",
	"tipuriActeIdentitate": "The identity document types for this citizenship, as REGES gives them",
	"categorieAngajator":   "The employer category, as REGES gives it",
	"extraInfo":            "The extraInfo number REGES gives the entry",
	"formaJuridica":        "The legal form, as REGES gives it",
	"abreviere":            "The country letters",
	"tipNorma":             "The type of working time, as REGES gives it",
	"categorie":            "The category of the ground",
	"abrogat":              "True for a ground that was repealed",
	"actNormativ":          "The legal act of the ground",
	"traduceri":            "The name in other languages, as REGES gives it",
	"legea153":             "True for a contract type under Law 153/2017 on the pay of public staff",
	"descriere":            "The description REGES gives",
	"numeAfisat":           "The name REGES shows",
}

// dropped are fields left out of a list because another list has the same entries.
var dropped = map[string]string{"judet": "localitati"}

// FileName turns a list name of REGES into a file name: tipContractMunca is tip-contract-munca.jsonl,
// actIdentitatePF is act-identitate-pf.jsonl.
func FileName(list string) string {
	var b strings.Builder
	runes := []rune(list)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			startsWord := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) ||
				(i+1 < len(runes) && unicode.IsLower(runes[i+1])))
			if startsWord {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		if unicode.IsDigit(r) && i > 0 && unicode.IsLower(runes[i-1]) {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String() + ".jsonl"
}

// List is one list with its entries, sorted.
type List struct {
	Name    string
	File    string
	Fields  []string
	Entries []map[string]any
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// Key is what identifies an entry in its list: the id, and the code where there is one.
func Key(entry map[string]any) string {
	return text(entry["id"]) + "|" + text(entry["cod"])
}

// less orders entries by id, numbers as numbers, then by code.
func less(a, b map[string]any) bool {
	ia, na := a["id"].(json.Number)
	ib, nb := b["id"].(json.Number)
	if na && nb {
		x, errX := ia.Int64()
		y, errY := ib.Int64()
		if errX == nil && errY == nil && x != y {
			return x < y
		}
	}
	if text(a["id"]) != text(b["id"]) {
		return text(a["id"]) < text(b["id"])
	}
	return text(a["cod"]) < text(b["cod"])
}

// Read reads the answer of the service: every list with entries, by name.
func Read(r io.Reader) ([]List, []string, error) {
	decoder := json.NewDecoder(io.LimitReader(r, maxAnswer))
	decoder.UseNumber()
	var answer map[string][]map[string]any
	if err := decoder.Decode(&answer); err != nil {
		return nil, nil, fmt.Errorf("not the REGES nomenclature answer: %w", err)
	}

	var lists []List
	var empty []string
	files := map[string]string{}
	for name, entries := range answer {
		if len(entries) == 0 {
			empty = append(empty, name)
			continue
		}
		file := FileName(name)
		if other, twice := files[file]; twice {
			return nil, nil, fmt.Errorf("lists %s and %s have the same file name %s", other, name, file)
		}
		files[file] = name

		seenField := map[string]bool{}
		seenKey := map[string]bool{}
		for _, e := range entries {
			if drop, ok := dropped[name]; ok {
				delete(e, drop)
			}
			if e["id"] == nil {
				return nil, nil, fmt.Errorf("list %s has an entry without id", name)
			}
			for field := range e {
				if Fields[field] == "" {
					return nil, nil, fmt.Errorf("list %s has the field %s, which the build does not know", name, field)
				}
				seenField[field] = true
			}
			if seenKey[Key(e)] {
				return nil, nil, fmt.Errorf("list %s has the entry %s twice", name, Key(e))
			}
			seenKey[Key(e)] = true
		}
		sort.SliceStable(entries, func(i, j int) bool { return less(entries[i], entries[j]) })

		var fields []string
		for f := range seenField {
			fields = append(fields, f)
		}
		// id, nume and cod first, the others by name.
		order := map[string]int{"id": 0, "nume": 1, "cod": 2}
		sort.Slice(fields, func(i, j int) bool {
			oi, iok := order[fields[i]]
			oj, jok := order[fields[j]]
			switch {
			case iok && jok:
				return oi < oj
			case iok != jok:
				return iok
			}
			return fields[i] < fields[j]
		})
		lists = append(lists, List{Name: name, File: file, Fields: fields, Entries: entries})
	}
	if len(lists) == 0 {
		return nil, nil, fmt.Errorf("the answer has no list with entries")
	}
	sort.Slice(lists, func(i, j int) bool { return lists[i].File < lists[j].File })
	sort.Strings(empty)
	return lists, empty, nil
}

// PublishedKeys reads one file of the published dataset and gives the key of each entry.
func PublishedKeys(r io.Reader) ([]string, error) {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()
	var keys []string
	for {
		var e map[string]any
		err := decoder.Decode(&e)
		if err == io.EOF {
			return keys, nil
		}
		if err != nil {
			return nil, fmt.Errorf("published entries: %w", err)
		}
		keys = append(keys, Key(e))
	}
}

// Dataset reads the service and builds the dataset. published gives, for each file, the keys of the
// dataset as published before; an entry missing now stops the build, since an entry is never removed.
func Dataset(get Get, published map[string][]string) (dataset.Dataset, error) {
	body, err := get(Address)
	if err != nil {
		return dataset.Dataset{}, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxAnswer+1))
	if err != nil {
		return dataset.Dataset{}, err
	}
	if len(data) > maxAnswer {
		return dataset.Dataset{}, fmt.Errorf("%s is larger than expected", Address)
	}
	lists, empty, err := Read(bytes.NewReader(data))
	if err != nil {
		return dataset.Dataset{}, err
	}

	have := map[string]map[string]bool{}
	var files []dataset.File
	for _, l := range lists {
		have[l.File] = map[string]bool{}
		var records []any
		for _, e := range l.Entries {
			have[l.File][Key(e)] = true
			records = append(records, e)
		}
		var fields []dataset.Field
		for _, f := range l.Fields {
			fields = append(fields, dataset.Field{Name: f, Description: Fields[f]})
		}
		files = append(files, dataset.File{Name: l.File, Fields: fields, Records: records})
	}
	var missing []string
	for file, keys := range published {
		for _, k := range keys {
			if !have[file][k] {
				missing = append(missing, file+" "+k)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return dataset.Dataset{}, fmt.Errorf("REGES no longer has the published entries %s: a person decides", strings.Join(missing, ", "))
	}

	description := "The nomenclatures of REGES-Online, the employee register of the Labour Inspectorate, as its public nomenclature service gives them: " +
		"COR occupations in every version with the current one marked, activity domains (CAEN), counties and localities with their SIRUTA codes, " +
		"nationalities, legal and organisation forms, contract types, working time, grounds for termination, suspension and secondment, " +
		"predefined bonuses, identity documents, disability grades, education levels, and the lists of the public pay law 153/2017. " +
		"One file for each list, named after it; entries as REGES gives them, with their REGES identifier, which a message to REGES needs. " +
		"Names are written as REGES writes them, some with Romanian letters and some without. " +
		"The localities inside each county are left out of judet.jsonl: localitate.jsonl has them, with judetId."
	if len(empty) > 0 {
		description += " Lists that have no entry today are not here: " + strings.Join(empty, ", ") + "."
	}

	return dataset.Dataset{
		Name:        "reges",
		Title:       "Nomenclatures of REGES-Online",
		Description: description,
		Source: dataset.Source{
			Name:    "Labour Inspectorate (Inspecția Muncii), REGES-Online nomenclature service " + Address + ", documented in " + Repository,
			URL:     Repository,
			Licence: "No licence is stated. The Labour Inspectorate publishes the nomenclatures through a public service that needs no key, and its documentation asks applications to store them",
		},
		Frequency: dataset.Monthly,
		Update:    "When the Labour Inspectorate changes a list; COR changes by order several times a year",
		Files:     files,
	}, nil
}
