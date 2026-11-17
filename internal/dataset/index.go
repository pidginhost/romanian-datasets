package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// IndexFormat is the version of the layout of index.json. It changes only when a reader has to change.
const IndexFormat = 1

// IndexName is the one file a reader starts from.
const IndexName = "index.json"

// Folder is where the datasets are, next to the index.
const Folder = "datasets"

// Index lists every published dataset. A reader downloads it first, then the files it names,
// and checks each file against its SHA-256.
type Index struct {
	Format    int            `json:"format"`
	Publisher string         `json:"publisher"`
	Licence   string         `json:"licence"`
	Datasets  []IndexDataset `json:"datasets"`
}

// IndexDataset is one dataset in the index.
type IndexDataset struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Source      Source `json:"source"`
	Update      string `json:"update"`
	// Revision starts at 1 and grows by one each time the content of the dataset changes.
	Revision int `json:"revision"`
	// Changed is the day of the build that made this revision, as YYYY-MM-DD.
	Changed string      `json:"changed"`
	Files   []IndexFile `json:"files"`
}

// IndexFile is one file of a dataset. Path is relative to the index, with forward slashes.
type IndexFile struct {
	Path    string  `json:"path"`
	SHA256  string  `json:"sha256"`
	Bytes   int     `json:"bytes"`
	Records int     `json:"records"`
	Fields  []Field `json:"fields"`
}

// ReadIndex reads the index of a repository folder. A folder with no index gives an empty one.
func ReadIndex(root string) (Index, error) {
	var index Index
	data, err := os.ReadFile(filepath.Join(root, IndexName))
	if errors.Is(err, fs.ErrNotExist) {
		return index, nil
	}
	if err != nil {
		return index, err
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return index, fmt.Errorf("%s: %w", IndexName, err)
	}
	return index, nil
}

// Publish writes the datasets and the index under root. A dataset whose content is the same as in the
// index already there keeps its revision and its day; one that changed gets the next revision and today.
// Files of a dataset that are no longer built are removed. It returns the names of the datasets that changed.
func Publish(root string, datasets []Dataset, today string) ([]string, error) {
	old, err := ReadIndex(root)
	if err != nil {
		return nil, err
	}
	before := map[string]IndexDataset{}
	for _, d := range old.Datasets {
		before[d.Name] = d
	}

	sorted := append([]Dataset(nil), datasets...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	index := Index{Format: IndexFormat, Publisher: "PidginHost", Licence: "CC-BY-4.0", Datasets: []IndexDataset{}}
	var changed []string
	for i, d := range sorted {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		if i > 0 && sorted[i-1].Name == d.Name {
			return nil, fmt.Errorf("dataset %s is built twice", d.Name)
		}

		entry := IndexDataset{
			Name: d.Name, Title: d.Title, Description: d.Description, Source: d.Source, Update: d.Update,
			Files: []IndexFile{},
		}
		keep := map[string]bool{}
		for _, f := range d.Files {
			data, err := Lines(f.Records)
			if err != nil {
				return nil, fmt.Errorf("dataset %s, file %s: %w", d.Name, f.Name, err)
			}
			if err := write(filepath.Join(root, Folder, d.Name, f.Name), data); err != nil {
				return nil, err
			}
			sum := sha256.Sum256(data)
			entry.Files = append(entry.Files, IndexFile{
				Path:    path.Join(Folder, d.Name, f.Name),
				SHA256:  hex.EncodeToString(sum[:]),
				Bytes:   len(data),
				Records: len(f.Records),
				Fields:  f.Fields,
			})
			keep[f.Name] = true
		}
		if err := removeOthers(filepath.Join(root, Folder, d.Name), keep); err != nil {
			return nil, err
		}

		previous, known := before[d.Name]
		if known && sameContent(previous, entry) {
			entry.Revision, entry.Changed = previous.Revision, previous.Changed
		} else {
			entry.Revision, entry.Changed = previous.Revision+1, today
			changed = append(changed, d.Name)
		}
		index.Datasets = append(index.Datasets, entry)
	}

	data, err := Document(index)
	if err != nil {
		return nil, err
	}
	return changed, write(filepath.Join(root, IndexName), data)
}

// Verify checks that every file the index names is there with the size, the SHA-256 and the number of
// records the index gives, and that the datasets folder holds nothing the index does not name.
func Verify(root string) error {
	index, err := ReadIndex(root)
	if err != nil {
		return err
	}
	if index.Format != IndexFormat {
		return fmt.Errorf("%s: format %d, this tool reads format %d", IndexName, index.Format, IndexFormat)
	}

	named := map[string]bool{}
	for _, d := range index.Datasets {
		if d.Revision < 1 || d.Changed == "" {
			return fmt.Errorf("dataset %s: no revision or day", d.Name)
		}
		for _, f := range d.Files {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
			if err != nil {
				return fmt.Errorf("dataset %s: %w", d.Name, err)
			}
			sum := sha256.Sum256(data)
			switch {
			case hex.EncodeToString(sum[:]) != f.SHA256:
				return fmt.Errorf("%s: the content does not match the SHA-256 in the index", f.Path)
			case len(data) != f.Bytes:
				return fmt.Errorf("%s: %d bytes, the index says %d", f.Path, len(data), f.Bytes)
			case strings.Count(string(data), "\n") != f.Records:
				return fmt.Errorf("%s: %d records, the index says %d", f.Path, strings.Count(string(data), "\n"), f.Records)
			}
			named[f.Path] = true
		}
	}

	return filepath.WalkDir(filepath.Join(root, Folder), func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && len(index.Datasets) == 0 {
				return fs.SkipAll
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if !named[filepath.ToSlash(rel)] {
			return fmt.Errorf("%s is not in the index", filepath.ToSlash(rel))
		}
		return nil
	})
}

func sameContent(a, b IndexDataset) bool {
	a.Revision, a.Changed, b.Revision, b.Changed = 0, "", 0, ""
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func write(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}

func removeOthers(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
