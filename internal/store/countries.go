package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SplitCodes turns "PT,ES" into [PT ES] (upper case, no blanks or repeats).
func SplitCodes(s string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, c := range strings.Split(s, ",") {
		c = strings.ToUpper(strings.TrimSpace(c))
		if len(c) == 2 && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// JoinCodes is the inverse of SplitCodes.
func JoinCodes(codes []string) string {
	return strings.Join(SplitCodes(strings.Join(codes, ",")), ",")
}

// Located is a row with coordinates whose country is not known yet.
type Located struct {
	Table string // places | lodging | trips
	ID    int64
	Lat   float64
	Lng   float64
}

// MissingCountries returns up to limit located rows without a country code,
// stays and trips first (they decide the days per country).
func (s *Store) MissingCountries(ctx context.Context, limit int) ([]Located, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT 'lodging', id, lat, lng FROM lodging WHERE country_code = '' AND lat IS NOT NULL AND lng IS NOT NULL
		UNION ALL SELECT 'trips', id, lat, lng FROM trips WHERE country_code = '' AND lat IS NOT NULL AND lng IS NOT NULL
		UNION ALL SELECT 'places', id, lat, lng FROM places WHERE country_code = '' AND lat IS NOT NULL AND lng IS NOT NULL
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Located{}
	for rows.Next() {
		var l Located
		if err := rows.Scan(&l.Table, &l.ID, &l.Lat, &l.Lng); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetCountryCode stores the country of a row, unless its coordinates moved
// since the lookup started ("-" marks a spot that is in no country, e.g. at sea).
func (s *Store) SetCountryCode(ctx context.Context, l Located, code string) error {
	switch l.Table {
	case "places", "lodging", "trips":
	default:
		return fmt.Errorf("no country for %s", l.Table)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE `+l.Table+` SET country_code = ? WHERE id = ? AND lat = ? AND lng = ?`, code, l.ID, l.Lat, l.Lng)
	return err
}

// CountryDays is how many days of a trip were spent in a country.
type CountryDays struct {
	Code  string `json:"code"`
	Days  int    `json:"days"`
	First string `json:"first"` // first day there (YYYY-MM-DD)
}

// CountryFact is a fact someone added to a country ("Plugs": "C, F").
type CountryFact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CountryNote is a person's own take on a country.
type CountryNote struct {
	Code      string            `json:"code"`
	Facts     map[string]string `json:"facts"` // corrections to built-in facts, by field
	Extra     []CountryFact     `json:"extra"`
	Notes     string            `json:"notes"`
	UpdatedAt string            `json:"updated_at"`
}

// GetCountryNote returns userID's note on a country (empty when none).
func (s *Store) GetCountryNote(ctx context.Context, userID int64, code string) (*CountryNote, error) {
	n := &CountryNote{Code: code, Facts: map[string]string{}, Extra: []CountryFact{}}
	var facts, extra string
	err := s.DB.QueryRowContext(ctx, `SELECT facts, extra, notes, updated_at FROM country_notes WHERE user_id = ? AND code = ?`, userID, code).Scan(&facts, &extra, &n.Notes, &n.UpdatedAt)
	if err != nil {
		if notFound(err) == ErrNotFound {
			return n, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(facts), &n.Facts)
	_ = json.Unmarshal([]byte(extra), &n.Extra)
	if n.Facts == nil {
		n.Facts = map[string]string{}
	}
	if n.Extra == nil {
		n.Extra = []CountryFact{}
	}
	return n, nil
}

// SaveCountryNote stores userID's note on a country.
func (s *Store) SaveCountryNote(ctx context.Context, userID int64, n *CountryNote) error {
	facts, _ := json.Marshal(n.Facts)
	extra, _ := json.Marshal(n.Extra)
	n.UpdatedAt = Now()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO country_notes (user_id, code, facts, extra, notes, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, code) DO UPDATE SET facts = excluded.facts, extra = excluded.extra, notes = excluded.notes, updated_at = excluded.updated_at`,
		userID, n.Code, string(facts), string(extra), n.Notes, n.UpdatedAt)
	return err
}
