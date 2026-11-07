package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/pockettrails"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// ---- Pocket Trails: the trip's hikes and rides ----
//
// The trip page's Outdoors card shows the person's own outings from Pocket
// Trails: the ones filed with the trip there (trip_ref), and the ones whose
// date falls within the trip that are not filed with another one, each
// opening in Trails. Only through Pocket Core: a five-minute pcx_ link token
// for trails:read, asked for each look. Per person in Settings
// (trails_mode): core (the default) | off. A shared trip shows the viewer's
// own outings.

// trailsScopes is what Travel reads in Pocket Trails for a person.
var trailsScopes = []string{"trails:read"}

// trailsUses is the manifest's Uses for Pocket Trails.
var trailsUses = []core.Use{
	{App: "trails", Scopes: trailsScopes, Reason: "Show the hikes and rides you planned or went on during a trip on its page, with distance, climb and status, each opening in Pocket Trails"},
}

// trailsTimeout bounds one look at Pocket Trails (Core's token included):
// the trip page loads it on its own and never waits for it.
var trailsTimeout = 6 * time.Second

// trailsMode is the person's Pocket Trails mode in effect: core unless they
// switched it off.
func trailsMode(st store.Settings) string {
	if st.TrailsMode == modeOff {
		return modeOff
	}
	return modeCore
}

// trailsReason is Core's refusal of the Trails link in words.
func trailsReason(reason string) string {
	switch reason {
	case "":
		return ""
	case "no_core":
		return "there is no Pocket Core on this box"
	case "target_missing":
		return "Pocket Trails is not registered with Pocket Core"
	case "not_offered":
		return "this Pocket Trails version does not share its outings with other apps yet"
	}
	if t := linkReason("Pocket Trails", reason); t != "" {
		return t
	}
	return reason
}

// trailsStatus says, without issuing a token, how the person reaches Pocket
// Trails: the mode in effect, "core" when it works ("" when not) and why not.
func (s *Server) trailsStatus(ctx context.Context, u *store.User, st store.Settings) (mode, via, detail string) {
	mode = trailsMode(st)
	if mode == modeOff {
		return mode, "", "switched off"
	}
	if u == nil || !s.core.Configured() {
		return mode, "", trailsReason("no_core")
	}
	on, reason := s.core.LinkOn(ctx, u.Email, "trails", trailsScopes...)
	if on {
		return mode, modeCore, ""
	}
	return mode, "", trailsReason(reason)
}

// trailsConn is one person's Pocket Trails, opened for a piece of work.
type trailsConn struct {
	client *pockettrails.Client
	public string // what the browser opens
}

// openTrails asks Core for a link token to the person's Pocket Trails.
// A *core.LinkError comes back as is, for the caller to tell why.
func (s *Server) openTrails(ctx context.Context, u *store.User) (*trailsConn, error) {
	tok, err := s.core.ActAs(ctx, u.Email, "trails", trailsScopes...)
	if err != nil {
		return nil, err
	}
	if tok.URL == "" {
		return nil, errors.New("Pocket Core did not say where Pocket Trails lives")
	}
	public := tok.PublicURL
	if public == "" {
		if res, err := s.core.Resolve(ctx, "trails"); err == nil {
			public = res.PublicURL
		}
	}
	return &trailsConn{
		client: pockettrails.New(tok.URL, tok.AccessToken, trailsTimeout),
		public: strings.TrimRight(firstNonEmpty(public, tok.URL), "/"),
	}, nil
}

// trailsCard is what the trip page's Outdoors card shows. Status: ok |
// no_core (Travel has no Pocket Core) | off (the person switched it off) |
// unavailable (Core refused the link; reason says which) | error (Trails
// or Core did not answer).
type trailsCard struct {
	Status  string                `json:"status"`
	Reason  string                `json:"reason,omitempty"`
	Detail  string                `json:"detail,omitempty"`
	URL     string                `json:"trails_url,omitempty"` // Pocket Trails' address; /outings/{id} opens an outing
	Outings []pockettrails.Outing `json:"outings"`
}

// handleTripPocketTrails is the trip's outings for the
// viewer. Always 200: what is not there says so in status.
func (s *Server) handleTripPocketTrails(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(w, http.StatusOK, s.tripTrails(ctx, userFrom(ctx), tripFrom(ctx).trip))
}

// tripTrails looks the trip's outings up in the person's Pocket Trails.
func (s *Server) tripTrails(ctx context.Context, u *store.User, t *store.Trip) trailsCard {
	out := trailsCard{Outings: []pockettrails.Outing{}}
	if !s.core.Configured() {
		out.Status, out.Detail = "no_core", trailsReason("no_core")
		return out
	}
	if u == nil {
		out.Status, out.Detail = "unavailable", trailsReason("unknown_user")
		return out
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	if trailsMode(st) == modeOff {
		out.Status, out.Detail = "off", "switched off in Settings → Integrations"
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, trailsTimeout)
	defer cancel()
	conn, err := s.openTrails(ctx, u)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			out.Status, out.Reason, out.Detail = "unavailable", le.Reason, trailsReason(le.Reason)
			return out
		}
		s.log.Debug("pocket trails: not reached", "user", u.Email, "err", err)
		out.Status, out.Detail = "error", trailsErr(ctx, err)
		return out
	}
	out.URL = conn.public
	os, err := conn.client.TripOutings(ctx, t.ID, t.StartDate, t.EndDate)
	if err != nil {
		s.log.Debug("pocket trails: trip outings failed", "user", u.Email, "trip", t.ID, "err", err)
		out.Status, out.Detail = "error", trailsErr(ctx, err)
		return out
	}
	out.Status, out.Outings = "ok", os
	return out
}

// trailsErr is a failed look in words (a timeout reads as one).
func trailsErr(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "Pocket Trails took too long to answer"
	}
	return err.Error()
}
