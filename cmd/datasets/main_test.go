package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
)

var day = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

// run2 runs the command with the real list of datasets.
func run2(args []string, now time.Time, out, errs *bytes.Buffer) int {
	return run(args, builders, now, out, errs)
}

func fake(name string, frequency dataset.Frequency, value *string) builder {
	return builder{name, frequency, func(time.Time, string) (dataset.Dataset, error) {
		return dataset.Dataset{
			Name: name, Title: name, Description: name, Frequency: frequency, Update: "In tests",
			Source: dataset.Source{Name: "Test", URL: "https://example.test", Licence: "None"},
			Files: []dataset.File{{
				Name:    "values.jsonl",
				Fields:  []dataset.Field{{Name: "value", Description: "A value"}},
				Records: []any{map[string]string{"value": *value}},
			}},
		}, nil
	}}
}

func TestAFrequencyBuildRebuildsOnlyItsDatasets(t *testing.T) {
	root := t.TempDir()
	daily, manual := "1", "1"
	all := []builder{fake("rates", dataset.Daily, &daily), fake("counties", dataset.Manual, &manual)}
	var out, errs bytes.Buffer
	if code := run([]string{"build", "-root", root}, all, day, &out, &errs); code != 0 {
		t.Fatalf("first build = %d: %s", code, errs.String())
	}

	daily, manual = "2", "2"
	out.Reset()
	if code := run([]string{"build", "-root", root, "-frequency", "daily"}, all, day, &out, &errs); code != 0 {
		t.Fatalf("daily build = %d: %s", code, errs.String())
	}

	if !strings.Contains(out.String(), "1 dataset(s) built, 1 changed rates") {
		t.Fatalf("build said %q", out.String())
	}
	rates, _ := os.ReadFile(filepath.Join(root, "datasets", "rates", "values.jsonl"))
	counties, _ := os.ReadFile(filepath.Join(root, "datasets", "counties", "values.jsonl"))
	if !strings.Contains(string(rates), `"2"`) || !strings.Contains(string(counties), `"1"`) {
		t.Fatalf("rates %s counties %s", rates, counties)
	}
	if code := run([]string{"verify", "-root", root}, all, day, &out, &errs); code != 0 {
		t.Fatalf("verify = %d: %s", code, errs.String())
	}
}

func TestOnlyBuildsOneDatasetEvenAManualOne(t *testing.T) {
	root := t.TempDir()
	daily, manual := "1", "1"
	all := []builder{fake("rates", dataset.Daily, &daily), fake("counties", dataset.Manual, &manual)}
	var out, errs bytes.Buffer
	run([]string{"build", "-root", root}, all, day, &out, &errs)

	daily, manual = "2", "2"
	out.Reset()
	run([]string{"build", "-root", root, "-only", "counties"}, all, day, &out, &errs)

	if !strings.Contains(out.String(), "1 dataset(s) built, 1 changed counties") {
		t.Fatalf("build said %q", out.String())
	}
}

func TestAFilterThatSelectsNothingRightIsRefused(t *testing.T) {
	value := "1"
	all := []builder{fake("rates", dataset.Daily, &value)}
	var out, errs bytes.Buffer
	for _, args := range [][]string{
		{"build", "-frequency", "manual"},
		{"build", "-frequency", "hourly"},
		{"build", "-only", "nothing"},
		{"build", "-only", "rates", "-frequency", "daily"},
	} {
		if code := run(append(args, "-root", t.TempDir()), all, day, &out, &errs); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}

func TestADatasetListedUnderAnotherFrequencyIsRefused(t *testing.T) {
	value := "1"
	wrong := fake("rates", dataset.Daily, &value)
	wrong.frequency = dataset.Weekly
	var out, errs bytes.Buffer
	if code := run([]string{"build", "-root", t.TempDir()}, []builder{wrong}, day, &out, &errs); code != 2 {
		t.Fatalf("build = %d, want 2", code)
	}
}

func TestListNamesEveryDatasetWithItsFrequency(t *testing.T) {
	var out, errs bytes.Buffer
	run2([]string{"list"}, day, &out, &errs)
	for _, want := range []string{"bnr-currencies\tmanual", "countries\tmanual", "public-holidays\tmanual", "vat-numbers\tmanual", "vat-rates\tweekly"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("list said %q, want %q in it", out.String(), want)
		}
	}
}

