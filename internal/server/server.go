// Package server wires the HTTP API, auth and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/config"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/fx"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/geo"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	geo      *geo.Client
	fx       *fx.Client

	// one in-flight sync per trip
	syncMu  sync.Mutex
	syncing map[int64]bool

	oidcMu sync.Mutex
	oidc   *oidcProvider

	core     *core.Client
	coreMu   sync.Mutex
	coreOK   bool
	coreErr  string
	resolved map[string]resolvedURL

	modesKick chan struct{} // a connection-mode report is due (modes.go)

	// countries of coordinates (countries.go)
	countryKick   chan struct{}
	countryLookup func(ctx context.Context, lat, lng float64) (string, error) // tests replace Nominatim
	nominatimMu   sync.Mutex
	nominatimLast time.Time

	// whether Pocket Movement is open to a person through Core (location.go)
	mvMu   sync.Mutex
	mvSeen map[string]movementSeen
}

type resolvedURL struct {
	url string
	at  time.Time
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	if cfg.DefaultCurrency != "" {
		st.DefaultCurrency = cfg.DefaultCurrency
	}
	return &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(),
		geo: geo.New(cfg.UserAgent, 25*time.Second), fx: fx.New(cfg.FXProvider, cfg.UserAgent, 8*time.Second), syncing: map[int64]bool{},
		oidc:     &oidcProvider{issuerURL: cfg.OIDCIssuer},
		core:     core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: cfg.UserAgent}),
		resolved: map[string]resolvedURL{}, modesKick: make(chan struct{}, 1), countryKick: make(chan struct{}, 1), mvSeen: map[string]movementSeen{}}
}

// Bootstrap seeds the first admin (if configured), integration defaults and demo data.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(s.cfg.FilesDir, 0o755); err != nil {
		return fmt.Errorf("files dir: %w", err)
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveGlobalSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	// The integration credentials in the environment belong to the owner (the
	// first admin); they fill the owner's empty settings, never a member's.
	if owner := s.store.FirstAdminID(ctx); owner > 0 {
		if err := s.seedOwnerSettings(ctx, owner); err != nil {
			return err
		}
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	go s.janitor(ctx)
	go s.countryLoop(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
		go s.modesLoop(ctx)
	}
	return nil
}

// seedOwnerSettings gives the owner any personal settings still stored
// box-wide and fills their empty integration settings from the environment
// (the UI values win afterwards). Members never get these. Each value is
// copied once: clearing it in Settings (to switch Pocket Money over to
// Pocket Core, say) must not bring it back at the next start. A value
// already there counts as copied.
func (s *Server) seedOwnerSettings(ctx context.Context, owner int64) error {
	if err := s.store.ClaimLegacySettings(ctx, owner); err != nil {
		return err
	}
	st, err := s.store.GetSettings(ctx, owner)
	if err != nil {
		return err
	}
	changed := false
	var marks []string
	set := func(name string, dst *string, v string) {
		key := "seeded:" + name
		if s.store.HasUserSetting(ctx, owner, key) {
			return
		}
		if *dst == "" && v != "" {
			*dst = v
			changed = true
		}
		if *dst != "" {
			marks = append(marks, key)
		}
	}
	set("FIREFLY_URL", &st.FireflyURL, s.cfg.FireflyURL)
	set("FIREFLY_TOKEN", &st.FireflyToken, s.cfg.FireflyToken)
	set("DAWARICH_URL", &st.DawarichURL, s.cfg.DawarichURL)
	set("DAWARICH_API_KEY", &st.DawarichAPIKey, s.cfg.DawarichAPIKey)
	set("IMMICH_URL", &st.ImmichURL, s.cfg.ImmichURL)
	set("IMMICH_API_KEY", &st.ImmichAPIKey, s.cfg.ImmichAPIKey)
	set("IMMICH_PUBLIC_URL", &st.ImmichPublicURL, s.cfg.ImmichPublicURL)
	set("OPENAI_API_KEY", &st.OpenAIKey, s.cfg.OpenAIKey)
	set("OPENAI_MODEL", &st.OpenAIModel, s.cfg.OpenAIModel)
	set("OPENAI_BASE_URL", &st.OpenAIBaseURL, s.cfg.OpenAIBaseURL)
	if changed {
		if err := s.store.SaveUserSettings(ctx, owner, st); err != nil {
			return err
		}
	}
	for _, k := range marks {
		if err := s.store.MarkUserSetting(ctx, owner, k, "1"); err != nil {
			return err
		}
	}
	return nil
}

