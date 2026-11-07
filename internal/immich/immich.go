// Package immich finds the photos taken during a trip and proxies their
// thumbnails so the browser never needs the API key.
package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to one Immich server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// Progress, when set, hears how many assets a search has seen so far.
	Progress func(seen int)
}

// New creates a client.
func New(baseURL, key string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: key, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and key are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.APIKey != "" }

// Asset is one photo or video.
type Asset struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"` // IMAGE | VIDEO
	FileName  string   `json:"file_name"`
	TakenAt   string   `json:"taken_at"` // local time, RFC3339
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
	City      string   `json:"city"`
	Country   string   `json:"country"`
	Thumbhash string   `json:"thumbhash"`
	Favorite  bool     `json:"favorite"`
	Duration  string   `json:"duration,omitempty"`
	Camera    string   `json:"camera,omitempty"`
	Path      string   `json:"path,omitempty"` // folder of the original
}

func (c *Client) req(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if !c.Configured() {
		return nil, errors.New("immich is not configured")
	}
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.HTTP.Do(req)
}

// Version returns the server version (connection test).
func (c *Client) Version(ctx context.Context) (string, error) {
	res, err := c.req(ctx, http.MethodGet, "/api/server/version", nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return "", errors.New("immich rejected the API key (401)")
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("immich HTTP %d", res.StatusCode)
	}
	var v struct {
		Major, Minor, Patch int
	}
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch), nil
}

// Search returns every asset taken between two instants (inclusive start, exclusive end).
func (c *Client) Search(ctx context.Context, after, before time.Time, limit int) ([]Asset, error) {
	var out []Asset
	for page := 1; page < 400; page++ {
		body := map[string]any{"takenAfter": after.UTC().Format(time.RFC3339), "takenBefore": before.UTC().Format(time.RFC3339), "size": 1000, "page": page, "withExif": true, "order": "asc", "visibility": "timeline"}
		res, err := c.req(ctx, http.MethodPost, "/api/search/metadata", body)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("immich search HTTP %d", res.StatusCode)
		}
		var r struct {
			Assets struct {
				Items []struct {
					ID               string          `json:"id"`
					Type             string          `json:"type"`
					OriginalFileName string          `json:"originalFileName"`
					LocalDateTime    string          `json:"localDateTime"`
					Thumbhash        string          `json:"thumbhash"`
					IsFavorite       bool            `json:"isFavorite"`
					Duration         json.RawMessage `json:"duration"`
					OriginalPath     string          `json:"originalPath"`
					ExifInfo         *struct {
						Make      *string  `json:"make"`
						Model     *string  `json:"model"`
						Latitude  *float64 `json:"latitude"`
						Longitude *float64 `json:"longitude"`
						City      *string  `json:"city"`
						Country   *string  `json:"country"`
					} `json:"exifInfo"`
				} `json:"items"`
				NextPage *string `json:"nextPage"`
			} `json:"assets"`
		}
		err = json.NewDecoder(res.Body).Decode(&r)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, it := range r.Assets.Items {
			a := Asset{ID: it.ID, Type: it.Type, FileName: it.OriginalFileName, TakenAt: it.LocalDateTime, Thumbhash: it.Thumbhash, Favorite: it.IsFavorite}
			if it.Type == "VIDEO" {
				a.Duration = durationText(it.Duration)
			}
			if i := strings.LastIndex(it.OriginalPath, "/"); i > 0 {
				a.Path = it.OriginalPath[:i]
			}
			if it.ExifInfo != nil {
				if it.ExifInfo.Model != nil && *it.ExifInfo.Model != "" {
					mk := ""
					if it.ExifInfo.Make != nil {
						mk = *it.ExifInfo.Make
					}
					a.Camera = strings.TrimSpace(mk + " " + *it.ExifInfo.Model)
				}
				a.Lat, a.Lng = it.ExifInfo.Latitude, it.ExifInfo.Longitude
				if it.ExifInfo.City != nil {
					a.City = *it.ExifInfo.City
				}
				if it.ExifInfo.Country != nil {
					a.Country = *it.ExifInfo.Country
				}
			}
			out = append(out, a)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
		if c.Progress != nil {
			c.Progress(len(out))
		}
		if r.Assets.NextPage == nil || len(r.Assets.Items) == 0 {
			break
		}
	}
	return out, nil
}

// Thumbnail streams a thumbnail (size thumbnail|preview) to w.
func (c *Client) Thumbnail(ctx context.Context, w http.ResponseWriter, id, size string) error {
	if size != "preview" {
		size = "thumbnail"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/assets/"+id+"/thumbnail?size="+size, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "image/*")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("immich thumbnail HTTP %d", res.StatusCode)
	}
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, err = io.Copy(w, res.Body)
	return err
}

// ThumbnailBytes returns a thumbnail (size thumbnail|preview) and its type.
func (c *Client) ThumbnailBytes(ctx context.Context, id, size string) ([]byte, string, error) {
	var b bufWriter
	err := c.Thumbnail(ctx, &b, id, size)
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

// durationText accepts Immich's duration as "0:00:12.000" (older) or milliseconds (3.x).
func durationText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		sec := int(f/1000 + 0.5)
		return fmt.Sprintf("0:%02d:%02d.000", sec/60, sec%60)
	}
	return ""
}
