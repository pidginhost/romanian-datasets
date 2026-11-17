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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/anafforms"
	"github.com/pidginhost/romanian-datasets/internal/banks"
	"github.com/pidginhost/romanian-datasets/internal/bnr"
	"github.com/pidginhost/romanian-datasets/internal/budget"
	"github.com/pidginhost/romanian-datasets/internal/caen"
	"github.com/pidginhost/romanian-datasets/internal/chart"
	"github.com/pidginhost/romanian-datasets/internal/countries"
	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/efactura"
	"github.com/pidginhost/romanian-datasets/internal/en16931"
	"github.com/pidginhost/romanian-datasets/internal/fixedassets"
	"github.com/pidginhost/romanian-datasets/internal/holidays"
	"github.com/pidginhost/romanian-datasets/internal/mfp"
	"github.com/pidginhost/romanian-datasets/internal/reges"
	"github.com/pidginhost/romanian-datasets/internal/singleentry"
	"github.com/pidginhost/romanian-datasets/internal/siruta"
	"github.com/pidginhost/romanian-datasets/internal/taxoffices"
	"github.com/pidginhost/romanian-datasets/internal/travel"
	"github.com/pidginhost/romanian-datasets/internal/treasury"
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
	{"anaf-forms", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		var keys []string
		file, err := published(root, "anaf-forms", "versions.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		if file != nil {
			keys, err = anafforms.PublishedKeys(file)
			file.Close()
			if err != nil {
				return dataset.Dataset{}, err
			}
		}
		return anafforms.Dataset(anafforms.FromSite(&http.Client{Timeout: 10 * time.Minute}), keys)
	}},
	{"banks", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		keys, err := publishedBanks(root)
		if err != nil {
			return dataset.Dataset{}, err
		}
		return banks.Dataset(banks.FromSite(&http.Client{Timeout: 2 * time.Minute}), keys)
	}},
	{"budget-classification", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		var readers [2]io.Reader
		for k, name := range []string{"indicators.jsonl", "departments.jsonl"} {
			file, err := published(root, "budget-classification", name)
			if err != nil {
				return dataset.Dataset{}, err
			}
			if file != nil {
				defer file.Close()
				readers[k] = file
			}
		}
		keys, err := budget.ReadPublished(readers[0], readers[1])
		if err != nil {
			return dataset.Dataset{}, err
		}
		return budget.Dataset(budget.FromSite(&http.Client{Timeout: 3 * time.Minute}), keys)
	}},
	{"bnr-currencies", dataset.Manual, func(now time.Time, _ string) (dataset.Dataset, error) {
		return bnr.Dataset(bnr.FromSite(&http.Client{Timeout: time.Minute}), now.UTC().Year())
	}},
	{"caen", dataset.Manual, func(time.Time, string) (dataset.Dataset, error) {
		return caen.Dataset(caen.FromSite(&http.Client{Timeout: 2 * time.Minute}))
	}},
	{"chart-of-accounts", dataset.Monthly, chartBuilder(chart.Companies)},
	{"chart-of-accounts-nonprofit", dataset.Monthly, chartBuilder(chart.Nonprofit)},
	{"chart-of-accounts-public", dataset.Monthly, chartBuilder(chart.Public)},
	{"countries", dataset.Manual, func(time.Time, string) (dataset.Dataset, error) {
		client := &http.Client{Timeout: 2 * time.Minute}
		invoice, err := en16931.NewestCountries(en16931.FromSite(client))
		if err != nil {
			return dataset.Dataset{}, err
		}
		return countries.Dataset(countries.FromSite(client), countries.VIESFromSite(client), invoice)
	}},
	{"efactura", dataset.Monthly, func(time.Time, string) (dataset.Dataset, error) {
		return efactura.Dataset(efactura.FromSite(&http.Client{Timeout: 2 * time.Minute}))
	}},
	{"en16931-codes", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		codes, err := publishedEN16931(root)
		if err != nil {
			return dataset.Dataset{}, err
		}
		return en16931.Dataset(en16931.FromSite(&http.Client{Timeout: 2 * time.Minute}), codes)
	}},
	{"fixed-assets", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		var rows []fixedassets.Asset
		file, err := published(root, "fixed-assets", "assets.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		if file != nil {
			rows, err = fixedassets.PublishedRows(file)
			file.Close()
			if err != nil {
				return dataset.Dataset{}, err
			}
		}
		return fixedassets.Dataset(fixedassets.FromSite(&http.Client{Timeout: 3 * time.Minute}), rows)
	}},
	{"mfp-localities", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		places, err := readSIRUTA[mfp.Place](root, "units.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		counties, err := readSIRUTA[mfp.SIRUTACounty](root, "counties.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		var keys mfp.Published
		file, err := published(root, "mfp-localities", "localities.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		if file != nil {
			keys, err = mfp.PublishedKeys(file)
			file.Close()
			if err != nil {
				return dataset.Dataset{}, err
			}
		}
		get, modified := mfp.FromSite(&http.Client{Timeout: 3 * time.Minute})
		return mfp.Dataset(get, modified, places, counties, keys)
	}},
	{"public-holidays", dataset.Manual, func(time.Time, string) (dataset.Dataset, error) {
		return holidays.Dataset(), nil
	}},
	{"reges", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		keys, err := publishedREGES(root)
		if err != nil {
			return dataset.Dataset{}, err
		}
		return reges.Dataset(reges.FromSite(&http.Client{Timeout: 2 * time.Minute}), keys)
	}},
	{"single-entry-categories", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		var keys []string
		file, err := published(root, "single-entry-categories", "categories.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		if file != nil {
			keys, err = singleentry.PublishedKeys(file)
			file.Close()
			if err != nil {
				return dataset.Dataset{}, err
			}
		}
		return singleentry.Dataset(singleentry.FromSite(&http.Client{Timeout: 5 * time.Minute}), keys)
	}},
	{"siruta", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		published, err := publishedSIRUTA(root)
		if err != nil {
			return dataset.Dataset{}, err
		}
		return siruta.Dataset(siruta.FromSite(&http.Client{Timeout: 5 * time.Minute}), published)
	}},
	{"tax-offices", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		places, err := readSIRUTA[taxoffices.Place](root, "units.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		counties, err := readSIRUTA[taxoffices.County](root, "counties.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		keys, err := publishedTaxOffices(root)
		if err != nil {
			return dataset.Dataset{}, err
		}
		return taxoffices.Dataset(taxoffices.FromSite(&http.Client{Timeout: 2 * time.Minute}), places, counties, keys)
	}},
	{"travel-allowances", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		file, err := published(root, "countries", "countries.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		if file == nil {
			return dataset.Dataset{}, fmt.Errorf("the countries dataset is not built")
		}
		var countries []travel.Country
		decoder := json.NewDecoder(file)
		for decoder.More() {
			var c travel.Country
			if err := decoder.Decode(&c); err != nil {
				file.Close()
				return dataset.Dataset{}, fmt.Errorf("countries: %w", err)
			}
			countries = append(countries, c)
		}
		file.Close()
		var rows []travel.Allowance
		if file, err = published(root, "travel-allowances", "allowances.jsonl"); err != nil {
			return dataset.Dataset{}, err
		}
		if file != nil {
			rows, err = travel.PublishedRows(file)
			file.Close()
			if err != nil {
				return dataset.Dataset{}, err
			}
		}
		return travel.Dataset(travel.FromSite(&http.Client{Timeout: 3 * time.Minute}), countries, rows)
	}},
	{"treasury", dataset.Monthly, func(_ time.Time, root string) (dataset.Dataset, error) {
		places, err := readSIRUTA[treasury.Place](root, "units.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		counties, err := readSIRUTA[treasury.County](root, "counties.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		keys, err := publishedTreasury(root)
		if err != nil {
			return dataset.Dataset{}, err
		}
		return treasury.Dataset(treasury.FromSite(&http.Client{Timeout: 5 * time.Minute}), places, counties, keys)
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

// published opens a file of a dataset as committed; nil before the first build of the dataset.
func published(root, name, file string) (*os.File, error) {
	f, err := os.Open(filepath.Join(root, "datasets", name, file))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

// publishedBanks gives the register numbers and the code keys of the banks dataset as committed.
func publishedBanks(root string) (banks.Published, error) {
	var out banks.Published
	file, err := published(root, "banks", "institutions.jsonl")
	if file == nil || err != nil {
		return out, err
	}
	out.Numbers, err = banks.PublishedNumbers(file)
	file.Close()
	if err != nil {
		return out, err
	}
	file, err = published(root, "banks", "codes.jsonl")
	if file == nil || err != nil {
		return out, err
	}
	defer file.Close()
	out.Codes, err = banks.PublishedCodes(file)
	return out, err
}

// chartBuilder builds one chart of accounts, keeping every account published before.
func chartBuilder(spec chart.Spec) func(time.Time, string) (dataset.Dataset, error) {
	return func(_ time.Time, root string) (dataset.Dataset, error) {
		var keys []string
		file, err := published(root, spec.Name, "accounts.jsonl")
		if err != nil {
			return dataset.Dataset{}, err
		}
		if file != nil {
			keys, err = chart.PublishedKeys(file)
			file.Close()
			if err != nil {
				return dataset.Dataset{}, err
			}
		}
		return chart.Dataset(chart.FromSite(&http.Client{Timeout: 5 * time.Minute}), spec, keys)
	}
}

// readSIRUTA reads one file of the SIRUTA dataset as committed, which treasury and tax-offices are matched
// to. The SIRUTA dataset must be built first.
func readSIRUTA[T any](root, name string) ([]T, error) {
	file, err := published(root, "siruta", name)
	if err != nil {
		return nil, err
	}
	if file == nil {
		return nil, fmt.Errorf("the siruta dataset is not built")
	}
	defer file.Close()
	var out []T
	decoder := json.NewDecoder(file)
	for decoder.More() {
		var v T
		if err := decoder.Decode(&v); err != nil {
			return nil, fmt.Errorf("siruta %s: %w", name, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// publishedTaxOffices gives the unit ids and area codes of the tax-offices dataset as committed.
func publishedTaxOffices(root string) (taxoffices.Published, error) {
	var out taxoffices.Published
	file, err := published(root, "tax-offices", "units.jsonl")
	if file == nil || err != nil {
		return out, err
	}
	out.Units, err = taxoffices.PublishedUnits(file)
	file.Close()
	if err != nil {
		return out, err
	}
	file, err = published(root, "tax-offices", "areas.jsonl")
	if file == nil || err != nil {
		return out, err
	}
	defer file.Close()
	out.Areas, err = taxoffices.PublishedAreas(file)
	return out, err
}

// publishedTreasury gives the unit codes and account keys of the treasury dataset as committed.
func publishedTreasury(root string) (treasury.Published, error) {
	var out treasury.Published
	file, err := published(root, "treasury", "units.jsonl")
	if file == nil || err != nil {
		return out, err
	}
	out.Units, err = treasury.PublishedUnits(file)
	file.Close()
	if err != nil {
		return out, err
	}
	file, err = published(root, "treasury", "accounts.jsonl")
	if file == nil || err != nil {
		return out, err
	}
	defer file.Close()
	out.Accounts, err = treasury.PublishedAccounts(file)
	return out, err
}

// publishedSIRUTA gives the codes of the SIRUTA dataset as committed, or none before its first build.
func publishedSIRUTA(root string) ([]int, error) {
	file, err := published(root, "siruta", "units.jsonl")
	if file == nil || err != nil {
		return nil, err
	}
	defer file.Close()
	return siruta.PublishedCodes(file)
}

// publishedEN16931 gives the codes of each file of the EN 16931 dataset as committed.
func publishedEN16931(root string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, l := range en16931.Lists {
		file, err := published(root, "en16931-codes", l.File)
		if err != nil {
			return nil, err
		}
		if file == nil {
			continue
		}
		codes, err := en16931.PublishedCodes(file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.File, err)
		}
		out[l.File] = codes
	}
	return out, nil
}

// publishedREGES gives the keys of each file of the REGES dataset as committed. The files are named after
// the lists of REGES, so every file of the folder is read.
func publishedREGES(root string) (map[string][]string, error) {
	names, err := filepath.Glob(filepath.Join(root, "datasets", "reges", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, name := range names {
		file, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		keys, err := reges.PublishedKeys(file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[filepath.Base(name)] = keys
	}
	return out, nil
}
