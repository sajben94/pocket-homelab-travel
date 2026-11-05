package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// Countries come from coordinates: every located stay, place and trip
// destination is reverse-geocoded once in the background (Nominatim, at most
// one request a second, cached per ~100 m), and a trip's days are spread over
// the countries of where you slept and what you visited each day.

// noCountry marks a spot that is in no country (open sea): looked up, nothing found.
const noCountry = "-"

// kickCountriesOnWrite wakes the country worker after anything was saved:
// new coordinates are looked up within seconds.
func (s *Server) kickCountriesOnWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.kickCountries()
		}
	})
}

// kickCountries asks the background worker to look up new coordinates soon.
func (s *Server) kickCountries() {
	select {
	case s.countryKick <- struct{}{}:
	default:
	}
}

// countryLoop looks up countries for located rows that have none, whenever
// something was saved and every 15 minutes (for rows that failed before).
func (s *Server) countryLoop(ctx context.Context) {
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	s.detectCountries(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.countryKick:
		case <-t.C:
		}
		s.detectCountries(ctx)
	}
}

// detectCountries fills country codes until none are missing or a lookup fails.
func (s *Server) detectCountries(ctx context.Context) int {
	done := 0
	for ctx.Err() == nil {
		rows, err := s.store.MissingCountries(ctx, 50)
		if err != nil || len(rows) == 0 {
			return done
		}
		progress := false
		for _, l := range rows {
			code, err := s.countryAt(ctx, l.Lat, l.Lng)
			if err != nil {
				s.log.Debug("country lookup", "err", err)
				return done
			}
			if err := s.store.SetCountryCode(ctx, l, code); err == nil {
				done++
				progress = true
			}
		}
		if !progress {
			return done
		}
	}
	return done
}

// countryAt returns the country code at a spot, from the cache or Nominatim.
func (s *Server) countryAt(ctx context.Context, lat, lng float64) (string, error) {
	key := fmt.Sprintf("country:%.3f,%.3f", lat, lng)
	if v, _, ok := s.store.CacheGet(ctx, key); ok && v != "" {
		return v, nil
	}
	lookup := s.countryLookup
	if lookup == nil {
		lookup = func(ctx context.Context, lat, lng float64) (string, error) {
			// Nominatim's policy: at most one request per second.
			s.nominatimMu.Lock()
			defer s.nominatimMu.Unlock()
			if wait := time.Until(s.nominatimLast.Add(1100 * time.Millisecond)); wait > 0 {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(wait):
				}
			}
			defer func() { s.nominatimLast = time.Now() }()
			r, err := s.geo.Reverse(ctx, lat, lng)
			if err != nil {
				return "", err
			}
			return r.CountryCode, nil
		}
	}
	code, err := lookup(ctx, lat, lng)
	if err != nil {
		return "", err
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		code = noCountry
	}
	_ = s.store.CachePut(ctx, key, code)
	return code, nil
}

// CountryDays is how many days of a trip were spent in a country.
type CountryDays = store.CountryDays

func knownCountry(c string) bool { return len(c) == 2 }

// countryDays spreads a trip's days over countries. A day counts for every
// country you slept in the night before or after it (a stay from check-in to
// check-out) and every country of a place scheduled that day, so a travel
// day between two countries counts for both. A day with nothing known
// continues the day before; the first such days take the destination's
// country. With a manual list only those countries count, and a day that
// would then be empty takes the only listed country.
func countryDays(t *store.Trip, stays []store.Lodging, places []store.Place) []CountryDays {
	if t.StartDate == "" || t.EndDate == "" || t.EndDate < t.StartDate {
		return []CountryDays{}
	}
	start, err1 := store.ParseDate(t.StartDate)
	end, err2 := store.ParseDate(t.EndDate)
	if err1 != nil || err2 != nil {
		return []CountryDays{}
	}
	manual := map[string]bool{}
	for _, c := range t.CountriesManual {
		manual[c] = true
	}
	allowed := func(c string) bool { return knownCountry(c) && (len(manual) == 0 || manual[c]) }
	byDay := map[string][]string{}
	add := func(day, code string) {
		if !allowed(code) {
			return
		}
		for _, c := range byDay[day] {
			if c == code {
				return
			}
		}
		byDay[day] = append(byDay[day], code)
	}
	// stays in check-in order, places in itinerary order, so "first" follows the trip
	sort.SliceStable(stays, func(i, j int) bool { return stays[i].CheckIn < stays[j].CheckIn })
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		day := d.Format("2006-01-02")
		for _, l := range stays {
			in, out := dateOnly(l.CheckIn), dateOnly(l.CheckOut)
			if in == "" {
				continue
			}
			if out == "" {
				out = in
			}
			if out == day || (in <= day && day < out) {
				add(day, l.CountryCode)
			}
		}
		for _, p := range places {
			if p.DayDate == day {
				add(day, p.CountryCode)
			}
		}
	}
	fallback := ""
	if allowed(t.CountryCode) {
		fallback = t.CountryCode
	} else if len(t.CountriesManual) == 1 {
		fallback = t.CountriesManual[0]
	}
	out := []CountryDays{}
	idx := map[string]int{}
	var prev []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		day := d.Format("2006-01-02")
		codes := byDay[day]
		if len(codes) == 0 {
			codes = prev
		}
		if len(codes) == 0 && fallback != "" {
			codes = []string{fallback}
		}
		for _, c := range codes {
			if i, ok := idx[c]; ok {
				out[i].Days++
			} else {
				idx[c] = len(out)
				out = append(out, CountryDays{Code: c, Days: 1, First: day})
			}
		}
		if len(codes) > 0 {
			prev = codes
		}
	}
	// days before anything was known: the first known country, when no destination country
	if fallback == "" && len(out) > 0 {
		missing := 0
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			if len(byDay[d.Format("2006-01-02")]) > 0 {
				break
			}
			missing++
		}
		out[0].Days += missing
		out[0].First = t.StartDate
	}
	// manual countries with no days found still belong to the trip
	for _, c := range t.CountriesManual {
		if _, ok := idx[c]; !ok {
			out = append(out, CountryDays{Code: c})
		}
	}
	return out
}

