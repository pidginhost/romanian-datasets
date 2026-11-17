package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var day = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

func TestBuildThenVerifyThenBuildAgainChangesNothing(t *testing.T) {
	root := t.TempDir()
	var out, errs bytes.Buffer

	if code := run([]string{"build", "-root", root}, day, &out, &errs); code != 0 {
		t.Fatalf("build = %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "1 changed public-holidays") {
		t.Fatalf("build said %q", out.String())
	}
	if code := run([]string{"verify", "-root", root}, day, &out, &errs); code != 0 {
		t.Fatalf("verify = %d: %s", code, errs.String())
	}

	index, _ := os.ReadFile(filepath.Join(root, "index.json"))
	out.Reset()
	if code := run([]string{"build", "-root", root}, day.AddDate(0, 0, 20), &out, &errs); code != 0 {
		t.Fatalf("second build = %d: %s", code, errs.String())
	}
	again, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if !bytes.Equal(index, again) || !strings.Contains(out.String(), "0 changed") {
		t.Fatalf("the second build changed the index; it said %q", out.String())
	}
}

func TestANewYearAddsAYearOfHolidays(t *testing.T) {
	root := t.TempDir()
	var out, errs bytes.Buffer
	run([]string{"build", "-root", root}, day, &out, &errs)
	out.Reset()

	run([]string{"build", "-root", root}, day.AddDate(1, 0, 0), &out, &errs)

	if !strings.Contains(out.String(), "1 changed public-holidays") {
		t.Fatalf("build said %q", out.String())
	}
	data, _ := os.ReadFile(filepath.Join(root, "datasets", "public-holidays", "holidays.jsonl"))
	if !strings.Contains(string(data), `"date":"2028-01-01"`) {
		t.Fatal("2028 is not in the file")
	}
}

func TestVerifyFailsOnADamagedFile(t *testing.T) {
	root := t.TempDir()
	var out, errs bytes.Buffer
	run([]string{"build", "-root", root}, day, &out, &errs)
	os.WriteFile(filepath.Join(root, "datasets", "public-holidays", "holidays.jsonl"), []byte("{}\n"), 0o644)

	if code := run([]string{"verify", "-root", root}, day, &out, &errs); code != 1 {
		t.Fatalf("verify = %d, want 1", code)
	}
}

func TestAWrongCommandIsRefused(t *testing.T) {
	var out, errs bytes.Buffer
	for _, args := range [][]string{{}, {"publish"}, {"build", "-nope"}} {
		if code := run(args, day, &out, &errs); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}
