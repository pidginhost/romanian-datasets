// Package holidays builds the public holidays of Romania: the days on which no work is done by law.
//
// The list is article 139 (1) of the Labour Code (Law 53/2003, republished), as in force from
// 1 January 2024, when Law 52/2023 added 6 and 7 January. Years before 2024 had other lists and are not built.
// Days declared free by a Government decision for one year only are not public holidays and are not here.
package holidays

import (
	"fmt"
	"sort"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
)

// FirstYear is the first year the rule in this package holds for.
const FirstYear = 2024

// Holiday is one public holiday on one day.
type Holiday struct {
	Date string `json:"date"`
	Name string `json:"name"`
	// Movable is true for the days that follow Orthodox Easter and so fall on another date each year.
	Movable bool `json:"movable"`
}

type fixed struct {
	month time.Month
	day   int
	name  string
}

var fixedDays = []fixed{
	{time.January, 1, "Anul Nou"},
	{time.January, 2, "Anul Nou"},
	{time.January, 6, "Botezul Domnului - Boboteaza"},
	{time.January, 7, "Soborul Sfântului Proroc Ioan Botezătorul"},
	{time.January, 24, "Ziua Unirii Principatelor Române"},
	{time.May, 1, "Ziua Muncii"},
	{time.June, 1, "Ziua Copilului"},
	{time.August, 15, "Adormirea Maicii Domnului"},
	{time.November, 30, "Sfântul Apostol Andrei"},
	{time.December, 1, "Ziua Națională a României"},
	{time.December, 25, "Crăciunul"},
	{time.December, 26, "Crăciunul"},
}

var movableDays = []struct {
	fromEaster int
	name       string
}{
	{-2, "Vinerea Mare"},
	{0, "Paștele"},
	{1, "Paștele"},
	{49, "Rusaliile"},
	{50, "Rusaliile"},
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

// Of lists the public holidays of one year, in date order.
func Of(year int) ([]Holiday, error) {
	if year < FirstYear {
		return nil, fmt.Errorf("public holidays are built from %d on, not for %d", FirstYear, year)
	}

	var days []Holiday
	for _, f := range fixedDays {
		date := time.Date(year, f.month, f.day, 0, 0, 0, 0, time.UTC)
		days = append(days, Holiday{Date: date.Format(time.DateOnly), Name: f.name})
	}
	easter := OrthodoxEaster(year)
	for _, m := range movableDays {
		date := easter.AddDate(0, 0, m.fromEaster)
		days = append(days, Holiday{Date: date.Format(time.DateOnly), Name: m.name, Movable: true})
	}

	// A movable day can fall on a fixed one, such as Easter Monday on 1 May. Both stay: the day is one, the reasons two.
	sort.SliceStable(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return days, nil
}

// Dataset builds the dataset for the years from FirstYear to lastYear.
func Dataset(lastYear int) (dataset.Dataset, error) {
	var records []any
	for year := FirstYear; year <= lastYear; year++ {
		days, err := Of(year)
		if err != nil {
			return dataset.Dataset{}, err
		}
		for _, d := range days {
			records = append(records, d)
		}
	}

	return dataset.Dataset{
		Name:  "public-holidays",
		Title: "Public holidays of Romania",
		Description: "The days on which no work is done by law, from 2024 to the year after the build. " +
			"Days declared free by a Government decision for one year only are not included. " +
			"The two days each of three yearly feasts of other legal religions have no fixed date and are not included.",
		Source: dataset.Source{
			Name:    "Labour Code (Law 53/2003, republished), article 139 (1); Orthodox Easter is computed",
			URL:     "https://legislatie.just.ro/Public/DetaliiDocument/128647",
			Licence: "Romanian legislation, not subject to copyright (Law 8/1996, article 9)",
		},
		Update: "When article 139 of the Labour Code changes, and once a year for the next year",
		Files: []dataset.File{{
			Name: "holidays.jsonl",
			Fields: []dataset.Field{
				{Name: "date", Description: "The day, as YYYY-MM-DD"},
				{Name: "name", Description: "The name of the holiday, in Romanian"},
				{Name: "movable", Description: "True for a day that follows Orthodox Easter"},
			},
			Records: records,
		}},
	}, nil
}