// ---- Pocket Core ----

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		was := s.coreOK
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err == nil && !was {
			s.modesChanged() // first report right after registering
		}
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "travel", Name: "Pocket Travel",
		Description: "A trip planner that keeps the whole trip on one page: the plan, what it really cost, where you actually went and what you photographed.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "TRAVEL",
		Publishes: []string{"travel.trip_created", "travel.trip_updated", "travel.expense_linked"},
		Offers:    linkOffers,
		Uses:      append(append(append(append([]core.Use{}, moneyUses...), movementUses...), placesUses...), core.InferenceUse()),
		Services:  serviceUses,
	}
	// Ask Core for a relying-party client so "Sign in with Pocket Core" needs no configuration.
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.oidcCallbackURL()}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// publish persists an event locally; the SDK retries network delivery. ownerID is
// the person the data belongs to; their email rides along as owner_email so
// consumers can keep it with that person.

// resolveSibling asks Core where a sibling app lives (cached a minute) so the
// *_URL settings may stay empty on a box with Core.
func (s *Server) resolveSibling(ctx context.Context, slug string) string {
	if !s.core.Configured() {
		return ""
	}
	s.coreMu.Lock()
	if r, ok := s.resolved[slug]; ok && time.Since(r.at) < time.Minute {
		s.coreMu.Unlock()
		return r.url
	}
	s.coreMu.Unlock()
	url := ""
	if res, err := s.core.Resolve(ctx, slug); err == nil {
		url = res.URL
	}
	s.coreMu.Lock()
	s.resolved[slug] = resolvedURL{url: url, at: time.Now()}
	s.coreMu.Unlock()
	return url
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// janitor syncs trip costs from Pocket Money for active trips every 30 minutes.
// It also brings repeating bills up to date.
func (s *Server) janitor(ctx context.Context) {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	s.repeatBills(ctx, store.Today())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.autoSync(ctx)
			s.repeatBills(ctx, store.Today())
		}
	}
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(180 * time.Second))

	r.Get("/pocket-ask.js", s.core.AskWidget("travel"))
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	// Nextcloud Cospend's public API, for MoneyBuster (cospend.go)
	s.cospendRoutes(r)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.kickCountriesOnWrite)
			r.Use(withPeopleFromQuery)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/home", s.handleHome)
			r.Get("/currencies", s.handleCurrencies)
			r.Get("/fx", s.handleFX)
			r.Get("/meta", s.handleMeta)

			// outside world
			r.Get("/geo/search", s.handleGeoSearch)
			r.Get("/geo/reverse", s.handleGeoReverse)
			r.Get("/geo/discover", s.handleGeoDiscover)
			r.Get("/geo/wiki", s.handleGeoWiki)
			r.Get("/geo/weather", s.handleGeoWeather)

			r.Get("/search", s.handleSearch)
			r.Get("/places/saved", s.handleSavedPlaces)
			r.Get("/stats", s.handleStats)
			r.Get("/countries", s.handleCountries)
			r.Get("/people", s.handleListPeople)
			r.Post("/people", s.handleCreatePerson)
			r.Get("/people/{id}", s.handleGetPerson)
			r.Put("/people/{id}", s.handleUpdatePerson)
			r.Delete("/people/{id}", s.handleDeletePerson)
			r.Post("/people/{id}/merge", s.handleMergePerson)
			r.Get("/countries/{code}", s.handleCountry)
			r.Put("/countries/{code}/note", s.handlePutCountryNote)
			r.Post("/countries/detect", s.handleDetectCountries)
			r.Get("/trips", s.handleListTrips)
			r.Post("/trips", s.handleCreateTrip)
			r.Route("/trips/{tripID}", func(r chi.Router) {
				r.Use(s.tripAccess)
				r.Get("/", s.handleGetTrip)
				r.Put("/", s.handleUpdateTrip)
				r.Delete("/", s.handleDeleteTrip)
				r.Get("/overview", s.handleTripOverview)
				r.Get("/activity", s.handleTripActivity)
				r.Put("/members", s.handleTripMembers)
				r.Get("/people", s.handleListTripPeople)
				r.Post("/people", s.handleAddTripPerson)
				r.Put("/people/{id}", s.handleUpdateTripPerson)
				r.Delete("/people/{id}", s.handleDeleteTripPerson)
				r.Post("/cover", s.handleTripCover)
				r.Get("/cover", s.handleTripCoverImage)
				r.Delete("/cover", s.handleTripCoverDelete)
				r.Post("/duplicate", s.handleDuplicateTrip)

				r.Get("/places", s.handleListPlaces)
				r.Post("/places", s.handleCreatePlace)
				r.Put("/places/reorder", s.handleReorderPlaces)
				r.Get("/places/{id}", s.handleGetPlace)
				r.Put("/places/{id}", s.handleUpdatePlace)
				r.Delete("/places/{id}", s.handleDeletePlace)
				r.Post("/places/{id}/image", s.handlePlaceImage)
				r.Get("/places/{id}/image", s.handlePlaceImageGet)
				r.Get("/places/{id}/photos", s.handleListPlacePhotos)
				r.Post("/places/{id}/photos", s.handleUploadPlacePhoto)
				r.Post("/places/{id}/photos/find", s.handleFindPlacePhotos)
				r.Post("/places/photos/find-missing", s.handleFindMissingPhotos)
				r.Get("/places/photos/{photoID}", s.handlePlacePhotoFile)
				r.Delete("/places/photos/{photoID}", s.handleDeletePlacePhoto)
				r.Post("/places/photos/{photoID}/cover", s.handleSetPlaceCover)

				r.Get("/lodging", s.handleListLodging)
				r.Post("/lodging", s.handleCreateLodging)
				r.Put("/lodging/{id}", s.handleUpdateLodging)
				r.Delete("/lodging/{id}", s.handleDeleteLodging)
				r.Post("/lodging/{id}/image", s.handleLodgingImage)
				r.Get("/lodging/{id}/image", s.handleLodgingImageGet)

				r.Get("/transport", s.handleListTransport)
				r.Post("/transport", s.handleCreateTransport)
				r.Put("/transport/{id}", s.handleUpdateTransport)
				r.Delete("/transport/{id}", s.handleDeleteTransport)

				r.Get("/expenses", s.handleListExpenses)
				r.Post("/expenses", s.handleCreateExpense)
				r.Put("/expenses/{id}", s.handleUpdateExpense)
				r.Delete("/expenses/{id}", s.handleDeleteExpense)
				r.Get("/budget", s.handleBudget)
				r.Put("/budget/plan", s.handlePutBudgetPlan)
				r.Get("/split", s.handleSplit)
				r.Get("/split/share", s.handleGetShare)
				r.Put("/split/share", s.handlePutShare)
				r.Delete("/split/share", s.handleDeleteShare)
				r.Post("/split/settle", s.handleSettle)
				r.Get("/split/trash", s.handleTrash)
				r.Delete("/split/trash", s.handleEmptyTrash)
				r.Post("/expenses/{id}/restore", s.handleRestoreExpense)
				r.Post("/split/categories", s.handleSaveCategory)
				r.Put("/split/categories/{id}", s.handleSaveCategory)
				r.Delete("/split/categories/{id}", s.handleDeleteCategory)
				r.Post("/split/payment-modes", s.handleSavePaymentMode)
				r.Put("/split/payment-modes/{id}", s.handleSavePaymentMode)
				r.Delete("/split/payment-modes/{id}", s.handleDeletePaymentMode)
				r.Post("/split/currencies", s.handleSaveCurrency)
				r.Put("/split/currencies/{id}", s.handleSaveCurrency)
				r.Delete("/split/currencies/{id}", s.handleDeleteCurrency)
				r.Post("/money/sync", s.handleMoneySync)
				r.Post("/firefly/sync", s.handleMoneySync) // before v0.6.0

				r.Get("/checklists", s.handleListChecklists)
				r.Post("/checklists", s.handleCreateChecklist)
				r.Put("/checklists/{id}", s.handleUpdateChecklist)
				r.Delete("/checklists/{id}", s.handleDeleteChecklist)
				r.Post("/checklists/{id}/items", s.handleAddChecklistItem)
				r.Put("/checklists/items/{id}", s.handleUpdateChecklistItem)
				r.Delete("/checklists/items/{id}", s.handleDeleteChecklistItem)

				r.Get("/journal", s.handleListJournal)
				r.Post("/journal", s.handleCreateJournal)
				r.Put("/journal/{id}", s.handleUpdateJournal)
				r.Delete("/journal/{id}", s.handleDeleteJournal)

				r.Get("/documents", s.handleListDocuments)
				r.Post("/documents", s.handleUploadDocument)
				r.Put("/documents/{id}", s.handleUpdateDocument)
				r.Get("/documents/{id}/file", s.handleDocumentFile)
				r.Delete("/documents/{id}", s.handleDeleteDocument)

				r.Get("/weather", s.handleTripWeather)
				r.Get("/location/route", s.handleRoute)
				r.Get("/location/timeline", s.handleTimeline)
				// the old names, for scripts written before Pocket Movement
				r.Get("/dawarich/route", s.handleRoute)
				r.Get("/dawarich/timeline", s.handleTimeline)
				r.Get("/photos", s.handleTripPhotos)
				r.Get("/pocket-places", s.handleTripPocketPlaces)
				r.Get("/photos/{id}/thumb", s.handlePhotoThumb)
				r.Post("/refresh", s.handleTripRefresh)

				r.Get("/export/calendar.ics", s.handleExportICS)
				r.Get("/export/trip.gpx", s.handleExportGPX)
				r.Get("/export/trip.json", s.handleExportJSON)

				r.Post("/ai/suggest", s.handleAISuggest)
				r.Post("/ai/plan", s.handleAIPlan)
				r.Post("/ai/describe", s.handleAIDescribe)
				r.Post("/ai/packing", s.handleAIPacking)
				r.Post("/ai/recap", s.handleAIRecap)
			})

			r.Get("/money/tags", s.handleMoneyTags)
			r.Get("/firefly/tags", s.handleMoneyTags) // before v0.6.0
			r.Post("/integrations/test", s.handleIntegrationTest)
			r.Get("/ai/models", s.handleInferenceModels)
			r.Post("/import/adventurelog/collections", s.handleALCollections)
			r.Post("/import/adventurelog", s.handleALImport)
			r.Post("/import/trip", s.handleImportJSON)
			r.Post("/import/cospend", s.handleCospendImport)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
		})
	})

	// Calendar subscription: token in the query so phones can subscribe.
	r.Get("/cal/{tripID}.ics", s.handleCalendarFeed)

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryFloat(r *http.Request, name string) (float64, bool) {
	f, err := strconv.ParseFloat(r.URL.Query().Get(name), 64)
	return f, err == nil
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_travel_info Build info.\n# TYPE pocket_travel_info gauge\npocket_travel_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_travel_uptime_seconds Process uptime.\n# TYPE pocket_travel_uptime_seconds gauge\npocket_travel_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_travel_http_requests_total HTTP requests served.\n# TYPE pocket_travel_http_requests_total counter\npocket_travel_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_travel_http_errors_total HTTP 5xx responses.\n# TYPE pocket_travel_http_errors_total counter\npocket_travel_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_travel_rows Rows per table.\n# TYPE pocket_travel_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_travel_rows{table=%q} %d\n", k, v)
	}
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// ---------- files ----------

