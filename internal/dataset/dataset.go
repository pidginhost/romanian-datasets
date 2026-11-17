// Package dataset defines what a published dataset is and writes it in a form
// that is the same on every run: the same input gives the same bytes.
package dataset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

// Source says where the data of a dataset comes from and under which terms.
type Source struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Licence string `json:"licence"`
}

// Field describes one field of the records of a file.
type Field struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// File is one JSON Lines file of a dataset: one record on each line.
type File struct {
	// Name is the file name inside the dataset folder, ending in .jsonl.
	Name   string
	Fields []Field
	// Records are written in the order given. The builder sorts them.
	Records []any
}

// Dataset is one published list with its description and its files.
type Dataset struct {
	// Name is the folder name: lower case letters, digits and dashes.
	Name        string
	Title       string
	Description string
	Source      Source
	// Update says how often the source changes, in words.
	Update string
	Files  []File
}

var (
	datasetName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	fileName    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*\.jsonl$`)
)

// Validate reports the first thing that stops a dataset from being published.
func (d Dataset) Validate() error {
	switch {
	case !datasetName.MatchString(d.Name):
		return fmt.Errorf("dataset name %q: use lower case letters, digits and dashes", d.Name)
	case d.Title == "" || d.Description == "" || d.Update == "":
		return fmt.Errorf("dataset %s: title, description and update are required", d.Name)
	case d.Source.Name == "" || d.Source.URL == "" || d.Source.Licence == "":
		return fmt.Errorf("dataset %s: the source needs a name, an address and a licence", d.Name)
	case len(d.Files) == 0:
		return fmt.Errorf("dataset %s: no files", d.Name)
	}

	seen := map[string]bool{}
	for _, f := range d.Files {
		switch {
		case !fileName.MatchString(f.Name):
			return fmt.Errorf("dataset %s: file name %q must be lower case and end in .jsonl", d.Name, f.Name)
		case seen[f.Name]:
			return fmt.Errorf("dataset %s: file %s is listed twice", d.Name, f.Name)
		case len(f.Fields) == 0:
			return fmt.Errorf("dataset %s: file %s describes no fields", d.Name, f.Name)
		case len(f.Records) == 0:
			return fmt.Errorf("dataset %s: file %s has no records", d.Name, f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}

// Lines encodes records as JSON Lines: UTF-8, one record on each line, each line ended by a line feed.
func Lines(records []any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	for i, r := range records {
		if err := enc.Encode(r); err != nil {
			return nil, fmt.Errorf("record %d: %w", i+1, err)
		}
	}
	return out.Bytes(), nil
}

// Document encodes a value as indented JSON ended by a line feed.
func Document(v any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
