package holidays

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
)

func TestOrthodoxEasterMatchesKnownYears(t *testing.T) {
	known := map[int]string{
		2024: "2024-05-05", 2025: "2025-04-20", 2026: "2026-04-12", 2027: "2027-05-02",
		2028: "2028-04-16", 2029: "2029-04-08", 2030: "2030-04-28", 2035: "2035-04-29",
	}
	for year, want := range known {
		if got := OrthodoxEaster(year).Format(time.DateOnly); got != want {
			t.Errorf("Easter %d = %s, want %s", year, got, want)
		}
	}
}

// days works out the holidays of a year from the rules, as a reader of the dataset does.
func days(year int) []string {
	var out []string
	for _, r := range Rules() {
		if date, ok := r.On(year); ok {
			out = append(out, date.Format(time.DateOnly))
		}
	}
	sort.Strings(out)
	return out
}

func TestTheRulesGiveEveryDayOfTheLawInAYear(t *testing.T) {
	want := []string{
		"2026-01-01", "2026-01-02", "2026-01-06", "2026-01-07", "2026-01-24",
		"2026-04-10", "2026-04-12", "2026-04-13", "2026-05-01", "2026-05-31",
		"2026-06-01", "2026-06-01", "2026-08-15", "2026-11-30", "2026-12-01",
		"2026-12-25", "2026-12-26",
	}
	if got := days(2026); !slices.Equal(got, want) {
		t.Fatalf("2026 gives\n%v\nwant\n%v", got, want)
	}
}

func TestTheDatasetHoldsTheRulesAndIsValid(t *testing.T) {
	d := Dataset()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || d.Files[0].Name != "rules.jsonl" || len(d.Files[0].Records) != 17 || d.Frequency != dataset.Manual {
		t.Fatalf("dataset is %+v", d)
	}
}

func TestTheRulesAreTheSeventeenDaysOfTheLaw(t *testing.T) {
	fixed, movable := 0, 0
	for _, r := range Rules() {
		switch {
		case r.Rule == Fixed && r.Month != nil && r.Day != nil && r.EasterOffset == nil:
			fixed++
		case r.Rule == OrthodoxEasterOffset && r.Month == nil && r.Day == nil && r.EasterOffset != nil:
			movable++
		default:
			t.Errorf("rule %+v is neither fixed nor after Easter", r)
		}
	}
	if fixed != 12 || movable != 5 {
		t.Fatalf("%d fixed and %d movable rules, want 12 and 5", fixed, movable)
	}
}

func TestARuleGivesItsDayInAnyYear(t *testing.T) {
	offset := -2
	friday := Rule{Rule: OrthodoxEasterOffset, EasterOffset: &offset, ValidFrom: "2024-01-01"}
	if date, ok := friday.On(2040); !ok || date.Format(time.DateOnly) != "2040-05-04" {
		t.Fatalf("Good Friday 2040 = %s, %v", date.Format(time.DateOnly), ok)
	}
}

func TestARuleOutsideItsValidityGivesNoHoliday(t *testing.T) {
	month, day, end := 1, 6, "2029-12-31"
	r := Rule{Rule: Fixed, Month: &month, Day: &day, ValidFrom: "2024-01-01", ValidTo: &end}
	if _, ok := r.On(2023); ok {
		t.Error("in force before its first day")
	}
	if _, ok := r.On(2029); !ok {
		t.Error("not in force inside its validity")
	}
	if _, ok := r.On(2030); ok {
		t.Error("in force after its last day")
	}
}
