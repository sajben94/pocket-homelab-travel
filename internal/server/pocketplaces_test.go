package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/config"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// placesHarness: Travel with a fake Pocket Places and a fake Pocket Core
// whose Travel → Places link state the test sets.
type placesHarness struct {
	t      *testing.T
	ts     *httptest.Server
	srv    *Server
	tok    string
	tripID int64

	mu         sync.Mutex
	link       string // on | off_admin | …
	slow       time.Duration
	auth       []string     // Authorization headers Places saw
	queries    []url.Values // trip-guide queries Places saw
	tokens     int
	registered core.Manifest
}

func newPlacesHarness(t *testing.T, withCore bool) *placesHarness {
	t.Helper()
	h := &placesHarness{t: t, link: "on"}
	pl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.auth = append(h.auth, r.Header.Get("Authorization"))
		slow := h.slow
		if r.URL.Path == "/api/v1/trip-guide" {
			h.queries = append(h.queries, r.URL.Query())
		}
		h.mu.Unlock()
		if slow > 0 {
			select {
			case <-time.After(slow):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/trip-guide":
			_, _ = w.Write([]byte(`{"trip_id":` + r.URL.Query().Get("trip_id") + `,
				"lists":[{"id":7,"name":"Lisbon eats","emoji":"🍤","count":3,"been":1}],
				"want":[{"id":11,"name":"Taberna da Rua","kind":"restaurant","cuisine":"Portuguese","city":"Lisbon","status":"want","rating":0,"price":2,"lat":38.71,"lng":-9.14}],
				"been":[{"id":12,"name":"Café Lua","kind":"cafe","city":"Lisbon","status":"been","favourite":true,"rating":4.5,"price":1}],
				"visits":[{"id":30,"place_id":12,"date":"2026-05-02","time":"09:30","rating":4,"with_whom":"Ana","spent":1250,"currency":"EUR","place_name":"Café Lua","place_kind":"cafe"}],
				"spent":[{"currency":"EUR","total":1250,"visits":1}],
				"url":"https://places.example/places?trip=1"}`))
		case "/api/v1/places":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(pl.Close)
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/apps/register":
			_ = json.NewDecoder(r.Body).Decode(&h.registered)
			_, _ = w.Write([]byte(`{"app":{}}`))
		case "/api/v1/links/status":
			if r.URL.Query().Get("app") != "places" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"app": "places", "scope": "places:read", "state": h.link, "on": h.link == "on"}})
		case "/api/v1/links/token":
			var in struct {
				App    string
				Scopes []string
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.App != "places" || h.link != "on" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "link off", "reason": h.link})
				return
			}
			if strings.Join(in.Scopes, " ") != "places:read" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "scopes", "reason": "not_declared"})
				return
			}
			h.tokens++
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_pl", "expires_in": 300, "app": "places", "url": pl.URL, "public_url": "https://places.example"})
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(coreSrv.Close)
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, DBPath: dir + "/t.db", FilesDir: dir + "/files", AuthMode: "local", DefaultCurrency: "EUR", FXProvider: "none",
		SessionTTL: time.Hour, MaxUploadBytes: 10 << 20, UserAgent: "test"}
	if withCore {
		cfg.CoreURL, cfg.CoreRegistrationKey = coreSrv.URL, "pcr_test"
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h.srv = New(cfg, st, log, nil)
	h.ts = httptest.NewServer(h.srv.Router())
	t.Cleanup(h.ts.Close)
	ctx := context.Background()
	u := &store.User{Email: "owner@example.com", Name: "owner", Role: "admin"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if h.tok, _, err = st.CreateAPIToken(ctx, u.ID, "test"); err != nil {
		t.Fatal(err)
	}
	bctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := h.srv.Bootstrap(bctx); err != nil {
		t.Fatal(err)
	}
	code, body := h.do("POST", "/api/v1/trips", map[string]any{"name": "Lisbon", "destination": "Lisbon, Portugal", "lat": 38.7223, "lng": -9.1393,
		"start_date": "2026-05-01", "end_date": "2026-05-05", "timezone": "UTC"})
	if code != 201 && code != 200 {
		t.Fatalf("trip %d %s", code, body)
	}
	var tr struct{ ID int64 }
	_ = json.Unmarshal([]byte(body), &tr)
	h.tripID = tr.ID
	return h
}

func (h *placesHarness) do(method, path string, body any) (int, string) {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+h.tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (h *placesHarness) card() placesCard {
	h.t.Helper()
	code, body := h.do("GET", "/api/v1/trips/"+strconv.FormatInt(h.tripID, 10)+"/pocket-places", nil)
	if code != 200 {
		h.t.Fatalf("pocket places %d %s", code, body)
	}
	var c placesCard
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		h.t.Fatalf("pocket places body %s: %v", body, err)
	}
	return c
}

func (h *placesHarness) settings() map[string]any {
	h.t.Helper()
	code, body := h.do("GET", "/api/v1/settings", nil)
	if code != 200 {
		h.t.Fatalf("settings %d %s", code, body)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	return m
}

func (h *placesHarness) waitRegistered() {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !h.srv.coreReady() {
		if time.Now().After(deadline) {
			h.t.Fatal("Travel did not register with Core")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// With Core and the link on: the trip's places come from the person's
// Pocket Places with a link token, for the trip's place and dates, and the
// links open Places' public address.
func TestPocketPlacesTripGuide(t *testing.T) {
	h := newPlacesHarness(t, true)
	h.waitRegistered()

	h.mu.Lock()
	uses := h.registered.Uses
	h.mu.Unlock()
	declared := false
	for _, u := range uses {
		if u.App == "places" && strings.Join(u.Scopes, " ") == "places:read" && u.Reason != "" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("manifest does not use places:read: %+v", uses)
	}

	c := h.card()
	if c.Status != "ok" || c.URL != "https://places.example" || c.TripURL != "https://places.example/places?label=Lisbon&near=38.72230%2C-9.13930&radius_km=40&sort=near" {
		t.Fatalf("card %+v", c)
	}
	if len(c.Lists) != 1 || c.Lists[0].Name != "Lisbon eats" || len(c.Want) != 1 || c.Want[0].Price != 2 || len(c.Been) != 1 || !c.Been[0].Favourite {
		t.Fatalf("places %+v", c)
	}
	if len(c.Visits) != 1 || c.Visits[0].Spent != 1250 || c.Visits[0].PlaceName != "Café Lua" || len(c.Spent) != 1 || c.Spent[0].Total != 1250 {
		t.Fatalf("visits %+v", c)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queries) != 1 {
		t.Fatalf("trip-guide calls %d", len(h.queries))
	}
	q := h.queries[0]
	if q.Get("trip_id") != strconv.FormatInt(h.tripID, 10) || q.Get("destination") != "Lisbon, Portugal" || q.Get("from") != "2026-05-01" || q.Get("to") != "2026-05-05" ||
		!strings.HasPrefix(q.Get("lat"), "38.722") || !strings.HasPrefix(q.Get("lng"), "-9.139") {
		t.Fatalf("query %v", q)
	}
	for _, a := range h.auth {
		if a != "Bearer pcx_pl" {
			t.Fatalf("Places saw %q, want the link token", a)
		}
	}
}

// Core refusing the link: the card says why (and nothing reaches Places);
// Settings shows the same.
func TestPocketPlacesLinkOff(t *testing.T) {
	h := newPlacesHarness(t, true)
	h.waitRegistered()
	h.mu.Lock()
	h.link = "off_admin"
	h.mu.Unlock()
	c := h.card()
	if c.Status != "unavailable" || c.Reason != "off_admin" || !strings.Contains(c.Detail, "Travel → Pocket Places link is switched off") {
		t.Fatalf("card %+v", c)
	}
	if c.Want == nil || c.Visits == nil {
		t.Fatal("lists should be empty, not null")
	}
	st := h.settings()
	if st["places_mode"] != "core" || st["places_via"] != "" || !strings.Contains(st["places_detail"].(string), "switched off in Pocket Core") {
		t.Fatalf("settings %v", st)
	}
	h.mu.Lock()
	h.link = "on"
	h.mu.Unlock()
	if st := h.settings(); st["places_via"] != "core" {
		t.Fatalf("after on %v", st)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.auth) != 0 {
		t.Fatal("Places reached while the link was off")
	}
}

// Switched off for oneself: no token is asked for; only core and off are choices.
func TestPocketPlacesOff(t *testing.T) {
	h := newPlacesHarness(t, true)
	h.waitRegistered()
	if code, body := h.do("PUT", "/api/v1/settings", map[string]any{"places_mode": "off"}); code != 200 {
		t.Fatalf("off %d %s", code, body)
	}
	if c := h.card(); c.Status != "off" {
		t.Fatalf("card %+v", c)
	}
	if code, _ := h.do("PUT", "/api/v1/settings", map[string]any{"places_mode": "direct"}); code != 400 {
		t.Fatalf("direct accepted: %d", code)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tokens != 0 {
		t.Fatalf("asked Core for %d tokens while off", h.tokens)
	}
}

// A slow Places never holds the trip page: the card gives up and says so.
func TestPocketPlacesSlow(t *testing.T) {
	old := placesTimeout
	placesTimeout = 200 * time.Millisecond
	defer func() { placesTimeout = old }()
	h := newPlacesHarness(t, true)
	h.waitRegistered()
	h.mu.Lock()
	h.slow = 3 * time.Second
	h.mu.Unlock()
	start := time.Now()
	c := h.card()
	if c.Status != "error" || !strings.Contains(c.Detail, "too long") {
		t.Fatalf("card %+v", c)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("waited %s for a slow Places", time.Since(start))
	}
	// the overview does not wait for Places at all
	h.mu.Lock()
	n := len(h.queries)
	h.mu.Unlock()
	if code, _ := h.do("GET", "/api/v1/trips/"+strconv.FormatInt(h.tripID, 10)+"/overview", nil); code != 200 {
		t.Fatalf("overview %d", code)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queries) != n {
		t.Fatal("the overview called Pocket Places")
	}
}

// Without Core: the card says it needs Pocket Core; nothing breaks.
func TestPocketPlacesWithoutCore(t *testing.T) {
	h := newPlacesHarness(t, false)
	if c := h.card(); c.Status != "no_core" {
		t.Fatalf("card %+v", c)
	}
	st := h.settings()
	if st["places_mode"] != "core" || st["places_via"] != "" || st["core_ready"] != false {
		t.Fatalf("settings %v", st)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.auth) != 0 || h.tokens != 0 {
		t.Fatal("Places reached without Core")
	}
}
