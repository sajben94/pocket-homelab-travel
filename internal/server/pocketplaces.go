package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/pocketplaces"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// ---- Pocket Places: your places at the destination ----
//
// The trip page shows what the person's own Pocket Places knows for the
// trip: the places on their list near the destination (to try, and the
// ones they liked before), the lists they made for the trip and the visits
// during it with what they cost. Only through Pocket Core: a five-minute
// pcx_ link token for places:read, asked for each look. Per person in
// Settings (places_mode): core (the default) | off. A shared trip shows the
// viewer's own places.

// placesScopes is what Travel reads in Pocket Places for a person.
var placesScopes = []string{"places:read"}

// placesUses is the manifest's Uses for Pocket Places.
var placesUses = []core.Use{
	{App: "places", Scopes: placesScopes, Reason: "Show the places on your list at a trip's destination, the lists made for it and where you went during it"},
}

// placesTimeout bounds one look at Pocket Places (Core's token included):
// the trip page loads it on its own and never waits for it.
var placesTimeout = 6 * time.Second

// placesMode is the person's Pocket Places mode in effect: core unless they
// switched it off.
func placesMode(st store.Settings) string {
	if st.PlacesMode == modeOff {
		return modeOff
	}
	return modeCore
}

// placesReason is Core's refusal of the Places link in words.
func placesReason(reason string) string {
	switch reason {
	case "":
		return ""
	case "no_core":
		return "there is no Pocket Core on this box"
	case "target_missing":
		return "Pocket Places is not registered with Pocket Core"
	case "not_offered":
		return "this Pocket Places version does not share its places with other apps yet"
	}
	if t := linkReason("Pocket Places", reason); t != "" {
		return t
	}
	return reason
}

// placesStatus says, without issuing a token, how the person reaches Pocket
// Places: the mode in effect, "core" when it works ("" when not) and why not.
func (s *Server) placesStatus(ctx context.Context, u *store.User, st store.Settings) (mode, via, detail string) {
	mode = placesMode(st)
	if mode == modeOff {
		return mode, "", "switched off"
	}
	if u == nil || !s.core.Configured() {
		return mode, "", placesReason("no_core")
	}
	on, reason := s.core.LinkOn(ctx, u.Email, "places", placesScopes...)
	if on {
		return mode, modeCore, ""
	}
	return mode, "", placesReason(reason)
}

// placesConn is one person's Pocket Places, opened for a piece of work.
type placesConn struct {
	client *pocketplaces.Client
	public string // what the browser opens
}

// openPlaces asks Core for a link token to the person's Pocket Places.
// A *core.LinkError comes back as is, for the caller to tell why.
func (s *Server) openPlaces(ctx context.Context, u *store.User) (*placesConn, error) {
	tok, err := s.core.ActAs(ctx, u.Email, "places", placesScopes...)
	if err != nil {
		return nil, err
	}
	if tok.URL == "" {
		return nil, errors.New("Pocket Core did not say where Pocket Places lives")
	}
	public := tok.PublicURL
	if public == "" {
		if res, err := s.core.Resolve(ctx, "places"); err == nil {
			public = res.PublicURL
		}
	}
	return &placesConn{
		client: pocketplaces.New(tok.URL, tok.AccessToken, placesTimeout),
		public: strings.TrimRight(firstNonEmpty(public, tok.URL), "/"),
	}, nil
}

// placesCard is what the trip page's Pocket Places card shows. Status:
// ok | no_core (Travel has no Pocket Core) | off (the person switched it
// off) | unavailable (Core refused the link; reason says which) | error
// (Places or Core did not answer).
type placesCard struct {
	Status  string               `json:"status"`
	Reason  string               `json:"reason,omitempty"`
	Detail  string               `json:"detail,omitempty"`
	URL     string               `json:"places_url,omitempty"` // Pocket Places' address; /places/{id}, /lists/{id}
	TripURL string               `json:"trip_url,omitempty"`   // this trip in Pocket Places
	Lists   []pocketplaces.List  `json:"lists"`
	Want    []pocketplaces.Place `json:"want"`
	Been    []pocketplaces.Place `json:"been"`
	Visits  []pocketplaces.Visit `json:"visits"`
	Spent   []pocketplaces.Spent `json:"spent"`
}

// handleTripPocketPlaces is the trip's Pocket Places card for the viewer.
// Always 200: what is not there says so in status, so the page stays calm.
func (s *Server) handleTripPocketPlaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := tripFrom(ctx).trip
	u := userFrom(ctx)
	writeJSON(w, http.StatusOK, s.tripPlaces(ctx, u, t))
}

// tripPlaces looks the trip up in the person's Pocket Places.
func (s *Server) tripPlaces(ctx context.Context, u *store.User, t *store.Trip) placesCard {
	out := placesCard{Lists: []pocketplaces.List{}, Want: []pocketplaces.Place{}, Been: []pocketplaces.Place{}, Visits: []pocketplaces.Visit{}, Spent: []pocketplaces.Spent{}}
	if !s.core.Configured() {
		out.Status, out.Detail = "no_core", placesReason("no_core")
		return out
	}
	if u == nil {
		out.Status, out.Detail = "unavailable", placesReason("unknown_user")
		return out
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	if placesMode(st) == modeOff {
		out.Status, out.Detail = "off", "switched off in Settings → Integrations"
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, placesTimeout)
	defer cancel()
	conn, err := s.openPlaces(ctx, u)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			out.Status, out.Reason, out.Detail = "unavailable", le.Reason, placesReason(le.Reason)
			return out
		}
		s.log.Debug("pocket places: not reached", "user", u.Email, "err", err)
		out.Status, out.Detail = "error", placesErr(ctx, err)
		return out
	}
	out.URL = conn.public
	g, err := conn.client.TripGuide(ctx, pocketplaces.GuideQuery{TripID: t.ID, Lat: t.Lat, Lng: t.Lng, Destination: t.Destination, From: t.StartDate, To: t.EndDate})
	if err != nil {
		s.log.Debug("pocket places: trip guide failed", "user", u.Email, "trip", t.ID, "err", err)
		out.Status, out.Detail = "error", placesErr(ctx, err)
		return out
	}
	out.Status = "ok"
	out.TripURL = placesTripURL(conn.public, g.URL, t)
	out.Lists, out.Want, out.Been, out.Visits, out.Spent = g.Lists, g.Want, g.Been, g.Visits, g.Spent
	return out
}

// placesTripURL is where "Open in Pocket Places" goes: its places near the
// trip's destination (the same 40 km the guide uses, named after it), else
// the address Places gave for the trip.
func placesTripURL(public, guideURL string, t *store.Trip) string {
	if t.Lat == nil || t.Lng == nil {
		return firstNonEmpty(guideURL, public+"/places")
	}
	q := url.Values{"near": {strconv.FormatFloat(*t.Lat, 'f', 5, 64) + "," + strconv.FormatFloat(*t.Lng, 'f', 5, 64)}, "radius_km": {"40"}, "sort": {"near"}}
	if label := strings.TrimSpace(strings.Split(t.Destination, ",")[0]); label != "" {
		q.Set("label", label)
	}
	return public + "/places?" + q.Encode()
}

// placesErr is a failed look in words (a timeout reads as one).
func placesErr(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "Pocket Places took too long to answer"
	}
	return err.Error()
}
