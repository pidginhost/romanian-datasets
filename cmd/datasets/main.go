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
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/bnr"
	"github.com/pidginhost/romanian-datasets/internal/countries"
	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/holidays"
)

// builder makes one dataset. Name and frequency are stated here so that a build can leave a dataset out
// without making it; they must be the ones the dataset itself carries.
type builder struct {
	name      string
	frequency dataset.Frequency
	build     func(now time.Time) (dataset.Dataset, error)
}

// builders lists every dataset. A new dataset is added here.
var builders = []builder{
	{"bnr-currencies", dataset.Manual, func(now time.Time) (dataset.Dataset, error) {
		return bnr.Dataset(bnr.FromSite(&http.Client{Timeout: time.Minute}), now.UTC().Year())
	}},
	{"countries", dataset.Manual, func(time.Time) (dataset.Dataset, error) {
		client := &http.Client{Timeout: 2 * time.Minute}
		return countries.Dataset(countries.FromSite(client), countries.VIESFromSite(client))
	}},
	{"public-holidays", dataset.Manual, func(time.Time) (dataset.Dataset, error) {
		return holidays.Dataset(), nil
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
		built, kept, err := build(all, *frequency, *only, now)
		if err != nil {
			fmt.Fprintln(errs, err)
			return 2
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

// build makes the datasets the filter selects and names the ones left as they are.
func build(all []builder, frequency, only string, now time.Time) (built []dataset.Dataset, kept []string, err error) {
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
		d, err := b.build(now)
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
