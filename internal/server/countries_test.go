package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

func TestCountryDays(t *testing.T) {
	trip := &store.Trip{StartDate: "2025-07-01", EndDate: "2025-07-07", CountryCode: "PT"}
	stays := []store.Lodging{
		{CheckIn: "2025-07-01T15:00", CheckOut: "2025-07-04T11:00", CountryCode: "PT"},
		{CheckIn: "2025-07-04", CheckOut: "2025-07-07", CountryCode: "ES"},
	}
	places := []store.Place{{DayDate: "2025-07-06", CountryCode: "FR"}, {DayDate: "", CountryCode: "IT"}}
	got := countryDays(trip, stays, places)
	want := map[string]int{"PT": 4, "ES": 4, "FR": 1} // 4 Jul counts for both PT (check-out) and ES (check-in)
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, c := range got {
		if want[c.Code] != c.Days {
			t.Fatalf("%s: %d days, want %d (%+v)", c.Code, c.Days, want[c.Code], got)
		}
	}
	if got[0].Code != "PT" || got[0].First != "2025-07-01" || got[1].Code != "ES" {
		t.Fatalf("order %+v", got)
	}

	// nothing located: every day goes to the destination's country
	if got := countryDays(&store.Trip{StartDate: "2025-01-01", EndDate: "2025-01-03", CountryCode: "AT"}, nil, nil); len(got) != 1 || got[0].Days != 3 {
		t.Fatalf("destination only: %+v", got)
	}
	// no destination country: the days before the first stay join its country
	if got := countryDays(&store.Trip{StartDate: "2025-01-01", EndDate: "2025-01-05"}, []store.Lodging{{CheckIn: "2025-01-03", CheckOut: "2025-01-05", CountryCode: "HR"}}, nil); len(got) != 1 || got[0].Days != 5 || got[0].First != "2025-01-01" {
		t.Fatalf("leading days: %+v", got)
	}
	// a manual list keeps only its countries and adds the ones with no data
	manual := &store.Trip{StartDate: "2025-07-01", EndDate: "2025-07-07", CountriesManual: []string{"PT", "MA"}}
	got = countryDays(manual, stays, places)
	if len(got) != 2 || got[0].Code != "PT" || got[1].Code != "MA" || got[1].Days != 0 {
		t.Fatalf("manual: %+v", got)
	}
	// unknown and "no country" codes are ignored
	if got := countryDays(&store.Trip{StartDate: "2025-01-01", EndDate: "2025-01-01"}, []store.Lodging{{CheckIn: "2025-01-01", CheckOut: "2025-01-02", CountryCode: noCountry}}, nil); len(got) != 0 {
		t.Fatalf("no country: %+v", got)
	}
}

func TestCountriesDetectedAndStats(t *testing.T) {
	h := newIsoHarness(t)
	calls := 0
	h.srv.countryLookup = func(ctx context.Context, lat, lng float64) (string, error) {
		calls++
		if lng < -20 {
			return "pt", nil
		}
		return "es", nil
	}
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "Iberia 2024", "start_date": "2024-07-01", "end_date": "2024-07-06", "currency": "EUR"})
	h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/lodging", tr), map[string]any{"name": "Azores", "lat": 37.7, "lng": -25.6, "check_in": "2024-07-01", "check_out": "2024-07-04"})
	h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/lodging", tr), map[string]any{"name": "Sevilla", "lat": 37.38, "lng": -5.98, "check_in": "2024-07-04", "check_out": "2024-07-06"})
	if n := h.srv.detectCountries(context.Background()); n != 2 {
		t.Fatalf("detected %d", n)
	}
	// cached per spot: a second stay at the same place needs no lookup
	h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/places", tr), map[string]any{"name": "Sete Cidades", "lat": 37.7, "lng": -25.6, "day_date": "2024-07-02"})
	before := calls
	h.srv.detectCountries(context.Background())
	if calls != before {
		t.Fatalf("cache missed: %d calls", calls-before)
	}

	var ov struct {
		Countries []CountryDays `json:"countries"`
	}
	_, b := h.do(h.ownerTok, http.MethodGet, fmt.Sprintf("/api/v1/trips/%d/overview", tr), nil)
	_ = json.Unmarshal(b, &ov)
	if len(ov.Countries) != 2 || ov.Countries[0].Code != "PT" || ov.Countries[0].Days != 4 || ov.Countries[1].Days != 3 {
		t.Fatalf("overview countries = %+v", ov.Countries)
	}
	// the trip is now found by its countries' names
	_, b = h.do(h.ownerTok, http.MethodGet, "/api/v1/search?q=spain", nil)
	var sr struct {
		Trips []struct{ ID int64 } `json:"trips"`
	}
	_ = json.Unmarshal(b, &sr)
	if len(sr.Trips) != 1 {
		t.Fatalf("search spain = %s", b)
	}

	var st struct {
		Countries []StatsCountry `json:"countries"`
		Years     []StatsYear    `json:"years"`
		Totals    struct {
			Days      int `json:"days"`
			Countries int `json:"countries"`
		} `json:"totals"`
	}
	_, b = h.do(h.ownerTok, http.MethodGet, "/api/v1/stats", nil)
	_ = json.Unmarshal(b, &st)
	if st.Totals.Countries != 2 || st.Totals.Days != 6 || len(st.Years) != 1 || st.Years[0].Year != 2024 || len(st.Years[0].New) != 2 {
		t.Fatalf("stats = %s", b)
	}
	if st.Countries[0].Name != "Portugal" || st.Countries[0].Days != 4 {
		t.Fatalf("countries = %+v", st.Countries)
	}

	// manual override
	if code, b := h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("/api/v1/trips/%d", tr), map[string]any{"countries_manual": []string{"pt"}}); code != http.StatusOK {
		t.Fatalf("put: %d %s", code, b)
	}
	_, b = h.do(h.ownerTok, http.MethodGet, fmt.Sprintf("/api/v1/trips/%d/overview", tr), nil)
	ov.Countries = nil
	_ = json.Unmarshal(b, &ov)
	if len(ov.Countries) != 1 || ov.Countries[0].Code != "PT" {
		t.Fatalf("manual = %+v", ov.Countries)
	}
	if code, _ := h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("/api/v1/trips/%d", tr), map[string]any{"countries_manual": []string{"Portugal"}}); code != http.StatusBadRequest {
		t.Fatalf("bad code accepted: %d", code)
	}
	// moving a stay forgets its country
	_, b = h.do(h.ownerTok, http.MethodGet, fmt.Sprintf("/api/v1/trips/%d/lodging", tr), nil)
	var ls []store.Lodging
	_ = json.Unmarshal(b, &ls)
	h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("/api/v1/trips/%d/lodging/%d", tr, ls[1].ID), map[string]any{"lat": 41.39, "lng": 2.17})
	_, b = h.do(h.ownerTok, http.MethodGet, fmt.Sprintf("/api/v1/trips/%d/lodging", tr), nil)
	_ = json.Unmarshal(b, &ls)
	if ls[1].CountryCode != "" || ls[0].CountryCode != "PT" {
		t.Fatalf("after move: %+v", ls)
	}
}

