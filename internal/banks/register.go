package banks

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RegisterAddress is where BNR gives one table of the register of credit institutions as HTML.
const RegisterAddress = "https://www.bnr.ro/stathtml?bid="

// RegisterPage is the page of BNR that lists the register.
const RegisterPage = "https://www.bnr.ro/2349-registre-si-liste"

// Tables are the tables of the register BNR publishes, by their block number: the institutions in Part I,
// the branches in Part II and the institutions removed from the register.
var Tables = []int{27302, 27303, 27304, 27305, 27307, 27308, 27311}

// maxTable is many times the largest table.
const maxTable = 4 << 20

// Kinds of institution. The numbers are the ones the table of removed institutions uses.
var Kinds = map[string]string{
	"1": "bank",
	"2": "housing-bank",
	"3": "mortgage-bank",
	"4": "cooperative-central",
	"5": "cooperative",
	"6": "branch-third-country",
	"7": "branch-eu",
}

// headings name the kind of the institutions in a table, by words of the heading above it. The more
// particular words come first.
var headings = []struct{ words, kind string }{
	{"economisire", "housing-bank"},
	{"credit ipotecar", "mortgage-bank"},
	{"Case centrale", "cooperative-central"},
	{"Cooperative de credit", "cooperative"},
	{"state ter", "branch-third-country"},
	{"alte state membre", "branch-eu"},
	{"Bănci", "bank"},
}

// Institution is one row of the register.
type Institution struct {
	// Number is the number of the entry in the register, which BNR does not reuse.
	Number     string  `json:"number"`
	Kind       string  `json:"kind"`
	Name       string  `json:"name"`
	Address    *string `json:"address"`
	Registered string  `json:"registered"`
	CUI        *string `json:"cui"`
	// TradeRegister is the number in the trade register.
	TradeRegister *string `json:"trade_register"`
	LEI           *string `json:"lei"`
	// HeadOffice is the LEI of the institution a branch belongs to, for a branch the register gives no
	// LEI for.
	HeadOffice *string `json:"head_office_lei"`
	Start      *string `json:"start"`
	Removed    *string `json:"removed"`
	Remarks    *string `json:"remarks"`
}

// column says which field a header of the register fills. The more particular headers come first.
var columns = []struct{ header, field string }{
	{"Data radierii", "removed"},
	{"Data de la care", "start"},
	{"Tip entitate", "kind"},
	{"Denumire", "name"},
	{"Adresa", "address"},
	{"Cod de identificare fiscal", "cui"},
	{"registrul comer", "trade_register"},
	{"(LEI)", "lei"},
	{"Observa", "remarks"},
}

var (
	tagPattern    = regexp.MustCompile(`<[^>]*>`)
	headPattern   = regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3>`)
	tablePattern  = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	rowPattern    = regexp.MustCompile(`(?is)<tr[^>]*>`)
	cellPattern   = regexp.MustCompile(`(?is)<t([dh])([^>]*)>`)
	spanPattern   = regexp.MustCompile(`(?i)colspan\s*=\s*"?(\d+)`)
	numberPattern = regexp.MustCompile(`^[A-Z]+(-[A-Z0-9]+)+$`)
	leiPattern    = regexp.MustCompile(`^[0-9A-Z]{18}[0-9]{2}$`)
)

// text gives the words of an HTML fragment, without tags and with the spacing made single.
func text(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))), " ")
}

type cell struct {
	header bool
	span   int
	text   string
}

// rows splits a table into its rows and cells. The tables of BNR are not well formed: a row or cell is
// often not closed, so each one runs until the next one starts.
func rows(table string) [][]cell {
	var out [][]cell
	starts := rowPattern.FindAllStringIndex(table, -1)
	for i, s := range starts {
		end := len(table)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		row := table[s[1]:end]
		var cells []cell
		found := cellPattern.FindAllStringSubmatchIndex(row, -1)
		for j, c := range found {
			cellEnd := len(row)
			if j+1 < len(found) {
				cellEnd = found[j+1][0]
			}
			attributes := row[c[4]:c[5]]
			span := 1
			if m := spanPattern.FindStringSubmatch(attributes); m != nil {
				fmt.Sscan(m[1], &span)
			}
			cells = append(cells, cell{
				header: strings.EqualFold(row[c[2]:c[3]], "h"),
				span:   span,
				text:   text(row[c[1]:cellEnd]),
			})
		}
		if len(cells) > 0 {
			out = append(out, cells)
		}
	}
	return out
}

