// Package pockettrails reads Pocket Trails, the person's own hikes, rides
// and the routes they take. Travel only reads the outings of a trip, and
// only through Pocket Core, with a five-minute pcx_ link token for
// trails:read.
package pockettrails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client is one person's Pocket Trails, for one piece of work.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client: baseURL is Trails' address from Core, token the
// pcx_ link token.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Outing is a hike, ride or other outing, as Travel hands it on. Distances
// and heights are in metres, Minutes is the route's time; StartTime is
// "HH:MM" (may be empty), BackBy "YYYY-MM-DDTHH:MM" (may be empty).
type Outing struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Activity  string  `json:"activity"` // hike | bike | run | …
	Date      string  `json:"date"`
	StartTime string  `json:"start_time"`
	BackBy    string  `json:"back_by"`
	Status    string  `json:"status"` // planned | out | done | cancelled
	RouteName string  `json:"route_name"`
	DistanceM float64 `json:"distance_m"`
	GainM     float64 `json:"gain_m"`
	Minutes   int     `json:"minutes"`
	TripRef   string  `json:"trip_ref"`
	TripName  string  `json:"trip_name"`
}

// TripOutings is the trip's outings: the ones filed with it in Trails, and
// the ones between from and to (YYYY-MM-DD, either may be "") that are not
// filed with another trip. One list, each outing once, by date and time.
func (c *Client) TripOutings(ctx context.Context, tripID int64, from, to string) ([]Outing, error) {
	ref := strconv.FormatInt(tripID, 10)
	filed, err := c.outings(ctx, url.Values{"trip_ref": {ref}})
	if err != nil {
		return nil, err
	}
	var during []Outing
	if from != "" && to != "" {
		if during, err = c.outings(ctx, url.Values{"from": {from}, "to": {to}}); err != nil {
			return nil, err
		}
	}
	seen := make(map[int64]bool, len(filed)+len(during))
	out := make([]Outing, 0, len(filed)+len(during))
	for _, o := range filed {
		if !seen[o.ID] {
			seen[o.ID] = true
			out = append(out, o)
		}
	}
	for _, o := range during {
		if seen[o.ID] || (o.TripRef != "" && o.TripRef != ref) {
			continue
		}
		seen[o.ID] = true
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if (a.StartTime == "") != (b.StartTime == "") {
			return b.StartTime == "" // the ones without a time last on the day
		}
		return a.StartTime < b.StartTime
	})
	return out, nil
}

// outings is one look at GET /api/v1/outings.
func (c *Client) outings(ctx context.Context, q url.Values) ([]Outing, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/outings", q, &raw); err != nil {
		return nil, err
	}
	var out []Outing
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	}
	var wrap struct {
		Outings []Outing `json:"outings"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	return wrap.Outings, nil
}

// Ping is the connection test: the summary, read with the token.
func (c *Client) Ping(ctx context.Context) error {
	var out any
	return c.get(ctx, "/api/v1/summary", nil, &out)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	if c == nil || c.BaseURL == "" || c.Token == "" {
		return errors.New("pocket trails is not connected")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4<<10)).Decode(&e)
		if e.Error != "" {
			return fmt.Errorf("pocket trails HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("pocket trails HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(v)
}