func TestCountryPage(t *testing.T) {
	h := newIsoHarness(t)
	h.srv.countryLookup = func(ctx context.Context, lat, lng float64) (string, error) { return "BA", nil }
	h.srv.geo.HTTP = &http.Client{Transport: offline{}} // no Wikipedia from tests
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "Bosna Road Trip", "start_date": "2025-07-28", "end_date": "2025-07-31", "currency": "EUR"})
	h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/lodging", tr), map[string]any{"name": "Mostar", "lat": 43.34, "lng": 17.81, "check_in": "2025-07-28", "check_out": "2025-07-31"})
	h.srv.detectCountries(context.Background())

	var out struct {
		Country struct {
			Name    string   `json:"name"`
			Capital []string `json:"capital"`
		} `json:"country"`
		Trips []CountryTrip `json:"trips"`
		Pins  []CountryPin  `json:"pins"`
		Note  store.CountryNote
	}
	code, b := h.do(h.ownerTok, http.MethodGet, "/api/v1/countries/ba", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, b)
	}
	_ = json.Unmarshal(b, &out)
	if out.Country.Name != "Bosnia and Herzegovina" || len(out.Trips) != 1 || out.Trips[0].Days != 4 || len(out.Pins) != 1 {
		t.Fatalf("country = %s", b)
	}
	// someone else sees the facts, not the trips
	_, b = h.do(h.memberTok, http.MethodGet, "/api/v1/countries/BA", nil)
	out.Trips = nil
	_ = json.Unmarshal(b, &out)
	if len(out.Trips) != 0 {
		t.Fatalf("member sees %s", b)
	}
	// notes are per person
	note := map[string]any{"facts": map[string]string{"capital": "Sarajevo (city of 1000 minarets)"}, "extra": []map[string]string{{"label": "Emergency", "value": "112"}, {"label": "", "value": "x"}}, "notes": "Ćevapi in Baščaršija"}
	if code, b := h.do(h.ownerTok, http.MethodPut, "/api/v1/countries/BA/note", note); code != http.StatusOK {
		t.Fatalf("put note: %d %s", code, b)
	}
	if code, _ := h.do(h.ownerTok, http.MethodPut, "/api/v1/countries/BA/note", map[string]any{"facts": map[string]string{"president": "x"}}); code != http.StatusBadRequest {
		t.Fatalf("unknown fact accepted: %d", code)
	}
	_, b = h.do(h.ownerTok, http.MethodGet, "/api/v1/countries/BA?brief=1", nil)
	var brief struct {
		Note  store.CountryNote `json:"note"`
		Trips any               `json:"trips"`
	}
	_ = json.Unmarshal(b, &brief)
	if brief.Note.Facts["capital"] == "" || len(brief.Note.Extra) != 1 || brief.Note.Notes == "" || brief.Trips != nil {
		t.Fatalf("brief = %s", b)
	}
	_, b = h.do(h.memberTok, http.MethodGet, "/api/v1/countries/BA?brief=1", nil)
	brief.Note = store.CountryNote{}
	_ = json.Unmarshal(b, &brief)
	if brief.Note.Notes != "" {
		t.Fatalf("member sees owner's note: %s", b)
	}
	if code, _ := h.do(h.ownerTok, http.MethodGet, "/api/v1/countries/ZZ", nil); code != http.StatusNotFound {
		t.Fatalf("ZZ: %d", code)
	}
	// trip lists carry their countries
	_, b = h.do(h.ownerTok, http.MethodGet, "/api/v1/trips", nil)
	var trips []struct {
		Countries []CountryDays `json:"countries"`
	}
	_ = json.Unmarshal(b, &trips)
	if len(trips) != 1 || len(trips[0].Countries) != 1 || trips[0].Countries[0].Code != "BA" {
		t.Fatalf("trips = %s", b)
	}
}

// offline fails every outbound request.
type offline struct{}

func (offline) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }
