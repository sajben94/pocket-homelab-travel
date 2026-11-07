package server

// Photos from the viewer's own Immich and PhotoPrism: a trip's Photos tab
// (with pictures hidden from it), thumbnails, and trips found in the photos'
// dates and places.

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/countries"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/geo"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/immich"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/photoprism"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// tripPhoto is one picture as the UI gets it, whichever library it is from.
type tripPhoto struct {
	ID        string   `json:"id"`
	Src       string   `json:"src"`            // immich | photoprism
	Hash      string   `json:"hash,omitempty"` // PhotoPrism: what its thumbnails go by
	Type      string   `json:"type"`           // IMAGE | VIDEO
	FileName  string   `json:"file_name"`
	TakenAt   string   `json:"taken_at"`
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
	City      string   `json:"city"`
	Country   string   `json:"country"`
	Favorite  bool     `json:"favorite"`
	Duration  string   `json:"duration,omitempty"`
	Thumbhash string   `json:"thumbhash,omitempty"`
	Open      string   `json:"open,omitempty"` // the photo in its own app
	Device    string   `json:"device"`         // which phone or app it came from: "iPhone 15 Pro", "WhatsApp"
	Hidden    bool     `json:"hidden,omitempty"`
}

func (p tripPhoto) key() string { return p.Src + ":" + p.ID }

func fromImmich(a immich.Asset, public string) tripPhoto {
	p := tripPhoto{ID: a.ID, Src: "immich", Type: a.Type, FileName: a.FileName, TakenAt: a.TakenAt, Lat: a.Lat, Lng: a.Lng, City: a.City, Country: a.Country, Favorite: a.Favorite, Duration: a.Duration, Thumbhash: a.Thumbhash, Device: deviceLabel(a.FileName, a.Path, a.Camera)}
	if public != "" {
		p.Open = public + "/photos/" + a.ID
	}
	return p
}

func fromPhotoPrism(a photoprism.Photo, public string) tripPhoto {
	p := tripPhoto{ID: a.ID, Src: "photoprism", Hash: a.Hash, Type: a.Type, FileName: a.FileName, TakenAt: a.TakenAt, Lat: a.Lat, Lng: a.Lng, City: a.City, Country: a.Country, Favorite: a.Favorite, Duration: a.Duration, Device: deviceLabel(a.FileName, a.Path, a.Camera)}
	if c := countries.Get(a.Country); c != nil {
		p.Country = c.Name
	}
	if public != "" {
		p.Open = photoprism.OpenURL(public, a.ID)
	}
	return p
}

var (
	waName     = regexp.MustCompile(`(?i)^(IMG|VID|AUD|PTT)-\d{8}-WA\d+`)
	cameraJunk = regexp.MustCompile(`(?i)^(apple|google)\s+`) // the model says it all: "iPhone 15 Pro", "Pixel 8"
)

// deviceLabel says where a photo came from, so a trip's photos can be told
// apart with one click: a messaging app when the file name or folder says so
// (photos received in WhatsApp, Viber, Telegram, Messenger), screenshots, else
// the camera ("iPhone 15 Pro", "Galaxy S23"), else "Unknown camera".
func deviceLabel(file, path, camera string) string {
	f, d := strings.ToLower(file), strings.ToLower(path)
	switch {
	case waName.MatchString(file) || strings.Contains(d, "whatsapp") || strings.Contains(f, "whatsapp"):
		return "WhatsApp"
	case strings.Contains(f, "viber") || strings.Contains(d, "viber"):
		return "Viber"
	case strings.Contains(f, "telegram") || strings.Contains(d, "telegram"):
		return "Telegram"
	case strings.HasPrefix(f, "fb_img") || strings.Contains(d, "messenger") || strings.Contains(f, "messenger") || strings.Contains(d, "facebook"):
		return "Messenger"
	case strings.Contains(d, "signal") || strings.HasPrefix(f, "signal-"):
		return "Signal"
	case strings.Contains(f, "screenshot") || strings.Contains(d, "screenshot"):
		return "Screenshots"
	}
	if camera = strings.TrimSpace(camera); camera != "" {
		// "Apple iPhone 15 Pro" → "iPhone 15 Pro"; "samsung SM-S911B" → "Samsung SM-S911B"
		c := cameraJunk.ReplaceAllString(camera, "")
		if c == "" {
			c = camera
		}
		if f := strings.Fields(c); c == camera && len(f) > 1 && len(f[0]) > 2 {
			f[0] = strings.ToUpper(f[0][:1]) + strings.ToLower(f[0][1:]) // "samsung", "SAMSUNG" → "Samsung"
			c = strings.Join(f, " ")
		}
		return c
	}
	return "Unknown camera"
}

