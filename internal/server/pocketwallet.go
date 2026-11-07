package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/pocketwallet"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// ---- Pocket Wallet: the trip's tickets and boarding passes ----
//
// The trip page shows the person's own passes from Pocket Wallet: the ones
// filed with the trip there, and the ones starting during the trip that are
// not filed with another one, each opening full screen in Wallet for the
// gate. Only through Pocket Core: a five-minute pcx_ link token for
// wallet:read, asked for each look. Per person in Settings (wallet_mode):
// core (the default) | off. A shared trip shows the viewer's own passes.

// walletScopes is what Travel reads in Pocket Wallet for a person.
var walletScopes = []string{"wallet:read"}

// walletUses is the manifest's Uses for Pocket Wallet.
var walletUses = []core.Use{
	{App: "wallet", Scopes: walletScopes, Reason: "Show each trip's boarding passes and tickets, ready to open at the gate"},
}

// walletTimeout bounds one look at Pocket Wallet (Core's token included):
// the trip page loads it on its own and never waits for it.
var walletTimeout = 6 * time.Second

// walletMode is the person's Pocket Wallet mode in effect: core unless they
// switched it off.
func walletMode(st store.Settings) string {
	if st.WalletMode == modeOff {
		return modeOff
	}
	return modeCore
}

// walletReason is Core's refusal of the Wallet link in words.
func walletReason(reason string) string {
	switch reason {
	case "":
		return ""
	case "no_core":
		return "there is no Pocket Core on this box"
	case "target_missing":
		return "Pocket Wallet is not registered with Pocket Core"
	case "not_offered":
		return "this Pocket Wallet version does not share its passes with other apps yet"
	}
	if t := linkReason("Pocket Wallet", reason); t != "" {
		return t
	}
	return reason
}

// walletStatus says, without issuing a token, how the person reaches Pocket
// Wallet: the mode in effect, "core" when it works ("" when not) and why not.
func (s *Server) walletStatus(ctx context.Context, u *store.User, st store.Settings) (mode, via, detail string) {
	mode = walletMode(st)
	if mode == modeOff {
		return mode, "", "switched off"
	}
	if u == nil || !s.core.Configured() {
		return mode, "", walletReason("no_core")
	}
	on, reason := s.core.LinkOn(ctx, u.Email, "wallet", walletScopes...)
	if on {
		return mode, modeCore, ""
	}
	return mode, "", walletReason(reason)
}

// walletConn is one person's Pocket Wallet, opened for a piece of work.
type walletConn struct {
	client *pocketwallet.Client
	public string // what the browser opens
}

// openWallet asks Core for a link token to the person's Pocket Wallet.
// A *core.LinkError comes back as is, for the caller to tell why.
func (s *Server) openWallet(ctx context.Context, u *store.User) (*walletConn, error) {
	tok, err := s.core.ActAs(ctx, u.Email, "wallet", walletScopes...)
	if err != nil {
		return nil, err
	}
	if tok.URL == "" {
		return nil, errors.New("Pocket Core did not say where Pocket Wallet lives")
	}
	public := tok.PublicURL
	if public == "" {
		if res, err := s.core.Resolve(ctx, "wallet"); err == nil {
			public = res.PublicURL
		}
	}
	return &walletConn{
		client: pocketwallet.New(tok.URL, tok.AccessToken, walletTimeout),
		public: strings.TrimRight(firstNonEmpty(public, tok.URL), "/"),
	}, nil
}

// walletCard is what the trip page's tickets card shows. Status: ok |
// no_core (Travel has no Pocket Core) | off (the person switched it off) |
// unavailable (Core refused the link; reason says which) | error (Wallet
// or Core did not answer).
type walletCard struct {
	Status string              `json:"status"`
	Reason string              `json:"reason,omitempty"`
	Detail string              `json:"detail,omitempty"`
	URL    string              `json:"wallet_url,omitempty"` // Pocket Wallet's address; /cards/{id}?show=1 opens a pass full screen
	Passes []pocketwallet.Pass `json:"passes"`
}

// handleTripPocketWallet is the trip's tickets and boarding passes for the
// viewer. Always 200: what is not there says so in status.
func (s *Server) handleTripPocketWallet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(w, http.StatusOK, s.tripWallet(ctx, userFrom(ctx), tripFrom(ctx).trip))
}

// tripWallet looks the trip's passes up in the person's Pocket Wallet.
func (s *Server) tripWallet(ctx context.Context, u *store.User, t *store.Trip) walletCard {
	out := walletCard{Passes: []pocketwallet.Pass{}}
	if !s.core.Configured() {
		out.Status, out.Detail = "no_core", walletReason("no_core")
		return out
	}
	if u == nil {
		out.Status, out.Detail = "unavailable", walletReason("unknown_user")
		return out
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	if walletMode(st) == modeOff {
		out.Status, out.Detail = "off", "switched off in Settings → Integrations"
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, walletTimeout)
	defer cancel()
	conn, err := s.openWallet(ctx, u)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			out.Status, out.Reason, out.Detail = "unavailable", le.Reason, walletReason(le.Reason)
			return out
		}
		s.log.Debug("pocket wallet: not reached", "user", u.Email, "err", err)
		out.Status, out.Detail = "error", walletErr(ctx, err)
		return out
	}
	out.URL = conn.public
	ps, err := conn.client.TripPasses(ctx, t.ID, t.StartDate, t.EndDate)
	if err != nil {
		s.log.Debug("pocket wallet: trip passes failed", "user", u.Email, "trip", t.ID, "err", err)
		out.Status, out.Detail = "error", walletErr(ctx, err)
		return out
	}
	out.Status, out.Passes = "ok", ps
	return out
}

// walletErr is a failed look in words (a timeout reads as one).
func walletErr(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "Pocket Wallet took too long to answer"
	}
	return err.Error()
}
