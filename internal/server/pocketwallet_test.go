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

// walletHarness: Travel with a fake Pocket Wallet and a fake Pocket Core
// whose Travel → Wallet link state the test sets.
type walletHarness struct {
	t      *testing.T
	ts     *httptest.Server
	srv    *Server
	tok    string
	tripID int64

	mu         sync.Mutex
	link       string // on | off_admin | …
	slow       time.Duration
	auth       []string     // Authorization headers Wallet saw
	queries    []url.Values // /passes queries Wallet saw
	tokens     int
	registered core.Manifest
}

func newWalletHarness(t *testing.T, withCore bool) *walletHarness {
	t.Helper()
	h := &walletHarness{t: t, link: "on"}
	wl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.auth = append(h.auth, r.Header.Get("Authorization"))
		slow := h.slow
		if r.URL.Path == "/api/v1/passes" {
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
		if r.URL.Path != "/api/v1/passes" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		switch {
		case q.Get("trip_id") != "":
			// filed with the trip: the flight out, and the museum ticket (archived)
			_, _ = w.Write([]byte(`[
				{"id":1,"kind":"boarding","name":"Sarajevo → Lisbon","issuer":"TAP","starts_at":"2026-05-01T06:40","venue":"SJJ","destination":"LIS","seat":"14C","gate":"A3","ref":"TP1234","code_format":"pdf417","trip_id":` + q.Get("trip_id") + `,"archived":false},
				{"id":2,"kind":"ticket","name":"Gulbenkian Museum","starts_at":"2026-05-03T19:00","venue":"Gulbenkian","code_format":"qr","trip_id":` + q.Get("trip_id") + `,"archived":true}]`))
		case q.Get("from") != "":
			// starting during the trip: the museum again, a tram ticket not
			// filed anywhere, and a concert filed with another trip
			_, _ = w.Write([]byte(`[
				{"id":2,"kind":"ticket","name":"Gulbenkian Museum","starts_at":"2026-05-03T19:00","code_format":"qr","trip_id":` + trip + `,"archived":true},
				{"id":3,"kind":"ticket","name":"Tram 28","starts_at":"2026-05-02T10:00","code_format":"","trip_id":null},
				{"id":4,"kind":"ticket","name":"Concert","starts_at":"2026-05-02T21:00","code_format":"qr","trip_id":999}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(wl.Close)
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/apps/register":
			_ = json.NewDecoder(r.Body).Decode(&h.registered)
			_, _ = w.Write([]byte(`{"app":{}}`))
		case "/api/v1/links/status":
			if r.URL.Query().Get("app") != "wallet" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"app": "wallet", "scope": "wallet:read", "state": h.link, "on": h.link == "on"}})
		case "/api/v1/links/token":
			var in struct {
				App    string
				Scopes []string
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.App != "wallet" || h.link != "on" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "link off", "reason": h.link})
				return
			}
			if strings.Join(in.Scopes, " ") != "wallet:read" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "scopes", "reason": "not_declared"})
				return
			}
			h.tokens++
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_wl", "expires_in": 300, "app": "wallet", "url": wl.URL, "public_url": "https://wallet.example/"})
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
	h.mu.Lock()
	h.tripID = tr.ID
	h.mu.Unlock()
	return h
}

func (h *walletHarness) do(method, path string, body any) (int, string) {
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

func (h *walletHarness) card() walletCard {
	h.t.Helper()
	code, body := h.do("GET", "/api/v1/trips/"+strconv.FormatInt(h.tripID, 10)+"/pocket-wallet", nil)
	if code != 200 {
		h.t.Fatalf("pocket wallet %d %s", code, body)
	}
	var c walletCard
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		h.t.Fatalf("pocket wallet body %s: %v", body, err)
	}
	return c
}

func (h *walletHarness) settings() map[string]any {
	h.t.Helper()
	code, body := h.do("GET", "/api/v1/settings", nil)
	if code != 200 {
		h.t.Fatalf("settings %d %s", code, body)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	return m
}

func (h *walletHarness) waitRegistered() {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !h.srv.coreReady() {
		if time.Now().After(deadline) {
			h.t.Fatal("Travel did not register with Core")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// With Core and the link on: the passes filed with the trip and the ones
// starting during it (not filed with another trip), each once, by time,
// read with a link token; the card says where Wallet lives.
func TestPocketWalletTripPasses(t *testing.T) {
	h := newWalletHarness(t, true)
	h.waitRegistered()

	h.mu.Lock()
	uses := h.registered.Uses
	h.mu.Unlock()
	declared := false
	for _, u := range uses {
		if u.App == "wallet" && strings.Join(u.Scopes, " ") == "wallet:read" && u.Reason != "" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("manifest does not use wallet:read: %+v", uses)
	}

	c := h.card()
	if c.Status != "ok" || c.URL != "https://wallet.example" {
		t.Fatalf("card %+v", c)
	}
	var ids []int64
	for _, p := range c.Passes {
		ids = append(ids, p.ID)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 3 || ids[2] != 2 {
		t.Fatalf("passes %v, want 1 3 2", ids)
	}
	out, tram, museum := c.Passes[0], c.Passes[1], c.Passes[2]
	if out.Kind != "boarding" || out.Seat != "14C" || out.Gate != "A3" || out.Ref != "TP1234" || out.Venue != "SJJ" || out.Destination != "LIS" || !out.HasCode {
		t.Fatalf("flight %+v", out)
	}
	if tram.HasCode || tram.TripID != nil || !museum.Archived || !museum.HasCode {
		t.Fatalf("tram %+v museum %+v", tram, museum)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queries) != 2 {
		t.Fatalf("passes calls %d", len(h.queries))
	}
	if h.queries[0].Get("trip_id") != strconv.FormatInt(h.tripID, 10) || h.queries[1].Get("from") != "2026-05-01" || h.queries[1].Get("to") != "2026-05-05" {
		t.Fatalf("queries %v", h.queries)
	}
	for _, a := range h.auth {
		if a != "Bearer pcx_wl" {
			t.Fatalf("Wallet saw %q, want the link token", a)
		}
	}
}

// Core refusing the link (it arrives switched off): the card says why and
// nothing reaches Wallet; Settings shows the same; Test says so too.
func TestPocketWalletLinkOff(t *testing.T) {
	h := newWalletHarness(t, true)
	h.waitRegistered()
	h.mu.Lock()
	h.link = "off_admin"
	h.mu.Unlock()
	c := h.card()
	if c.Status != "unavailable" || c.Reason != "off_admin" || !strings.Contains(c.Detail, "Travel → Pocket Wallet link is switched off") {
		t.Fatalf("card %+v", c)
	}
	if c.Passes == nil {
		t.Fatal("passes should be empty, not null")
	}
	st := h.settings()
	if st["wallet_mode"] != "core" || st["wallet_via"] != "" || !strings.Contains(st["wallet_detail"].(string), "switched off in Pocket Core") {
		t.Fatalf("settings %v", st)
	}
	if _, body := h.do("POST", "/api/v1/integrations/test", map[string]any{"which": "wallet"}); !strings.Contains(body, "switched off in Pocket Core") {
		t.Fatalf("test while off: %s", body)
	}
	h.mu.Lock()
	h.link = "on"
	h.mu.Unlock()
	if st := h.settings(); st["wallet_via"] != "core" {
		t.Fatalf("after on %v", st)
	}
	h.mu.Lock()
	if len(h.auth) != 0 {
		h.mu.Unlock()
		t.Fatal("Wallet reached while the link was off")
	}
	h.mu.Unlock()
	if _, body := h.do("POST", "/api/v1/integrations/test", map[string]any{"which": "wallet"}); !strings.Contains(body, `"ok":true`) || !strings.Contains(body, "Pocket Wallet reachable") {
		t.Fatalf("test while on: %s", body)
	}
}

// Switched off for oneself: no token is asked for; only core and off are choices.
func TestPocketWalletOff(t *testing.T) {
	h := newWalletHarness(t, true)
	h.waitRegistered()
	if code, body := h.do("PUT", "/api/v1/settings", map[string]any{"wallet_mode": "off"}); code != 200 {
		t.Fatalf("off %d %s", code, body)
	}
	if c := h.card(); c.Status != "off" {
		t.Fatalf("card %+v", c)
	}
	if st := h.settings(); st["wallet_mode"] != "off" {
		t.Fatalf("settings %v", st)
	}
	if code, _ := h.do("PUT", "/api/v1/settings", map[string]any{"wallet_mode": "direct"}); code != 400 {
		t.Fatalf("direct accepted: %d", code)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tokens != 0 {
		t.Fatalf("asked Core for %d tokens while off", h.tokens)
	}
}

// A slow Wallet never holds the trip page: the card gives up and says so.
func TestPocketWalletSlow(t *testing.T) {
	old := walletTimeout
	walletTimeout = 200 * time.Millisecond
	defer func() { walletTimeout = old }()
	h := newWalletHarness(t, true)
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
		t.Fatalf("waited %s for a slow Wallet", time.Since(start))
	}
	h.mu.Lock()
	n := len(h.queries)
	h.mu.Unlock()
	if code, _ := h.do("GET", "/api/v1/trips/"+strconv.FormatInt(h.tripID, 10)+"/overview", nil); code != 200 {
		t.Fatalf("overview %d", code)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queries) != n {
		t.Fatal("the overview called Pocket Wallet")
	}
}

// Without Core: the card says it needs Pocket Core; nothing breaks.
func TestPocketWalletWithoutCore(t *testing.T) {
	h := newWalletHarness(t, false)
	if c := h.card(); c.Status != "no_core" {
		t.Fatalf("card %+v", c)
	}
	st := h.settings()
	if st["wallet_mode"] != "core" || st["wallet_via"] != "" || st["core_ready"] != false {
		t.Fatalf("settings %v", st)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.auth) != 0 || h.tokens != 0 {
		t.Fatal("Wallet reached without Core")
	}
}
