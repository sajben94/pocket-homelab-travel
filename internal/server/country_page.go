package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/countries"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// editableFacts are the built-in facts a person may correct for themselves.
var editableFacts = map[string]bool{"capital": true, "population": true, "area": true, "languages": true, "currency": true, "calling_code": true, "drives": true, "tld": true, "timezone": true}

// CountryTrip is a trip that went through the country, with the days spent there.
type CountryTrip struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Color     string   `json:"color"`
	Status    string   `json:"status"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
	Days      int      `json:"days"`      // days in this country
	TripDays  int      `json:"trip_days"` // the whole trip
	Others    []string `json:"others"`    // the trip's other countries
	HasCover  bool     `json:"has_cover"`
	Upcoming  bool     `json:"upcoming"`
	UpdatedAt string   `json:"updated_at"`
}

// CountryPin is a located stay or place in the country, for its map.
type CountryPin struct {
	Kind     string  `json:"kind"` // place | lodging
	ID       int64   `json:"id"`
	TripID   int64   `json:"trip_id"`
	TripName string  `json:"trip_name"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Status   string  `json:"status"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

// handleCountries lists every country (for pickers and the index page).
func (s *Server) handleCountries(w http.ResponseWriter, r *http.Request) {
	type lite struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Region    string `json:"region"`
		Subregion string `json:"subregion"`
		Capital   string `json:"capital"`
	}
	all := countries.All()
	out := make([]lite, 0, len(all))
	for _, c := range all {
		capital := ""
		if len(c.Capital) > 0 {
			capital = c.Capital[0]
		}
		out = append(out, lite{c.Code, c.Name, c.Region, c.Subregion, capital})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCountry answers GET /api/v1/countries/{code}: the fact sheet, your
// corrections and notes, a Wikipedia summary, and your trips, stays and
// places there. ?brief=1 leaves out the trips, pins and Wikipedia (the trip
// overview's "where you're going" card).
func (s *Server) handleCountry(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ctx := r.Context()
	c := countries.Get(chi.URLParam(r, "code"))
	if c == nil {
		writeErr(w, http.StatusNotFound, "unknown country")
		return
	}
	note, err := s.store.GetCountryNote(ctx, u.ID, c.Code)
	if err != nil {
		s.fail(w, err, "country")
		return
	}
	out := map[string]any{"country": c, "note": note}
	if queryBool(r, "brief") {
		writeJSON(w, http.StatusOK, out)
		return
	}
	trips, pins, err := s.countryTravels(ctx, u.ID, c.Code)
	if err != nil {
		s.fail(w, err, "country")
		return
	}
	today := store.Today()
	var days, planned int
	first, last := "", ""
	for _, t := range trips {
		if t.Upcoming {
			planned += t.Days
			continue
		}
		days += t.Days
		if first == "" || t.StartDate < first {
			first = t.StartDate
		}
		end := t.EndDate
		if end > today {
			end = today
		}
		if end > last {
			last = end
		}
	}
	out["trips"], out["pins"] = trips, pins
	out["totals"] = map[string]any{"days": days, "planned_days": planned, "trips": len(trips), "first": first, "last": last}
	out["wiki"] = s.countryWiki(ctx, c)
	writeJSON(w, http.StatusOK, out)
}

// countryTravels finds the person's trips through a country, and their
// located stays and places there.
func (s *Server) countryTravels(ctx context.Context, userID int64, code string) ([]CountryTrip, []CountryPin, error) {
	trips, err := s.store.ListTrips(ctx, s.visibleTrips(ctx, userID))
	if err != nil {
		return nil, nil, err
	}
	today := store.Today()
	outT := []CountryTrip{}
	outP := []CountryPin{}
	for i := range trips {
		t := &trips[i]
		if t.Status == store.StatusCancelled {
			continue
		}
		stays, _ := s.store.ListLodging(ctx, t.ID)
		places, _ := s.store.ListPlaces(ctx, t.ID)
		cds := countryDays(t, stays, places)
		in, others := -1, []string{}
		for j, cd := range cds {
			if cd.Code == code {
				in = j
			} else if cd.Days > 0 {
				others = append(others, cd.Code)
			}
		}
		if in < 0 {
			continue
		}
		outT = append(outT, CountryTrip{ID: t.ID, Name: t.Name, Color: t.Color, Status: t.Status, StartDate: t.StartDate, EndDate: t.EndDate,
			Days: cds[in].Days, TripDays: t.Days, Others: others, HasCover: t.HasCover, Upcoming: t.StartDate > today, UpdatedAt: t.UpdatedAt})
		for _, l := range stays {
			if l.Lat != nil && l.CountryCode == code {
				outP = append(outP, CountryPin{Kind: "lodging", ID: l.ID, TripID: t.ID, TripName: t.Name, Name: l.Name, Category: l.Type, Lat: *l.Lat, Lng: *l.Lng})
			}
		}
		for _, p := range places {
			if p.Lat != nil && p.CountryCode == code {
				outP = append(outP, CountryPin{Kind: "place", ID: p.ID, TripID: t.ID, TripName: t.Name, Name: p.Name, Category: p.Category, Status: p.Status, Lat: *p.Lat, Lng: *p.Lng})
			}
		}
	}
	sort.Slice(outT, func(i, j int) bool { return outT[i].StartDate > outT[j].StartDate })
	return outT, outP, nil
}

// countryWiki is the Wikipedia summary of a country, cached for a month.
func (s *Server) countryWiki(ctx context.Context, c *countries.Country) any {
	key := "wiki-country:" + c.Code
	if v, at, ok := s.store.CacheGet(ctx, key); ok {
		if ts, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(ts) < 30*24*time.Hour {
			return json.RawMessage(v)
		}
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	w, err := s.geo.Wikipedia(wctx, c.Name, "en")
	if err != nil || w == nil {
		return nil
	}
	if b, err := json.Marshal(w); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	return w
}

// handlePutCountryNote saves the caller's corrections, own facts and notes on a country.
func (s *Server) handlePutCountryNote(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	c := countries.Get(chi.URLParam(r, "code"))
	if c == nil {
		writeErr(w, http.StatusNotFound, "unknown country")
		return
	}
	var in store.CountryNote
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n := &store.CountryNote{Code: c.Code, Facts: map[string]string{}, Extra: []store.CountryFact{}, Notes: strings.TrimSpace(in.Notes)}
	for k, v := range in.Facts {
		if !editableFacts[k] {
			writeErr(w, http.StatusBadRequest, "unknown fact: "+k)
			return
		}
		if v = strings.TrimSpace(v); v != "" {
			n.Facts[k] = v
		}
	}
	for _, f := range in.Extra {
		f.Label, f.Value = strings.TrimSpace(f.Label), strings.TrimSpace(f.Value)
		if f.Label != "" && f.Value != "" {
			n.Extra = append(n.Extra, f)
		}
	}
	if len(n.Notes) > 20000 || len(n.Extra) > 50 {
		writeErr(w, http.StatusBadRequest, "too long")
		return
	}
	if err := s.store.SaveCountryNote(r.Context(), u.ID, n); err != nil {
		s.fail(w, err, "country note")
		return
	}
	writeJSON(w, http.StatusOK, n)
}
