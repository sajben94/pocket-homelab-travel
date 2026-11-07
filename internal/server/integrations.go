package server

import (
	"context"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strconv"

	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/dawarich"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/export"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/firefly"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/geo"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/immich"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

func (s *Server) publicSettings(ctx context.Context, st store.Settings, u *store.User) map[string]any {
	moneyMode, moneyVia, moneyDetail := s.moneyStatus(ctx, u, st)
	dwMode, dwVia, dwDetail, dwSource := s.serviceStatus(ctx, u, savedDawarich(st), true)
	imMode, imVia, imDetail, imSource := s.serviceStatus(ctx, u, savedImmich(st), true)
	_, aiStatus := s.inferenceFor(ctx, u.ID, st.AIProvider, st.OpenAIKey != "")
	locSrc, locWhy := s.locationChoice(ctx, u, st, true)
	mvOn, _ := s.movementState(ctx, u, false) // just asked above (or no Core)
	plMode, plVia, plDetail := s.placesStatus(ctx, u, st)
	wlMode, wlVia, wlDetail := s.walletStatus(ctx, u, st)
	trMode, trVia, trDetail := s.trailsStatus(ctx, u, st)
	return map[string]any{
		// core_ready: connected and registered; choices that only work
		// through Pocket Core are offered only then.
		"core_ready":      s.coreReady(),
		"location_source": st.LocationSource, "location_effective": locSrc, "location_detail": locWhy, "movement_available": mvOn,
		"ai_provider": aiStatus.Provider, "ai_pocket_model": st.AIPocketModel, "ai_status": aiStatus, "ai_configured": aiStatus.Ready,
		"core_configured": s.core.Configured(),
		"money_mode":      moneyMode, "money_via": moneyVia, "money_detail": moneyDetail,
		"dawarich_mode": dwMode, "dawarich_via": dwVia, "dawarich_detail": dwDetail, "dawarich_source": dwSource,
		"immich_mode": imMode, "immich_via": imVia, "immich_detail": imDetail, "immich_source": imSource,
		"places_mode": plMode, "places_via": plVia, "places_detail": plDetail,
		"wallet_mode": wlMode, "wallet_via": wlVia, "wallet_detail": wlDetail,
		"trails_mode": trMode, "trails_via": trVia, "trails_detail": trDetail,
		"title": st.Title, "home_currency": st.HomeCurrency, "date_format": st.DateFormat, "time_format": st.TimeFormat,
		"distance_unit": st.DistanceUnit, "fx_auto": st.FXAuto, "fx_provider": s.cfg.FXProvider, "map_tiles": st.MapTiles, "auto_photos": st.AutoPhotos,
		"firefly_url": st.FireflyURL, "firefly_token": mask(st.FireflyToken), "firefly_configured": moneyVia != "",
		"dawarich_url": st.DawarichURL, "dawarich_api_key": mask(st.DawarichAPIKey), "dawarich_configured": dwVia != "",
		"immich_url": st.ImmichURL, "immich_public_url": st.ImmichPublicURL, "immich_api_key": mask(st.ImmichAPIKey), "immich_configured": imVia != "",
		"openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL, "openai_configured": st.OpenAIKey != "",
		"max_upload_bytes": s.cfg.MaxUploadBytes, "setup_complete": st.SetupComplete, "can_edit_global": u != nil && u.Role == "admin",
	}
}

// handleGetSettings returns the box-wide settings plus the caller's own
// preferences and (masked) integration credentials.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	st, err := s.store.GetSettings(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), st, u))
}

