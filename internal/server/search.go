package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/search"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// SearchHit is a place or stay inside a trip that matched the search.
type SearchHit struct {
	Kind      string `json:"kind"` // place | lodging
	ID        int64  `json:"id"`
	TripID    int64  `json:"trip_id"`
	TripName  string `json:"trip_name"`
	TripStart string `json:"trip_start"`
	TripEnd   string `json:"trip_end"`
	Name      string `json:"name"`
	Category  string `json:"category"` // place category or lodging type
	Address   string `json:"address"`
	Date      string `json:"date"`     // day of the place / check-in
	DateEnd   string `json:"date_end"` // check-out
	Status    string `json:"status"`
}

// SearchTrip is a trip that matched itself or through its places and stays.
type SearchTrip struct {
	store.Trip
	Direct bool        `json:"direct"` // the trip's own name/destination/dates matched
	Hits   []SearchHit `json:"hits"`   // places and stays that matched in it
}

// SearchResult is the answer of GET /api/v1/search.
type SearchResult struct {
	Query struct {
		Words   []string `json:"words"`
		Years   []int    `json:"years"`
		Months  []int    `json:"months"`
		Seasons []string `json:"seasons"`
	} `json:"query"`
	Trips  []SearchTrip `json:"trips"`
	Places []SearchHit  `json:"places"`
	Stays  []SearchHit  `json:"stays"`
}

const searchHitLimit = 60

// handleSearch finds trips, places and stays across everything the person can
// see: accents are ignored ("acores" finds "Açores"), and years, months and
// seasons narrow by date ("summer 2025", "july", "winter").
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	q := search.Parse(r.URL.Query().Get("q"))
	res, err := s.runSearch(r.Context(), u.ID, q)
	if err == nil && len(res.Trips) == 0 {
		if lit, ok := q.Literal(); ok {
			res, err = s.runSearch(r.Context(), u.ID, lit)
			q = lit
		}
	}
	if err != nil {
		s.fail(w, err, "search")
		return
	}
	res.Query.Words, res.Query.Years, res.Query.Seasons = q.Words, q.Years, q.Seasons
	for _, m := range q.Months {
		res.Query.Months = append(res.Query.Months, int(m))
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) runSearch(ctx context.Context, userID int64, q search.Query) (*SearchResult, error) {
	res := &SearchResult{Trips: []SearchTrip{}, Places: []SearchHit{}, Stays: []SearchHit{}}
	if q.Empty() {
		return res, nil
	}
	trips, err := s.store.ListTrips(ctx, s.visibleTrips(ctx, userID))
	if err != nil {
		return nil, err
	}
	byID := map[int64]*store.Trip{}
	tripText := map[int64]string{}
	ids := make([]int64, len(trips))
	for i := range trips {
		ids[i] = trips[i].ID
	}
	names, _ := s.store.TripPeopleNames(ctx, ids) // "ana" finds the trips with Ana
	for i := range trips {
		t := &trips[i]
		byID[t.ID] = t
		words := append([]string{t.Name, t.Destination}, s.tripCountryWords(ctx, t)...)
		tripText[t.ID] = search.Fold(strings.Join(append(words, names[t.ID]...), " "))
	}
	hitsByTrip := map[int64][]SearchHit{}
	// Places and stays only when there is text to look for: "2025" alone lists trips.
	if len(q.Words) > 0 {
		places, err := s.store.PlacesForUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, p := range places {
			t := byID[p.TripID]
			if t == nil {
				continue
			}
			if !itemMatches(q.Words, search.Fold(p.Name+" "+p.Address+" "+p.Tags), tripText[t.ID]) {
				continue
			}
			start, end := p.DayDate, p.DayDate
			if start == "" {
				start, end = t.StartDate, t.EndDate
			}
			if !q.MatchDates(start, end) {
				continue
			}
			h := SearchHit{Kind: "place", ID: p.ID, TripID: t.ID, TripName: t.Name, TripStart: t.StartDate, TripEnd: t.EndDate, Name: p.Name, Category: p.Category, Address: p.Address, Date: p.DayDate, Status: p.Status}
			hitsByTrip[t.ID] = append(hitsByTrip[t.ID], h)
			if len(res.Places) < searchHitLimit {
				res.Places = append(res.Places, h)
			}
		}
		stays, err := s.store.LodgingForUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, l := range stays {
			t := byID[l.TripID]
			if t == nil {
				continue
			}
			if !itemMatches(q.Words, search.Fold(l.Name+" "+l.Address), tripText[t.ID]) {
				continue
			}
			start, end := dateOnly(l.CheckIn), dateOnly(l.CheckOut)
			if start == "" {
				start, end = t.StartDate, t.EndDate
			}
			if !q.MatchDates(start, end) {
				continue
			}
			h := SearchHit{Kind: "lodging", ID: l.ID, TripID: t.ID, TripName: t.Name, TripStart: t.StartDate, TripEnd: t.EndDate, Name: l.Name, Category: l.Type, Address: l.Address, Date: start, DateEnd: end}
			hitsByTrip[t.ID] = append(hitsByTrip[t.ID], h)
			if len(res.Stays) < searchHitLimit {
				res.Stays = append(res.Stays, h)
			}
		}
	}
	for i := range trips {
		t := &trips[i]
		direct := allTrue(search.MatchText(q.Words, tripText[t.ID])) && q.MatchDates(t.StartDate, t.EndDate)
		hits := hitsByTrip[t.ID]
		if !direct && len(hits) == 0 {
			continue
		}
		t.Counts, _ = s.store.TripCounts(ctx, t.ID)
		if hits == nil {
			hits = []SearchHit{}
		}
		res.Trips = append(res.Trips, SearchTrip{Trip: *t, Direct: direct, Hits: hits})
	}
	// A place found only because its trip's name matched is noise in the
	// place list when the trip itself is already listed: keep places whose
	// own text carries at least one word (itemMatches guarantees it).
	return res, nil
}

