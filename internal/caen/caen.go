// Package caen builds the classification of economic activities in Romania (CAEN), Rev.2 and Rev.3, from
// the nomenclatures the National Trade Register Office (ONRC) publishes on data.gov.ro, with the links
// between the classes of the two revisions from the correspondence table of Eurostat.
package caen

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/fetch"
)

// Catalogue asks data.gov.ro for the nomenclature datasets of ONRC, newest first.
const Catalogue = "https://data.gov.ro/api/3/action/package_search?q=nomenclatoare&fq=organization%3Aonrc&sort=metadata_created%20desc&rows=20"

// The limits are many times the size of the answer and of the files.
const (
	maxCatalogue = 4 << 20
	maxFile      = 8 << 20
)

// Versions are the versions of the source that are published, with the revision they are.
var Versions = map[int]string{2: "Rev.2", 3: "Rev.3"}

// Get gives the body of one public file.
type Get func(address string) (io.ReadCloser, error)

// FromSite reads the files over HTTPS.
func FromSite(client *http.Client) Get {
	return func(address string) (io.ReadCloser, error) { return fetch.Get(client, address, "") }
}

// Activity is one entry of a version: a section, a division, a group or a class.
type Activity struct {
	Version  int    `json:"version"`
	Revision string `json:"revision"`
	Level    string `json:"level"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	// Parent is the code of the entry one level up, or null for a section.
	Parent  *string `json:"parent"`
	Section string  `json:"section"`
}

// Version names one version of the source.
type Version struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

type catalogue struct {
	Success bool `json:"success"`
	Result  struct {
		Results []struct {
			Name      string `json:"name"`
			Created   string `json:"metadata_created"`
			Resources []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"resources"`
		} `json:"results"`
	} `json:"result"`
}

// Files are the addresses of the two files of one nomenclature dataset.
type Files struct {
	CAEN     string
	Versions string
}

func fileName(name, address string) string {
	if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
		return name
	}
	return strings.ToLower(path.Base(address))
}

// Newest reads the catalogue answer and gives the newest nomenclature dataset that has both files.
func Newest(r io.Reader) (Files, error) {
	var c catalogue
	if err := json.NewDecoder(io.LimitReader(r, maxCatalogue)).Decode(&c); err != nil {
		return Files{}, fmt.Errorf("not a data.gov.ro catalogue answer: %w", err)
	}
	if !c.Success {
		return Files{}, fmt.Errorf("data.gov.ro answered without success")
	}
	packages := c.Result.Results
	sort.SliceStable(packages, func(i, j int) bool { return packages[i].Created > packages[j].Created })
	for _, p := range packages {
		if !strings.HasPrefix(p.Name, "nomenclatoare") {
			continue
		}
		var files Files
		for _, res := range p.Resources {
			switch fileName(res.Name, res.URL) {
			case "n_caen.csv":
				files.CAEN = res.URL
			case "n_versiune_caen.csv":
				files.Versions = res.URL
			}
		}
		if files.CAEN != "" && files.Versions != "" {
			return files, nil
		}
	}
	return Files{}, fmt.Errorf("data.gov.ro lists no ONRC nomenclature with N_CAEN.CSV and N_VERSIUNE_CAEN.CSV")
}

// table reads a file of ONRC: UTF-8 with a byte order mark, fields separated by '^', a header line.
func table(r io.Reader, columns ...string) ([]map[string]string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFile+1))
	switch {
	case err != nil:
		return nil, err
	case len(data) > maxFile:
		return nil, fmt.Errorf("larger than expected")
	case !utf8.Valid(data):
		return nil, fmt.Errorf("not UTF-8 text")
	}
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	reader.Comma = '^'
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("not an ONRC file: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("no rows")
	}
	at := map[string]int{}
	for i, h := range rows[0] {
		at[strings.TrimSpace(h)] = i
	}
	for _, c := range columns {
		if _, ok := at[c]; !ok {
			return nil, fmt.Errorf("no column %s", c)
		}
	}
	var out []map[string]string
	for _, row := range rows[1:] {
		m := map[string]string{}
		for _, c := range columns {
			m[c] = strings.TrimSpace(row[at[c]])
		}
		out = append(out, m)
	}
	return out, nil
}

// ReadVersions reads N_VERSIUNE_CAEN.CSV.
func ReadVersions(r io.Reader) ([]Version, error) {
	rows, err := table(r, "COD", "DESCRIERE")
	if err != nil {
		return nil, fmt.Errorf("N_VERSIUNE_CAEN.CSV: %w", err)
	}
	var out []Version
	for _, row := range rows {
		code, err := strconv.Atoi(row["COD"])
		if err != nil {
			return nil, fmt.Errorf("N_VERSIUNE_CAEN.CSV: code %q", row["COD"])
		}
		out = append(out, Version{Code: code, Name: row["DESCRIERE"]})
	}
	return out, nil
}