// handlePutSettings saves the caller's own preferences and integration
// credentials; the box-wide ones (title, online exchange rates) need an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	cur, err := s.store.GetSettings(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	global := (keys["title"] && strings.TrimSpace(in.Title) != cur.Title) || (keys["fx_auto"] && in.FXAuto != cur.FXAuto)
	if global && u.Role != "admin" {
		writeErr(w, http.StatusForbidden, "only an admin can change the app title or exchange-rate lookups")
		return
	}
	if keys["title"] {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["home_currency"] {
		if !currency.Valid(in.HomeCurrency) {
			writeErr(w, http.StatusBadRequest, "unknown currency")
			return
		}
		cur.HomeCurrency = strings.ToUpper(in.HomeCurrency)
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["distance_unit"] {
		cur.DistanceUnit = in.DistanceUnit
	}
	if keys["fx_auto"] {
		cur.FXAuto = in.FXAuto
	}
	if keys["map_tiles"] {
		cur.MapTiles = in.MapTiles
	}
	if keys["auto_photos"] {
		cur.AutoPhotos = in.AutoPhotos
	}
	// Secrets: a masked value (contains ••) means "keep".
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	if keys["location_source"] {
		switch in.LocationSource {
		case "", srcMovement, srcDawarich:
			cur.LocationSource = in.LocationSource
		default:
			writeErr(w, http.StatusBadRequest, "choose Pocket Movement or Dawarich")
			return
		}
	}
	if keys["places_mode"] {
		switch in.PlacesMode {
		case "", modeCore, modeOff:
			cur.PlacesMode = in.PlacesMode
		default:
			writeErr(w, http.StatusBadRequest, "choose Pocket Core or Off for Pocket Places")
			return
		}
	}
	if keys["wallet_mode"] {
		switch in.WalletMode {
		case "", modeCore, modeOff:
			cur.WalletMode = in.WalletMode
		default:
			writeErr(w, http.StatusBadRequest, "choose Pocket Core or Off for Pocket Wallet")
			return
		}
	}
	if keys["trails_mode"] {
		switch in.TrailsMode {
		case "", modeCore, modeOff:
			cur.TrailsMode = in.TrailsMode
		default:
			writeErr(w, http.StatusBadRequest, "choose Pocket Core or Off for Pocket Trails")
			return
		}
	}
	for k, m := range map[string][2]*string{"money_mode": {&cur.MoneyMode, &in.MoneyMode}, "dawarich_mode": {&cur.DawarichMode, &in.DawarichMode}, "immich_mode": {&cur.ImmichMode, &in.ImmichMode}} {
		if keys[k] {
			switch v := *m[1]; v {
			case modeCore, modeDirect, modeOff:
				*m[0] = v
			}
		}
	}
	if keys["firefly_url"] {
		cur.FireflyURL = strings.TrimRight(strings.TrimSpace(in.FireflyURL), "/")
	}
	secret("firefly_token", &cur.FireflyToken, in.FireflyToken)
	if keys["dawarich_url"] {
		cur.DawarichURL = strings.TrimRight(strings.TrimSpace(in.DawarichURL), "/")
	}
	secret("dawarich_api_key", &cur.DawarichAPIKey, in.DawarichAPIKey)
	if keys["immich_url"] {
		cur.ImmichURL = strings.TrimRight(strings.TrimSpace(in.ImmichURL), "/")
	}
	secret("immich_api_key", &cur.ImmichAPIKey, in.ImmichAPIKey)
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			writeErr(w, 400, "invalid Pocket AI model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			writeErr(w, 400, "choose automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["openai_model"] {
		cur.OpenAIModel = strings.TrimSpace(in.OpenAIModel)
	}
	if keys["openai_base_url"] {
		cur.OpenAIBaseURL = strings.TrimRight(strings.TrimSpace(in.OpenAIBaseURL), "/")
	}
	if keys["immich_public_url"] {
		cur.ImmichPublicURL = strings.TrimRight(strings.TrimSpace(in.ImmichPublicURL), "/")
	}
	if err := s.store.SaveUserSettings(r.Context(), u.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if global {
		if err := s.store.SaveGlobalSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	// Only this person's route/photo caches depend on their credentials.
	_ = s.store.CacheDeleteLike(r.Context(), "trip:%:u"+strconv.FormatInt(u.ID, 10)+":%")
	for _, k := range []string{"money_mode", "dawarich_mode", "immich_mode", "firefly_token", "dawarich_api_key", "immich_api_key", "location_source", "places_mode", "wallet_mode", "trails_mode"} {
		if keys[k] {
			s.modesChanged() // a mode changed or a connection was removed: tell Core's map
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), cur, u))
}

// handleIntegrationTest checks a connection; a masked key means the caller's own stored one.
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which string `json:"which"`
		URL   string `json:"url"`
		Key   string `json:"key"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), userFrom(r.Context()).ID)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var msg string
	var err error
	switch in.Which {
	case "money", "firefly":
		// A token typed into the form is tried as is (with the typed URL, or
		// Money found through Pocket Core); otherwise the saved mode decides.
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		key := in.Key
		if !typed {
			key = st.FireflyToken
		}
		if key != "" && (typed || connMode(st.MoneyMode, st.FireflyToken) == modeDirect) {
			base := strings.TrimRight(strings.TrimSpace(in.URL), "/")
			if base == "" {
				base = st.FireflyURL
			}
			if base == "" {
				if u := s.resolveSibling(ctx, "money"); u != "" {
					base = u + "/firefly"
				}
			}
			msg, err = firefly.New(base, key, 15*time.Second).Describe(ctx)
			msg += ", direct connection"
		} else {
			var conn *moneyConn
			conn, err = s.moneyFor(ctx, userFrom(r.Context()).ID)
			if err == nil {
				msg, err = conn.client.Describe(ctx)
				msg += ", through Pocket Core as you"
			}
		}
	case "dawarich":
		// A key typed into the form is tried as is; otherwise the saved mode decides.
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var c *dawarich.Client
		how := "direct connection"
		if typed || s.serviceMode(st.DawarichMode, st.DawarichAPIKey) == modeDirect {
			key := in.Key
			if !typed {
				key = st.DawarichAPIKey
			}
			c = dawarich.New(firstNonEmpty(strings.TrimSpace(in.URL), st.DawarichURL), key, 15*time.Second)
		} else {
			var conn *serviceConn
			if c, conn, err = s.dawarichFor(freshCreds(ctx), userFrom(r.Context()).ID); err == nil { // a test is always live
				how = viaText(conn)
			}
		}
		if err == nil {
			err = c.Health(ctx)
		}
		if err == nil {
			var pts []dawarich.Point
			pts, err = c.Points(ctx, time.Now().AddDate(0, 0, -30), time.Now(), 1)
			msg = "Dawarich reachable"
			if err == nil && len(pts) > 0 {
				msg += ", points in the last 30 days"
			}
			msg += ", " + how
		}
	case "movement":
		// Always live: a fresh link token from Core.
		u := userFrom(r.Context())
		if !s.core.Configured() {
			err = errors.New("Pocket Movement needs Pocket Core: there is no Pocket Core on this box")
			break
		}
		var lc *locConn
		if lc, err = s.openMovement(ctx, u); err == nil {
			var pts []dawarich.Point
			pts, err = lc.dw.Points(ctx, time.Now().AddDate(0, 0, -30), time.Now(), 1)
			msg = "Pocket Movement reachable through Pocket Core"
			if err == nil && len(pts) > 0 {
				msg += ", points in the last 30 days"
			}
		}
	case "places":
		// Always live: a fresh link token from Core.
		u := userFrom(r.Context())
		if !s.core.Configured() {
			err = errors.New("Pocket Places needs Pocket Core: there is no Pocket Core on this box")
			break
		}
		var pc *placesConn
		if pc, err = s.openPlaces(ctx, u); err == nil {
			err = pc.client.Ping(ctx)
			msg = "Pocket Places reachable through Pocket Core"
		} else {
			var le *core.LinkError
			if errors.As(err, &le) {
				err = errors.New(placesReason(le.Reason))
			}
		}
	case "wallet":
		// Always live: a fresh link token from Core.
		u := userFrom(r.Context())
		if !s.core.Configured() {
			err = errors.New("Pocket Wallet needs Pocket Core: there is no Pocket Core on this box")
			break
		}
		var wc *walletConn
		if wc, err = s.openWallet(ctx, u); err == nil {
			err = wc.client.Ping(ctx)
			msg = "Pocket Wallet reachable through Pocket Core"
		} else {
			var le *core.LinkError
			if errors.As(err, &le) {
				err = errors.New(walletReason(le.Reason))
			}
		}
	case "trails":
		// Always live: a fresh link token from Core.
		u := userFrom(r.Context())
		if !s.core.Configured() {
			err = errors.New("Pocket Trails needs Pocket Core: there is no Pocket Core on this box")
			break
		}
		var tc *trailsConn
		if tc, err = s.openTrails(ctx, u); err == nil {
			err = tc.client.Ping(ctx)
			msg = "Pocket Trails reachable through Pocket Core"
		} else {
			var le *core.LinkError
			if errors.As(err, &le) {
				err = errors.New(trailsReason(le.Reason))
			}
		}
	case "openai":
		key := in.Key
		if strings.Contains(key, "••") || key == "" {
			key = st.OpenAIKey
		}
		var ids []string
		ids, err = ai.New(in.URL, key, st.OpenAIModel, 15*time.Second).Models(ctx)
		model := st.OpenAIModel
		if model == "" {
			model = ai.DefaultModel
		}
		msg = fmt.Sprintf("OpenAI reachable, %d models, using %s", len(ids), model)
		if err == nil {
			found := false
			for _, id := range ids {
				if id == model {
					found = true
				}
			}
			if !found && len(ids) > 0 {
				msg += " (not in the model list — check the name)"
			}
		}
	case "immich":
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var c *immich.Client
		how := "direct connection"
		if typed || s.serviceMode(st.ImmichMode, st.ImmichAPIKey) == modeDirect {
			key := in.Key
			if !typed {
				key = st.ImmichAPIKey
			}
			c = immich.New(firstNonEmpty(strings.TrimSpace(in.URL), st.ImmichURL), key, 15*time.Second)
		} else {
			var conn *serviceConn
			if c, conn, err = s.immichFor(freshCreds(ctx), userFrom(r.Context()).ID); err == nil { // a test is always live
				how = viaText(conn)
			}
		}
		if err == nil {
			var v string
			v, err = c.Version(ctx)
			msg = "Immich " + v + ", " + how
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var size, files int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	_ = filepath.WalkDir(s.cfg.FilesDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				files += fi.Size()
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size, "files_bytes": files,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.Counts(r.Context()),
	})
}

// handleBackup downloads the whole database: box administration, admins only.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-travel-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	defer os.Remove(dst)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	http.ServeFile(w, r, dst)
}

// handleMeta returns the static lists the UI needs once.
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	tpl := make([]string, 0, len(PackingTemplates))
	for k := range PackingTemplates {
		tpl = append(tpl, k)
	}
	sort.Strings(tpl)
	writeJSON(w, http.StatusOK, map[string]any{
		"place_categories":    store.PlaceCategories,
		"expense_categories":  store.ExpenseCategories,
		"discover_categories": geo.DiscoverCategories(),
		"packing_templates":   tpl,
		"lodging_types":       []string{"hotel", "apartment", "hostel", "house", "camping", "friends", "other"},
		"transport_types":     []string{"flight", "train", "bus", "car", "ferry", "taxi", "bike", "walk", "other"},
	})
}

// ---- home ----

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	stats, err := s.store.HomeStats(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	trips, _ := s.store.ListTrips(ctx, store.TripFilter{UserID: u.ID})
	today := store.Today()
	var current, next *store.Trip
	recent := []store.Trip{}
	upcoming := []store.Trip{}
	for i := range trips {
		t := &trips[i]
		if t.Archived || t.Status == store.StatusCancelled {
			continue
		}
		t.Counts, _ = s.store.TripCounts(ctx, t.ID)
		switch {
		case t.StartDate != "" && t.StartDate <= today && t.EndDate >= today:
			if current == nil {
				current = t
			}
		case t.StartDate > today:
			upcoming = append(upcoming, *t)
		default:
			if len(recent) < 6 {
				recent = append(recent, *t)
			}
		}
	}
	sort.Slice(upcoming, func(i, j int) bool { return upcoming[i].StartDate < upcoming[j].StartDate })
	if len(upcoming) > 0 {
		next = &upcoming[0]
	}
	// Totals that live in the integration caches: recorded km and photo counts.
	var recordedKm float64
	photosN := 0
	locSrc, _ := s.locationStatus(ctx, u.ID)
	for i := range trips {
		if trips[i].Archived {
			continue
		}
		if v, _, ok := s.store.CacheGet(ctx, userCacheKey(trips[i].ID, u.ID, locCacheName(locSrc, "route"))); ok {
			var rr RouteResponse
			if json.Unmarshal([]byte(v), &rr) == nil {
				recordedKm += rr.DistanceKm
			}
		}
		if v, _, ok := s.store.CacheGet(ctx, userCacheKey(trips[i].ID, u.ID, "immich-photos")); ok {
			var ph []json.RawMessage
			if json.Unmarshal([]byte(v), &ph) == nil {
				photosN += len(ph)
			}
		}
	}
	journal, _ := s.store.LatestJournal(ctx, u.ID, 6)
	// Pins for the "where you've been" map: the trip's centre, else its first located place.
	type pin struct {
		ID     int64   `json:"id"`
		Name   string  `json:"name"`
		Status string  `json:"status"`
		Start  string  `json:"start_date"`
		End    string  `json:"end_date"`
		Lat    float64 `json:"lat"`
		Lng    float64 `json:"lng"`
		Cover  bool    `json:"has_cover"`
	}
	pins := []pin{}
	for i := range trips {
		t := &trips[i]
		if t.Archived || t.Status == store.StatusCancelled {
			continue
		}
		if lat, lng, ok := s.tripCenter(ctx, t); ok {
			pins = append(pins, pin{ID: t.ID, Name: t.Name, Status: t.Status, Start: t.StartDate, End: t.EndDate, Lat: lat, Lng: lng, Cover: t.HasCover})
		}
	}
	// Weather at the destination of the trip that matters now (today, or the start day of the next one).
	var hero *store.Trip
	if current != nil {
		hero = current
	} else if next != nil {
		hero = next
	}
	var heroWx any
	if hero != nil {
		if lat, lng, ok := s.tripCenter(ctx, hero); ok {
			day := today
			if hero.StartDate > today {
				day = hero.StartDate
			}
			key := cacheKey(hero.ID, "wx-home:"+day)
			if v, at, ok := s.store.CacheGet(ctx, key); ok {
				if ts, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(ts) < 3*time.Hour {
					var w any
					if json.Unmarshal([]byte(v), &w) == nil {
						heroWx = w
					}
				}
			}
			if heroWx == nil {
				if days, err := s.geo.Weather(ctx, lat, lng, day, day); err == nil && len(days) > 0 {
					heroWx = days[0]
					if b, err := json.Marshal(days[0]); err == nil {
						_ = s.store.CachePut(ctx, key, string(b))
					}
				}
			}
		}
	}
	out := map[string]any{"stats": stats, "current": current, "next": next, "upcoming": upcoming, "recent": recent,
		"recorded_km": recordedKm, "photos": photosN, "journal": journal, "pins": pins, "hero_weather": heroWx}
	// Today's plan for a running trip.
	if current != nil {
		places, _ := s.store.ListPlaces(ctx, current.ID)
		var todays []store.Place
		for _, p := range places {
			if p.DayDate == today {
				todays = append(todays, p)
			}
		}
		if todays == nil {
			todays = []store.Place{}
		}
		lodging, _ := s.store.ListLodging(ctx, current.ID)
		var stay *store.Lodging
		for i := range lodging {
			l := lodging[i]
			if len(l.CheckIn) >= 10 && len(l.CheckOut) >= 10 && l.CheckIn[:10] <= today && today < l.CheckOut[:10] {
				stay = &lodging[i]
			}
		}
		expenses, _ := s.store.ListExpenses(ctx, current.ID)
		out["today"] = map[string]any{"date": today, "day_number": store.DaysBetween(current.StartDate, today), "places": todays, "stay": stay, "budget": s.budgetSummary(current, expenses)}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDashboard is the Homepage widget: small, flat numbers.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	st, err := s.store.HomeStats(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	out := map[string]any{"trips": st.Trips, "upcoming": st.Upcoming, "days_traveled": st.Days, "places": st.Places, "next_trip": "", "next_in_days": 0, "next_trip_text": "none planned"}
	if st.NextTripID > 0 {
		if t, err := s.store.GetTrip(r.Context(), st.NextTripID, u.ID); err == nil {
			d, _ := store.ParseDate(t.StartDate)
			days := int(time.Until(d).Hours()/24) + 1
			out["next_trip"] = t.Name
			out["next_in_days"] = days
			out["next_trip_text"] = fmt.Sprintf("%s · %dd", shortName(t.Name, 16), days)
		}
	}
	trips, _ := s.store.ListTrips(r.Context(), store.TripFilter{UserID: u.ID})
	today := store.Today()
	for _, t := range trips {
		if !t.Archived && t.StartDate != "" && t.StartDate <= today && t.EndDate >= today {
			out["current_trip"] = t.Name
			out["current_day"] = store.DaysBetween(t.StartDate, today)
			out["next_trip_text"] = fmt.Sprintf("%s · day %d/%d", shortName(t.Name, 14), store.DaysBetween(t.StartDate, today), t.Days)
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- geo ----

func (s *Server) handleGeoSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	// "42.42, 18.77", a Google/Apple Maps link, or a maps.app.goo.gl short link:
	// resolve to coordinates and reverse-geocode instead of text search.
	if lat, lng, ok := s.coordsFromInput(r.Context(), q); ok {
		res, err := s.geo.Reverse(r.Context(), lat, lng)
		if err != nil {
			res = &geo.Result{Name: fmt.Sprintf("%.5f, %.5f", lat, lng), Source: "coords"}
		}
		res.Lat, res.Lng = lat, lng
		// A Google Maps place link carries the place name: /place/Kastel+Fortress/…
		if m := placeNameRe.FindStringSubmatch(q); m != nil {
			if n, err := url.PathUnescape(strings.ReplaceAll(m[1], "+", " ")); err == nil && n != "" {
				res.Name = n
			}
		}
		if res.Display == "" {
			res.Display = fmt.Sprintf("%.5f, %.5f", lat, lng)
		}
		writeJSON(w, http.StatusOK, []geo.Result{*res})
		return
	}
	res, err := s.geo.Search(r.Context(), q, queryInt(r, "limit"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "search failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGeoReverse(w http.ResponseWriter, r *http.Request) {
	lat, ok1 := queryFloat(r, "lat")
	lng, ok2 := queryFloat(r, "lng")
	if !ok1 || !ok2 {
		writeErr(w, http.StatusBadRequest, "lat and lng are required")
		return
	}
	res, err := s.geo.Reverse(r.Context(), lat, lng)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "reverse geocoding failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGeoDiscover(w http.ResponseWriter, r *http.Request) {
	lat, ok1 := queryFloat(r, "lat")
	lng, ok2 := queryFloat(r, "lng")
	if !ok1 || !ok2 {
		writeErr(w, http.StatusBadRequest, "lat and lng are required")
		return
	}
	cat := r.URL.Query().Get("category")
	if cat == "" {
		cat = "sight"
	}
	key := fmt.Sprintf("discover:%s:%.3f:%.3f:%d", cat, lat, lng, queryInt(r, "radius"))
	if v, at, ok := s.store.CacheGet(r.Context(), key); ok {
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 24*time.Hour {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(v))
			return
		}
	}
	res, err := s.geo.Discover(r.Context(), lat, lng, queryInt(r, "radius"), cat)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "discover failed: "+err.Error())
		return
	}
	b, _ := json.Marshal(res)
	_ = s.store.CachePut(r.Context(), key, string(b))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) handleGeoWiki(w http.ResponseWriter, r *http.Request) {
	res, err := s.geo.Wikipedia(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("lang"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGeoWeather(w http.ResponseWriter, r *http.Request) {
	lat, ok1 := queryFloat(r, "lat")
	lng, ok2 := queryFloat(r, "lng")
	if !ok1 || !ok2 {
		writeErr(w, http.StatusBadRequest, "lat and lng are required")
		return
	}
	res, err := s.geo.Weather(r.Context(), lat, lng, r.URL.Query().Get("start"), r.URL.Query().Get("end"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// tripCenter picks coordinates for weather: the trip's own, else the first stay, else the first place.
func (s *Server) tripCenter(ctx context.Context, t *store.Trip) (float64, float64, bool) {
	if t.Lat != nil && t.Lng != nil {
		return *t.Lat, *t.Lng, true
	}
	if lodging, _ := s.store.ListLodging(ctx, t.ID); len(lodging) > 0 {
		for _, l := range lodging {
			if l.Lat != nil && l.Lng != nil {
				return *l.Lat, *l.Lng, true
			}
		}
	}
	if places, _ := s.store.ListPlaces(ctx, t.ID); len(places) > 0 {
		for _, p := range places {
			if p.Lat != nil && p.Lng != nil {
				return *p.Lat, *p.Lng, true
			}
		}
	}
	return 0, 0, false
}

func (s *Server) handleTripWeather(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	if t.StartDate == "" || t.EndDate == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	lat, lng, ok := s.tripCenter(r.Context(), t)
	if !ok {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	key := cacheKey(t.ID, fmt.Sprintf("weather:%.2f:%.2f:%s:%s", lat, lng, t.StartDate, t.EndDate))
	if v, at, ok := s.store.CacheGet(r.Context(), key); ok && !queryBool(r, "refresh") {
		if ts, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(ts) < 3*time.Hour {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(v))
			return
		}
	}
	res, err := s.geo.Weather(r.Context(), lat, lng, t.StartDate, t.EndDate)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	b, _ := json.Marshal(res)
	_ = s.store.CachePut(r.Context(), key, string(b))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

// ---- Dawarich ----

// tripWindow returns the trip's dates as instants in the server timezone (whole days).
func tripWindow(t *store.Trip) (time.Time, time.Time, bool) {
	if t.StartDate == "" || t.EndDate == "" {
		return time.Time{}, time.Time{}, false
	}
	loc := time.Local
	if t.Timezone != "" {
		if l, err := time.LoadLocation(t.Timezone); err == nil {
			loc = l
		}
	}
	a, err1 := time.ParseInLocation("2006-01-02", t.StartDate, loc)
	b, err2 := time.ParseInLocation("2006-01-02", t.EndDate, loc)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, false
	}
	return a, b.AddDate(0, 0, 1), true
}

// cacheTTL: finished trips change rarely, running ones every hour.
func cacheTTL(t *store.Trip) time.Duration {
	if t.EndDate != "" && t.EndDate < store.Today() {
		return 7 * 24 * time.Hour
	}
	return time.Hour
}

// cached answers from the per-person cache (the data came from the caller's own account).
func (s *Server) cached(r *http.Request, t *store.Trip, key string, fetch func() (any, error)) ([]byte, error) {
	k := userCacheKey(t.ID, actorID(r.Context()), key)
	if !queryBool(r, "refresh") {
		if v, at, ok := s.store.CacheGet(r.Context(), k); ok {
			if ts, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(ts) < cacheTTL(t) {
				return []byte(v), nil
			}
		}
	}
	v, err := fetch()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	_ = s.store.CachePut(r.Context(), k, string(b))
	return b, nil
}

// RouteResponse is the recorded route, simplified for the map.
type RouteResponse struct {
	Points     [][3]float64       `json:"points"` // lat, lng, ts — every kept point, for GPX
	RawCount   int                `json:"raw_count"`
	DistanceKm float64            `json:"distance_km"`
	Days       map[string]float64 `json:"days_km"`
	ByDay      []RouteDay         `json:"days"`             // what the map draws
	Bounds     *[4]float64        `json:"bounds,omitempty"` // minLat, minLng, maxLat, maxLng
	Source     string             `json:"source,omitempty"` // movement | dawarich
	// Countries you were in (Pocket Movement only), in order of entry.
	Countries []RouteCountry `json:"countries,omitempty"`
}

// RouteCountry is a country visited during the trip and on how many days.
type RouteCountry struct {
	Code string `json:"code"` // ISO 3166-1 alpha-2
	Days int    `json:"days"`
}

// RouteDay is one day's track as separate segments: a segment ends where
// the recording stopped (a gap, a teleport), so no straight lines are drawn
// across what the phone did not see.
type RouteDay struct {
	Date     string         `json:"date"`
	Km       float64        `json:"km"`
	Segments [][][2]float64 `json:"segments"`
	Start    string         `json:"start,omitempty"`
	End      string         `json:"end,omitempty"`
	// Countries that day (Pocket Movement only).
	Countries []string `json:"countries,omitempty"`
}

func (s *Server) fetchRoute(ctx context.Context, lc *locConn, t *store.Trip) (*RouteResponse, error) {
	dw := lc.dw
	from, to, ok := tripWindow(t)
	if !ok {
		return &RouteResponse{Points: [][3]float64{}, Days: map[string]float64{}, Source: lc.src}, nil
	}
	pts, err := dw.Points(ctx, from, to, 400000)
	if err != nil {
		return nil, err
	}
	raw := make([]geo.Pt, 0, len(pts))
	for _, p := range pts {
		raw = append(raw, geo.Pt{Lat: p.Lat, Lng: p.Lng, TS: p.TS})
	}
	// Distance per day over the raw points, then simplify for drawing.
	days := map[string]float64{}
	loc := from.Location()
	for i := 1; i < len(raw); i++ {
		d := geo.Haversine(raw[i-1].Lat, raw[i-1].Lng, raw[i].Lat, raw[i].Lng)
		dt := raw[i].TS - raw[i-1].TS
		// A GPS jump (implied speed over 250 km/h) or a gap of more than a day is not travel.
		if dt <= 0 || dt > 24*3600 || (d > 1500 && d/float64(dt) > 70) {
			continue
		}
		day := time.Unix(raw[i].TS, 0).In(loc).Format("2006-01-02")
		days[day] += d / 1000
	}
	// Movement's day summaries know the journeys (and the countries); a
	// longer distance there wins over the raw points, as with Dawarich's
	// timeline below.
	dayCountries := map[string][]string{}
	var countries []RouteCountry
	if lc.mv != nil {
		if md, err := lc.mv.Days(ctx, t.StartDate, t.EndDate); err == nil {
			seen := map[string]int{}
			for _, d := range md {
				if km := d.Distance / 1000; km > days[d.Date] {
					days[d.Date] = km
				}
				dayCountries[d.Date] = d.Countries
				for _, c := range d.Countries {
					if i, ok := seen[c]; ok {
						countries[i].Days++
						continue
					}
					seen[c] = len(countries)
					countries = append(countries, RouteCountry{Code: c, Days: 1})
				}
			}
		} else {
			s.log.Debug("pocket movement: day summaries failed", "trip", t.ID, "err", err)
		}
	}
	// Dawarich's timeline knows about tracks and gaps the raw points miss (a
	// phone that slept through a motorway); prefer its per-day distances.
	if lc.mv == nil {
		if tl, err := dw.Timeline(ctx, from, to); err == nil && len(tl) > 0 {
			tlDays := map[string]float64{}
			for _, d := range tl {
				if d.Summary.TotalDistance > 0 {
					tlDays[d.Date] = d.Summary.TotalDistance
				}
			}
			for k, v := range tlDays {
				if v > days[k] {
					days[k] = v
				}
			}
		}
	}
	total := 0.0
	for _, v := range days {
		total += v
	}
	// Cut the recording into segments at gaps, then simplify each.
	var segs [][]geo.Pt
	var cur []geo.Pt
	for i, p := range raw {
		if i > 0 {
			prev := raw[i-1]
			d := geo.Haversine(prev.Lat, prev.Lng, p.Lat, p.Lng)
			dt := p.TS - prev.TS
			// 20 minutes of silence, or a jump faster than 250 km/h: the line would be fiction.
			if dt > 20*60 || (dt > 0 && d > 300 && d/float64(dt) > 70) || (dt <= 0 && d > 300) {
				if len(cur) > 1 {
					segs = append(segs, cur)
				}
				cur = nil
			}
		}
		cur = append(cur, p)
	}
	if len(cur) > 1 {
		segs = append(segs, cur)
	}
	tol := 15.0
	if len(raw) > 60000 {
		tol = 40
	}
	byDay := map[string]*RouteDay{}
	var order []string
	out := &RouteResponse{RawCount: len(raw), DistanceKm: total, Days: days, Points: [][3]float64{}, ByDay: []RouteDay{}, Source: lc.src, Countries: countries}
	for _, sg := range segs {
		simp := geo.Simplify(sg, tol)
		if len(simp) < 2 {
			continue
		}
		day := time.Unix(simp[0].TS, 0).In(loc).Format("2006-01-02")
		rd := byDay[day]
		if rd == nil {
			rd = &RouteDay{Date: day, Km: days[day], Countries: dayCountries[day]}
			byDay[day] = rd
			order = append(order, day)
		}
		seg := make([][2]float64, 0, len(simp))
		for _, p := range simp {
			seg = append(seg, [2]float64{p.Lat, p.Lng})
			out.Points = append(out.Points, [3]float64{p.Lat, p.Lng, float64(p.TS)})
		}
		rd.Segments = append(rd.Segments, seg)
		if rd.Start == "" {
			rd.Start = time.Unix(simp[0].TS, 0).In(loc).Format("15:04")
		}
		rd.End = time.Unix(simp[len(simp)-1].TS, 0).In(loc).Format("15:04")
	}
	sort.Strings(order)
	for _, d := range order {
		out.ByDay = append(out.ByDay, *byDay[d])
	}
	for _, p := range out.Points {
		if out.Bounds == nil {
			out.Bounds = &[4]float64{p[0], p[1], p[0], p[1]}
			continue
		}
		if p[0] < out.Bounds[0] {
			out.Bounds[0] = p[0]
		}
		if p[1] < out.Bounds[1] {
			out.Bounds[1] = p[1]
		}
		if p[0] > out.Bounds[2] {
			out.Bounds[2] = p[0]
		}
		if p[1] > out.Bounds[3] {
			out.Bounds[3] = p[1]
		}
	}
	return out, nil
}

// handleRoute is the viewer's recorded route for the trip, from their own
// Pocket Movement or Dawarich (a trip shared with them shows their route).
func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	lc, err := s.tripLocation(r.Context(), t)
	if err != nil {
		writeJSON(w, http.StatusOK, RouteResponse{Points: [][3]float64{}, Days: map[string]float64{}, ByDay: []RouteDay{}})
		return
	}
	b, err := s.cached(r, t, lc.cacheName("route"), func() (any, error) { return s.fetchRoute(r.Context(), lc, t) })
	if err != nil {
		writeErr(w, http.StatusBadGateway, strings.ToLower(lc.label())+": "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

// handleTimeline is the day-by-day timeline (journeys and stops) in
// Dawarich's shape, which Pocket Movement answers too (with named visits).
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	lc, err := s.tripLocation(r.Context(), t)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	b, err := s.cached(r, t, lc.cacheName("timeline"), func() (any, error) {
		from, to, ok := tripWindow(t)
		if !ok {
			return []any{}, nil
		}
		days, err := lc.dw.Timeline(r.Context(), from, to)
		if err != nil {
			return nil, err
		}
		if days == nil {
			days = []dawarich.Day{}
		}
		return days, nil
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, strings.ToLower(lc.label())+": "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

// ---- Immich ----

func (s *Server) handleTripPhotos(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	if !t.Immich {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	// the viewer's own Immich (a trip shared with them shows their photos)
	im, _, err := s.immichFor(r.Context(), actorID(r.Context()))
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	b, err := s.cached(r, t, "immich-photos", func() (any, error) {
		from, to, ok := tripWindow(t)
		if !ok {
			return []any{}, nil
		}
		a, err := im.Search(r.Context(), from, to, 5000)
		if err != nil {
			return nil, err
		}
		if a == nil {
			a = []immich.Asset{}
		}
		return a, nil
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "immich: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) handlePhotoThumb(w http.ResponseWriter, r *http.Request) {
	// the viewer's own Immich, resolved for every thumbnail (the key never
	// reaches the browser, and a switch-off applies at once)
	im, _, err := s.immichFor(r.Context(), actorID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusNotFound, "immich: "+err.Error())
		return
	}
	id := strings.Map(func(c rune) rune {
		if c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			return c
		}
		return -1
	}, chi.URLParam(r, "id"))
	if err := im.Thumbnail(r.Context(), w, id, r.URL.Query().Get("size")); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
	}
}

// ---- exports ----

func (s *Server) handleExportICS(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	s.writeICS(w, r, t)
}

func (s *Server) writeICS(w http.ResponseWriter, r *http.Request, t *store.Trip) {
	ctx := r.Context()
	places, _ := s.store.ListPlaces(ctx, t.ID)
	lodging, _ := s.store.ListLodging(ctx, t.ID)
	transport, _ := s.store.ListTransport(ctx, t.ID)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(t.Name)+`.ics"`)
	_, _ = w.Write([]byte(export.ICS(t, places, lodging, transport, s.cfg.BaseURL)))
}

// handleCalendarFeed serves the ICS for subscriptions: /cal/{id}.ics?token=pt_…
func (s *Server) handleCalendarFeed(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	u, err := s.store.GetUserByAPIToken(r.Context(), tok)
	if err != nil || u == nil {
		writeErr(w, http.StatusUnauthorized, "a valid API token is required (?token=…)")
		return
	}
	id := idParam(r, "tripID")
	if s.store.TripRole(r.Context(), id, u) == "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	t, err := s.store.GetTrip(r.Context(), id, u.ID)
	if err != nil {
		s.fail(w, err, "trip")
		return
	}
	s.writeICS(w, r, t)
}

func safeName(s string) string {
	s = strings.Map(func(c rune) rune {
		if c == '"' || c == '/' || c == '\\' || c < 32 {
			return '-'
		}
		return c
	}, s)
	if s == "" {
		return "trip"
	}
	return s
}

func (s *Server) handleExportGPX(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	ctx := r.Context()
	places, _ := s.store.ListPlaces(ctx, t.ID)
	lodging, _ := s.store.ListLodging(ctx, t.ID)
	var route []export.RoutePoint
	if queryBool(r, "route") {
		if lc, err := s.tripLocation(ctx, t); err == nil {
			if b, err := s.cached(r, t, lc.cacheName("route"), func() (any, error) { return s.fetchRoute(ctx, lc, t) }); err == nil {
				var rr RouteResponse
				if json.Unmarshal(b, &rr) == nil {
					for _, p := range rr.Points {
						route = append(route, export.RoutePoint{Lat: p[0], Lng: p[1], TS: int64(p[2])})
					}
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/gpx+xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(t.Name)+`.gpx"`)
	_, _ = w.Write([]byte(export.GPX(t, places, lodging, route)))
}

// TripExport is the portable JSON form of a trip.
type TripExport struct {
	Format     string               `json:"format"`
	Version    string               `json:"version"`
	ExportedAt string               `json:"exported_at"`
	Trip       *store.Trip          `json:"trip"`
	Places     []store.Place        `json:"places"`
	Lodging    []store.Lodging      `json:"lodging"`
	Transport  []store.Transport    `json:"transport"`
	Expenses   []store.Expense      `json:"expenses"`
	Checklists []store.Checklist    `json:"checklists"`
	Journal    []store.JournalEntry `json:"journal"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	ctx := r.Context()
	ex := TripExport{Format: "pocket-travel-trip", Version: Version, ExportedAt: store.Now(), Trip: t}
	ex.Places, _ = s.store.ListPlaces(ctx, t.ID)
	ex.Lodging, _ = s.store.ListLodging(ctx, t.ID)
	ex.Transport, _ = s.store.ListTransport(ctx, t.ID)
	ex.Expenses, _ = s.store.ListExpenses(ctx, t.ID)
	ex.Checklists, _ = s.store.ListChecklists(ctx, t.ID)
	ex.Journal, _ = s.store.ListJournal(ctx, t.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(t.Name)+`.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ex)
}

func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var ex TripExport
	if err := decode(r, &ex); err != nil || ex.Trip == nil {
		writeErr(w, http.StatusBadRequest, "not a Pocket Travel trip export")
		return
	}
	ctx := r.Context()
	t := *ex.Trip
	t.ID, t.OwnerID, t.CoverPath, t.ExternalID = 0, u.ID, "", ""
	if t.Currency == "" || !currency.Valid(t.Currency) {
		t.Currency = "EUR"
	}
	if !store.ValidStatus(t.Status) {
		t.Status = store.StatusPlanning
	}
	if err := s.store.CreateTrip(ctx, &t); err != nil {
		s.fail(w, err, "create trip")
		return
	}
	for _, p := range ex.Places {
		p.ID, p.TripID, p.ImagePath = 0, t.ID, ""
		_ = s.store.CreatePlace(ctx, &p)
	}
	for _, l := range ex.Lodging {
		l.ID, l.TripID, l.ImagePath = 0, t.ID, ""
		_ = s.store.CreateLodging(ctx, &l)
	}
	for _, tr := range ex.Transport {
		tr.ID, tr.TripID = 0, t.ID
		_ = s.store.CreateTransport(ctx, &tr)
	}
	for _, e := range ex.Expenses {
		e.ID, e.TripID = 0, t.ID
		if e.Source == "item" {
			e.LinkKind, e.LinkID, e.Source = "", 0, "manual"
		}
		_ = s.store.CreateExpense(ctx, &e)
	}
	for _, c := range ex.Checklists {
		nc := store.Checklist{TripID: t.ID, Name: c.Name, Kind: c.Kind}
		if err := s.store.CreateChecklist(ctx, &nc); err == nil {
			for _, it := range c.Items {
				_ = s.store.AddChecklistItem(ctx, t.ID, &store.ChecklistItem{ChecklistID: nc.ID, Text: it.Text, Done: it.Done})
			}
		}
	}
	for _, j := range ex.Journal {
		j.ID, j.TripID, j.UserID = 0, t.ID, u.ID
		_ = s.store.CreateJournal(ctx, &j)
	}
	s.store.LogActivity(ctx, t.ID, u.ID, "imported", "from JSON")
	out, _ := s.store.GetTrip(ctx, t.ID, u.ID)
	writeJSON(w, http.StatusCreated, out)
}

var (
	placeNameRe = regexp.MustCompile(`/maps/place/([^/@?]+)`)
	coordPairRe = regexp.MustCompile(`^\s*(-?\d{1,2}(?:\.\d+)?)\s*,\s*(-?\d{1,3}(?:\.\d+)?)\s*$`)
	atCoordsRe  = regexp.MustCompile(`@(-?\d{1,2}\.\d+),(-?\d{1,3}\.\d+)`)
	bangCoords  = regexp.MustCompile(`!3d(-?\d{1,2}\.\d+)!4d(-?\d{1,3}\.\d+)`)
	qCoordsRe   = regexp.MustCompile(`[?&](?:q|ll|query|destination|daddr|sll)=(-?\d{1,2}\.\d+),(-?\d{1,3}\.\d+)`)
)

// coordsFromInput understands "lat, lng" and the common map-link shapes.
func (s *Server) coordsFromInput(ctx context.Context, q string) (float64, float64, bool) {
	parse := func(m []string) (float64, float64, bool) {
		if m == nil {
			return 0, 0, false
		}
		lat, e1 := strconv.ParseFloat(m[1], 64)
		lng, e2 := strconv.ParseFloat(m[2], 64)
		if e1 != nil || e2 != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
			return 0, 0, false
		}
		return lat, lng, true
	}
	if lat, lng, ok := parse(coordPairRe.FindStringSubmatch(q)); ok {
		return lat, lng, true
	}
	if !strings.HasPrefix(q, "http://") && !strings.HasPrefix(q, "https://") {
		return 0, 0, false
	}
	try := func(u string) (float64, float64, bool) {
		for _, re := range []*regexp.Regexp{bangCoords, atCoordsRe, qCoordsRe} {
			if lat, lng, ok := parse(re.FindStringSubmatch(u)); ok {
				return lat, lng, true
			}
		}
		return 0, 0, false
	}
	if lat, lng, ok := try(q); ok {
		return lat, lng, true
	}
	// Short links (maps.app.goo.gl, goo.gl/maps, apple) redirect to a long URL with the coordinates.
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q, nil)
	if err != nil {
		return 0, 0, false
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	res, err := client.Do(req)
	if err != nil {
		return 0, 0, false
	}
	defer res.Body.Close()
	if lat, lng, ok := try(res.Request.URL.String()); ok {
		return lat, lng, true
	}
	// Last resort: the page body of a Google Maps place often carries the coordinates.
	b, _ := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	return try(string(b))
}

// shortName trims a trip name for one-line widgets.
func shortName(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