// itemMatches: every word is in the item or in its trip, and at least one is
// in the item itself ("portugal azores" finds the Azores place of the
// Portugal trip; "portugal" alone finds the trip, not each of its places).
func itemMatches(words []string, item, trip string) bool {
	own := false
	for _, w := range words {
		in := strings.Contains(item, w)
		if in {
			own = true
		} else if !strings.Contains(trip, w) {
			return false
		}
	}
	return own
}

func allTrue(b []bool) bool {
	for _, x := range b {
		if !x {
			return false
		}
	}
	return true
}

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// SavedPlace is a place from any of the person's trips, offered when adding
// a place again (the same beach on day 2 and day 5, the café from last year).
type SavedPlace struct {
	store.Place
	TripName string `json:"trip_name"`
	Visits   int    `json:"visits"` // how many entries share this name and spot
	LastDate string `json:"last_date"`
}

// handleSavedPlaces lists the person's places across all their trips, one
// per name and spot (about 10 m), the current trip's first, then the most
// recently used; q filters by name or address, ignoring accents.
func (s *Server) handleSavedPlaces(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ctx := r.Context()
	q := search.Fold(strings.TrimSpace(r.URL.Query().Get("q")))
	current := int64(0)
	if v := r.URL.Query().Get("trip"); v != "" {
		fmt.Sscan(v, &current)
	}
	places, err := s.store.PlacesForUser(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "saved places")
		return
	}
	trips, err := s.store.ListTrips(ctx, store.TripFilter{UserID: u.ID})
	if err != nil {
		s.fail(w, err, "saved places")
		return
	}
	tripByID := map[int64]*store.Trip{}
	for i := range trips {
		tripByID[trips[i].ID] = &trips[i]
	}
	words := strings.Fields(q)
	byKey := map[string]*SavedPlace{}
	order := []string{}
	for _, p := range places {
		t := tripByID[p.TripID]
		if t == nil {
			continue
		}
		if len(words) > 0 && !allTrue(search.MatchText(words, search.Fold(p.Name+" "+p.Address))) {
			continue
		}
		key := search.Fold(p.Name)
		if p.Lat != nil && p.Lng != nil {
			key += fmt.Sprintf("@%.4f,%.4f", *p.Lat, *p.Lng)
		}
		when := p.DayDate
		if when == "" {
			when = t.StartDate
		}
		sp := byKey[key]
		if sp == nil {
			sp = &SavedPlace{Place: p, TripName: t.Name}
			sp.Photos = nil
			byKey[key] = sp
			order = append(order, key)
		}
		sp.Visits++
		// describe it by its latest entry, preferring the current trip's
		if when > sp.LastDate || (p.TripID == current && sp.TripID != current) {
			sp.LastDate = when
			visits := sp.Visits
			*sp = SavedPlace{Place: p, TripName: t.Name, Visits: visits, LastDate: when}
		}
	}
	out := make([]SavedPlace, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := out[i].TripID == current, out[j].TripID == current
		if ci != cj {
			return ci
		}
		return out[i].LastDate > out[j].LastDate
	})
	limit := 300 // the form filters these as you type
	if q != "" {
		limit = 40
	}
	if len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, out)
}
