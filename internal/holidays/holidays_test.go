package holidays

import (
	"testing"
	"time"
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

func TestAYearHasEveryDayOfTheLaw(t *testing.T) {
	days, err := Of(2026)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"2026-01-01", "2026-01-02", "2026-01-06", "2026-01-07", "2026-01-24",
		"2026-04-10", "2026-04-12", "2026-04-13", "2026-05-01", "2026-05-31",
		"2026-06-01", "2026-06-01", "2026-08-15", "2026-11-30", "2026-12-01",
		"2026-12-25", "2026-12-26",
	}
	if len(days) != len(want) {
		t.Fatalf("%d days, want %d: %v", len(days), len(want), days)
	}
	for i, d := range days {
		if d.Date != want[i] {
			t.Errorf("day %d = %s, want %s", i, d.Date, want[i])
		}
	}
}

func TestAMovableDayOnAFixedDayKeepsBothReasons(t *testing.T) {
	// In 2026 the second day of Pentecost is 1 June, which is also Children's Day.
	days, _ := Of(2026)
	var names []string
	for _, d := range days {
		if d.Date == "2026-06-01" {
			names = append(names, d.Name)
		}
	}
	if len(names) != 2 || names[0] == names[1] {
		t.Fatalf("1 June 2026 has %v, want two different reasons", names)
	}
}

func TestMovableDaysAreMarked(t *testing.T) {
	days, _ := Of(2025)
	movable := 0
	for _, d := range days {
		if d.Movable {
			movable++
		}
	}
	if movable != 5 {
		t.Fatalf("%d movable days, want 5", movable)
	}
}

func TestAYearBeforeTheRuleIsRefused(t *testing.T) {
	if _, err := Of(2023); err == nil {
		t.Fatal("2023 was built, but the list was different then")
	}
}

func TestTheDatasetCoversEveryYearAndIsValid(t *testing.T) {
	d, err := Dataset(2027)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, want := len(d.Files[0].Records), 4*17; got != want {
		t.Fatalf("%d records, want %d", got, want)
	}
}
