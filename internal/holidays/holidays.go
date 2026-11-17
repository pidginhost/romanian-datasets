// Package holidays builds the public holidays of Romania: the days on which no work is done by law.
//
// The list is article 139 (1) of the Labour Code (Law 53/2003, republished), as in force from
// 1 January 2024, when Law 52/2023 added 6 and 7 January. Earlier lists are not described.
// The dataset holds the rules, not the days: a reader works out the days of a year from them.
// Days declared free by a Government decision for one year only are not public holidays and are not here.
package holidays

import (
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
)

// The two kinds of rule.
const (
	// Fixed is the same month and day every year.
	Fixed = "fixed"
	// OrthodoxEasterOffset is a number of days from Orthodox Easter Sunday.
	OrthodoxEasterOffset = "orthodox-easter-offset"
)

// Rule is how one public holiday is found in any year.
type Rule struct {
	Name string `json:"name"`
	// Rule is "fixed" or "orthodox-easter-offset".
	Rule string `json:"rule"`
	// Month and Day are set for a fixed rule.
	Month *int `json:"month"`
	Day   *int `json:"day"`
	// EasterOffset is set for a rule that follows Easter: -2 is the Friday before, 49 is Pentecost Sunday.
	EasterOffset *int `json:"easter_offset"`
	// ValidFrom is the first day this dataset vouches for the rule, not the day the holiday was first made law.
	ValidFrom string `json:"valid_from"`
	// ValidTo is the last day of the rule, or null while it is in force.
	ValidTo *string `json:"valid_to"`
}

const validFrom = "2024-01-01"

func fixed(month time.Month, day int, name string) Rule {
	m := int(month)
	return Rule{Name: name, Rule: Fixed, Month: &m, Day: &day, ValidFrom: validFrom}
}

func afterEaster(days int, name string) Rule {
	return Rule{Name: name, Rule: OrthodoxEasterOffset, EasterOffset: &days, ValidFrom: validFrom}
}

// Rules is article 139 (1) of the Labour Code as rules, in the order of the law.
func Rules() []Rule {
	return []Rule{
		fixed(time.January, 1, "Anul Nou"),
		fixed(time.January, 2, "Anul Nou"),
		fixed(time.January, 6, "Botezul Domnului - Boboteaza"),
		fixed(time.January, 7, "Soborul Sfântului Proroc Ioan Botezătorul"),
		fixed(time.January, 24, "Ziua Unirii Principatelor Române"),
		afterEaster(-2, "Vinerea Mare"),
		afterEaster(0, "Paștele"),
		afterEaster(1, "Paștele"),
		fixed(time.May, 1, "Ziua Muncii"),
		fixed(time.June, 1, "Ziua Copilului"),
		afterEaster(49, "Rusaliile"),
		afterEaster(50, "Rusaliile"),
		fixed(time.August, 15, "Adormirea Maicii Domnului"),
		fixed(time.November, 30, "Sfântul Apostol Andrei"),
		fixed(time.December, 1, "Ziua Națională a României"),
		fixed(time.December, 25, "Crăciunul"),
		fixed(time.December, 26, "Crăciunul"),
	}
}

// On is the day a rule gives in a year, and whether the rule is in force on that day.
func (r Rule) On(year int) (time.Time, bool) {
	var date time.Time
	switch r.Rule {
	case Fixed:
		date = time.Date(year, time.Month(*r.Month), *r.Day, 0, 0, 0, 0, time.UTC)
	case OrthodoxEasterOffset:
		date = OrthodoxEaster(year).AddDate(0, 0, *r.EasterOffset)
	default:
		return date, false
	}
	day := date.Format(time.DateOnly)
	return date, day >= r.ValidFrom && (r.ValidTo == nil || day <= *r.ValidTo)
}

// OrthodoxEaster is Easter Sunday of the Orthodox Church in a year, as a date of the civil calendar.
// It is computed in the Julian calendar (Meeus) and moved by the difference between the two calendars.
func OrthodoxEaster(year int) time.Time {
	a := year % 4
	b := year % 7
	c := year % 19
	d := (19*c + 15) % 30
	e := (2*a + 4*b - d + 34) % 7
	month := (d + e + 114) / 31
	day := (d+e+114)%31 + 1
	gap := year/100 - year/400 - 2
	return time.Date(year, time.Month(month), day+gap, 0, 0, 0, 0, time.UTC)
}

// Dataset builds the dataset of the rules.
func Dataset() dataset.Dataset {
	var rules []any
	for _, r := range Rules() {
		rules = append(rules, r)
	}

	return dataset.Dataset{
		Name:  "public-holidays",
		Title: "Public holidays of Romania",
		Description: "The days on which no work is done by law, as rules that give the days of any year: " +
			"a fixed month and day, or a number of days from Orthodox Easter Sunday. " +
			"Orthodox Easter Sunday is the Julian computus moved to the civil calendar. " +
			"A day can come from two rules, such as the second day of Pentecost on 1 June. " +
			"Days declared free by a Government decision for one year only are not included. " +
			"The two days each of three yearly feasts of other legal religions have no fixed date and are not included.",
		Source: dataset.Source{
			Name:    "Labour Code (Law 53/2003, republished), article 139 (1); Orthodox Easter is computed",
			URL:     "https://legislatie.just.ro/Public/DetaliiDocument/128647",
			Licence: "Romanian legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Frequency: dataset.Manual,
		Update:    "When article 139 of the Labour Code changes",
		Files: []dataset.File{{
			Name: "rules.jsonl",
			Fields: []dataset.Field{
				{Name: "name", Description: "The name of the holiday, in Romanian"},
				{Name: "rule", Description: "fixed: the same month and day every year. orthodox-easter-offset: a number of days from Orthodox Easter Sunday"},
				{Name: "month", Description: "The month, 1 to 12, of a fixed rule; null otherwise"},
				{Name: "day", Description: "The day of the month of a fixed rule; null otherwise"},
				{Name: "easter_offset", Description: "Days from Orthodox Easter Sunday, negative before it; null for a fixed rule"},
				{Name: "valid_from", Description: "The first day this dataset vouches for the rule, as YYYY-MM-DD. Not the day the holiday became law"},
				{Name: "valid_to", Description: "The last day of the rule, as YYYY-MM-DD, or null while it is in force"},
			},
			Records: rules,
		}},
	}
}
