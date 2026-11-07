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
	"sync"
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
	Open      string   `json:"open,omitempty"`   // the photo in its own app
	Device    string   `json:"device"`           // which phone or app it came from: "iPhone 15 Pro", "WhatsApp"
	Folder    string   `json:"folder,omitempty"` // where it is stored ("Photos/Camera-Jarka"), dated sub-folders left off
	Hidden    bool     `json:"hidden,omitempty"`
}

func (p tripPhoto) key() string { return p.Src + ":" + p.ID }

func fromImmich(a immich.Asset, public string) tripPhoto {
	p := tripPhoto{ID: a.ID, Src: "immich", Type: a.Type, FileName: a.FileName, TakenAt: a.TakenAt, Lat: a.Lat, Lng: a.Lng, City: a.City, Country: a.Country, Favorite: a.Favorite, Duration: a.Duration, Thumbhash: a.Thumbhash, Device: deviceLabel(a.FileName, a.Path, a.Camera), Folder: photoFolder(a.Path)}
	if public != "" {
		p.Open = public + "/photos/" + a.ID
	}
	return p
}

func fromPhotoPrism(a photoprism.Photo, public string) tripPhoto {
	p := tripPhoto{ID: a.ID, Src: "photoprism", Hash: a.Hash, Type: a.Type, FileName: a.FileName, TakenAt: a.TakenAt, Lat: a.Lat, Lng: a.Lng, City: a.City, Country: a.Country, Favorite: a.Favorite, Duration: a.Duration, Device: deviceLabel(a.FileName, a.Path, a.Camera), Folder: photoFolder(a.Path)}
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

var datedPart = regexp.MustCompile(`^(\d{1,4}|\d{4}[-_.]\d{2}([-_.]\d{2})?)$`)

// photoFolder is the folder a photo is kept in without the dated
// sub-folders libraries sort into ("Photos/Camera-Jarka/2025/07" →
// "Photos/Camera-Jarka"): two phones of the same model that upload into
// their own folders are told apart by it.
func photoFolder(path string) string {
	parts := strings.Split(strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/"), "/")
	for len(parts) > 0 && (parts[len(parts)-1] == "" || datedPart.MatchString(parts[len(parts)-1])) {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "/")
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
	// SplitBefore: this trip starts where the person split a longer one
	// (it can be joined back).
	SplitBefore bool `json:"split_before,omitempty"`
	// DayInfo summarizes each day away, so a trip can be split without
	// looking through the library again.
	DayInfo []tripDay `json:"day_info,omitempty"`
}

// tripDay is one day away from home in a found trip.
type tripDay struct {
	Date      string         `json:"date"`
	Photos    int            `json:"photos"`
	SumLat    float64        `json:"sum_lat"`
	SumLng    float64        `json:"sum_lng"`
	MaxKm     float64        `json:"max_km"`
	Cities    map[string]int `json:"cities,omitempty"`
	Countries map[string]int `json:"countries,omitempty"`
	Order     []string       `json:"order,omitempty"` // countries in the order they appear
	Cover     tripPhoto      `json:"cover"`
	Fav       bool           `json:"fav,omitempty"` // the cover is a favourite
}

func summarizeDay(date string, far []geoPhoto, hl, hg float64) tripDay {
	d := tripDay{Date: date, Photos: len(far), Cities: map[string]int{}, Countries: map[string]int{}}
	for _, p := range far {
		d.SumLat += p.lat
		d.SumLng += p.lng
		d.MaxKm = math.Max(d.MaxKm, distKm(hl, hg, p.lat, p.lng))
		if p.city != "" {
			d.Cities[p.city]++
		}
		if p.country != "" {
			if d.Countries[p.country] == 0 {
				d.Order = append(d.Order, p.country)
			}
			d.Countries[p.country]++
		}
		if !d.Fav && p.photo.Favorite && p.photo.Type == "IMAGE" {
			d.Cover, d.Fav = p.photo, true
		}
	}
	if !d.Fav && len(far) > 0 {
		d.Cover = far[len(far)/2].photo
	}
	return d
}

func dayGap(a, b string) int {
	ta, _ := time.Parse("2006-01-02", a)
	tb, _ := time.Parse("2006-01-02", b)
	return int(tb.Sub(ta).Hours()/24) - 1
}

// buildPhotoTrip names and measures a found trip from its days away
// (oldest first): the main stop (and a second one with 15 %+ of the
// photos), the countries with 5 %+ of them, a cover.
func buildPhotoTrip(ds []tripDay) PhotoTrip {
	f := PhotoTrip{Start: ds[0].Date, End: ds[len(ds)-1].Date, Days: dayGap(ds[0].Date, ds[len(ds)-1].Date) + 2, DayInfo: ds}
	cities, ctry := map[string]int{}, map[string]int{}
	var order []string
	var sl, sg, maxKm float64
	for _, d := range ds {
		f.Photos += d.Photos
		sl += d.SumLat
		sg += d.SumLng
		maxKm = math.Max(maxKm, d.MaxKm)
		for c, n := range d.Cities {
			cities[c] += n
		}
		for _, c := range d.Order {
			if ctry[c] == 0 {
				order = append(order, c)
			}
			ctry[c] += d.Countries[c]
		}
	}
	if f.Photos > 0 {
		f.Lat, f.Lng = sl/float64(f.Photos), sg/float64(f.Photos)
	}
	f.Km = int(maxKm)
	for _, c := range order {
		if ctry[c]*20 >= f.Photos { // a stray border shot is not a country visited
			f.Countries = append(f.Countries, c)
		}
	}
	city := topKey(cities)
	second := ""
	for c, k := range cities {
		if c != city && k*100 >= f.Photos*15 && (second == "" || k > cities[second] || k == cities[second] && c < second) {
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
	f.Name += " " + ds[0].Date[:4]
	// cover: a favourite, else the middle day's
	cover := ds[len(ds)/2].Cover
	for _, d := range ds {
		if d.Fav {
			cover = d.Cover
			break
		}
	}
	f.Cover = &cover
	return f
}

// splitPhotoTrips cuts found trips where the person said a new trip starts
// (a date inside a trip: that day and the ones after are the next trip).
func splitPhotoTrips(found []PhotoTrip, splits map[string]bool) []PhotoTrip {
	if len(splits) == 0 {
		return found
	}
	var out []PhotoTrip
	for _, f := range found {
		if len(f.DayInfo) < 2 {
			out = append(out, f)
			continue
		}
		var part []tripDay
		var parts [][]tripDay
		for i, d := range f.DayInfo {
			cut := false
			if i > 0 {
				for sd := range splits {
					if sd > f.DayInfo[i-1].Date && sd <= d.Date {
						cut = true
					}
				}
			}
			if cut && len(part) > 0 {
				parts = append(parts, part)
				part = nil
			}
			part = append(part, d)
		}
		parts = append(parts, part)
		for i, p := range parts {
			t := buildPhotoTrip(p)
			t.SplitBefore = i > 0
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start > out[j].Start })
	return out
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

// A scan of someone's whole photo library, running in the background: big
// libraries take minutes, far longer than a request (or a proxy) waits.
type photoScan struct {
	running bool
	seen    int // photos looked at so far
	located int // of them with a location
	started time.Time
	err     string
}

var (
	scansMu sync.Mutex
	scans   = map[int64]*photoScan{}
)

func scanState(uid int64) photoScan {
	scansMu.Lock()
	defer scansMu.Unlock()
	if sc := scans[uid]; sc != nil {
		return *sc
	}
	return photoScan{}
}

// handlePhotoTrips finds trips in the viewer's photo locations and dates:
// days whose photos were taken far from home, in runs, where no trip of
// theirs is. The answer is kept a day; without one (or with ?refresh=1) a
// scan starts in the background and the answer says how far it got
// (state scanning | done | error); the page asks again until it is done.
func (s *Server) handlePhotoTrips(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ctx := r.Context()
	_, _, ppErr := s.photoprismFor(ctx, u.ID)
	_, _, imErr := s.immichFor(ctx, u.ID)
	if ppErr != nil && imErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "state": "done", "trips": []PhotoTrip{}})
		return
	}
	key := photoTripsKey(u.ID)
	sc := scanState(u.ID)
	if sc.running {
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "state": "scanning", "seen": sc.seen, "located": sc.located, "started_at": sc.started.Format(time.RFC3339)})
		return
	}
	if !queryBool(r, "refresh") {
		if v, at, ok := s.store.CacheGet(ctx, key); ok {
			if ts, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(ts) < 24*time.Hour {
				var c struct {
					Trips   []PhotoTrip `json:"trips"`
					Sources []string    `json:"sources"`
					Home    PhotoHome   `json:"home"`
					Seen    int         `json:"seen"`
					Located int         `json:"located"`
				}
				if json.Unmarshal([]byte(v), &c) == nil {
					// drop what a trip now covers and what was dismissed (both
					// change without a rescan)
					trips, _ := s.store.ListTrips(ctx, store.TripFilter{UserID: u.ID})
					dismissed, _ := s.store.DismissedPhotoTrips(ctx, u.ID)
					splits, _ := s.store.PhotoTripSplits(ctx, u.ID)
					out := []PhotoTrip{}
					for _, f := range splitPhotoTrips(c.Trips, splits) {
						if !dismissed[f.Start] && !coveredByTrip(f, trips) {
							f.DayInfo = nil // only needed to split
							out = append(out, f)
						}
					}
					writeJSON(w, http.StatusOK, map[string]any{"connected": true, "state": "done", "trips": out, "sources": c.Sources, "home": c.Home, "seen": c.Seen, "located": c.Located, "scanned_at": ts.Format(time.RFC3339)})
					return
				}
			}
		}
		if sc.err != "" { // the last scan failed: say so until asked to try again
			writeJSON(w, http.StatusOK, map[string]any{"connected": true, "state": "error", "error": sc.err, "seen": sc.seen})
			return
		}
	}
	s.startPhotoScan(u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "state": "scanning", "seen": 0, "started_at": time.Now().Format(time.RFC3339)})
}

