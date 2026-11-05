package server

import (
	"context"
	"testing"
	"time"
)

// waitModes waits for report number n (1-based) Core received and returns it
// as "user target" → mode.
func (h *svcHarness) waitModes(n int) map[string]string {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		if len(h.modes) >= n {
			out := map[string]string{}
			for _, c := range h.modes[n-1] {
				out[c.User+" "+c.Target] = c.Mode
			}
			h.mu.Unlock()
			return out
		}
		h.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("no connection-mode report #%d from Travel", n)
	return nil
}

func (h *svcHarness) modeReports() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.modes)
}

func TestConnectionModesReportedToCore(t *testing.T) {
	h := newSvcHarness(t, true)
	// The owner already has a Money token saved, no mode chosen.
	st, _ := h.st.GetSettings(context.Background(), h.owner.ID)
	st.FireflyURL, st.FireflyToken = "http://money.invalid/firefly", "pm_owner"
	if err := h.st.SaveUserSettings(context.Background(), h.owner.ID, st); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.srv.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}

	// Right after registering: everyone, every target, the mode in effect
	// (a saved token is Direct; nothing saved is Pocket Core on a Core box).
	want := map[string]string{
		"owner@example.com money": "direct", "owner@example.com service:dawarich": "core", "owner@example.com service:immich": "core",
		"member@example.com money": "core", "member@example.com service:dawarich": "core", "member@example.com service:immich": "core",
		"owner@example.com movement": "core", "member@example.com movement": "core",
		"owner@example.com places": "core", "member@example.com places": "core",
	}
	got := h.waitModes(1)
	if len(got) != len(want) {
		t.Fatalf("first report %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("first report %s = %q, want %q (%v)", k, got[k], v, got)
		}
	}

	// A person switching a service off.
	if code, body := h.do(h.memberTok, "PUT", "/api/v1/settings", map[string]any{"immich_mode": "off"}); code != 200 {
		t.Fatalf("mode off %d %s", code, body)
	}
	got = h.waitModes(2)
	if got["member@example.com service:immich"] != "off" || got["owner@example.com money"] != "direct" || got["member@example.com service:dawarich"] != "core" {
		t.Fatalf("after off %v", got)
	}

	// A saved key with no mode chosen is Direct.
	if code, body := h.do(h.ownerTok, "PUT", "/api/v1/settings", map[string]any{"dawarich_url": "http://dawarich.invalid", "dawarich_api_key": "own-key"}); code != 200 {
		t.Fatalf("save key %d %s", code, body)
	}
	got = h.waitModes(3)
	if got["owner@example.com service:dawarich"] != "direct" {
		t.Fatalf("after key %v", got)
	}

	// Disconnecting Money removes the token: back to Pocket Core.
	if code, body := h.do(h.ownerTok, "PUT", "/api/v1/settings", map[string]any{"firefly_url": "", "firefly_token": ""}); code != 200 {
		t.Fatalf("disconnect %d %s", code, body)
	}
	got = h.waitModes(4)
	if got["owner@example.com money"] != "core" {
		t.Fatalf("after disconnect %v", got)
	}

	// Switching Pocket Places off for oneself.
	if code, body := h.do(h.memberTok, "PUT", "/api/v1/settings", map[string]any{"places_mode": "off"}); code != 200 {
		t.Fatalf("places off %d %s", code, body)
	}
	got = h.waitModes(5)
	if got["member@example.com places"] != "off" || got["owner@example.com places"] != "core" {
		t.Fatalf("after places off %v", got)
	}

	// Saving something unrelated sends nothing.
	n := h.modeReports()
	h.do(h.ownerTok, "PUT", "/api/v1/settings", map[string]any{"home_currency": "BAM"})
	time.Sleep(100 * time.Millisecond)
	if h.modeReports() != n {
		t.Fatal("a currency change should not report modes")
	}
}

// Without Pocket Core nothing is reported (and nothing breaks).
func TestConnectionModesWithoutCore(t *testing.T) {
	h := newSvcHarness(t, false)
	h.srv.modesChanged()
	h.srv.reportModes(context.Background())
	if code, body := h.do(h.memberTok, "PUT", "/api/v1/settings", map[string]any{"money_mode": "off"}); code != 200 {
		t.Fatalf("mode %d %s", code, body)
	}
	if n := h.modeReports(); n != 0 {
		t.Fatalf("reported without Core: %d", n)
	}
}
