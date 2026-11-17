package treasury

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pidginhost/romanian-datasets/internal/pdftext"
)

// Account is one line of the list of a treasury.
type Account struct {
	Source    string
	Code      string
	IBAN      string
	Name      string
	Published string
}

// List is what the file of one treasury says.
type List struct {
	Name     string
	Sources  map[string]string
	Accounts []Account
}

// The columns of a list, by where their text starts on the page.
const (
	ibanColumn = 60.0
	codeColumn = 150.0
	nameColumn = 240.0
)

var (
	datePattern    = regexp.MustCompile(`^\d{2}-\d{2}-\d{2}$`)
	ibanPattern    = regexp.MustCompile(`^RO\d{2}TREZ\d{3}[0-9A-Z]{13}$`)
	codePattern    = regexp.MustCompile(`^[0-9A-Z]+$`)
	letterPattern  = regexp.MustCompile(`^[A-Z]$`)
	printedPattern = regexp.MustCompile(`^\d{2}/\d{2}/\d{4}$`)
	pagePattern    = regexp.MustCompile(`^\d+$`)
)

// headers are the names of the columns, which a page can split over two heights.
var headers = map[string]bool{"Data publicarii": true, "Cont IBAN": true, "Cont": true, "Denumire": true}

func header(cells []string) bool {
	for _, c := range cells {
		if !headers[c] {
			return false
		}
	}
	return true
}

// words makes the spacing of a text single.
func words(s string) string { return strings.Join(strings.Fields(s), " ") }

// ValidIBAN checks the two check digits of an IBAN, as ISO 13616 sets them.
func ValidIBAN(iban string) bool {
	if len(iban) < 5 {
		return false
	}
	var digits strings.Builder
	for _, r := range iban[4:] + iban[:4] {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			fmt.Fprintf(&digits, "%d", r-'A'+10)
		default:
			return false
		}
	}
	n, ok := new(big.Int).SetString(digits.String(), 10)
	return ok && new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}

type line struct {
	y     float64
	texts []pdftext.Text
}

// lineGap is the most two texts of one line differ in height. Lines stand at least 9 units apart.
const lineGap = 2.0

// lines groups the text of a page into lines, from the top. A cell can stand a little above or below the
// others of its line.
func lines(page []pdftext.Text) []line {
	texts := append([]pdftext.Text(nil), page...)
	sort.SliceStable(texts, func(i, j int) bool { return texts[i].Y > texts[j].Y })
	var out []line
	for _, t := range texts {
		if n := len(out); n > 0 && out[n-1].y-t.Y <= lineGap {
			out[n-1].texts = append(out[n-1].texts, t)
			continue
		}
		out = append(out, line{t.Y, []pdftext.Text{t}})
	}
	for _, l := range out {
		sort.SliceStable(l.texts, func(i, j int) bool { return l.texts[i].X < l.texts[j].X })
	}
	return out
}

// ReadList reads the PDF file of one treasury. Every IBAN is checked by its check digits; a text the
// reader does not place stops it.
func ReadList(data []byte) (List, error) {
	pages, err := pdftext.Pages(data)
	if err != nil {
		return List{}, err
	}
	list := List{Sources: map[string]string{}}
	source, inSource := "", false
	var last *Account
	for n, page := range pages {
		fail := func(format string, a ...any) (List, error) {
			return List{}, fmt.Errorf("page %d: %s", n+1, fmt.Sprintf(format, a...))
		}
		for _, l := range lines(page) {
			first := words(l.texts[0].S)
			var cells []string
			for _, t := range l.texts {
				cells = append(cells, words(t.S))
			}
			switch {
			case first == "Coduri IBAN" && len(cells) == 1,
				printedPattern.MatchString(first) && len(cells) == 1,
				header(cells),
				first == "Pag." && len(cells) <= 2, pagePattern.MatchString(first) && len(cells) == 1:
				// Headings and the page number.
			case strings.HasPrefix(first, "Trezorerie") && len(cells) == 1:
				if list.Name != "" && list.Name != first {
					return fail("the treasury is %q, and %q before", first, list.Name)
				}
				list.Name = first
			case first == "Sursa de finantare:":
				// The last sections of a list have no funding source: the line has only its label.
				letter, name := "", ""
				switch {
				case len(cells) == 3 && letterPattern.MatchString(cells[1]):
					letter, name = cells[1], cells[2]
				case len(cells) != 1:
					return fail("a funding source line %q", strings.Join(cells, " | "))
				}
				if known, ok := list.Sources[letter]; ok && known != name {
					return fail("the funding source %s is %q, and %q before", letter, name, known)
				}
				// Each page repeats the funding source it starts in; a name can go on past the repeat.
				if !inSource || letter != source {
					last = nil
				}
				source, inSource = letter, true
				if letter != "" {
					list.Sources[letter] = name
				}
			case datePattern.MatchString(first):
				if len(l.texts) != 4 || l.texts[1].X < ibanColumn || l.texts[1].X >= codeColumn ||
					l.texts[2].X < codeColumn || l.texts[2].X >= nameColumn || l.texts[3].X < nameColumn {
					return fail("an account line %q", strings.Join(cells, " | "))
				}
				if !inSource {
					return fail("an account before any funding source")
				}
				day, err := time.Parse("02-01-06", first)
				if err != nil {
					return fail("the date %q", first)
				}
				iban, code := cells[1], cells[2]
				if !ibanPattern.MatchString(iban) || !ValidIBAN(iban) {
					return fail("%q is not a valid IBAN of the Treasury", iban)
				}
				if !codePattern.MatchString(code) || cells[3] == "" {
					return fail("an account line %q", strings.Join(cells, " | "))
				}
				list.Accounts = append(list.Accounts, Account{Source: source, Code: code, IBAN: iban, Name: cells[3], Published: day.Format(time.DateOnly)})
				last = &list.Accounts[len(list.Accounts)-1]
			case len(l.texts) == 1 && l.texts[0].X >= nameColumn && last != nil:
				// The name of the account above goes on.
				last.Name = words(last.Name + " " + first)
			default:
				return fail("a line the reader does not know: %q", strings.Join(cells, " | "))
			}
		}
	}
	if list.Name == "" || len(list.Accounts) == 0 {
		return List{}, fmt.Errorf("the file has no treasury name or no account")
	}
	return list, nil
}