func TestBuildThenVerifyThenBuildAgainChangesNothing(t *testing.T) {
	root := t.TempDir()
	value := "1"
	all := []builder{fake("counties", dataset.Manual, &value)}
	var out, errs bytes.Buffer

	if code := run([]string{"build", "-root", root}, all, day, &out, &errs); code != 0 {
		t.Fatalf("build = %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "1 changed counties") {
		t.Fatalf("build said %q", out.String())
	}
	if code := run([]string{"verify", "-root", root}, all, day, &out, &errs); code != 0 {
		t.Fatalf("verify = %d: %s", code, errs.String())
	}

	index, _ := os.ReadFile(filepath.Join(root, "index.json"))
	out.Reset()
	if code := run([]string{"build", "-root", root}, all, day.AddDate(0, 0, 20), &out, &errs); code != 0 {
		t.Fatalf("second build = %d: %s", code, errs.String())
	}
	again, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if !bytes.Equal(index, again) || !strings.Contains(out.String(), "0 changed") {
		t.Fatalf("the second build changed the index; it said %q", out.String())
	}
}

func TestVerifyFailsOnADamagedFile(t *testing.T) {
	root := t.TempDir()
	value := "1"
	all := []builder{fake("counties", dataset.Manual, &value)}
	var out, errs bytes.Buffer
	run([]string{"build", "-root", root}, all, day, &out, &errs)
	os.WriteFile(filepath.Join(root, "datasets", "counties", "values.jsonl"), []byte("{}\n"), 0o644)

	if code := run([]string{"verify", "-root", root}, all, day, &out, &errs); code != 1 {
		t.Fatalf("verify = %d, want 1", code)
	}
}

func TestAWrongCommandIsRefused(t *testing.T) {
	var out, errs bytes.Buffer
	for _, args := range [][]string{{}, {"publish"}, {"build", "-nope"}} {
		if code := run2(args, day, &out, &errs); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}

func TestEveryListedDatasetHasANameOfItsOwnAndAFrequency(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range builders {
		if seen[b.name] || !b.frequency.Valid() || b.build == nil {
			t.Errorf("builder %q (%s) is listed twice or is not complete", b.name, b.frequency)
		}
		seen[b.name] = true
	}
}

func TestADatasetNeverBuiltIsNamedAndVerifyFailsUntilItIsBuilt(t *testing.T) {
	root := t.TempDir()
	first, second := "1", "1"
	all := []builder{fake("counties", dataset.Manual, &first), fake("rates", dataset.Weekly, &second)}
	var out, errs bytes.Buffer

	if code := run([]string{"build", "-root", root, "-only", "counties"}, all, day, &out, &errs); code != 0 {
		t.Fatalf("build = %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "not built yet: rates") {
		t.Fatalf("build said %q", out.String())
	}
	if code := run([]string{"verify", "-root", root}, all, day, &out, &errs); code != 1 {
		t.Fatalf("verify = %d, want 1 while rates is not built", code)
	}

	run([]string{"build", "-root", root, "-frequency", "weekly"}, all, day, &out, &errs)
	if code := run([]string{"verify", "-root", root}, all, day, &out, &errs); code != 0 {
		t.Fatalf("verify = %d after both were built: %s", code, errs.String())
	}
}

func TestPublishedSIRUTAIsEmptyBeforeTheFirstBuildAndReadsTheCodesAfter(t *testing.T) {
	root := t.TempDir()
	if codes, err := publishedSIRUTA(root); err != nil || codes != nil {
		t.Fatalf("before the first build: %v %v", codes, err)
	}
	dir := filepath.Join(root, "datasets", "siruta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "units.jsonl"), []byte("{\"code\":10}\n{\"code\":1017}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if codes, err := publishedSIRUTA(root); err != nil || len(codes) != 2 || codes[1] != 1017 {
		t.Fatalf("after: %v %v", codes, err)
	}
}