// tripCountries is the trip's countries with days, from its stays and places.
func (s *Server) tripCountries(ctx context.Context, t *store.Trip) []CountryDays {
	stays, _ := s.store.ListLodging(ctx, t.ID)
	places, _ := s.store.ListPlaces(ctx, t.ID)
	return countryDays(t, stays, places)
}

// countryName is the English name of a country code ("PT" → "Portugal").
func countryName(code string) string {
	r, err := language.ParseRegion(code)
	if err != nil {
		return code
	}
	if n := display.English.Regions().Name(r); n != "" {
		return n
	}
	return code
}

// tripCountryWords are extra words a trip is found by: the names and codes
// of the countries it went through.
func (s *Server) tripCountryWords(ctx context.Context, t *store.Trip) []string {
	codes := map[string]bool{}
	for _, c := range t.CountriesManual {
		codes[c] = true
	}
	if knownCountry(t.CountryCode) {
		codes[t.CountryCode] = true
	}
	if len(t.CountriesManual) == 0 {
		rows, err := s.store.DB.QueryContext(ctx, `SELECT country_code FROM lodging WHERE trip_id = ? UNION SELECT country_code FROM places WHERE trip_id = ?`, t.ID, t.ID)
		if err == nil {
			for rows.Next() {
				var c string
				if rows.Scan(&c) == nil && knownCountry(c) {
					codes[c] = true
				}
			}
			rows.Close()
		}
	}
	out := []string{}
	for c := range codes {
		out = append(out, countryName(c))
	}
	return out
}

// StatsCountry is one country over all trips.
type StatsCountry struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Days        int     `json:"days"`         // days spent there up to today
	PlannedDays int     `json:"planned_days"` // days still ahead (upcoming trips, the rest of a running one)
	Trips       []int64 `json:"trips"`
	First       string  `json:"first"` // first day there
	Last        string  `json:"last"`  // last day there (up to today)
}

// StatsTrip is a trip on the timeline.
type StatsTrip struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Color     string        `json:"color"`
	Status    string        `json:"status"`
	StartDate string        `json:"start_date"`
	EndDate   string        `json:"end_date"`
	Days      int           `json:"days"`
	Countries []CountryDays `json:"countries"`
	Places    int           `json:"places"`
	Visited   int           `json:"visited"`
	Upcoming  bool          `json:"upcoming"`
}

// StatsYear sums a calendar year.
type StatsYear struct {
	Year      int      `json:"year"`
	Trips     int      `json:"trips"`
	Days      int      `json:"days"` // days away that year (a trip over new year is split)
	Countries []string `json:"countries"`
	New       []string `json:"new_countries"` // countries first visited that year
}

