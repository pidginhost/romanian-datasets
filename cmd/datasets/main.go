// Command datasets builds the published datasets and checks them.
//
//	datasets build  [-root dir] [-frequency f | -only name]
//	datasets verify [-root dir]
//	datasets list
//
// build with no filter builds every dataset. With -frequency it builds the datasets of one frequency, as the
// scheduled job of that frequency does, and with -only one dataset. The others stay as they are in the index.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/bnr"
	"github.com/pidginhost/romanian-datasets/internal/countries"
	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/holidays"
	"github.com/pidginhost/romanian-datasets/internal/siruta"
	"github.com/pidginhost/romanian-datasets/internal/vatnode"
)

// builder makes one dataset. Name and frequency are stated here so that a build can leave a dataset out
// without making it; they must be the ones the dataset itself carries.
type builder struct {
	name      string
	frequency dataset.Frequency
	build     func(now time.Time, root string) (dataset.Dataset, error)
}

// builders lists every dataset. A new dataset is added here.
var builders = []builder{
	{"bnr-currencies", dataset.Manual, func(now time.Time, _ string) (dataset.Dataset, error) {
		return bnr.Dataset(bnr.FromSite(&http.Client{Timeout: time.Minute}), now.UTC().Year())
	}},
	{"countries", dataset.Manual, func(time.Time, string) (dataset.Dataset, error) {
		client := &http.Client{Timeout: 2 * time.Minute}
		return countries.Dataset(countries.FromSite(client), countries.VIESFromSite(client))
	}},
	{"public-holidays", dataset.Manual, func(time.Time, string) (dataset.Dataset, error) {
		return holidays.Dataset(), nil
	}},
	{"siruta", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		published, err := publishedSIRUTA(root)
		if err != nil {
			return dataset.Dataset{}, err
		}
		return siruta.Dataset(siruta.FromSite(&http.Client{Timeout: 5 * time.Minute}), published)
	}},
	{"vat-numbers", dataset.Manual, func(time.Time, string) (dataset.Dataset, error) {
		client := &http.Client{Timeout: time.Minute}
		return vatnode.Numbers(vatnode.FromSite(client), vatnode.VIESFromSite(client))
	}},
	{"vat-rates", dataset.Weekly, func(time.Time, string) (dataset.Dataset, error) {
		client := &http.Client{Timeout: time.Minute}
		return vatnode.Rates(vatnode.FromSite(client), vatnode.VIESFromSite(client))
	}},
}

func main() {
	os.Exit(run(os.Args[1:], builders, time.Now(), os.Stdout, os.Stderr))
}

func run(args []string, all []builder, now time.Time, out, errs io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errs, "usage: datasets build [-frequency f | -only name] | verify | list  [-root dir]")
		return 2
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(errs)
	root := flags.String("root", ".", "the repository folder")
	frequency := flags.String("frequency", "", "build only the datasets of this frequency")
	only := flags.String("only", "", "build only this dataset")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}

	switch args[0] {
	case "build":
		built, kept, err := build(all, *frequency, *only, now, *root)
		if err != nil {
			fmt.Fprintln(errs, err)
			return 2
		}
		// A dataset that was never built has nothing to keep. It is named, and verify fails until it is built.
		kept, missing, err := known(*root, kept)
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		for _, name := range missing {
			fmt.Fprintf(out, "not built yet: %s\n", name)
		}
		changed, err := dataset.Publish(*root, built, kept, now.UTC().Format(time.DateOnly))
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		fmt.Fprintf(out, "%d dataset(s) built, %d changed", len(built), len(changed))
		for _, name := range changed {
			fmt.Fprintf(out, " %s", name)
		}
		fmt.Fprintln(out)
		return 0
	case "verify":
		if err := dataset.Verify(*root); err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		var names []string
		for _, b := range all {
			names = append(names, b.name)
		}
		if _, missing, err := known(*root, names); err != nil || len(missing) > 0 {
			fmt.Fprintf(errs, "listed but not in the index: %v %v\n", missing, err)
			return 1
		}
		fmt.Fprintln(out, "the files match the index")
		return 0
	case "list":
		for _, b := range all {
			fmt.Fprintf(out, "%s\t%s\n", b.name, b.frequency)
		}
		return 0
	default:
		fmt.Fprintf(errs, "unknown command %q\n", args[0])
		return 2
	}
}

// known splits names into the datasets the index already has and the ones it does not.
func known(root string, names []string) (in, missing []string, err error) {
	index, err := dataset.ReadIndex(root)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]bool{}
	for _, d := range index.Datasets {
		have[d.Name] = true
	}
	for _, name := range names {
		if have[name] {
			in = append(in, name)
		} else {
			missing = append(missing, name)
		}
	}
	return in, missing, nil
}

// build makes the datasets the filter selects and names the ones left as they are.
func build(all []builder, frequency, only string, now time.Time, root string) (built []dataset.Dataset, kept []string, err error) {
	switch {
	case frequency != "" && only != "":
		return nil, nil, fmt.Errorf("use -frequency or -only, not both")
	case frequency != "" && (!dataset.Frequency(frequency).Valid() || dataset.Frequency(frequency) == dataset.Manual):
		return nil, nil, fmt.Errorf("-frequency is daily, weekly, monthly or yearly; a manual dataset is built with -only or with no filter")
	}

	found := false
	for _, b := range all {
		if (frequency != "" && string(b.frequency) != frequency) || (only != "" && b.name != only) {
			kept = append(kept, b.name)
			continue
		}
		found = true
		d, err := b.build(now, root)
		if err != nil {
			return nil, nil, fmt.Errorf("dataset %s: %w", b.name, err)
		}
		if d.Name != b.name || d.Frequency != b.frequency {
			return nil, nil, fmt.Errorf("dataset %s (%s) is listed as %s (%s)", d.Name, d.Frequency, b.name, b.frequency)
		}
		built = append(built, d)
	}
	if only != "" && !found {
		return nil, nil, fmt.Errorf("no dataset named %s", only)
	}
	return built, kept, nil
}

// publishedSIRUTA gives the codes of the SIRUTA dataset as committed, or none before its first build.
func publishedSIRUTA(root string) ([]int, error) {
	file, err := os.Open(filepath.Join(root, "datasets", "siruta", "units.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return siruta.PublishedCodes(file)
}
