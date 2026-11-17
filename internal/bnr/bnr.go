// Package bnr builds the list of currencies for which the National Bank of Romania publishes a reference
// rate. It reads the yearly rate files and keeps only facts: the currency, the number of units the rate is
// given for, and the first and last day a rate was published. No rate is copied.
package bnr

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
)

// FirstYear is the first year BNR publishes a file for.
const FirstYear = 2005

// maxFile is more than ten times the largest yearly file. A longer answer is not a rate file.
const maxFile = 8 << 20

// Day is what one day of a rate file says, without the rates: the currencies and their multipliers.
type Day struct {
	Date        string
	Multipliers map[string]int
}

// Currency is one unbroken stretch of days on which BNR published a rate for a currency with one multiplier.
type Currency struct {
	Code string `json:"code"`
	// Multiplier is the number of units the rate is given for: 100 for HUF, 1 for most.
	Multiplier int    `json:"multiplier"`
	ValidFrom  string `json:"valid_from"`
	// ValidTo is the last day with a rate, or null while BNR still publishes one.
	ValidTo *string `json:"valid_to"`
}

type file struct {
	Cubes []struct {
		Date  string `xml:"date,attr"`
		Rates []struct {
			Currency   string `xml:"currency,attr"`
			Multiplier *int   `xml:"multiplier,attr"`
		} `xml:"Rate"`
	} `xml:"Body>Cube"`
}

// Parse reads a BNR rate file. The older files use another XML namespace than the newer ones; both are read.
func Parse(r io.Reader) ([]Day, error) {
	var f file
	if err := xml.NewDecoder(io.LimitReader(r, maxFile)).Decode(&f); err != nil {
		return nil, fmt.Errorf("not a BNR rate file: %w", err)
	}

	var days []Day
	for _, c := range f.Cubes {
		if _, err := time.Parse(time.DateOnly, c.Date); err != nil {
			return nil, fmt.Errorf("day %q is not a date", c.Date)
		}
		day := Day{Date: c.Date, Multipliers: map[string]int{}}
		for _, rate := range c.Rates {
			if len(rate.Currency) != 3 {
				return nil, fmt.Errorf("day %s: currency %q is not three letters", c.Date, rate.Currency)
			}
			multiplier := 1
			if rate.Multiplier != nil {
				multiplier = *rate.Multiplier
			}
			if multiplier < 1 {
				return nil, fmt.Errorf("day %s, %s: multiplier %d", c.Date, rate.Currency, multiplier)
			}
			day.Multipliers[rate.Currency] = multiplier
		}
		days = append(days, day)
	}
	if len(days) == 0 {
		return nil, fmt.Errorf("the file has no days")
	}
	return days, nil
}

// Periods turns the days into stretches, sorted by currency and first day. A currency that is missing on a
// day ends its stretch on the day before; if it comes back, or its multiplier changes, a new stretch starts.
// A stretch that reaches the last day is still open.
func Periods(days []Day) []Currency {
	sorted := append([]Day(nil), days...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })

	type open struct {
		Currency
		last string
	}
	current := map[string]*open{}
	var done []Currency
	closeIt := func(o *open) {
		end := o.last
		o.ValidTo = &end
		done = append(done, o.Currency)
	}

	for _, day := range sorted {
		for code, o := range current {
			if multiplier, there := day.Multipliers[code]; !there || multiplier != o.Multiplier {
				closeIt(o)
				delete(current, code)
			}
		}
		for code, multiplier := range day.Multipliers {
			if o, there := current[code]; there {
				o.last = day.Date
			} else {
				current[code] = &open{Currency{Code: code, Multiplier: multiplier, ValidFrom: day.Date}, day.Date}
			}
		}
	}
	for _, o := range current {
		done = append(done, o.Currency)
	}

	sort.Slice(done, func(i, j int) bool {
		if done[i].Code != done[j].Code {
			return done[i].Code < done[j].Code
		}
		return done[i].ValidFrom < done[j].ValidFrom
	})
	return done
}

// Fetch gives the rate file of one year.
type Fetch func(year int) (io.ReadCloser, error)

// FromSite reads the yearly files from the BNR server kept for applications.
func FromSite(client *http.Client) Fetch {
	return func(year int) (io.ReadCloser, error) {
		address := fmt.Sprintf("https://curs.bnr.ro/files/xml/years/nbrfxrates%d.xml", year)
		request, err := http.NewRequest(http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", "romanian-datasets (https://github.com/pidginhost/romanian-datasets)")
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("%s answered %s", address, response.Status)
		}
		return response.Body, nil
	}
}

// Dataset reads every year from FirstYear to lastYear and builds the dataset.
func Dataset(fetch Fetch, lastYear int) (dataset.Dataset, error) {
	var days []Day
	for year := FirstYear; year <= lastYear; year++ {
		body, err := fetch(year)
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("year %d: %w", year, err)
		}
		more, err := Parse(body)
		body.Close()
		if err != nil {
			return dataset.Dataset{}, fmt.Errorf("year %d: %w", year, err)
		}
		days = append(days, more...)
	}

	var records []any
	for _, c := range Periods(days) {
		records = append(records, c)
	}

	return dataset.Dataset{
		Name:  "bnr-currencies",
		Title: "Currencies with a reference rate of the National Bank of Romania",
		Description: "The currencies for which the National Bank of Romania (BNR) publishes a reference rate against the leu, " +
			"with the number of units the rate is given for and the first and last day of publication, from 2005. " +
			"XAU is one gram of gold and XDR is the special drawing right. " +
			"A currency BNR stopped publishing stays in the list with its last day. The rates themselves are not here: read them from BNR.",
		Source: dataset.Source{
			Name: "National Bank of Romania, yearly reference rate files",
			URL:  "https://curs.bnr.ro/",
			Licence: "Facts taken from the files; no rate is copied. " +
				"BNR's own terms for its content: https://www.bnr.ro/2281-termeni-si-conditii",
		},
		Frequency: dataset.Manual,
		Update:    "When BNR adds a currency, stops publishing one or changes a multiplier",
		Files: []dataset.File{{
			Name: "currencies.jsonl",
			Fields: []dataset.Field{
				{Name: "code", Description: "The currency code of BNR, three letters, as in ISO 4217"},
				{Name: "multiplier", Description: "The number of units the rate is given for: the rate of HUF is for 100 forints"},
				{Name: "valid_from", Description: "The first day BNR published a rate with this multiplier, as YYYY-MM-DD; not before 2005-01-03, the first day of the files"},
				{Name: "valid_to", Description: "The last day BNR published a rate, as YYYY-MM-DD, or null while it still does"},
			},
			Records: records,
		}},
	}, nil
}
