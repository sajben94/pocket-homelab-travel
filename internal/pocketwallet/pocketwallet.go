// Package pocketwallet reads Pocket Wallet, the person's own loyalty cards,
// tickets, boarding passes and IDs. Travel only reads the passes of a trip,
// and only through Pocket Core, with a five-minute pcx_ link token for
// wallet:read.
package pocketwallet

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

// Client is one person's Pocket Wallet, for one piece of work.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client: baseURL is Wallet's address from Core, token the
// pcx_ link token.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Pass is a ticket or a boarding pass, as Travel hands it on. Times are
// local where they happen: "2026-12-01T06:40".
type Pass struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"` // ticket | boarding
	Name        string `json:"name"` // "Sarajevo → Vienna"
	Issuer      string `json:"issuer"`
	StartsAt    string `json:"starts_at"`
	EndsAt      string `json:"ends_at"`
	Venue       string `json:"venue"` // from, or where
	Destination string `json:"destination"`
	Seat        string `json:"seat"`
	Gate        string `json:"gate"`
	Ref         string `json:"ref"` // flight number or booking
	Holder      string `json:"holder"`
	HasCode     bool   `json:"has_code"`
	TripID      *int64 `json:"trip_id"`
	Archived    bool   `json:"archived"`
}

// card is a pass as Wallet sends it (only what Travel uses).
type card struct {
	Pass
	CodeFormat string `json:"code_format"`
}

// TripPasses is the trip's passes: the ones filed with it, and the ones
// starting between from and to (YYYY-MM-DD, either may be "") that are not
// filed with another trip. One list, each pass once, by when it starts.
func (c *Client) TripPasses(ctx context.Context, tripID int64, from, to string) ([]Pass, error) {
	filed, err := c.passes(ctx, url.Values{"trip_id": {strconv.FormatInt(tripID, 10)}})
	if err != nil {
		return nil, err
	}
	var during []Pass
	if from != "" && to != "" {
		if during, err = c.passes(ctx, url.Values{"from": {from}, "to": {to}}); err != nil {
			return nil, err
		}
	}
	seen := make(map[int64]bool, len(filed)+len(during))
	out := make([]Pass, 0, len(filed)+len(during))
	for _, p := range filed {
		if !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	for _, p := range during {
		if seen[p.ID] || (p.TripID != nil && *p.TripID != tripID) {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].StartsAt, out[j].StartsAt
		if (a == "") != (b == "") {
			return b == "" // the ones without a time last
		}
		return a < b
	})
	return out, nil
}

// passes is one look at GET /api/v1/passes.
func (c *Client) passes(ctx context.Context, q url.Values) ([]Pass, error) {
	var cs []card
	if err := c.get(ctx, "/api/v1/passes", q, &cs); err != nil {
		return nil, err
	}
	out := make([]Pass, 0, len(cs))
	for _, x := range cs {
		p := x.Pass
		p.HasCode = x.CodeFormat != ""
		out = append(out, p)
	}
	return out, nil
}

// Ping is the connection test: the next passes, read with the token.
func (c *Client) Ping(ctx context.Context) error {
	var out any
	return c.get(ctx, "/api/v1/passes", url.Values{"limit": {"1"}}, &out)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	if c == nil || c.BaseURL == "" || c.Token == "" {
		return errors.New("pocket wallet is not connected")
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
			return fmt.Errorf("pocket wallet HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("pocket wallet HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(v)
}
