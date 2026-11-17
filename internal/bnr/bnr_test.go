package bnr

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The two shapes of the BNR files: the older ones use an http namespace and line breaks,
// the newer ones an https namespace on one line. The rates are made up.
const older = `<?xml version="1.0" encoding="utf-8"?>
<DataSet xmlns="http://www.bnr.ro/xsd" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.bnr.ro/xsd nbrfxrates.xsd">
<Header><Publisher>National Bank of Romania</Publisher><PublishingDate>2011-10-31</PublishingDate><MessageType>DR</MessageType></Header>
<Body>
<Subject>Reference rates</Subject>
<OrigCurrency>RON</OrigCurrency>
<Cube date="2005-01-03">
     <Rate currency="EUR">1.0000</Rate>
     <Rate currency="HUF" multiplier="100">1.0000</Rate>
     <Rate currency="SKK">1.0000</Rate>
</Cube>
<Cube date="2005-01-04">
     <Rate currency="EUR">1.0000</Rate>
     <Rate currency="HUF" multiplier="100">1.0000</Rate>
     <Rate currency="SKK">1.0000</Rate>
</Cube>
</Body>
</DataSet>`

const newer = `<?xml version="1.0" encoding="utf-8"?><DataSet xmlns="https://www.bnr.ro/xsd" xmlns:xsi="https://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="https://curs.bnr.ro/xsd/nbrfxrates.xsd"><Header><Publisher>National Bank of Romania</Publisher><PublishingDate>2006-01-04</PublishingDate><MessageType>DR</MessageType></Header><Body><Subject>Reference rates</Subject><OrigCurrency>RON</OrigCurrency><Cube date="2006-01-03"><Rate currency="EUR">1.0000</Rate><Rate currency="HUF" multiplier="100">1.0000</Rate></Cube><Cube date="2006-01-04"><Rate currency="EUR">1.0000</Rate><Rate currency="HUF" multiplier="100">1.0000</Rate><Rate currency="THB">1.0000</Rate></Cube></Body></DataSet>`

func day(date string, multipliers ...any) Day {
	d := Day{Date: date, Multipliers: map[string]int{}}
	for i := 0; i < len(multipliers); i += 2 {
		d.Multipliers[multipliers[i].(string)] = multipliers[i+1].(int)
	}
	return d
}

func lines(periods []Currency) string {
	var out []string
	for _, p := range periods {
		to := "open"
		if p.ValidTo != nil {
			to = *p.ValidTo
		}
		out = append(out, fmt.Sprintf("%s x%d %s..%s", p.Code, p.Multiplier, p.ValidFrom, to))
	}
	return strings.Join(out, "\n")
}

func TestParseReadsBothShapesOfFileWithoutTheRates(t *testing.T) {
	for name, text := range map[string]string{"older": older, "newer": newer} {
		days, err := Parse(strings.NewReader(text))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(days) != 2 || days[0].Multipliers["EUR"] != 1 || days[0].Multipliers["HUF"] != 100 {
			t.Fatalf("%s: days are %+v", name, days)
		}
	}
}

func TestParseRefusesWhatIsNotARateFile(t *testing.T) {
	for name, text := range map[string]string{
		"a web page":     "<html><body>Not found</body></html>",
		"no days":        `<DataSet><Body></Body></DataSet>`,
		"a bad date":     `<DataSet><Body><Cube date="yesterday"><Rate currency="EUR">1</Rate></Cube></Body></DataSet>`,
		"a bad currency": `<DataSet><Body><Cube date="2026-01-05"><Rate currency="EURO">1</Rate></Cube></Body></DataSet>`,
		"a bad multiple": `<DataSet><Body><Cube date="2026-01-05"><Rate currency="EUR" multiplier="0">1</Rate></Cube></Body></DataSet>`,
	} {
		if _, err := Parse(strings.NewReader(text)); err == nil {
			t.Errorf("%s was read as a rate file", name)
		}
	}
}

func TestACurrencyStillPublishedIsOpenAndADroppedOneEndsOnItsLastDay(t *testing.T) {
	got := lines(Periods([]Day{
		day("2026-01-07", "EUR", 1),
		day("2026-01-05", "EUR", 1, "BGN", 1),
		day("2026-01-06", "EUR", 1, "BGN", 1),
	}))
	want := "BGN x1 2026-01-05..2026-01-06\nEUR x1 2026-01-05..open"
	if got != want {
		t.Fatalf("periods are\n%s\nwant\n%s", got, want)
	}
}

func TestACurrencyAddedLaterStartsOnItsFirstDay(t *testing.T) {
	got := lines(Periods([]Day{day("2026-01-05", "EUR", 1), day("2026-01-06", "EUR", 1, "THB", 1)}))
	if want := "EUR x1 2026-01-05..open\nTHB x1 2026-01-06..open"; got != want {
		t.Fatalf("periods are\n%s", got)
	}
}

func TestAChangedMultiplierOrAGapStartsANewStretch(t *testing.T) {
	got := lines(Periods([]Day{
		day("2026-01-05", "HUF", 1, "RUB", 1),
		day("2026-01-06", "HUF", 100),
		day("2026-01-07", "HUF", 100, "RUB", 1),
	}))
	want := "HUF x1 2026-01-05..2026-01-05\nHUF x100 2026-01-06..open\nRUB x1 2026-01-05..2026-01-05\nRUB x1 2026-01-07..open"
	if got != want {
		t.Fatalf("periods are\n%s\nwant\n%s", got, want)
	}
}

func recorded(year int) (io.ReadCloser, error) {
	switch year {
	case 2005:
		return io.NopCloser(strings.NewReader(older)), nil
	case 2006:
		return io.NopCloser(strings.NewReader(newer)), nil
	}
	return nil, fmt.Errorf("no recording for %d", year)
}

func TestTheDatasetJoinsTheYearsAndIsValid(t *testing.T) {
	d, err := Dataset(recorded, 2006)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	var periods []Currency
	for _, r := range d.Files[0].Records {
		periods = append(periods, r.(Currency))
	}
	want := "EUR x1 2005-01-03..open\nHUF x100 2005-01-03..open\nSKK x1 2005-01-03..2005-01-04\nTHB x1 2006-01-04..open"
	if got := lines(periods); got != want {
		t.Fatalf("periods are\n%s\nwant\n%s", got, want)
	}
}

func TestAYearThatCannotBeReadStopsTheBuild(t *testing.T) {
	if _, err := Dataset(recorded, 2007); err == nil || !strings.Contains(err.Error(), "2007") {
		t.Fatalf("Dataset = %v, want an error that names 2007", err)
	}
}

func TestFromSiteAsksForTheYearAndRefusesAnAnswerThatIsNotOK(t *testing.T) {
	var asked []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "2006.xml") {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, older)
	}))
	defer server.Close()

	// The client sends every request to the test server, whatever host the address names.
	client := server.Client()
	client.Transport = redirect{server.Client().Transport, strings.TrimPrefix(server.URL, "https://")}
	fetch := FromSite(client)

	body, err := fetch(2005)
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	if _, err := fetch(2006); err == nil {
		t.Fatal("a 404 was taken as a rate file")
	}
	if want := "/files/xml/years/nbrfxrates2005.xml"; asked[0] != want {
		t.Fatalf("asked for %s, want %s", asked[0], want)
	}
}

type redirect struct {
	next http.RoundTripper
	host string
}

func (r redirect) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Host = r.host
	return r.next.RoundTrip(request)
}
