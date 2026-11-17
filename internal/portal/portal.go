// Package portal reads the Legislative Portal of the Ministry of Justice: the consolidated forms of an act
// and the text of a page.
package portal

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Address is where the portal shows an act or one of its consolidated forms, by number.
const Address = "https://legislatie.just.ro/Public/DetaliiDocument/"

var (
	formPattern   = regexp.MustCompile(`title='Consolidarea din (\d{2}\.\d{2}\.\d{4})'[^>]*href='[^']*DetaliiDocument/(\d+)'`)
	tagPattern    = regexp.MustCompile(`<[^>]*>`)
	scriptPattern = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>`)
)

// Lines gives the text of a page, one line for each piece of text between tags.
func Lines(page string) []string {
	page = scriptPattern.ReplaceAllString(page, "")
	var out []string
	for _, l := range strings.Split(tagPattern.ReplaceAllString(page, "\n"), "\n") {
		if l = strings.TrimSpace(html.UnescapeString(l)); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Form is a consolidated form of an act: the text in force from its date.
type Form struct {
	Date time.Time
	ID   string
}

// Forms reads the page of an act and gives its consolidated forms, the oldest first.
func Forms(page string) ([]Form, error) {
	var out []Form
	for _, m := range formPattern.FindAllStringSubmatch(page, -1) {
		d, err := time.Parse("02.01.2006", m[1])
		if err != nil {
			return nil, err
		}
		out = append(out, Form{d, m[2]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the act has no consolidated forms")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

// FromHorizon gives the form in force on the horizon and those after it.
func FromHorizon(forms []Form, horizon time.Time) ([]Form, error) {
	first := -1
	for i, f := range forms {
		if !f.Date.After(horizon) {
			first = i
		}
	}
	if first < 0 {
		return nil, fmt.Errorf("no consolidated form is in force on %s", horizon.Format(time.DateOnly))
	}
	return forms[first:], nil
}

// Read reads a page, refusing one larger than limit.
func Read(get func(string) (io.ReadCloser, error), address string, limit int64) (string, error) {
	body, err := get(address)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", fmt.Errorf("%s is larger than expected", address)
	}
	return string(data), nil
}
