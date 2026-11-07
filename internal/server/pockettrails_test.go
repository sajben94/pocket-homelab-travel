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

// trailsHarness: Travel with a fake Pocket Trails and a fake Pocket Core
// whose Travel → Trails link state the test sets.
type trailsHarness struct {
	t      *testing.T
	ts     *httptest.Server
	srv    *Server
	tok    string
	tripID int64

	mu         sync.Mutex
	link       string // on | off_admin | …
	slow       time.Duration
	auth       []string     // Authorization headers Trails saw
	queries    []url.Values // /outings queries Trails saw
	tokens     int
	registered core.Manifest
}

func newTrailsHarness(t *testing.T, withCore bool) *trailsHarness {
	t.Helper()
	h := &trailsHarness{t: t, link: "on"}
	tr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.auth = append(h.auth, r.Header.Get("Authorization"))
		slow := h.slow
		if r.URL.Path == "/api/v1/outings" {
			h.queries = append(h.queries, r.URL.Query())
		}
		trip := strconv.FormatInt(h.tripID, 10)
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
		case "/api/v1/summary":
			_, _ = w.Write([]byte(`{"out_now":null,"next":null}`))
			return
		case "/api/v1/outings":
		default:
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		switch {
		case q.Get("trip_ref") != "":
			// filed with the trip: a hike already done, and one planned
			// for the day after the trip ends (still the trip's)
			_, _ = w.Write([]byte(`[
				{"id":1,"title":"Sintra hills","activity":"hike","date":"2026-05-02","start_time":"09:00","status":"done","route_name":"Sintra loop","distance_m":12400,"gain_m":610,"minutes":250,"trip_ref":"` + q.Get("trip_ref") + `","trip_name":"Lisbon"},
				{"id":2,"title":"Coast walk","activity":"hike","date":"2026-05-06","status":"planned","distance_m":8000,"gain_m":120,"trip_ref":"` + q.Get("trip_ref") + `","trip_name":"Lisbon"}]`))
		case q.Get("from") != "":
			// during the trip: the hike again, a ride not filed anywhere
			// and one filed with another trip
			_, _ = w.Write([]byte(`[
				{"id":1,"title":"Sintra hills","activity":"hike","date":"2026-05-02","start_time":"09:00","status":"done","trip_ref":"` + trip + `"},
				{"id":3,"title":"Tagus ride","activity":"bike","date":"2026-05-01","start_time":"17:30","status":"planned","distance_m":30000,"gain_m":90,"trip_ref":""},
				{"id":4,"title":"Elsewhere","activity":"hike","date":"2026-05-03","status":"planned","trip_ref":"999"}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(tr.Close)
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/apps/register":
			_ = json.NewDecoder(r.Body).Decode(&h.registered)
			_, _ = w.Write([]byte(`{"app":{}}`))
		case "/api/v1/links/status":
			if r.URL.Query().Get("app") != "trails" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"app": "trails", "scope": "trails:read", "state": h.link, "on": h.link == "on"}})
		case "/api/v1/links/token":
			var in struct {
				App    string
				Scopes []string
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.App != "trails" || h.link != "on" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "link off", "reason": h.link})
				return
			}
			if strings.Join(in.Scopes, " ") != "trails:read" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "scopes", "reason": "not_declared"})
				return
			}
			h.tokens++
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_tr", "expires_in": 300, "app": "trails", "url": tr.URL, "public_url": "https://trails.example/"})
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
	var trip struct{ ID int64 }
	_ = json.Unmarshal([]byte(body), &trip)
	h.mu.Lock()
	h.tripID = trip.ID
	h.mu.Unlock()
	return h
}

func (h *trailsHarness) do(method, path string, body any) (int, string) {
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

func (h *trailsHarness) card() trailsCard {
	h.t.Helper()
	code, body := h.do("GET", "/api/v1/trips/"+strconv.FormatInt(h.tripID, 10)+"/pocket-trails", nil)
	if code != 200 {
		h.t.Fatalf("pocket trails %d %s", code, body)
	}
	var c trailsCard
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		h.t.Fatalf("pocket trails body %s: %v", body, err)
	}
	return c
}

func (h *trailsHarness) settings() map[string]any {
	h.t.Helper()
	code, body := h.do("GET", "/api/v1/settings", nil)
	if code != 200 {
		h.t.Fatalf("settings %d %s", code, body)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	return m
}

func (h *trailsHarness) waitRegistered() {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !h.srv.coreReady() {
		if time.Now().After(deadline) {
			h.t.Fatal("Travel did not register with Core")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// With Core and the link on: the outings filed with the trip and the ones
// during it (not filed with another trip), each once, by date and time,
// read with a link token; the card says where Trails lives.
func TestPocketTrailsTripOutings(t *testing.T) {
	h := newTrailsHarness(t, true)
	h.waitRegistered()

	h.mu.Lock()
	uses := h.registered.Uses
	h.mu.Unlock()
	declared := false
	for _, u := range uses {
		if u.App == "trails" && strings.Join(u.Scopes, " ") == "trails:read" && u.Reason != "" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("manifest does not use trails:read: %+v", uses)
	}

	c := h.card()
	if c.Status != "ok" || c.URL != "https://trails.example" {
		t.Fatalf("card %+v", c)
	}
	var ids []int64
	for _, o := range c.Outings {
		ids = append(ids, o.ID)
	}
	if len(ids) != 3 || ids[0] != 3 || ids[1] != 1 || ids[2] != 2 {
		t.Fatalf("outings %v, want 3 1 2", ids)
	}
	ride, hike, walk := c.Outings[0], c.Outings[1], c.Outings[2]
	if ride.Activity != "bike" || ride.DistanceM != 30000 || ride.Status != "planned" || ride.StartTime != "17:30" {
		t.Fatalf("ride %+v", ride)
	}
	if hike.Status != "done" || hike.GainM != 610 || hike.RouteName != "Sintra loop" || walk.Date != "2026-05-06" {
		t.Fatalf("hike %+v walk %+v", hike, walk)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queries) != 2 {
		t.Fatalf("outings calls %d", len(h.queries))
	}
	if h.queries[0].Get("trip_ref") != strconv.FormatInt(h.tripID, 10) || h.queries[1].Get("from") != "2026-05-01" || h.queries[1].Get("to") != "2026-05-05" {
		t.Fatalf("queries %v", h.queries)
	}
	for _, a := range h.auth {
		if a != "Bearer pcx_tr" {
			t.Fatalf("Trails saw %q, want the link token", a)
		}
	}
}

// Core refusing the link (it arrives switched off): the card says why and
// nothing reaches Trails; Settings shows the same; Test says so too.
func TestPocketTrailsLinkOff(t *testing.T) {
	h := newTrailsHarness(t, true)
	h.waitRegistered()
	h.mu.Lock()
	h.link = "off_admin"
	h.mu.Unlock()
	c := h.card()
	if c.Status != "unavailable" || c.Reason != "off_admin" || !strings.Contains(c.Detail, "Travel → Pocket Trails link is switched off") {
		t.Fatalf("card %+v", c)
	}
	if c.Outings == nil {
		t.Fatal("outings should be empty, not null")
	}
	st := h.settings()
	if st["trails_mode"] != "core" || st["trails_via"] != "" || !strings.Contains(st["trails_detail"].(string), "switched off in Pocket Core") {
		t.Fatalf("settings %v", st)
	}
	if _, body := h.do("POST", "/api/v1/integrations/test", map[string]any{"which": "trails"}); !strings.Contains(body, "switched off in Pocket Core") {
		t.Fatalf("test while off: %s", body)
	}
	h.mu.Lock()
	h.link = "on"
	h.mu.Unlock()
	if st := h.settings(); st["trails_via"] != "core" {
		t.Fatalf("after on %v", st)
	}
	h.mu.Lock()
	if len(h.auth) != 0 {
		h.mu.Unlock()
		t.Fatal("Trails reached while the link was off")
	}
	h.mu.Unlock()
	if _, body := h.do("POST", "/api/v1/integrations/test", map[string]any{"which": "trails"}); !strings.Contains(body, `"ok":true`) || !strings.Contains(body, "Pocket Trails reachable") {
		t.Fatalf("test while on: %s", body)
	}
}

// Switched off for oneself: no token is asked for; only core and off are choices.
func TestPocketTrailsOff(t *testing.T) {
	h := newTrailsHarness(t, true)
	h.waitRegistered()
	if code, body := h.do("PUT", "/api/v1/settings", map[string]any{"trails_mode": "off"}); code != 200 {
		t.Fatalf("off %d %s", code, body)
	}
	if c := h.card(); c.Status != "off" {
		t.Fatalf("card %+v", c)
	}
	if st := h.settings(); st["trails_mode"] != "off" {
		t.Fatalf("settings %v", st)
	}
	if code, _ := h.do("PUT", "/api/v1/settings", map[string]any{"trails_mode": "direct"}); code != 400 {
		t.Fatalf("direct accepted: %d", code)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tokens != 0 {
		t.Fatalf("asked Core for %d tokens while off", h.tokens)
	}
}

// A slow Trails never holds the trip page: the card gives up and says so.
func TestPocketTrailsSlow(t *testing.T) {
	old := trailsTimeout
	trailsTimeout = 200 * time.Millisecond
	defer func() { trailsTimeout = old }()
	h := newTrailsHarness(t, true)
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
		t.Fatalf("waited %s for a slow Trails", time.Since(start))
	}
}

// Without Core: the card says it needs Pocket Core; nothing breaks.
func TestPocketTrailsWithoutCore(t *testing.T) {
	h := newTrailsHarness(t, false)
	if c := h.card(); c.Status != "no_core" {
		t.Fatalf("card %+v", c)
	}
	st := h.settings()
	if st["trails_mode"] != "core" || st["trails_via"] != "" || st["core_ready"] != false {
		t.Fatalf("settings %v", st)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.auth) != 0 || h.tokens != 0 {
		t.Fatal("Trails reached without Core")
	}
}
