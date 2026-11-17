package reges

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
)

// An answer in the shape of the service: COR with UUIDs, counties with their localities inside, a list whose
// ids repeat with other codes, a nested object, numbers that must stay numbers, and an empty list.
const recorded = `{
"cor": [
 {"id": "b0", "nume": "MEDIAPLANNER", "versiune": 11, "cod": 265423, "corId": "c1", "versiuneCurenta": true, "ocupatie": true},
 {"id": "a0", "nume": "INSPECTORI DE VAMĂ ŞI FRONTIERĂ", "versiune": 7, "cod": 3351, "corId": null, "versiuneCurenta": false, "ocupatie": false}
],
"judet": [
 {"id": 10, "nume": "BUCURESTI", "cod": "B", "localitati": [{"id": "x", "codSiruta": 179132, "nume": "Bucuresti", "judetId": 10}]},
 {"id": 2, "nume": "ARGES", "cod": "AG", "localitati": []}
],
"formaJuridica": [
 {"id": 0, "nume": "Societate Comerciala", "cod": "SocietateComerciala", "categorieAngajator": {"id": 0, "nume": "Persoana Juridica", "cod": "PersoanaJuridica"}, "extraInfo": 0},
 {"id": 0, "nume": "Persoana fizica", "cod": "PF", "categorieAngajator": {"id": 1, "nume": "Persoana Fizica", "cod": "PersoanaFizica"}, "extraInfo": 0}
],
"tipSporAngajator": []
}`

type site map[string]string

func (s site) get(address string) (io.ReadCloser, error) {
	text, ok := s[address]
	if !ok {
		return nil, fmt.Errorf("%s answered 404 Not Found", address)
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

func TestFileNameFollowsTheListName(t *testing.T) {
	for list, want := range map[string]string{
		"cor":                               "cor.jsonl",
		"tipContractMunca":                  "tip-contract-munca.jsonl",
		"actIdentitatePF":                   "act-identitate-pf.jsonl",
		"anexaL153":                         "anexa-l153.jsonl",
		"specialitateStructuraAprobataL153": "specialitate-structura-aprobata-l153.jsonl",
	} {
		if got := FileName(list); got != want {
			t.Errorf("%s: %s, want %s", list, got, want)
		}
	}
}

func lines(t *testing.T, records []any) string {
	t.Helper()
	data, err := dataset.Lines(records)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReadSortsEachListAndLeavesOutTheRepeatedLocalities(t *testing.T) {
	lists, empty, err := Read(strings.NewReader(recorded))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(empty, ",") != "tipSporAngajator" || len(lists) != 3 {
		t.Fatalf("lists %d, empty %v", len(lists), empty)
	}
	byFile := map[string]List{}
	for _, l := range lists {
		byFile[l.File] = l
	}

	var judet []any
	for _, e := range byFile["judet.jsonl"].Entries {
		judet = append(judet, e)
	}
	if got := lines(t, judet); got != "{\"cod\":\"AG\",\"id\":2,\"nume\":\"ARGES\"}\n{\"cod\":\"B\",\"id\":10,\"nume\":\"BUCURESTI\"}\n" {
		t.Fatalf("judet\n%s", got)
	}
	if f := strings.Join(byFile["judet.jsonl"].Fields, ","); f != "id,nume,cod" {
		t.Fatalf("judet fields %s", f)
	}

	var cor []any
	for _, e := range byFile["cor.jsonl"].Entries {
		cor = append(cor, e)
	}
	want := `{"cod":3351,"corId":null,"id":"a0","nume":"INSPECTORI DE VAMĂ ŞI FRONTIERĂ","ocupatie":false,"versiune":7,"versiuneCurenta":false}` + "\n" +
		`{"cod":265423,"corId":"c1","id":"b0","nume":"MEDIAPLANNER","ocupatie":true,"versiune":11,"versiuneCurenta":true}` + "\n"
	if got := lines(t, cor); got != want {
		t.Fatalf("cor\n%s\nwant\n%s", got, want)
	}

	if keys := []string{Key(byFile["forma-juridica.jsonl"].Entries[0]), Key(byFile["forma-juridica.jsonl"].Entries[1])}; strings.Join(keys, " ") != "0|PF 0|SocietateComerciala" {
		t.Fatalf("forma juridica keys %v", keys)
	}
}

func TestReadStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, text := range map[string]string{
		"unknown field":  `{"tipTura": [{"id": 1, "nume": "Tura 1", "cod": "T1", "durata": 8}]}`,
		"twice":          `{"tipTura": [{"id": 1, "nume": "Tura 1", "cod": "T1"}, {"id": 1, "nume": "Tura 1", "cod": "T1"}]}`,
		"no id":          `{"tipTura": [{"nume": "Tura 1", "cod": "T1"}]}`,
		"only empty":     `{"tipTura": []}`,
		"not the answer": `<html>`,
	} {
		if _, _, err := Read(strings.NewReader(text)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestDatasetReadsTheRecordedService(t *testing.T) {
	d, err := Dataset(site{Address: recorded}.get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 3 || !strings.Contains(d.Description, "not here: tipSporAngajator.") {
		t.Fatalf("files %d, description %s", len(d.Files), d.Description)
	}
	for _, f := range d.Files {
		for _, field := range f.Fields {
			if field.Description == "" {
				t.Errorf("%s: field %s has no description", f.Name, field.Name)
			}
		}
	}
}

func TestDatasetStopsWhenAPublishedEntryIsGone(t *testing.T) {
	published := map[string][]string{"cor.jsonl": {"a0|3351", "gone|1"}, "judet.jsonl": {"2|AG"}}
	_, err := Dataset(site{Address: recorded}.get, published)
	if err == nil || !strings.Contains(err.Error(), "cor.jsonl gone|1") {
		t.Fatalf("error %v", err)
	}
}

func TestPublishedKeysReadsAFileOfTheDataset(t *testing.T) {
	keys, err := PublishedKeys(bytes.NewReader([]byte("{\"cod\":3351,\"id\":\"a0\"}\n{\"id\":7}\n")))
	if err != nil || strings.Join(keys, " ") != "a0|3351 7|" {
		t.Fatalf("%v %v", keys, err)
	}
}
