// Package photoprism finds the photos taken during a trip in PhotoPrism and
// proxies their thumbnails, so the browser never needs the app password.
//
// Auth is an app password (PhotoPrism → Settings → Account → Apps and
// Devices) sent as a Bearer token; an instance running without
// authentication works with an empty one.
package photoprism

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to one PhotoPrism server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether the URL is set (the token may be empty).
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" }

// Photo is one picture or video, shaped like the Immich assets the UI shows.
type Photo struct {
	ID       string   `json:"id"`   // PhotoPrism UID
	Hash     string   `json:"hash"` // primary file hash: what thumbnails are fetched by
	Type     string   `json:"type"` // IMAGE | VIDEO
	FileName string   `json:"file_name"`
	TakenAt  string   `json:"taken_at"` // local time, "2006-01-02T15:04:05"
	Lat      *float64 `json:"lat"`
	Lng      *float64 `json:"lng"`
	City     string   `json:"city"`
	Country  string   `json:"country"` // ISO code, upper case ("" when unknown)
	Favorite bool     `json:"favorite"`
	Duration string   `json:"duration,omitempty"`
	Camera   string   `json:"camera,omitempty"` // "Apple iPhone 15 Pro"
	Path     string   `json:"path,omitempty"`   // folder in originals
}

func (c *Client) do(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	if !c.Configured() {
		return nil, errors.New("photoprism is not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("X-Auth-Token", c.Token) // older versions
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if t := res.Header.Get("X-Preview-Token"); t != "" {
		setPreviewToken(c.BaseURL, t)
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		res.Body.Close()
		return nil, fmt.Errorf("photoprism rejected the app password (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("photoprism HTTP %d", res.StatusCode)
	}
	return res, nil
}

// Version returns the server version (connection test); it also checks
// that the app password may search.
func (c *Client) Version(ctx context.Context) (string, error) {
	res, err := c.do(ctx, "/api/v1/photos", url.Values{"count": {"1"}})
	if err != nil {
		return "", err
	}
	res.Body.Close()
	v := res.Header.Get("X-Version")
	if v == "" {
		v = "reachable"
	}
	return v, nil
}

type rawPhoto struct {
	UID          string  `json:"UID"`
	Type         string  `json:"Type"`
	TakenAtLocal string  `json:"TakenAtLocal"`
	TakenAt      string  `json:"TakenAt"`
	FileName     string  `json:"FileName"`
	OriginalName string  `json:"OriginalName"`
	Name         string  `json:"Name"`
	Hash         string  `json:"Hash"`
	Favorite     bool    `json:"Favorite"`
	Lat          float64 `json:"Lat"`
	Lng          float64 `json:"Lng"`
	PlaceCity    string  `json:"PlaceCity"`
	PlaceCountry string  `json:"PlaceCountry"`
	Duration     int64   `json:"Duration"` // nanoseconds
	CameraMake   string  `json:"CameraMake"`
	CameraModel  string  `json:"CameraModel"`
	Path         string  `json:"Path"`
}

func (r rawPhoto) photo() Photo {
	p := Photo{ID: r.UID, Hash: r.Hash, Type: "IMAGE", FileName: r.FileName, Favorite: r.Favorite, City: r.PlaceCity, Path: r.Path}
	if r.CameraModel != "" && r.CameraModel != "Unknown" {
		p.Camera = strings.TrimSpace(strings.TrimPrefix(r.CameraMake, "Unknown") + " " + r.CameraModel)
	}
	if p.Path == "" {
		if i := strings.LastIndex(r.FileName, "/"); i > 0 {
			p.Path = r.FileName[:i]
		}
	}
	if p.FileName == "" {
		p.FileName = firstNonEmpty(r.OriginalName, r.Name)
	}
	if i := strings.LastIndex(p.FileName, "/"); i >= 0 {
		p.FileName = p.FileName[i+1:]
	}
	if r.Type == "video" || r.Type == "live" && r.Duration > 3e9 {
		p.Type = "VIDEO"
		sec := int(r.Duration / 1e9)
		p.Duration = fmt.Sprintf("0:%02d:%02d.000", sec/60, sec%60)
	}
	// TakenAtLocal is local wall time written with a "Z"; drop the zone.
	t := firstNonEmpty(r.TakenAtLocal, r.TakenAt)
	if len(t) >= 19 {
		t = t[:19]
	}
	p.TakenAt = t
	if r.Lat != 0 || r.Lng != 0 {
		lat, lng := r.Lat, r.Lng
		p.Lat, p.Lng = &lat, &lng
	}
	if cc := strings.ToUpper(r.PlaceCountry); len(cc) == 2 && cc != "ZZ" {
		p.Country = cc
	}
	if p.City == "Unknown" {
		p.City = ""
	}
	return p
}

// Search returns the photos taken on the local days from..to (inclusive,
// "2006-01-02"), oldest first. geoOnly keeps only located ones.
func (c *Client) Search(ctx context.Context, from, to string, geoOnly bool, limit int) ([]Photo, error) {
	q := url.Values{"merged": {"true"}, "order": {"oldest"}, "count": {"500"}}
	// PhotoPrism compares against UTC; widen by a day and trim on local time.
	if from != "" {
		if d, err := time.Parse("2006-01-02", from); err == nil {
			q.Set("after", d.AddDate(0, 0, -1).Format("2006-01-02"))
		}
	}
	if to != "" {
		if d, err := time.Parse("2006-01-02", to); err == nil {
			q.Set("before", d.AddDate(0, 0, 2).Format("2006-01-02"))
		}
	}
	if geoOnly {
		q.Set("geo", "true")
	}
	var out []Photo
	seen := map[string]bool{}
	// With merged=true "count" limits files, not photos: a photo with a RAW and
	// a JPEG (or a live photo) takes two, so a full batch can hold fewer than
	// 500 photos. Move on by the photos received (a batch that overlaps the
	// next is harmless, they are de-duplicated) and stop only on an empty one.
	for offset, n := 0, 0; n < 2000; n++ {
		q.Set("offset", strconv.Itoa(offset))
		res, err := c.do(ctx, "/api/v1/photos", q)
		if err != nil {
			return nil, err
		}
		var page []rawPhoto
		err = json.NewDecoder(res.Body).Decode(&page)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if r.UID == "" || seen[r.UID] {
				continue
			}
			seen[r.UID] = true
			p := r.photo()
			day := p.TakenAt
			if len(day) >= 10 {
				day = day[:10]
			}
			if (from != "" && day < from) || (to != "" && day > to) {
				continue
			}
			if geoOnly && p.Lat == nil {
				continue
			}
			out = append(out, p)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
		if len(page) == 0 {
			break
		}
		offset += len(page)
	}
	return out, nil
}

// thumbnail sizes PhotoPrism always has
var sizes = map[string]string{"thumbnail": "tile_500", "preview": "fit_1920"}

// Thumbnail streams a thumbnail (size thumbnail|preview) of the file with
// this hash to w.
func (c *Client) Thumbnail(ctx context.Context, w http.ResponseWriter, hash, size string) error {
	sz, ok := sizes[size]
	if !ok {
		sz = sizes["thumbnail"]
	}
	tok := previewToken(c.BaseURL)
	if tok == "" { // any authenticated call tells it
		if _, err := c.Version(ctx); err != nil {
			return err
		}
		if tok = previewToken(c.BaseURL); tok == "" {
			tok = "public"
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/t/"+hash+"/"+url.PathEscape(tok)+"/"+sz, nil)
	if err != nil {
		return err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		setPreviewToken(c.BaseURL, "") // stale token: ask again next time
		return fmt.Errorf("photoprism thumbnail HTTP %d", res.StatusCode)
	}
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, err = io.Copy(w, res.Body)
	return err
}

// ThumbnailBytes returns a thumbnail (size thumbnail|preview) and its type.
func (c *Client) ThumbnailBytes(ctx context.Context, hash, size string) ([]byte, string, error) {
	var b bufWriter
	err := c.Thumbnail(ctx, &b, hash, size)
	return b.buf.Bytes(), b.h.Get("Content-Type"), err
}

type bufWriter struct {
	buf bytes.Buffer
	h   http.Header
}

func (b *bufWriter) Header() http.Header {
	if b.h == nil {
		b.h = http.Header{}
	}
	return b.h
}
func (b *bufWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }
func (b *bufWriter) WriteHeader(int)             {}

// OpenURL is the PhotoPrism page that shows one photo.
func OpenURL(public, uid string) string {
	return strings.TrimRight(public, "/") + "/library/browse?view=cards&q=" + url.QueryEscape("uid:"+uid)
}

// Preview tokens are per server and long-lived; remember them between calls.
var (
	tokMu  sync.Mutex
	tokens = map[string]string{}
)

func previewToken(base string) string { tokMu.Lock(); defer tokMu.Unlock(); return tokens[base] }
func setPreviewToken(base, t string) {
	tokMu.Lock()
	defer tokMu.Unlock()
	if t == "" {
		delete(tokens, base)
	} else {
		tokens[base] = t
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