// fields reads the two header rows of a table and gives the field of each column.
func fields(header [][]cell) ([]string, error) {
	if len(header) < 2 {
		return nil, fmt.Errorf("the table has no two header rows")
	}
	var names []string
	second := header[1]
	for _, c := range header[0] {
		if c.span > 1 {
			if len(second) < c.span {
				return nil, fmt.Errorf("the header %q has fewer columns under it than it spans", c.text)
			}
			for _, sub := range second[:c.span] {
				names = append(names, sub.text)
			}
			second = second[c.span:]
			continue
		}
		names = append(names, c.text)
	}

	var out []string
	for i, name := range names {
		field := ""
		switch {
		case i == 0 && name == "Număr":
			field = "number"
		case i == 1 && name == "Data":
			field = "registered"
		default:
			for _, c := range columns {
				if strings.Contains(name, c.header) {
					field = c.field
					break
				}
			}
		}
		if field == "" {
			return nil, fmt.Errorf("the column %q is not known to the build", name)
		}
		out = append(out, field)
	}
	return out, nil
}

// date turns a date of the register, day.month.year, into year-month-day. An empty cell is no date.
func date(value string) (*string, error) {
	if value == "" {
		return nil, nil
	}
	d, err := time.Parse("02.01.2006", value)
	if err != nil {
		return nil, fmt.Errorf("%q is not a date", value)
	}
	s := d.Format(time.DateOnly)
	return &s, nil
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ReadTable reads one table of the register as BNR gives it.
func ReadTable(r io.Reader) ([]Institution, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTable+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTable {
		return nil, fmt.Errorf("the table is larger than expected")
	}
	page := string(data)

	tables := tablePattern.FindAllStringSubmatchIndex(page, -1)
	if len(tables) == 0 {
		return nil, fmt.Errorf("not a table of the register")
	}
	var out []Institution
	for _, t := range tables {
		kind := ""
		for _, h := range headPattern.FindAllStringSubmatch(page[:t[0]], -1) {
			if words := text(h[1]); words != "" {
				kind = words
			}
		}
		if kind != "" {
			heading := kind
			kind = ""
			for _, h := range headings {
				if strings.Contains(heading, h.words) {
					kind = h.kind
					break
				}
			}
			if kind == "" {
				return nil, fmt.Errorf("the heading %q is not known to the build", heading)
			}
		}

		all := rows(page[t[2]:t[3]])
		var header [][]cell
		var body [][]cell
		for _, r := range all {
			if r[0].header {
				header = append(header, r)
			} else {
				body = append(body, r)
			}
		}
		names, err := fields(header)
		if err != nil {
			return nil, err
		}
		for _, r := range body {
			if len(r) != len(names) {
				return nil, fmt.Errorf("a row has %d cells, the header %d", len(r), len(names))
			}
			values := map[string]string{"kind": ""}
			for i, c := range r {
				values[names[i]] = c.text
			}
			institution, err := institution(values, kind)
			if err != nil {
				return nil, err
			}
			out = append(out, institution)
		}
	}
	return out, nil
}

func institution(values map[string]string, kind string) (Institution, error) {
	number := values["number"]
	if !numberPattern.MatchString(number) {
		return Institution{}, fmt.Errorf("%q is not a register number", number)
	}
	fail := func(err error) (Institution, error) { return Institution{}, fmt.Errorf("%s: %w", number, err) }

	if values["kind"] != "" {
		k, ok := Kinds[values["kind"]]
		if !ok {
			return fail(fmt.Errorf("kind %q is not known to the build", values["kind"]))
		}
		kind = k
	}
	if kind == "" {
		return fail(fmt.Errorf("no kind: the table has no heading and the row no kind"))
	}
	if values["name"] == "" {
		return fail(fmt.Errorf("no name"))
	}
	registered, err := date(values["registered"])
	if err != nil || registered == nil {
		return fail(fmt.Errorf("the date of registration %q", values["registered"]))
	}
	start, err := date(values["start"])
	if err != nil {
		return fail(err)
	}
	removed, err := date(values["removed"])
	if err != nil {
		return fail(err)
	}
	lei := optional(values["lei"])
	if lei != nil && !leiPattern.MatchString(*lei) {
		return fail(fmt.Errorf("%q is not a LEI", *lei))
	}
	return Institution{
		Number:        number,
		Kind:          kind,
		Name:          values["name"],
		Address:       optional(values["address"]),
		Registered:    *registered,
		CUI:           optional(values["cui"]),
		TradeRegister: optional(values["trade_register"]),
		LEI:           lei,
		Start:         start,
		Removed:       removed,
		Remarks:       optional(values["remarks"]),
	}, nil
}

// Register reads every table of the register. A number in two tables stops the build.
func Register(get Get) ([]Institution, error) {
	var all []Institution
	seen := map[string]bool{}
	for _, bid := range Tables {
		address := fmt.Sprintf("%s%d", RegisterAddress, bid)
		body, err := get(address)
		if err != nil {
			return nil, err
		}
		institutions, err := ReadTable(body)
		body.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", address, err)
		}
		for _, i := range institutions {
			if seen[i.Number] {
				return nil, fmt.Errorf("the register has %s twice", i.Number)
			}
			seen[i.Number] = true
			all = append(all, i)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Number < all[j].Number })
	return all, nil
}