// handleStats answers GET /api/v1/stats: where you went, for how long, when.
// Cancelled trips are left out; archived ones count (they happened).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ctx := r.Context()
	trips, err := s.store.ListTrips(ctx, s.visibleTrips(ctx, u.ID))
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	today := store.Today()
	byCode := map[string]*StatsCountry{}
	years := map[int]*StatsYear{}
	yearCountries := map[int]map[string]bool{}
	firstYear := map[string]int{}
	out := struct {
		Countries []StatsCountry `json:"countries"`
		Trips     []StatsTrip    `json:"trips"`
		Years     []StatsYear    `json:"years"`
		Totals    struct {
			Trips     int `json:"trips"`
			Days      int `json:"days"`
			Countries int `json:"countries"`
			Places    int `json:"places_visited"`
			Pending   int `json:"pending_lookups"` // located things whose country is still being looked up
		} `json:"totals"`
	}{Countries: []StatsCountry{}, Trips: []StatsTrip{}, Years: []StatsYear{}}
	for i := range trips {
		t := &trips[i]
		if t.Status == store.StatusCancelled || t.StartDate == "" || t.EndDate == "" {
			continue
		}
		stays, _ := s.store.ListLodging(ctx, t.ID)
		places, _ := s.store.ListPlaces(ctx, t.ID)
		cds := countryDays(t, stays, places)
		visited := 0
		for _, p := range places {
			if p.Status == "visited" {
				visited++
			}
			if p.Lat != nil && p.CountryCode == "" {
				out.Totals.Pending++
			}
		}
		for _, l := range stays {
			if l.Lat != nil && l.CountryCode == "" {
				out.Totals.Pending++
			}
		}
		upcoming := t.StartDate > today
		out.Trips = append(out.Trips, StatsTrip{ID: t.ID, Name: t.Name, Color: t.Color, Status: t.Status, StartDate: t.StartDate, EndDate: t.EndDate, Days: t.Days, Countries: cds, Places: len(places), Visited: visited, Upcoming: upcoming})
		if !upcoming {
			out.Totals.Trips++
			out.Totals.Places += visited
		}
		// the days of this trip that are behind us, per country and per year
		past := countryDays(clipTrip(t, today), stays, places)
		for _, cd := range cds {
			c := byCode[cd.Code]
			if c == nil {
				c = &StatsCountry{Code: cd.Code, Name: countryName(cd.Code), Trips: []int64{}}
				byCode[cd.Code] = c
			}
			c.Trips = append(c.Trips, t.ID)
			c.PlannedDays += cd.Days // minus the days already behind us, below
		}
		for _, cd := range past {
			c := byCode[cd.Code]
			if c == nil {
				continue
			}
			c.Days += cd.Days
			c.PlannedDays -= cd.Days
			if c.PlannedDays < 0 {
				c.PlannedDays = 0
			}
			if cd.Days > 0 && (c.First == "" || cd.First < c.First) {
				c.First = cd.First
			}
			last := t.EndDate
			if last > today {
				last = today
			}
			if cd.Days > 0 && last > c.Last {
				c.Last = last
			}
			if cd.Days > 0 {
				y, _ := strconv.Atoi(cd.First[:4])
				if fy, ok := firstYear[cd.Code]; !ok || y < fy {
					firstYear[cd.Code] = y
				}
			}
		}
		if upcoming {
			continue
		}
		// days away per calendar year
		start, _ := store.ParseDate(t.StartDate)
		end, _ := store.ParseDate(t.EndDate)
		if t.EndDate > today {
			end, _ = store.ParseDate(today)
		}
		seenYear := map[int]bool{}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			y := d.Year()
			ys := years[y]
			if ys == nil {
				ys = &StatsYear{Year: y, Countries: []string{}, New: []string{}}
				years[y] = ys
				yearCountries[y] = map[string]bool{}
			}
			ys.Days++
			out.Totals.Days++
			if !seenYear[y] {
				seenYear[y] = true
				ys.Trips++
			}
		}
		for _, cd := range past {
			if cd.Days > 0 {
				y, _ := strconv.Atoi(cd.First[:4])
				if yearCountries[y] != nil {
					yearCountries[y][cd.Code] = true
				}
			}
		}
	}
	for _, c := range byCode {
		out.Countries = append(out.Countries, *c)
		if c.Days > 0 {
			out.Totals.Countries++
		}
	}
	sort.Slice(out.Countries, func(i, j int) bool {
		if out.Countries[i].Days != out.Countries[j].Days {
			return out.Countries[i].Days > out.Countries[j].Days
		}
		return out.Countries[i].PlannedDays > out.Countries[j].PlannedDays
	})
	for y, ys := range years {
		for c := range yearCountries[y] {
			ys.Countries = append(ys.Countries, c)
			if firstYear[c] == y {
				ys.New = append(ys.New, c)
			}
		}
		sort.Strings(ys.Countries)
		sort.Strings(ys.New)
		out.Years = append(out.Years, *ys)
	}
	sort.Slice(out.Years, func(i, j int) bool { return out.Years[i].Year < out.Years[j].Year })
	sort.Slice(out.Trips, func(i, j int) bool { return out.Trips[i].StartDate < out.Trips[j].StartDate })
	writeJSON(w, http.StatusOK, out)
}

// clipTrip is the part of a trip up to today (none of it for a future trip).
func clipTrip(t *store.Trip, today string) *store.Trip {
	c := *t
	if c.StartDate > today {
		c.EndDate = "" // no days
	} else if c.EndDate > today {
		c.EndDate = today
	}
	return &c
}

// handleDetectCountries looks up countries now (Settings, the stats page) and
// says how many are still missing.
func (s *Server) handleDetectCountries(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	n := s.detectCountries(ctx)
	left, _ := s.store.MissingCountries(r.Context(), 10000)
	writeJSON(w, http.StatusOK, map[string]int{"detected": n, "missing": len(left)})
}
