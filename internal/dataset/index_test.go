package dataset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type county struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func sample(names ...string) Dataset {
	var records []any
	for i, n := range names {
		records = append(records, county{Code: string(rune('A' + i)), Name: n})
	}
	return Dataset{
		Name: "sample", Title: "Sample", Description: "A sample.", Update: "Never",
		Source: Source{Name: "Test", URL: "https://example.test", Licence: "None"},
		Files: []File{{
			Name:    "counties.jsonl",
			Fields:  []Field{{Name: "code", Description: "Code"}, {Name: "name", Description: "Name"}},
			Records: records,
		}},
	}
}

func publish(t *testing.T, root string, d Dataset, today string) []string {
	t.Helper()
	changed, err := Publish(root, []Dataset{d}, today)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPublishWritesOneRecordOnEachLineAsTyped(t *testing.T) {
	root := t.TempDir()
	publish(t, root, sample("Alba", "Brașov <&>"), "2026-11-17")

	got := read(t, filepath.Join(root, "datasets", "sample", "counties.jsonl"))
	want := "{\"code\":\"A\",\"name\":\"Alba\"}\n{\"code\":\"B\",\"name\":\"Brașov <&>\"}\n"
	if got != want {
		t.Fatalf("file is\n%s\nwant\n%s", got, want)
	}
}

func TestTheIndexNamesTheFileWithItsHashSizeAndRecords(t *testing.T) {
	root := t.TempDir()
	publish(t, root, sample("Alba"), "2026-11-17")

	index, err := ReadIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	d := index.Datasets[0]
	f := d.Files[0]
	// printf '{"code":"A","name":"Alba"}\n' | shasum -a 256
	const sum = "436f1a61fb6087f2ff5aef748f805c3e77a79929d43570572ae3b4d7df33b393"
	if index.Format != 1 || index.Licence != "CC-BY-4.0" || d.Revision != 1 || d.Changed != "2026-11-17" {
		t.Fatalf("index head is %+v, dataset %+v", index, d)
	}
	if f.Path != "datasets/sample/counties.jsonl" || f.Bytes != 27 || f.Records != 1 || f.SHA256 != sum {
		t.Fatalf("file entry is %+v", f)
	}
	if err := Verify(root); err != nil {
		t.Fatal(err)
	}
}

func TestASecondBuildWithTheSameContentChangesNothing(t *testing.T) {
	root := t.TempDir()
	publish(t, root, sample("Alba"), "2026-11-17")
	before := read(t, filepath.Join(root, IndexName))

	changed := publish(t, root, sample("Alba"), "2026-12-01")

	if len(changed) != 0 {
		t.Fatalf("changed = %v, want none", changed)
	}
	if after := read(t, filepath.Join(root, IndexName)); after != before {
		t.Fatalf("the index changed:\n%s", after)
	}
}

func TestChangedContentGetsTheNextRevisionAndTheDay(t *testing.T) {
	root := t.TempDir()
	publish(t, root, sample("Alba"), "2026-11-17")

	changed := publish(t, root, sample("Alba", "Arad"), "2026-12-01")

	index, _ := ReadIndex(root)
	if len(changed) != 1 || changed[0] != "sample" {
		t.Fatalf("changed = %v", changed)
	}
	if d := index.Datasets[0]; d.Revision != 2 || d.Changed != "2026-12-01" {
		t.Fatalf("revision %d on %s, want 2 on 2026-12-01", d.Revision, d.Changed)
	}
}

func TestAChangedDescriptionIsAChange(t *testing.T) {
	root := t.TempDir()
	publish(t, root, sample("Alba"), "2026-11-17")
	d := sample("Alba")
	d.Source.Licence = "Other"

	if changed := publish(t, root, d, "2026-12-01"); len(changed) != 1 {
		t.Fatalf("changed = %v, want the dataset", changed)
	}
}

func TestAFileNoLongerBuiltIsRemoved(t *testing.T) {
	root := t.TempDir()
	d := sample("Alba")
	d.Files = append(d.Files, File{Name: "old.jsonl", Fields: d.Files[0].Fields, Records: d.Files[0].Records})
	publish(t, root, d, "2026-11-17")

	publish(t, root, sample("Alba"), "2026-11-18")

	if _, err := os.Stat(filepath.Join(root, "datasets", "sample", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old.jsonl is still there: %v", err)
	}
	if err := Verify(root); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFindsWhatDoesNotMatchTheIndex(t *testing.T) {
	file := filepath.Join("datasets", "sample", "counties.jsonl")
	cases := map[string]func(root string) error{
		"does not match the SHA-256": func(root string) error {
			return os.WriteFile(filepath.Join(root, file), []byte("{\"code\":\"A\",\"name\":\"Alta\"}\n"), 0o644)
		},
		"no such file": func(root string) error { return os.Remove(filepath.Join(root, file)) },
		"is not in the index": func(root string) error {
			return os.WriteFile(filepath.Join(root, "datasets", "sample", "extra.jsonl"), []byte("{}\n"), 0o644)
		},
	}
	for want, damage := range cases {
		root := t.TempDir()
		publish(t, root, sample("Alba"), "2026-11-17")
		if err := damage(root); err != nil {
			t.Fatal(err)
		}
		if err := Verify(root); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Verify = %v, want an error with %q", err, want)
		}
	}
}

func TestADatasetThatCannotBePublishedIsRefused(t *testing.T) {
	cases := map[string]func(d *Dataset){
		"dataset name": func(d *Dataset) { d.Name = "Sample Data" },
		"licence":      func(d *Dataset) { d.Source.Licence = "" },
		"title":        func(d *Dataset) { d.Title = "" },
		"no files":     func(d *Dataset) { d.Files = nil },
		"file name":    func(d *Dataset) { d.Files[0].Name = "Counties.csv" },
		"no fields":    func(d *Dataset) { d.Files[0].Fields = nil },
		"no records":   func(d *Dataset) { d.Files[0].Records = nil },
		"twice":        func(d *Dataset) { d.Files = append(d.Files, d.Files[0]) },
	}
	for want, breakIt := range cases {
		d := sample("Alba")
		breakIt(&d)
		if _, err := Publish(t.TempDir(), []Dataset{d}, "2026-11-17"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Publish = %v, want an error with %q", err, want)
		}
	}
}

func TestTheSameDatasetTwiceIsRefused(t *testing.T) {
	if _, err := Publish(t.TempDir(), []Dataset{sample("Alba"), sample("Arad")}, "2026-11-17"); err == nil {
		t.Fatal("two datasets with one name were published")
	}
}