var (
	section  = regexp.MustCompile(`^[A-Z]$`)
	division = regexp.MustCompile(`^[0-9]{2}$`)
	group    = regexp.MustCompile(`^[0-9]{3}$`)
	class    = regexp.MustCompile(`^[0-9]{4}$`)
)

// entry places one row of a published version. A class has all its codes; a group has no class, or its own
// code again as class; a division has no group and no class, or its own code again as class, as Rev.2 writes
// some of them; a section has only its letter.
func entry(row map[string]string) (Activity, error) {
	s, d, g, c := row["SECTIUNEA"], row["DIVIZIUNEA"], row["GRUPA"], row["CLASA"]
	a := Activity{Section: s}
	switch {
	case !section.MatchString(s):
		return a, fmt.Errorf("section %q", s)
	case row["SUBSECTIUNEA"] != "":
		return a, fmt.Errorf("a subsection %q, which Rev.2 and Rev.3 do not have", row["SUBSECTIUNEA"])
	case class.MatchString(c) && g == c[:3] && d == c[:2]:
		a.Level, a.Code, a.Parent = "class", c, &g
	case group.MatchString(g) && (c == "" || c == g) && d == g[:2]:
		a.Level, a.Code, a.Parent = "group", g, &d
	case division.MatchString(d) && g == "" && (c == "" || c == d):
		a.Level, a.Code, a.Parent = "division", d, &s
	case d == "" && g == "" && c == "":
		a.Level, a.Code = "section", s
	default:
		return a, fmt.Errorf("division %q, group %q, class %q fit no level", d, g, c)
	}
	return a, nil
}

// depth orders the levels from the top.
var depth = map[string]int{"section": 0, "division": 1, "group": 2, "class": 3}

// Read reads N_CAEN.CSV and keeps the published versions, in the order of the classification.
func Read(r io.Reader) ([]Activity, error) {
	rows, err := table(r, "SECTIUNEA", "SUBSECTIUNEA", "DIVIZIUNEA", "GRUPA", "CLASA", "DENUMIRE", "VERSIUNE_CAEN")
	if err != nil {
		return nil, fmt.Errorf("N_CAEN.CSV: %w", err)
	}

	var all []Activity
	byKey := map[string]Activity{}
	key := func(version int, level, code string) string { return fmt.Sprintf("%d %s %s", version, level, code) }
	for i, row := range rows {
		version, err := strconv.Atoi(row["VERSIUNE_CAEN"])
		if err != nil {
			return nil, fmt.Errorf("N_CAEN.CSV row %d: version %q", i+2, row["VERSIUNE_CAEN"])
		}
		revision, published := Versions[version]
		if !published {
			continue
		}
		a, err := entry(row)
		if err != nil {
			return nil, fmt.Errorf("N_CAEN.CSV row %d, version %d: %w", i+2, version, err)
		}
		a.Version, a.Revision = version, revision
		a.Name = strings.Join(strings.Fields(row["DENUMIRE"]), " ")
		if a.Name == "" {
			return nil, fmt.Errorf("N_CAEN.CSV row %d: %s %s has no name", i+2, a.Level, a.Code)
		}
		k := key(version, a.Level, a.Code)
		if _, twice := byKey[k]; twice {
			return nil, fmt.Errorf("N_CAEN.CSV: version %d has %s %s twice", version, a.Level, a.Code)
		}
		byKey[k] = a
		all = append(all, a)
	}

	up := map[string]string{"class": "group", "group": "division", "division": "section"}
	seen := map[int]bool{}
	for _, a := range all {
		seen[a.Version] = true
		if a.Parent == nil {
			continue
		}
		parent, ok := byKey[key(a.Version, up[a.Level], *a.Parent)]
		switch {
		case !ok:
			return nil, fmt.Errorf("version %d: %s %s has no %s %s", a.Version, a.Level, a.Code, up[a.Level], *a.Parent)
		case parent.Section != a.Section:
			return nil, fmt.Errorf("version %d: %s %s is in section %s, its %s in %s", a.Version, a.Level, a.Code, a.Section, up[a.Level], parent.Section)
		}
	}
	for v := range Versions {
		if !seen[v] {
			return nil, fmt.Errorf("N_CAEN.CSV has no version %d", v)
		}
	}

	// Each section, then its divisions, groups and classes: the codes of one section sort as a tree.
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		if (a.Level == "section") != (b.Level == "section") {
			return a.Level == "section"
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return depth[a.Level] < depth[b.Level]
	})
	return all, nil
}

func read(get Get, address string, limit int64) ([]byte, error) {
	body, err := get(address)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", address)
	}
	return data, nil
}