func photoTripsKey(uid int64) string { return "user:" + strconv.FormatInt(uid, 10) + ":photo-trips" }

// startPhotoScan looks through uid's libraries in the background and keeps
// the trips found (unless a scan is already running).
func (s *Server) startPhotoScan(uid int64) {
	scansMu.Lock()
	if sc := scans[uid]; sc != nil && sc.running {
		scansMu.Unlock()
		return
	}
	sc := &photoScan{running: true, started: time.Now()}
	scans[uid] = sc
	scansMu.Unlock()
	progress := func(base int) func(int) {
		return func(n int) { scansMu.Lock(); sc.seen = base + n; scansMu.Unlock() }
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		var pts []geoPhoto
		var errs, sources []string
		if pp, public, err := s.photoprismFor(ctx, uid); err == nil {
			pp.HTTP.Timeout = 2 * time.Minute // one big batch can be slow on a busy box
			pp.Progress = progress(0)
			ps, err := pp.Search(ctx, "", "", true, 0)
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
		if im, conn, err := s.immichFor(ctx, uid); err == nil {
			im.HTTP.Timeout = 2 * time.Minute
			im.Progress = progress(scanState(uid).seen)
			as, err := im.Search(ctx, time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().AddDate(0, 0, 1), 0)
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
		finish := func(e string) {
			scansMu.Lock()
			sc.running, sc.err, sc.located = false, e, len(pts)
			scansMu.Unlock()
		}
		if len(sources) == 0 {
			msg := strings.Join(errs, "; ")
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				msg = "the library took longer than 20 minutes to look through (" + msg + ")"
			}
			s.log.Warn("photo trip scan failed", "user", uid, "err", msg)
			finish(firstNonEmpty(msg, "no photo library answered"))
			return
		}
		st, _ := s.store.GetSettings(ctx, uid)
		var set *[2]float64
		if st.HomeLat != nil && st.HomeLng != nil {
			set = &[2]float64{*st.HomeLat, *st.HomeLng}
		}
		found, home := findPhotoTrips(pts, set)
		if set != nil {
			home.Name, home.Set = firstNonEmpty(st.HomeName, home.Name), true
		}
		seen := scanState(uid).seen
		if b, err := json.Marshal(map[string]any{"trips": found, "sources": sources, "home": home, "seen": seen, "located": len(pts)}); err == nil {
			_ = s.store.CachePut(context.Background(), photoTripsKey(uid), string(b))
		}
		s.log.Info("photo trip scan done", "user", uid, "photos", seen, "located", len(pts), "trips", len(found), "took", time.Since(sc.started).Round(time.Second))
		finish("")
	}()
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
	gap := dayGap
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
		var ds []tripDay
		for _, d := range run {
			ds = append(ds, summarizeDay(d, away[d], hl, hg))
		}
		f := buildPhotoTrip(ds)
		if f.Days == 1 && f.Km < 300 {
			continue
		}
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

// handlePlaceFromPhotoSuggest fills in a new place from a photo's location:
// its name, address and category from the map (OpenStreetMap). Nothing is
// saved; the place form opens with these and the photo's day and time.
func (s *Server) handlePlaceFromPhotoSuggest(w http.ResponseWriter, r *http.Request) {
	lat, err1 := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, err2 := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "this photo has no location")
		return
	}
	out := map[string]any{"lat": lat, "lng": lng}
	gctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if g, err := s.geo.Reverse(gctx, lat, lng); err == nil && g != nil {
		out["name"], out["address"], out["country_code"] = g.Name, g.Display, g.CountryCode
		if g.Suggest != "" {
			out["category"] = g.Suggest
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAttachLibraryPhoto adds a photo from the viewer's Immich or
// PhotoPrism to a place (the large preview is copied in).
func (s *Server) handleAttachLibraryPhoto(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	p, err := s.store.GetPlace(ctx, t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get place")
		return
	}
	var in struct{ Src, ID, Hash string }
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(ctx)
	var img []byte
	var ct string
	switch in.Src {
	case "photoprism":
		pp, _, e := s.photoprismFor(ctx, uid)
		if e != nil {
			writeErr(w, http.StatusBadRequest, e.Error())
			return
		}
		img, ct, err = pp.ThumbnailBytes(ctx, cleanID(in.Hash, isHex), "preview")
	default:
		im, _, e := s.immichFor(ctx, uid)
		if e != nil {
			writeErr(w, http.StatusBadRequest, "immich: "+e.Error())
			return
		}
		img, ct, err = im.ThumbnailBytes(ctx, cleanID(in.ID, func(c rune) bool { return c == '-' || isHex(c) }), "preview")
	}
	if err != nil || len(img) == 0 || !strings.HasPrefix(ct, "image/") {
		writeErr(w, http.StatusBadGateway, "could not get the photo from the library")
		return
	}
	rel, err := s.saveFile(t.ID, extOf("", ct), img)
	if err != nil {
		s.fail(w, err, "save image")
		return
	}
	if err := s.store.AddPlacePhoto(ctx, &store.PlacePhoto{PlaceID: p.ID, Path: rel, Source: in.Src}); err != nil {
		s.fail(w, err, "add photo")
		return
	}
	list, _ := s.store.ListPlacePhotos(ctx, p.ID)
	writeJSON(w, http.StatusCreated, list)
}

// Sources (a phone, a folder, WhatsApp…) the viewer left out of a trip's
// photos; kept with the hidden photos as "source:<key>".
func (s *Server) handleGetPhotoSources(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	hidden, _ := s.store.HiddenPhotos(r.Context(), t.ID, actorID(r.Context()))
	off := []string{}
	for k := range hidden {
		if strings.HasPrefix(k, "source:") {
			off = append(off, strings.TrimPrefix(k, "source:"))
		}
	}
	sort.Strings(off)
	writeJSON(w, http.StatusOK, map[string]any{"off": off})
}

func (s *Server) handlePutPhotoSources(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	uid := actorID(r.Context())
	var in struct {
		Off []string `json:"off"`
	}
	if err := decode(r, &in); err != nil || len(in.Off) > 200 {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	hidden, _ := s.store.HiddenPhotos(r.Context(), t.ID, uid)
	var drop, add []string
	want := map[string]bool{}
	for _, k := range in.Off {
		if k = strings.TrimSpace(k); k != "" && len(k) < 200 {
			want["source:"+k] = true
		}
	}
	for k := range hidden {
		if strings.HasPrefix(k, "source:") && !want[k] {
			drop = append(drop, k)
		}
	}
	for k := range want {
		if !hidden[k] {
			add = append(add, k)
		}
	}
	if err := s.store.SetPhotosHidden(r.Context(), t.ID, uid, drop, false); err != nil {
		s.fail(w, err, "sources")
		return
	}
	if err := s.store.SetPhotosHidden(r.Context(), t.ID, uid, add, true); err != nil {
		s.fail(w, err, "sources")
		return
	}
	s.handleGetPhotoSources(w, r)
}

// PreviewDay is one day around a found trip, for checking it before it is
// added: how many photos, where, and whether it counts as away from home.
type PreviewDay struct {
	Date      string      `json:"date"`
	Photos    int         `json:"photos"`
	Located   int         `json:"located"`
	Away      bool        `json:"away"`
	Km        int         `json:"km"` // farthest photo from home that day
	Cities    []string    `json:"cities"`
	Countries []string    `json:"countries"`
	Lat       *float64    `json:"lat,omitempty"` // where most of the day's photos are
	Lng       *float64    `json:"lng,omitempty"`
	Sample    []tripPhoto `json:"sample"` // a few, spread over the day
	All       []tripPhoto `json:"all"`
}

// handlePhotoTripPreview shows the days of a found trip with three on each
// side, so a trip found a day off (a home morning, a late return) can be
// moved before it is added.
func (s *Server) handlePhotoTripPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	start, err1 := time.Parse("2006-01-02", r.URL.Query().Get("start"))
	end, err2 := time.Parse("2006-01-02", r.URL.Query().Get("end"))
	if err1 != nil || err2 != nil || end.Before(start) || end.Sub(start) > 120*24*time.Hour {
		writeErr(w, http.StatusBadRequest, "start and end dates are required")
		return
	}
	from, to := start.AddDate(0, 0, -3), end.AddDate(0, 0, 3)
	fromS, toS := from.Format("2006-01-02"), to.Format("2006-01-02")
	var photos []tripPhoto
	var errs []string
	if pp, public, err := s.photoprismFor(ctx, u.ID); err == nil {
		ps, err := pp.Search(ctx, fromS, toS, false, 5000)
		if err != nil {
			errs = append(errs, "PhotoPrism: "+err.Error())
		}
		for _, p := range ps {
			photos = append(photos, fromPhotoPrism(p, public))
		}
	}
	if im, conn, err := s.immichFor(ctx, u.ID); err == nil {
		as, err := im.Search(ctx, from.AddDate(0, 0, -1), to.AddDate(0, 0, 2), 5000)
		if err != nil {
			errs = append(errs, "Immich: "+err.Error())
		}
		for _, a := range as {
			if len(a.TakenAt) >= 10 && a.TakenAt[:10] >= fromS && a.TakenAt[:10] <= toS {
				photos = append(photos, fromImmich(a, conn.public))
			}
		}
	}
	if len(photos) == 0 && len(errs) > 0 {
		writeErr(w, http.StatusBadGateway, strings.Join(errs, "; "))
		return
	}
	sort.SliceStable(photos, func(i, j int) bool { return photos[i].TakenAt < photos[j].TakenAt })
	// home: set by hand, else the one the last scan detected
	var home *PhotoHome
	if st, _ := s.store.GetSettings(ctx, u.ID); st.HomeLat != nil && st.HomeLng != nil {
		home = &PhotoHome{Name: st.HomeName, Lat: *st.HomeLat, Lng: *st.HomeLng, Set: true}
	} else if v, _, ok := s.store.CacheGet(ctx, photoTripsKey(u.ID)); ok {
		var c struct{ Home PhotoHome }
		if json.Unmarshal([]byte(v), &c) == nil && (c.Home.Lat != 0 || c.Home.Lng != 0) {
			home = &c.Home
		}
	}
	byDay := map[string][]tripPhoto{}
	for _, p := range photos {
		if len(p.TakenAt) >= 10 {
			byDay[p.TakenAt[:10]] = append(byDay[p.TakenAt[:10]], p)
		}
	}
	days := []PreviewDay{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		ps := byDay[ds]
		day := PreviewDay{Date: ds, Photos: len(ps), Cities: []string{}, Countries: []string{}, Sample: []tripPhoto{}, All: ps}
		if day.All == nil {
			day.All = []tripPhoto{}
		}
		cities, ctry := map[string]int{}, map[string]int{}
		var lats, lngs []float64
		far := 0
		for _, p := range ps {
			if p.City != "" {
				cities[p.City]++
			}
			if p.Country != "" {
				ctry[p.Country]++
			}
			if p.Lat == nil || p.Lng == nil {
				continue
			}
			day.Located++
			lats, lngs = append(lats, *p.Lat), append(lngs, *p.Lng)
			if home != nil {
				km := distKm(home.Lat, home.Lng, *p.Lat, *p.Lng)
				if int(km) > day.Km {
					day.Km = int(km)
				}
				if km > awayKm {
					far++
				}
			}
		}
		day.Away = far > 0 && far*10 >= day.Located*3 // the rule trips are found by
		day.Cities, day.Countries = topKeys(cities, 3), topKeys(ctry, 3)
		if len(lats) > 0 { // the middle photo's spot, not an average across a travel day
			sort.Float64s(lats)
			sort.Float64s(lngs)
			la, lo := lats[len(lats)/2], lngs[len(lngs)/2]
			day.Lat, day.Lng = &la, &lo
		}
		for i := 0; i < 6 && i < len(ps); i++ {
			day.Sample = append(day.Sample, ps[i*len(ps)/min(6, len(ps))])
		}
		days = append(days, day)
	}
	out := map[string]any{"days": days, "home": home, "start_date": start.Format("2006-01-02"), "end_date": end.Format("2006-01-02")}
	if len(errs) > 0 {
		out["warning"] = strings.Join(errs, "; ")
	}
	writeJSON(w, http.StatusOK, out)
}

// topKeys are the n most frequent keys, most frequent first.
func topKeys(m map[string]int, n int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] || m[keys[i]] == m[keys[j]] && keys[i] < keys[j] })
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys
}

// handleSplitPhotoTrip: a found trip is two (or more): the one before ends
// before this date and the next starts with it. {"date": "2025-07-20"}
func (s *Server) handleSplitPhotoTrip(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date string `json:"date"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := time.Parse("2006-01-02", in.Date); err != nil {
		writeErr(w, http.StatusBadRequest, "date is required")
		return
	}
	if err := s.store.AddPhotoTripSplit(r.Context(), userFrom(r.Context()).ID, in.Date); err != nil {
		s.fail(w, err, "split")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleJoinPhotoTrip undoes a split: the trip starting at date joins the
// one before it. {"date": "<that trip's start>"}
func (s *Server) handleJoinPhotoTrip(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date string `json:"date"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date is required")
		return
	}
	// the split may sit on a day without photos just before the part's start
	from := d.AddDate(0, 0, -(maxGapDays + 1)).Format("2006-01-02")
	if err := s.store.RemovePhotoTripSplits(r.Context(), userFrom(r.Context()).ID, from, in.Date); err != nil {
		s.fail(w, err, "join")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
