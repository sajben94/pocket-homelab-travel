package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// A fake PhotoPrism: Bratislava (home) most days, Rome for four days in May
// 2025 with one photo-less day inside, and a trip already covering August.
func fakePhotoPrism(t *testing.T) *httptest.Server {
	type ph struct {
		UID          string
		Type         string
		TakenAtLocal string
		Hash         string
		Lat, Lng     float64
		PlaceCity    string
		PlaceCountry string
		FileName     string
	}
	var all []ph
	add := func(day string, n int, lat, lng float64, city, cc string) {
		for i := 0; i < n; i++ {
			k := len(all)
			all = append(all, ph{UID: fmt.Sprintf("pq%04d", k), Type: "image", TakenAtLocal: fmt.Sprintf("%sT1%d:00:00Z", day, i%10), Hash: fmt.Sprintf("%040x", k+1), Lat: lat, Lng: lng, PlaceCity: city, PlaceCountry: cc, FileName: fmt.Sprintf("2025/IMG_%d.jpg", k)})
		}
	}
	for d := 1; d <= 28; d++ {
		add(fmt.Sprintf("2025-04-%02d", d), 2, 48.1486, 17.1077, "Bratislava", "sk")
	}
	add("2025-05-01", 3, 48.15, 17.11, "Bratislava", "sk") // the morning at home
	add("2025-05-01", 6, 41.9028, 12.4964, "Rome", "it")
	add("2025-05-02", 10, 41.9028, 12.4964, "Rome", "it")
	// 05-03 no photos
	add("2025-05-04", 8, 41.89, 12.49, "Rome", "it")
	add("2025-05-05", 2, 48.1486, 17.1077, "Bratislava", "sk")
	add("2025-06-10", 4, 48.30, 17.20, "Pezinok", "sk")    // a 20 km day out: not a trip
	add("2025-08-02", 9, 45.8150, 15.9819, "Zagreb", "hr") // covered by a trip
	add("2025-08-03", 9, 45.8150, 15.9819, "Zagreb", "hr")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer app-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/photos":
			w.Header().Set("X-Preview-Token", "prev123")
			w.Header().Set("X-Version", "240915")
			q := r.URL.Query()
			var out []ph
			for _, p := range all {
				d := p.TakenAtLocal[:10]
				if a := q.Get("after"); a != "" && d < a {
					continue
				}
				if b := q.Get("before"); b != "" && d >= b {
					continue
				}
				out = append(out, p)
			}
			off, _ := strconv.Atoi(q.Get("offset"))
			cnt, _ := strconv.Atoi(q.Get("count"))
			if off > len(out) {
				off = len(out)
			}
			end := off + cnt
			if cnt == 0 || end > len(out) {
				end = len(out)
			}
			_ = json.NewEncoder(w).Encode(out[off:end])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestPhotoPrismPhotosHiddenAndFoundTrips(t *testing.T) {
	h := newIsoHarness(t)
	pp := fakePhotoPrism(t)
	defer pp.Close()
	tok := h.ownerTok

	if c, b := h.do(tok, "PUT", "/api/v1/settings", map[string]any{"photoprism_url": pp.URL, "photoprism_token": "app-pass"}); c != 200 {
		t.Fatalf("settings %d %s", c, b)
	}
	var tr struct{ OK bool }
	if c, b := h.do(tok, "POST", "/api/v1/integrations/test", map[string]any{"which": "photoprism"}); c != 200 || json.Unmarshal(b, &tr) != nil || !tr.OK {
		t.Fatalf("connection test %d %s", c, b)
	}

	// Rome trip: the photos of its days, the home ones from the first morning too
	c, b := h.do(tok, "POST", "/api/v1/trips", map[string]any{"name": "Rome", "start_date": "2025-05-01", "end_date": "2025-05-04"})
	if c != 201 {
		t.Fatalf("create %d %s", c, b)
	}
	var trip struct{ ID int64 }
	_ = json.Unmarshal(b, &trip)
	base := fmt.Sprintf("/api/v1/trips/%d", trip.ID)
	var photos []tripPhoto
	c, b = h.do(tok, "GET", base+"/photos", nil)
	if c != 200 || json.Unmarshal(b, &photos) != nil {
		t.Fatalf("photos %d %s", c, b)
	}
	if len(photos) != 27 {
		t.Fatalf("want 27 photos (3 home + 24 Rome), got %d", len(photos))
	}
	p0 := photos[0]
	if p0.Src != "photoprism" || p0.Hash == "" || p0.Country != "Slovakia" || p0.FileName != "IMG_56.jpg" || p0.TakenAt != "2025-05-01T10:00:00" {
		t.Fatalf("photo shape: %+v", p0)
	}

	// hide the three taken at home; they come back marked hidden, for this user only
	keys := []string{}
	for _, p := range photos {
		if p.City == "Bratislava" {
			keys = append(keys, p.key())
		}
	}
	if c, b := h.do(tok, "PUT", base+"/photos/hidden", map[string]any{"photos": keys, "hidden": true}); c != 200 {
		t.Fatalf("hide %d %s", c, b)
	}
	_, b = h.do(tok, "GET", base+"/photos", nil)
	_ = json.Unmarshal(b, &photos)
	hidden := 0
	for _, p := range photos {
		if p.Hidden {
			hidden++
		}
	}
	if hidden != 3 {
		t.Fatalf("want 3 hidden, got %d", hidden)
	}
	if c, _ := h.do(tok, "PUT", base+"/photos/hidden", map[string]any{"photos": keys[:1], "hidden": false}); c != 200 {
		t.Fatal("unhide")
	}

	// thumbnails go through Travel with the preview token
	thumbHits := 0
	pp.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/t/"+p0.Hash+"/prev123/tile_500" {
			thumbHits++
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("jpeg"))
			return
		}
		w.WriteHeader(404)
	})
	if c, b := h.do(tok, "GET", base+"/photos/"+p0.ID+"/thumb?src=photoprism&h="+p0.Hash, nil); c != 200 || string(b) != "jpeg" || thumbHits != 1 {
		t.Fatalf("thumb %d %s hits=%d", c, b, thumbHits)
	}
	pp.Close()

	// found trips: Rome is covered by the trip above, so add Zagreb's trip
	// and check what is left with a fresh library
	pp2 := fakePhotoPrism(t)
	defer pp2.Close()
	h.do(tok, "PUT", "/api/v1/settings", map[string]any{"photoprism_url": pp2.URL})
	h.do(tok, "DELETE", base, nil)
	h.do(tok, "POST", "/api/v1/trips", map[string]any{"name": "Zagreb", "start_date": "2025-08-01", "end_date": "2025-08-04"})
	var res struct {
		Trips     []PhotoTrip
		Connected bool
	}
	c, b = h.do(tok, "GET", "/api/v1/photo-trips?refresh=1", nil)
	if c != 200 || json.Unmarshal(b, &res) != nil {
		t.Fatalf("photo-trips %d %s", c, b)
	}
	if !res.Connected || len(res.Trips) != 1 {
		t.Fatalf("want only Rome, got %s", b)
	}
	f := res.Trips[0]
	if f.Start != "2025-05-01" || f.End != "2025-05-04" || f.Days != 4 || f.Photos != 24 || f.Name != "Rome 2025" || f.Place != "Rome, Italy" || len(f.Countries) != 1 || f.Countries[0] != "IT" || f.Cover == nil {
		t.Fatalf("found trip: %+v", f)
	}
	if f.Km < 700 || f.Km > 900 {
		t.Fatalf("km from home %d", f.Km)
	}
	// dismissed: gone, from the cache too
	h.do(tok, "POST", "/api/v1/photo-trips/dismiss", map[string]any{"start_date": "2025-05-01"})
	_, b = h.do(tok, "GET", "/api/v1/photo-trips", nil)
	_ = json.Unmarshal(b, &res)
	if len(res.Trips) != 0 {
		t.Fatalf("dismissed trip still listed: %s", b)
	}
	// another user's library is their own: nothing connected
	_, b = h.do(h.memberTok, "GET", "/api/v1/photo-trips", nil)
	_ = json.Unmarshal(b, &res)
	if res.Connected {
		t.Fatalf("member sees owner's PhotoPrism: %s", b)
	}
}