// Dataset reads the newest ONRC nomenclature and builds the dataset.
func Dataset(get Get) (dataset.Dataset, error) {
	answer, err := read(get, Catalogue, maxCatalogue)
	if err != nil {
		return dataset.Dataset{}, err
	}
	files, err := Newest(bytes.NewReader(answer))
	if err != nil {
		return dataset.Dataset{}, err
	}
	versionsFile, err := read(get, files.Versions, maxFile)
	if err != nil {
		return dataset.Dataset{}, err
	}
	versions, err := ReadVersions(bytes.NewReader(versionsFile))
	if err != nil {
		return dataset.Dataset{}, err
	}
	names := map[int]string{}
	for _, v := range versions {
		names[v.Code] = v.Name
	}
	codes := make([]int, 0, len(Versions))
	for v := range Versions {
		if names[v] == "" {
			return dataset.Dataset{}, fmt.Errorf("N_VERSIUNE_CAEN.CSV has no version %d", v)
		}
		codes = append(codes, v)
	}
	sort.Ints(codes)

	caenFile, err := read(get, files.CAEN, maxFile)
	if err != nil {
		return dataset.Dataset{}, err
	}
	all, err := Read(bytes.NewReader(caenFile))
	if err != nil {
		return dataset.Dataset{}, err
	}

	answer, err = read(get, CorrespondenceAddress(), maxAnswer)
	if err != nil {
		return dataset.Dataset{}, err
	}
	links, err := ReadCorrespondence(bytes.NewReader(answer))
	if err != nil {
		return dataset.Dataset{}, err
	}
	newInRev3, err := CheckLinks(links, all)
	if err != nil {
		return dataset.Dataset{}, err
	}

	var activities, published, correspondence []any
	for _, a := range all {
		activities = append(activities, a)
	}
	for _, l := range links {
		correspondence = append(correspondence, l)
	}
	for _, v := range codes {
		published = append(published, Version{Code: v, Name: names[v]})
	}

	return dataset.Dataset{
		Name:  "caen",
		Title: "Classification of economic activities in Romania (CAEN), Rev.2 and Rev.3, with the links between them",
		Description: "The sections, divisions, groups and classes of CAEN Rev.2 and CAEN Rev.3, with their names in Romanian, " +
			"as the National Trade Register Office (ONRC) uses them to decode the activities of companies. " +
			"A code is unique only within its version and level: Rev.2 and Rev.3 use some of the same codes for other activities, " +
			"so always read the version with the code. Rev.3 corresponds to NACE Rev.2.1 and Rev.2 to NACE Rev.2. " +
			"The older versions of the source (1998 and 2003) are not here. The days from which each version applies are not in the source. " +
			"correspondence.jsonl links each class of Rev.2 to the classes of Rev.3 that take its activities, from the correspondence table of Eurostat " +
			"between NACE Rev.2 and NACE Rev.2.1: CAEN writes the NACE class codes without the dot. Every class of Rev.2 has a link. " +
			"Classes of Rev.3 with no class of Rev.2 behind them: " + strings.Join(newInRev3, ", ") + ".",
		Source: dataset.Source{
			Name: "National Trade Register Office (ONRC), N_CAEN.CSV and N_VERSIUNE_CAEN.CSV of its newest nomenclatures on data.gov.ro" +
				"; links from Eurostat, correspondence table NACE Rev.2.1 to NACE Rev.2 (" + Correspondence + "), published by the Publications Office of the European Union",
			URL: "https://data.gov.ro/organization/onrc",
			Licence: "CC BY 4.0, under which ONRC publishes the nomenclatures on data.gov.ro; " +
				"the correspondence table under the European Commission reuse notice (Decision 2011/833/EU): reuse allowed with the source acknowledged",
		},
		Frequency: dataset.Manual,
		Update:    "When INS changes CAEN by order, which happens once in many years",
		Files: []dataset.File{
			{
				Name: "activities.jsonl",
				Fields: []dataset.Field{
					{Name: "version", Description: "The version code of ONRC, a code of versions.jsonl: 2 for Rev.2, 3 for Rev.3"},
					{Name: "revision", Description: "Rev.2 or Rev.3"},
					{Name: "level", Description: "section, division, group or class"},
					{Name: "code", Description: "A letter for a section; two, three or four digits for a division, a group or a class, as 62, 621, 6210"},
					{Name: "name", Description: "The name in Romanian"},
					{Name: "parent", Description: "The code of the entry one level up in the same version, or null for a section"},
					{Name: "section", Description: "The letter of the section the entry is in"},
				},
				Records: activities,
			},
			{
				Name: "versions.jsonl",
				Fields: []dataset.Field{
					{Name: "code", Description: "The version code of ONRC"},
					{Name: "name", Description: "The name ONRC gives the version"},
				},
				Records: published,
			},
			{
				Name: "correspondence.jsonl",
				Fields: []dataset.Field{
					{Name: "rev2", Description: "A class of CAEN Rev.2, four digits"},
					{Name: "rev3", Description: "A class of CAEN Rev.3 that takes all or part of its activities, four digits"},
					{Name: "kind", Description: "1:1 when the two classes match only each other; 1:n when the Rev.2 class is split; n:1 when Rev.2 classes are joined; n:m when both"},
				},
				Records: correspondence,
			},
		},
	}, nil
}
