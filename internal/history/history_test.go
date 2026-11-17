package history

import (
	"fmt"
	"strings"
	"testing"
)

type row struct {
	ID, Value     string
	From, To, Act *string
}

func period(r *row) (**string, **string, **string) { return &r.From, &r.To, &r.Act }

func show(rows []row) string {
	var out []string
	for _, r := range rows {
		f, t := "-", "-"
		if r.From != nil {
			f = *r.From
		}
		if r.To != nil {
			t = *r.To
		}
		out = append(out, fmt.Sprintf("%s=%s %s..%s", r.ID, r.Value, f, t))
	}
	return strings.Join(out, " | ")
}

func merge(now, published []row, changes map[string]Change) ([]row, error) {
	return Merge(now, published, func(r row) string { return r.ID }, func(r row) string { return r.Value }, period, changes, func(a, b string) bool { return a < b })
}

func TestMergeKeepsWhatDidNotChangeAndNeedsAPersonForTheRest(t *testing.T) {
	first, err := merge([]row{{ID: "b", Value: "2"}, {ID: "a", Value: "1"}}, nil, nil)
	if err != nil || show(first) != "a=1 -..- | b=2 -..-" {
		t.Fatalf("%s %v", show(first), err)
	}
	same, err := merge([]row{{ID: "a", Value: "1"}, {ID: "b", Value: "2"}}, first, nil)
	if err != nil || show(same) != show(first) {
		t.Fatalf("%s %v", show(same), err)
	}
	now := []row{{ID: "a", Value: "9"}, {ID: "c", Value: "3"}}
	for _, changes := range []map[string]Change{nil, {"a": {"2026-03-01", "HG 1/2026"}, "c": {"2026-03-01", "HG 1/2026"}}} {
		_, err := merge(now, first, changes)
		if err == nil {
			t.Fatal("a change nobody recorded was taken")
		}
	}
	all := map[string]Change{"a": {"2026-03-01", "HG 1/2026"}, "b": {"2026-03-01", "HG 1/2026"}, "c": {"2026-03-01", "HG 1/2026"}}
	got, err := merge(now, first, all)
	if err != nil {
		t.Fatal(err)
	}
	want := "a=1 -..2026-02-28 | a=9 2026-03-01..- | b=2 -..2026-02-28 | c=3 2026-03-01..-"
	if show(got) != want || *got[1].Act != "HG 1/2026" {
		t.Fatalf("got  %s\nwant %s", show(got), want)
	}
	if again, err := merge(now, got, nil); err != nil || show(again) != want {
		t.Fatalf("a second build changed the rows: %s %v", show(again), err)
	}
}
