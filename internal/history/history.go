// Package history keeps the rows of a table that a law sets, across builds: the rows of the first build are
// those in force on the horizon, and a later change of the law, which a person records with the day it
// applies from and the act, closes the old row and opens a new one. A change nobody recorded stops the build.
package history

import (
	"fmt"
	"sort"
	"time"
)

// Change is a change of a row: the day it applies from and the act that made it.
type Change struct {
	Applies string
	Act     string
}

// Period gives the fields of a row that hold when it applies: from, to and the act.
type Period[T any] func(row *T) (from, to, act **string)

// Merge joins the rows the source gives now with the rows published before. id names a row across builds;
// content is what a change changes. changes gives, by id, the change a person read in the law.
// less orders the ids in the file.
func Merge[T any](now, published []T, id, content func(T) string, period Period[T], changes map[string]Change, less func(a, b string) bool) ([]T, error) {
	if len(published) == 0 {
		out := append([]T(nil), now...)
		sort.SliceStable(out, func(i, j int) bool { return less(id(out[i]), id(out[j])) })
		return out, nil
	}
	out := append([]T(nil), published...)
	open := map[string]int{}
	for i := range out {
		if _, to, _ := period(&out[i]); *to == nil {
			if _, twice := open[id(out[i])]; twice {
				return nil, fmt.Errorf("%s is open twice in the published rows", id(out[i]))
			}
			open[id(out[i])] = i
		}
	}
	close := func(key string, at int) error {
		c, ok := changes[key]
		if !ok {
			return fmt.Errorf("%s changed in the law: a person records the day it applies and the act in Changes", key)
		}
		applies, err := time.Parse(time.DateOnly, c.Applies)
		if err != nil {
			return fmt.Errorf("Changes %s: %w", key, err)
		}
		if at >= 0 {
			end := applies.AddDate(0, 0, -1).Format(time.DateOnly)
			_, to, _ := period(&out[at])
			*to = &end
		}
		return nil
	}
	seen := map[string]bool{}
	for _, row := range now {
		key := id(row)
		seen[key] = true
		at, ok := open[key]
		if ok && content(out[at]) == content(row) {
			continue
		}
		if !ok {
			at = -1
		}
		if err := close(key, at); err != nil {
			return nil, err
		}
		from, act := changes[key].Applies, changes[key].Act
		f, _, a := period(&row)
		*f, *a = &from, &act
		out = append(out, row)
	}
	for key, at := range open {
		if !seen[key] {
			if err := close(key, at); err != nil {
				return nil, fmt.Errorf("%s left the law: %w", key, err)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := id(out[i]), id(out[j])
		if a != b {
			return less(a, b)
		}
		fi, _, _ := period(&out[i])
		fj, _, _ := period(&out[j])
		return *fi == nil || (*fj != nil && **fi < **fj)
	})
	return out, nil
}

// Key joins an id and the day a row applies from, as the key of a row in its file.
func Key(id string, from *string) string {
	if from == nil {
		return id + "|"
	}
	return id + "|" + *from
}