// photoprismFor is userID's PhotoPrism (direct only) and what a browser opens.
func (s *Server) photoprismFor(ctx context.Context, userID int64) (*photoprism.Client, string, error) {
	st, err := s.store.GetSettings(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if st.PhotoPrismURL == "" {
		return nil, "", errors.New("PhotoPrism is not connected")
	}
	return photoprism.New(st.PhotoPrismURL, st.PhotoPrismToken, 30*time.Second), firstNonEmpty(st.PhotoPrismPublicURL, st.PhotoPrismURL), nil
}

// handleTripPhotos lists the viewer's photos from the trip's days, from every
// library they connected, oldest first. A library that fails is named in
// X-Photos-Error while the others still show.
func (s *Server) handleTripPhotos(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	out := []tripPhoto{}
	if !t.Immich { // "photos on for this trip", from before there was more than Immich
		writeJSON(w, http.StatusOK, out)
		return
	}
	uid := actorID(r.Context())
	var errs []string
	tried := 0
	if im, conn, err := s.immichFor(r.Context(), uid); err == nil {
		tried++
		b, err := s.cached(r, t, "immich-photos", func() (any, error) {
			from, to, ok := tripWindow(t)
			if !ok {
				return []any{}, nil
			}
			a, err := im.Search(r.Context(), from, to, 5000)
			if a == nil {
				a = []immich.Asset{}
			}
			return a, err
		})
		var as []immich.Asset
		if err == nil {
			err = json.Unmarshal(b, &as)
		}
		if err != nil {
			errs = append(errs, "Immich: "+err.Error())
		}
		for _, a := range as {
			out = append(out, fromImmich(a, conn.public))
		}
	}
	if pp, public, err := s.photoprismFor(r.Context(), uid); err == nil {
		tried++
		b, err := s.cached(r, t, "photoprism-photos", func() (any, error) {
			if t.StartDate == "" || t.EndDate == "" {
				return []any{}, nil
			}
			a, err := pp.Search(r.Context(), t.StartDate, t.EndDate, false, 5000)
			if a == nil {
				a = []photoprism.Photo{}
			}
			return a, err
		})
		var ps []photoprism.Photo
		if err == nil {
			err = json.Unmarshal(b, &ps)
		}
		if err != nil {
			errs = append(errs, "PhotoPrism: "+err.Error())
		}
		for _, a := range ps {
			out = append(out, fromPhotoPrism(a, public))
		}
	}
	if tried > 0 && len(errs) == tried {
		writeErr(w, http.StatusBadGateway, strings.Join(errs, "; "))
		return
	}
	hidden, _ := s.store.HiddenPhotos(r.Context(), t.ID, uid)
	for i := range out {
		out[i].Hidden = hidden[out[i].key()]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TakenAt < out[j].TakenAt })
	if len(errs) > 0 {
		w.Header().Set("X-Photos-Error", strings.Join(errs, "; "))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleHidePhotos hides photos from the trip for the viewer, or shows them again.
func (s *Server) handleHidePhotos(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	var in struct {
		Photos []string `json:"photos"`
		Hidden bool     `json:"hidden"`
	}
	if err := decode(r, &in); err != nil || len(in.Photos) > 20000 {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	keep := in.Photos[:0]
	for _, p := range in.Photos {
		if (strings.HasPrefix(p, "immich:") || strings.HasPrefix(p, "photoprism:")) && len(p) < 120 {
			keep = append(keep, p)
		}
	}
	if err := s.store.SetPhotosHidden(r.Context(), t.ID, actorID(r.Context()), keep, in.Hidden); err != nil {
		s.fail(w, err, "hide photos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(keep)})
}

func cleanID(s string, ok func(rune) bool) string {
	return strings.Map(func(c rune) rune {
		if ok(c) {
			return c
		}
		return -1
	}, s)
}

func isHex(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// handlePhotoThumb streams a thumbnail from the viewer's own library (the
// key never reaches the browser, and a switch-off applies at once).
// ?src=photoprism&h=<file hash> for PhotoPrism; Immich by asset id.
func (s *Server) handlePhotoThumb(w http.ResponseWriter, r *http.Request) {
	uid := actorID(r.Context())
	size := r.URL.Query().Get("size")
	if r.URL.Query().Get("src") == "photoprism" {
		pp, _, err := s.photoprismFor(r.Context(), uid)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		h := cleanID(r.URL.Query().Get("h"), isHex)
		if h == "" {
			writeErr(w, http.StatusBadRequest, "missing hash")
			return
		}
		if err := pp.Thumbnail(r.Context(), w, h, size); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	im, _, err := s.immichFor(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "immich: "+err.Error())
		return
	}
	id := cleanID(chi.URLParam(r, "id"), func(c rune) bool { return c == '-' || isHex(c) })
	if err := im.Thumbnail(r.Context(), w, id, size); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
	}
}

// ---- trips found in the photos ----

// geoPhoto is one located photo, whatever library it came from.
type geoPhoto struct {
	day      string
	lat, lng float64
	city     string
	country  string // ISO code
	photo    tripPhoto
}

// PhotoTrip is a stretch of days spent away from home, seen in the photos,
// that no trip covers yet.
type PhotoTrip struct {
	Start     string     `json:"start_date"`
	End       string     `json:"end_date"`
	Days      int        `json:"days"`
	Photos    int        `json:"photos"`
	Name      string     `json:"name"`
	Place     string     `json:"destination"`
	Countries []string   `json:"countries"`
	Lat       float64    `json:"lat"`
	Lng       float64    `json:"lng"`
	Km        int        `json:"km_from_home"`
	Cover     *tripPhoto `json:"cover,omitempty"`
}

// PhotoHome is where trips start and end: set in Settings, else where the
// photos were taken on the most days.
type PhotoHome struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	Set  bool    `json:"set"` // chosen by the user, not detected
	Days int     `json:"days"`
}

var immichCountryCodes map[string]string

// countryCode turns Immich's country name into an ISO code ("" when unknown).
func countryCode(name string) string {
	if len(name) == 2 {
		return strings.ToUpper(name)
	}
	if immichCountryCodes == nil {
		m := map[string]string{}
		for _, c := range countries.All() {
			m[strings.ToLower(c.Name)] = c.Code
			m[strings.ToLower(c.Official)] = c.Code
		}
		immichCountryCodes = m
	}
	return immichCountryCodes[strings.ToLower(name)]
}

// handlePhotoTrips finds trips in the viewer's photo locations and dates:
// days whose photos were taken far from home, in runs, where no trip of
// theirs is. Scanning a whole library is slow, so the answer is kept a day
// (?refresh=1 scans again).
func (s *Server) handlePhotoTrips(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ctx := r.Context()
	key := "user:" + strconv.FormatInt(u.ID, 10) + ":photo-trips"
	var found []PhotoTrip
	var home PhotoHome
	sources := []string{}
	if v, at, ok := s.store.CacheGet(ctx, key); ok && !queryBool(r, "refresh") {
		if ts, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(ts) < 24*time.Hour {
			var c struct {
				Trips   []PhotoTrip `json:"trips"`
				Sources []string    `json:"sources"`
				Home    PhotoHome   `json:"home"`
			}
			if json.Unmarshal([]byte(v), &c) == nil {
				found, sources, home = c.Trips, c.Sources, c.Home
			}
		}
	}
	if found == nil {
		var pts []geoPhoto
		var errs []string
		scanCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		if pp, public, err := s.photoprismFor(scanCtx, u.ID); err == nil {
			ps, err := pp.Search(scanCtx, "", "", true, 0)
			if err != nil {
				errs = append(errs, "PhotoPrism: "+err.Error())
			} else {
				sources = append(sources, "photoprism")
			}
			for _, p := range ps {
				if p.Lat != nil && len(p.TakenAt) >= 10 {
					pts = append(pts, geoPhoto{day: p.TakenAt[:10], lat: *p.Lat, lng: *p.Lng, city: p.City, country: p.Country, photo: fromPhotoPrism(p, public)})
				}
			}
		}
		if im, conn, err := s.immichFor(scanCtx, u.ID); err == nil {
			as, err := im.Search(scanCtx, time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().AddDate(0, 0, 1), 0)
			if err != nil {
				errs = append(errs, "Immich: "+err.Error())
			} else {
				sources = append(sources, "immich")
			}
			for _, a := range as {
				if a.Lat != nil && a.Lng != nil && len(a.TakenAt) >= 10 {
					pts = append(pts, geoPhoto{day: a.TakenAt[:10], lat: *a.Lat, lng: *a.Lng, city: a.City, country: countryCode(a.Country), photo: fromImmich(a, conn.public)})
				}
			}
		}
		if len(sources) == 0 {
			if len(errs) > 0 {
				writeErr(w, http.StatusBadGateway, strings.Join(errs, "; "))
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"trips": []PhotoTrip{}, "sources": sources, "connected": false})
			return
		}
		st, _ := s.store.GetSettings(ctx, u.ID)
		var set *[2]float64
		if st.HomeLat != nil && st.HomeLng != nil {
			set = &[2]float64{*st.HomeLat, *st.HomeLng}
		}
		found, home = findPhotoTrips(pts, set)
		if set != nil {
			home.Name, home.Set = firstNonEmpty(st.HomeName, home.Name), true
		}
		if b, err := json.Marshal(map[string]any{"trips": found, "sources": sources, "home": home}); err == nil {
			_ = s.store.CachePut(ctx, key, string(b))
		}
	}
	// Drop what a trip now covers and what was dismissed (both change
	// without a rescan).
	trips, _ := s.store.ListTrips(ctx, store.TripFilter{UserID: u.ID})
	dismissed, _ := s.store.DismissedPhotoTrips(ctx, u.ID)
	out := []PhotoTrip{}
	for _, f := range found {
		if dismissed[f.Start] || coveredByTrip(f, trips) {
			continue
		}
		out = append(out, f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": out, "sources": sources, "connected": true, "home": home})
}

// handleDismissPhotoTrip: "this is not a trip".
func (s *Server) handleDismissPhotoTrip(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Start string `json:"start_date"`
	}
	if err := decode(r, &in); err != nil || len(in.Start) != 10 {
		writeErr(w, http.StatusBadRequest, "start_date is required")
		return
	}
	if err := s.store.DismissPhotoTrip(r.Context(), userFrom(r.Context()).ID, in.Start); err != nil {
		s.fail(w, err, "dismiss")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func coveredByTrip(f PhotoTrip, trips []store.Trip) bool {
	for _, t := range trips {
		if t.StartDate != "" && t.EndDate != "" && t.StartDate <= f.End && t.EndDate >= f.Start {
			return true
		}
	}
	return false
}

const (
	awayKm     = 80.0 // a day is "away" when its photos are this far from home
	maxGapDays = 4    // days without photos that may sit inside one trip
)

// findPhotoTrips groups located photos into trips: home is the area where
// photos were taken on the most days; days with most photos far from it are
// away, and runs of away days (gaps of up to two photo-less days) are trips.
// A single day counts only when it is far (300 km+). Newest first.
//
// A trip is everything between leaving home and coming back, however many
// places it went through (Nitra → Belgrade → Novi Sad → Nitra is one trip).
// set is the home from Settings; nil detects it.
func findPhotoTrips(pts []geoPhoto, set *[2]float64) ([]PhotoTrip, PhotoHome) {
	if len(pts) == 0 {
		h := PhotoHome{}
		if set != nil {
			h.Lat, h.Lng = set[0], set[1]
		}
		return []PhotoTrip{}, h
	}
	byDay := map[string][]geoPhoto{}
	for _, p := range pts {
		byDay[p.day] = append(byDay[p.day], p)
	}
	// home: the half-degree cell with the most distinct days
	type cell struct{ a, b int }
	cellDays := map[cell]map[string]bool{}
	for _, p := range pts {
		c := cell{int(math.Floor(p.lat * 2)), int(math.Floor(p.lng * 2))}
		if cellDays[c] == nil {
			cellDays[c] = map[string]bool{}
		}
		cellDays[c][p.day] = true
	}
	var home cell
	best := -1
	for c, ds := range cellDays {
		if len(ds) > best || len(ds) == best && (c.a < home.a || c.a == home.a && c.b < home.b) {
			home, best = c, len(ds)
		}
	}
	var hl, hg float64
	n := 0
	for _, p := range pts {
		if (cell{int(math.Floor(p.lat * 2)), int(math.Floor(p.lng * 2))}) == home {
			hl += p.lat
			hg += p.lng
			n++
		}
	}
	hl, hg = hl/float64(n), hg/float64(n)
	homeCities := map[string]int{}
	for _, p := range pts {
		if (cell{int(math.Floor(p.lat * 2)), int(math.Floor(p.lng * 2))}) == home && p.city != "" {
			homeCities[p.city]++
		}
	}
	ph := PhotoHome{Name: topKey(homeCities), Days: best}
	if set != nil {
		hl, hg = set[0], set[1]
		ph.Days = 0
		near := map[string]int{}
		days := map[string]bool{}
		for _, p := range pts {
			if distKm(hl, hg, p.lat, p.lng) <= awayKm {
				days[p.day] = true
				if p.city != "" {
					near[p.city]++
				}
			}
		}
		ph.Name, ph.Days = topKey(near), len(days)
	}
	ph.Lat, ph.Lng = hl, hg

	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)
	away := map[string][]geoPhoto{} // the far photos of each away day
	for _, d := range days {
		var far []geoPhoto
		for _, p := range byDay[d] {
			if distKm(hl, hg, p.lat, p.lng) > awayKm {
				far = append(far, p)
			}
		}
		if len(far) > 0 && len(far)*10 >= len(byDay[d])*3 { // 30 %+
			away[d] = far
		}
	}
	gap := func(a, b string) int {
		ta, _ := time.Parse("2006-01-02", a)
		tb, _ := time.Parse("2006-01-02", b)
		return int(tb.Sub(ta).Hours()/24) - 1
	}
	var runs [][]string
	var cur []string
	for _, d := range days {
		_, isAway := away[d]
		switch {
		case isAway && len(cur) > 0 && gap(cur[len(cur)-1], d) <= maxGapDays:
			cur = append(cur, d)
		case isAway:
			if len(cur) > 0 {
				runs = append(runs, cur)
			}
			cur = []string{d}
		default: // a home day ends the trip
			if len(cur) > 0 {
				runs = append(runs, cur)
				cur = nil
			}
		}
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}

	out := []PhotoTrip{}
	for _, run := range runs {
		var all []geoPhoto
		for _, d := range run {
			all = append(all, away[d]...)
		}
		f := PhotoTrip{Start: run[0], End: run[len(run)-1], Days: gap(run[0], run[len(run)-1]) + 2, Photos: len(all)}
		cities, ctry := map[string]int{}, map[string]int{}
		var order []string
		var sl, sg, maxKm float64
		for _, p := range all {
			sl += p.lat
			sg += p.lng
			maxKm = math.Max(maxKm, distKm(hl, hg, p.lat, p.lng))
			if p.city != "" {
				cities[p.city]++
			}
			if p.country != "" {
				if ctry[p.country] == 0 {
					order = append(order, p.country)
				}
				ctry[p.country]++
			}
		}
		if f.Days == 1 && maxKm < 300 {
			continue
		}
		f.Lat, f.Lng, f.Km = sl/float64(len(all)), sg/float64(len(all)), int(maxKm)
		for _, c := range order {
			if ctry[c]*20 >= len(all) { // 5 %+ of the photos: a stray border shot is not a country visited
				f.Countries = append(f.Countries, c)
			}
		}
		city := topKey(cities)
		// a second stop with a good share of the photos names the trip too
		second := ""
		for c, k := range cities {
			if c != city && k*100 >= len(all)*15 && (second == "" || k > cities[second] || k == cities[second] && c < second) {
				second = c
			}
		}
		country := ""
		if c := countries.Get(topKey(ctry)); c != nil {
			country = c.Name
		}
		f.Place = strings.Trim(city+", "+country, ", ")
		switch {
		case city != "" && second != "":
			f.Name = city + " & " + second
		case city != "" && len(f.Countries) > 1:
			f.Name = city + " & more"
		case city != "":
			f.Name = city
		case country != "":
			f.Name = country
		default:
			f.Name = "Trip"
		}
		f.Name += " " + run[0][:4]
		// cover: a favourite, else a photo from the middle of the trip
		cover := all[len(all)/2].photo
		for _, p := range all {
			if p.photo.Favorite && p.photo.Type == "IMAGE" {
				cover = p.photo
				break
			}
		}
		f.Cover = &cover
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start > out[j].Start })
	if len(out) > 60 {
		out = out[:60]
	}
	return out, ph
}

func distKm(lat1, lng1, lat2, lng2 float64) float64 {
	return geo.Haversine(lat1, lng1, lat2, lng2) / 1000
}

func topKey(m map[string]int) string {
	best, n := "", 0
	for k, v := range m {
		if v > n || v == n && k < best {
			best, n = k, v
		}
	}
	return best
}

// handlePlaceFromPhoto makes a visited place where a photo was taken: named
// from the map (OpenStreetMap), on the photo's day and time, with the photo
// attached. For trips written after the fact.
func (s *Server) handlePlaceFromPhoto(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	var in struct {
		Src     string   `json:"src"`
		ID      string   `json:"id"`
		Hash    string   `json:"hash"`
		Lat     *float64 `json:"lat"`
		Lng     *float64 `json:"lng"`
		TakenAt string   `json:"taken_at"`
		City    string   `json:"city"`
	}
	if err := decode(r, &in); err != nil || in.Lat == nil || in.Lng == nil {
		writeErr(w, http.StatusBadRequest, "this photo has no location")
		return
	}
	p := &store.Place{TripID: t.ID, Category: "sight", Status: "visited", Currency: t.Currency, Lat: in.Lat, Lng: in.Lng, Name: firstNonEmpty(in.City, "Photo spot")}
	if len(in.TakenAt) >= 10 {
		p.DayDate, p.VisitedAt = in.TakenAt[:10], in.TakenAt[:10]
	}
	if len(in.TakenAt) >= 16 {
		p.StartTime = in.TakenAt[11:16]
	}
	gctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if g, err := s.geo.Reverse(gctx, *in.Lat, *in.Lng); err == nil && g != nil {
		if g.Name != "" {
			p.Name = g.Name
		}
		p.Address = g.Display
		p.CountryCode = g.CountryCode
		if g.Suggest != "" {
			p.Category = g.Suggest
		}
	}
	cancel()
	if err := s.store.CreatePlace(ctx, p); err != nil {
		s.fail(w, err, "create place")
		return
	}
	// the photo itself, from the viewer's library
	uid := actorID(ctx)
	var img []byte
	var ct string
	var err error
	switch in.Src {
	case "photoprism":
		if pp, _, e := s.photoprismFor(ctx, uid); e == nil {
			img, ct, err = pp.ThumbnailBytes(ctx, cleanID(in.Hash, isHex), "preview")
		}
	default:
		if im, _, e := s.immichFor(ctx, uid); e == nil {
			img, ct, err = im.ThumbnailBytes(ctx, cleanID(in.ID, func(c rune) bool { return c == '-' || isHex(c) }), "preview")
		}
	}
	if err == nil && len(img) > 0 && strings.HasPrefix(ct, "image/") {
		if rel, err := s.saveFile(t.ID, extOf("", ct), img); err == nil {
			_ = s.store.AddPlacePhoto(ctx, &store.PlacePhoto{PlaceID: p.ID, Path: rel, Source: in.Src})
		}
	}
	s.store.LogActivity(ctx, t.ID, userFrom(ctx).ID, "added place", p.Name)
	s.store.TouchTrip(ctx, t.ID)
	p.Photos, _ = s.store.ListPlacePhotos(ctx, p.ID)
	if p.Photos == nil {
		p.Photos = []store.PlacePhoto{}
	}
	writeJSON(w, http.StatusCreated, p)
}