// saveFile writes bytes under FILES_DIR/<trip>/ with a random name and returns the relative path.
func (s *Server) saveFile(tripID int64, ext string, data []byte) (string, error) {
	dir := filepath.Join(s.cfg.FilesDir, strconv.FormatInt(tripID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext = strings.ToLower(filepath.Ext("x" + ext))
	if len(ext) > 8 {
		ext = ""
	}
	name := store.RandomToken(12) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return strconv.FormatInt(tripID, 10) + "/" + name, nil
}

func (s *Server) filePath(rel string) string {
	rel = filepath.Clean("/" + rel)
	return filepath.Join(s.cfg.FilesDir, rel)
}

func (s *Server) removeFile(rel string) {
	if rel == "" {
		return
	}
	_ = os.Remove(s.filePath(rel))
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, rel, mime, download string) {
	if rel == "" {
		writeErr(w, http.StatusNotFound, "no file")
		return
	}
	p := s.filePath(rel)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "file missing")
		return
	}
	if mime != "" {
		w.Header().Set("Content-Type", mime)
	}
	if download != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(download, `"`, "")+`"`)
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, p)
}

// readUpload reads one multipart file field (or the raw body) up to the limit.
func (s *Server) readUpload(r *http.Request, field string) ([]byte, string, string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, s.cfg.MaxUploadBytes)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
			return nil, "", "", err
		}
		f, hdr, err := r.FormFile(field)
		if err != nil {
			return nil, "", "", err
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			return nil, "", "", err
		}
		return b, hdr.Filename, hdr.Header.Get("Content-Type"), nil
	}
	b, err := io.ReadAll(r.Body)
	return b, r.URL.Query().Get("name"), ct, err
}
