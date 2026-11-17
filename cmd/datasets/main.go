// Command datasets builds the published datasets and checks them.
//
//	datasets build  [-root dir]   builds every dataset and writes the files and index.json
//	datasets verify [-root dir]   checks the files against index.json
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/dataset"
	"github.com/pidginhost/romanian-datasets/internal/holidays"
)

func main() {
	os.Exit(run(os.Args[1:], time.Now(), os.Stdout, os.Stderr))
}

func run(args []string, now time.Time, out, errs io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errs, "usage: datasets build|verify [-root dir]")
		return 2
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(errs)
	root := flags.String("root", ".", "the repository folder")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}

	switch args[0] {
	case "build":
		all, err := build(now)
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		changed, err := dataset.Publish(*root, all, now.UTC().Format(time.DateOnly))
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		fmt.Fprintf(out, "%d dataset(s) built, %d changed", len(all), len(changed))
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
	default:
		fmt.Fprintf(errs, "unknown command %q\n", args[0])
		return 2
	}
}

// build makes every dataset. A new dataset is added here.
func build(now time.Time) ([]dataset.Dataset, error) {
	publicHolidays, err := holidays.Dataset(now.UTC().Year() + 1)
	if err != nil {
		return nil, err
	}
	return []dataset.Dataset{publicHolidays}, nil
}
