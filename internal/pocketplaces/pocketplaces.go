// Package pocketplaces reads Pocket Places, the person's own list of places
// (restaurants, cafés, sights) they want to try or have been to, with their
// visits and what those cost. Travel only ever reaches it through Pocket
// Core, with a five-minute pcx_ link token for places:read.
package pocketplaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is one person's Pocket Places, for one piece of work.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client: baseURL is Places' address from Core, token the
// pcx_ link token.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Place is one place on the person's list.
type Place struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Cuisine   string   `json:"cuisine"`
	City      string   `json:"city"`
	Status    string   `json:"status"` // want | been
	Favourite bool     `json:"favourite"`
	Rating    float64  `json:"rating"` // 0–5
	Price     int      `json:"price"`  // 0–4
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
	Notes     string   `json:"notes"`
}

// List is one of the person's lists (one made for the trip, here).
type List struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Been  int    `json:"been"`
}

// Visit is a time the person went to a place, or a plan to go.
type Visit struct {
	ID        int64   `json:"id"`
	PlaceID   int64   `json:"place_id"`
	Date      string  `json:"date"`
	Time      string  `json:"time"`
	Planned   bool    `json:"planned"`
	Rating    float64 `json:"rating"`
	WithWhom  string  `json:"with_whom"`
	Spent     int64   `json:"spent"` // minor units
	Currency  string  `json:"currency"`
	PlaceName string  `json:"place_name"`
	PlaceKind string  `json:"place_kind"`
}

// Spent is what the visits cost in one currency.
type Spent struct {
	Currency string `json:"currency"`
	Total    int64  `json:"total"` // minor units
	Visits   int    `json:"visits"`
}

// Guide is what Places knows for a trip.
type Guide struct {
	TripID int64   `json:"trip_id"`
	Lists  []List  `json:"lists"`
	Want   []Place `json:"want"`
	Been   []Place `json:"been"`
	Visits []Visit `json:"visits"`
	Spent  []Spent `json:"spent"`
	URL    string  `json:"url"`
}

// GuideQuery is the trip the guide is for.
type GuideQuery struct {
	TripID      int64
	Lat, Lng    *float64
	Destination string
	From, To    string // YYYY-MM-DD
}

// TripGuide asks Places for the trip: the places at its destination (to
// try, and where the person has been), the lists made for it and the
// visits during it.
func (c *Client) TripGuide(ctx context.Context, q GuideQuery) (*Guide, error) {
	v := url.Values{}
	if q.TripID > 0 {
		v.Set("trip_id", strconv.FormatInt(q.TripID, 10))
	}
	if q.Lat != nil && q.Lng != nil {
		v.Set("lat", strconv.FormatFloat(*q.Lat, 'f', 6, 64))
		v.Set("lng", strconv.FormatFloat(*q.Lng, 'f', 6, 64))
	}
	if d := strings.TrimSpace(q.Destination); d != "" {
		v.Set("destination", d)
	}
	if q.From != "" {
		v.Set("from", q.From)
	}
	if q.To != "" {
		v.Set("to", q.To)
	}
	var g Guide
	if err := c.get(ctx, "/api/v1/trip-guide", v, &g); err != nil {
		return nil, err
	}
	// never null in what Travel hands on
	if g.Lists == nil {
		g.Lists = []List{}
	}
	if g.Want == nil {
		g.Want = []Place{}
	}
	if g.Been == nil {
		g.Been = []Place{}
	}
	if g.Visits == nil {
		g.Visits = []Visit{}
	}
	if g.Spent == nil {
		g.Spent = []Spent{}
	}
	return &g, nil
}

// Ping is the connection test: one place read with the token.
func (c *Client) Ping(ctx context.Context) error {
	var out any
	return c.get(ctx, "/api/v1/places", url.Values{"limit": {"1"}}, &out)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	if c == nil || c.BaseURL == "" || c.Token == "" {
		return errors.New("pocket places is not connected")
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
			return fmt.Errorf("pocket places HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("pocket places HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(v)
}
