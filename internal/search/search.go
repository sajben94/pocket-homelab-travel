// Package search folds text for accent-insensitive matching and parses the
// date words of a search ("2025", "july", "summer 2024") into a filter.
package search

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// letters that do not decompose into base letter + accent under NFD.
var special = strings.NewReplacer(
	"đ", "d", "Đ", "d", "ł", "l", "Ł", "l", "ø", "o", "Ø", "o", "æ", "ae", "Æ", "ae",
	"œ", "oe", "Œ", "oe", "ß", "ss", "ı", "i", "þ", "th", "Þ", "th", "ð", "d", "Ð", "d", "ħ", "h", "Ħ", "h",
)

// Fold lowercases s and strips diacritics: "Açores Ðakovo" → "acores dakovo".
func Fold(s string) string {
	s = special.Replace(s)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		out = s
	}
	return strings.ToLower(out)
}

// Season names (northern meteorological seasons).
const (
	Spring = "spring"
	Summer = "summer"
	Autumn = "autumn"
	Winter = "winter"
)

var monthWords = map[string]time.Month{}
var seasonWords = map[string]string{
	"spring": Spring, "summer": Summer, "autumn": Autumn, "fall": Autumn, "winter": Winter,
	// Serbian / Croatian / Bosnian, folded
	"proljece": Spring, "prolece": Spring, "leto": Summer, "ljeto": Summer, "jesen": Autumn, "zima": Winter,
}

func init() {
	for m := time.January; m <= time.December; m++ {
		name := strings.ToLower(m.String())
		monthWords[name] = m
		monthWords[name[:3]] = m
	}
	monthWords["sept"] = time.September
	// Serbian / Croatian month names, folded (Latin script)
	for i, names := range [][]string{
		{"januar", "sijecanj"}, {"februar", "veljaca"}, {"mart", "ozujak"}, {"april", "travanj"}, {"maj", "svibanj"}, {"jun", "juni", "lipanj"},
		{"jul", "juli", "srpanj"}, {"avgust", "kolovoz"}, {"septembar", "rujan"}, {"oktobar", "listopad"}, {"novembar", "studeni"}, {"decembar", "prosinac"},
	} {
		for _, n := range names {
			if _, taken := monthWords[n]; !taken {
				monthWords[n] = time.Month(i + 1)
			}
		}
	}
}

// Query is a parsed search: free-text words plus optional date constraints.
// Within a kind the values are alternatives (2024 or 2025); kinds combine
// (summer and 2025 = summer 2025).
type Query struct {
	Words   []string     // folded
	Years   []int        // 2025
	Months  []time.Month // july
	Seasons []string     // summer

	named []string // the month and season words as typed (folded)
}

// Literal returns the query with month and season words treated as plain
// words again, for when "Mar del Plata" or "Summer Hall" found nothing.
func (q Query) Literal() (Query, bool) {
	if len(q.named) == 0 {
		return q, false
	}
	out := Query{Words: append(append([]string{}, q.Words...), q.named...), Years: q.Years}
	return out, true
}

// HasDate reports whether the query constrains dates at all.
func (q Query) HasDate() bool { return len(q.Years)+len(q.Months)+len(q.Seasons) > 0 }

// Empty reports whether nothing was asked.
func (q Query) Empty() bool { return len(q.Words) == 0 && !q.HasDate() }

// Parse splits a search into words and date terms.
func Parse(s string) Query {
	var q Query
	for _, w := range strings.Fields(Fold(s)) {
		w = strings.Trim(w, ".,;:!?\"'()")
		if w == "" || w == "with" || w == "sa" { // "with Ana", "sa Anom": the name is what counts
			continue
		}
		if len(w) == 4 && (strings.HasPrefix(w, "19") || strings.HasPrefix(w, "20")) {
			if y, err := strconv.Atoi(w); err == nil {
				q.Years = append(q.Years, y)
				continue
			}
		}
		if m, ok := monthWords[w]; ok {
			q.Months = append(q.Months, m)
			q.named = append(q.named, w)
			continue
		}
		if se, ok := seasonWords[w]; ok {
			q.Seasons = append(q.Seasons, se)
			q.named = append(q.named, w)
			continue
		}
		q.Words = append(q.Words, w)
	}
	return q
}

// MatchText reports which of the words occur in the folded text.
func MatchText(words []string, folded string) []bool {
	out := make([]bool, len(words))
	for i, w := range words {
		out[i] = strings.Contains(folded, w)
	}
	return out
}

// seasonMonths are the months of each season; winter spans the new year.
var seasonMonths = map[string][3]time.Month{
	Spring: {time.March, time.April, time.May},
	Summer: {time.June, time.July, time.August},
	Autumn: {time.September, time.October, time.November},
	Winter: {time.December, time.January, time.February},
}

// SeasonOf returns the season a month belongs to and the "season year": the
// year in which that season began (January 2026 is winter 2025).
func SeasonOf(d time.Time) (string, int) {
	for name, ms := range seasonMonths {
		for _, m := range ms {
			if d.Month() == m {
				y := d.Year()
				if name == Winter && d.Month() != time.December {
					y--
				}
				return name, y
			}
		}
	}
	return "", d.Year()
}

// MatchDates reports whether any day in [start, end] (YYYY-MM-DD, inclusive)
// satisfies the date terms. Each day must meet every kind given: with
// "summer 2025" a day of July 2025 matches, a day of July 2024 does not.
// A year next to a season means the season year (winter 2025 = Dec 2025 –
// Feb 2026); a year with only months means the calendar year.
func (q Query) MatchDates(start, end string) bool {
	if !q.HasDate() {
		return true
	}
	if start == "" {
		return false
	}
	if end == "" || end < start {
		end = start
	}
	s, err1 := time.Parse("2006-01-02", start)
	e, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil {
		return false
	}
	// Walk month by month: every term is month-granular.
	cur := time.Date(s.Year(), s.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(e.Year(), e.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; !cur.After(last) && i < 240; i++ {
		if q.matchMonth(cur) {
			return true
		}
		cur = cur.AddDate(0, 1, 0)
	}
	return false
}

func (q Query) matchMonth(d time.Time) bool {
	if len(q.Months) > 0 && !containsMonth(q.Months, d.Month()) {
		return false
	}
	season, seasonYear := SeasonOf(d)
	if len(q.Seasons) > 0 && !containsStr(q.Seasons, season) {
		return false
	}
	if len(q.Years) > 0 {
		y := d.Year()
		if len(q.Seasons) > 0 {
			y = seasonYear
		}
		if !containsInt(q.Years, y) {
			return false
		}
	}
	return true
}

func containsMonth(l []time.Month, m time.Month) bool {
	for _, x := range l {
		if x == m {
			return true
		}
	}
	return false
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func containsInt(l []int, n int) bool {
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}
