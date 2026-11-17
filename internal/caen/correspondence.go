package caen

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Correspondence is the table of Eurostat between NACE Rev.2.1 and NACE Rev.2, as the Publications Office
// of the European Union publishes it. Its classes are the classes of CAEN Rev.3 and CAEN Rev.2.
const Correspondence = "http://data.europa.eu/ux2/nace2.1/NACE2.1_NACE2"

// Endpoint is the SPARQL service of the Publications Office.
const Endpoint = "https://publications.europa.eu/webapi/rdf/sparql"

// maxAnswer is many times the size of the answer.
const maxAnswer = 16 << 20

// correspondenceQuery asks for every link of the table with the notations of its two ends.
var correspondenceQuery = `PREFIX xkos: <http://rdf-vocabulary.ddialliance.org/xkos#>
PREFIX skos: <http://www.w3.org/2004/02/skos/core#>
SELECT ?rev21 ?rev2 WHERE {
  <` + Correspondence + `> xkos:madeOf ?link .
  ?link xkos:sourceConcept ?source ; xkos:targetConcept ?target .
  ?source skos:notation ?rev21 .
  ?target skos:notation ?rev2 .
}`

// CorrespondenceAddress is the request for the table.
func CorrespondenceAddress() string {
	return Endpoint + "?" + url.Values{"query": {correspondenceQuery}, "format": {"application/sparql-results+json"}}.Encode()
}

// Link is one link between a class of CAEN Rev.2 and a class of CAEN Rev.3.
type Link struct {
	Rev2 string `json:"rev2"`
	Rev3 string `json:"rev3"`
	// Kind says how the two classes relate: 1:1, 1:n when the Rev.2 class is split, n:1 when Rev.2 classes
	// are joined, n:m when both happen.
	Kind string `json:"kind"`
}

var naceClass = regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}$`)

type sparqlAnswer struct {
	Results struct {
		Bindings []map[string]struct {
			Value string `json:"value"`
		} `json:"bindings"`
	} `json:"results"`
}

// ReadCorrespondence reads the SPARQL answer and keeps the links between classes, written as CAEN writes
// them, without the dot: 01.11 is 0111.
func ReadCorrespondence(r io.Reader) ([]Link, error) {
	var answer sparqlAnswer
	if err := json.NewDecoder(io.LimitReader(r, maxAnswer)).Decode(&answer); err != nil {
		return nil, fmt.Errorf("not a SPARQL answer: %w", err)
	}

	seen := map[[2]string]bool{}
	for _, b := range answer.Results.Bindings {
		rev21, rev2 := strings.TrimSpace(b["rev21"].Value), strings.TrimSpace(b["rev2"].Value)
		classes := naceClass.MatchString(rev21)
		if classes != naceClass.MatchString(rev2) {
			return nil, fmt.Errorf("the table links %q to %q, a class to another level", rev2, rev21)
		}
		if !classes {
			// A section, division or group: CAEN keeps the links between classes.
			continue
		}
		seen[[2]string{strings.ReplaceAll(rev2, ".", ""), strings.ReplaceAll(rev21, ".", "")}] = true
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("the table has no links between classes")
	}

	from, to := map[string]int{}, map[string]int{}
	for p := range seen {
		from[p[0]]++
		to[p[1]]++
	}
	var links []Link
	for p := range seen {
		kind := "1:1"
		switch split, joined := from[p[0]] > 1, to[p[1]] > 1; {
		case split && joined:
			kind = "n:m"
		case split:
			kind = "1:n"
		case joined:
			kind = "n:1"
		}
		links = append(links, Link{Rev2: p[0], Rev3: p[1], Kind: kind})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Rev2 != links[j].Rev2 {
			return links[i].Rev2 < links[j].Rev2
		}
		return links[i].Rev3 < links[j].Rev3
	})
	return links, nil
}

// CheckLinks makes sure every class of a link is a class of its version, and that every class of Rev.2 has
// a link. A class of Rev.3 may have none: a class made new in Rev.3. Those are given back.
func CheckLinks(links []Link, all []Activity) (newInRev3 []string, err error) {
	classes := map[int]map[string]bool{2: {}, 3: {}}
	for _, a := range all {
		if a.Level == "class" {
			classes[a.Version][a.Code] = true
		}
	}
	linked := map[int]map[string]bool{2: {}, 3: {}}
	for _, l := range links {
		switch {
		case !classes[2][l.Rev2]:
			return nil, fmt.Errorf("the correspondence names %s, which is not a class of CAEN Rev.2", l.Rev2)
		case !classes[3][l.Rev3]:
			return nil, fmt.Errorf("the correspondence names %s, which is not a class of CAEN Rev.3", l.Rev3)
		}
		linked[2][l.Rev2], linked[3][l.Rev3] = true, true
	}
	for code := range classes[2] {
		if !linked[2][code] {
			return nil, fmt.Errorf("class %s of CAEN Rev.2 has no link to Rev.3", code)
		}
	}
	for code := range classes[3] {
		if !linked[3][code] {
			newInRev3 = append(newInRev3, code)
		}
	}
	sort.Strings(newInRev3)
	return newInRev3, nil
}
