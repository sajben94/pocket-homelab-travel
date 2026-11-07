package store

import (
	"context"
)

// DefaultTitle is the app title until someone changes it.
const DefaultTitle = "Pocket Travel"

// globalKeys are the box-wide settings: the app title, whether setup is done
// and whether exchange rates are looked up online. Admins change them.
// Everything else (preferences and integration credentials) is per person
// and lives in user_settings.
var globalKeys = map[string]bool{"title": true, "setup_complete": true, "fx_auto": true}

// GetSettings loads the box-wide settings plus userID's personal ones, with
// defaults applied. userID 0 returns the box-wide settings and defaults only
// (no one's credentials).
func (s *Store) GetSettings(ctx context.Context, userID int64) (Settings, error) {
	home := s.DefaultCurrency
	if home == "" {
		home = "EUR"
	}
	st := Settings{Title: DefaultTitle, HomeCurrency: home, DateFormat: "auto", TimeFormat: "auto", DistanceUnit: "km", FXAuto: true, MapTiles: "osm", AutoPhotos: true, CheckInTime: "15:00", CheckOutTime: "11:00"}
	kv := map[string]string{}
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return st, err
		}
		if globalKeys[k] {
			kv[k] = v
		}
	}
	rows.Close()
	if userID > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
		if err != nil {
			return st, err
		}
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				rows.Close()
				return st, err
			}
			if !globalKeys[k] {
				kv[k] = v
			}
		}
		rows.Close()
	}
	for k, v := range kv {
		switch k {
		case "title":
			if v != "" {
				st.Title = v
			}
		case "home_currency":
			if v != "" {
				st.HomeCurrency = v
			}
		case "date_format":
			if v != "" {
				st.DateFormat = v
			}
		case "time_format":
			if v != "" {
				st.TimeFormat = v
			}
		case "distance_unit":
			if v == "mi" {
				st.DistanceUnit = "mi"
			}
		case "fx_auto":
			st.FXAuto = v != "0"
		case "money_mode":
			st.MoneyMode = v
		case "firefly_url":
			st.FireflyURL = v
		case "firefly_token":
			st.FireflyToken = v
		case "dawarich_mode":
			st.DawarichMode = v
		case "immich_mode":
			st.ImmichMode = v
		case "dawarich_url":
			st.DawarichURL = v
		case "dawarich_api_key":
			st.DawarichAPIKey = v
		case "immich_url":
			st.ImmichURL = v
		case "immich_api_key":
			st.ImmichAPIKey = v
		case "immich_public_url":
			st.ImmichPublicURL = v
		case "photoprism_url":
			st.PhotoPrismURL = v
		case "photoprism_token":
			st.PhotoPrismToken = v
		case "photoprism_public_url":
			st.PhotoPrismPublicURL = v
		case "location_source":
			st.LocationSource = v
		case "places_mode":
			st.PlacesMode = v
		case "ai_pocket_model":
			st.AIPocketModel = v
		case "ai_provider":
			st.AIProvider = v
		case "openai_api_key":
			st.OpenAIKey = v
		case "openai_model":
			st.OpenAIModel = v
		case "openai_base_url":
			st.OpenAIBaseURL = v
		case "auto_photos":
			st.AutoPhotos = v != "0"
		case "map_tiles":
			if v != "" {
				st.MapTiles = v
			}
		case "checkin_time":
			if v != "" {
				st.CheckInTime = v
			}
		case "checkout_time":
			if v != "" {
				st.CheckOutTime = v
			}
		case "setup_complete":
			st.SetupComplete = v == "1"
		}
	}
	return st, nil
}

func personalKV(st Settings) map[string]string {
	b := map[bool]string{true: "1", false: "0"}
	return map[string]string{
		"home_currency":         st.HomeCurrency,
		"date_format":           st.DateFormat,
		"time_format":           st.TimeFormat,
		"distance_unit":         st.DistanceUnit,
		"money_mode":            st.MoneyMode,
		"firefly_url":           st.FireflyURL,
		"firefly_token":         st.FireflyToken,
		"dawarich_mode":         st.DawarichMode,
		"immich_mode":           st.ImmichMode,
		"dawarich_url":          st.DawarichURL,
		"dawarich_api_key":      st.DawarichAPIKey,
		"immich_url":            st.ImmichURL,
		"immich_api_key":        st.ImmichAPIKey,
		"immich_public_url":     st.ImmichPublicURL,
		"photoprism_url":        st.PhotoPrismURL,
		"photoprism_token":      st.PhotoPrismToken,
		"photoprism_public_url": st.PhotoPrismPublicURL,
		"location_source":       st.LocationSource,
		"places_mode":           st.PlacesMode,
		"ai_provider":           st.AIProvider, "ai_pocket_model": st.AIPocketModel,
		"openai_api_key":  st.OpenAIKey,
		"openai_model":    st.OpenAIModel,
		"openai_base_url": st.OpenAIBaseURL,
		"map_tiles":       st.MapTiles,
		"checkin_time":    st.CheckInTime,
		"checkout_time":   st.CheckOutTime,
		"auto_photos":     b[st.AutoPhotos],
	}
}

// SaveUserSettings persists userID's personal settings (preferences and
// integration credentials). Box-wide fields in st are ignored.
func (s *Store) SaveUserSettings(ctx context.Context, userID int64, st Settings) error {
	if userID <= 0 {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, v := range personalKV(st) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?) ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, k, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SaveGlobalSettings persists the box-wide settings (title, setup flag,
// online exchange rates). Personal fields in st are ignored.
func (s *Store) SaveGlobalSettings(ctx context.Context, st Settings) error {
	b := map[bool]string{true: "1", false: "0"}
	kv := map[string]string{"title": st.Title, "setup_complete": b[st.SetupComplete], "fx_auto": b[st.FXAuto]}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ClaimLegacySettings moves personal keys still stored box-wide (from before
// settings were per person) to userID, without overwriting what they set.
func (s *Store) ClaimLegacySettings(ctx context.Context, userID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_settings (user_id, key, value) SELECT ?, key, value FROM settings WHERE key NOT IN ('title', 'setup_complete', 'fx_auto')`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key NOT IN ('title', 'setup_complete', 'fx_auto')`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// HasUserSetting reports whether userID stored a value for key.
func (s *Store) HasUserSetting(ctx context.Context, userID int64, key string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM user_settings WHERE user_id = ? AND key = ?`, userID, key).Scan(&n)
	return n > 0
}

// MarkUserSetting stores one personal key outside the Settings struct (e.g.
// the "seeded:" markers that remember an environment value was copied once).
func (s *Store) MarkUserSetting(ctx context.Context, userID int64, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?) ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, key, value)
	return err
}

// ---- generic cache ----

// CacheGet returns a cached value and when it was fetched.
func (s *Store) CacheGet(ctx context.Context, key string) (string, string, bool) {
	var v, at string
	if err := s.DB.QueryRowContext(ctx, `SELECT value, fetched_at FROM cache WHERE key = ?`, key).Scan(&v, &at); err != nil {
		return "", "", false
	}
	return v, at, true
}

// CachePut stores a value.
func (s *Store) CachePut(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO cache (key, value, fetched_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, fetched_at = excluded.fetched_at`, key, value, Now())
	return err
}

// CacheDelete removes every key with the prefix.
func (s *Store) CacheDelete(ctx context.Context, prefix string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cache WHERE key LIKE ? || '%'`, prefix)
	return err
}

// CacheDeleteLike removes every key matching a LIKE pattern.
func (s *Store) CacheDeleteLike(ctx context.Context, pattern string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cache WHERE key LIKE ?`, pattern)
	return err
}

// ---- fx cache ----

// GetFXRate returns a cached rate.
func (s *Store) GetFXRate(ctx context.Context, base, quote, date string) (float64, string, bool) {
	var r float64
	var src string
	if err := s.DB.QueryRowContext(ctx, `SELECT rate, source FROM fx_cache WHERE base = ? AND quote = ? AND date = ?`, base, quote, date).Scan(&r, &src); err != nil {
		return 0, "", false
	}
	return r, src, true
}

// PutFXRate caches a rate.
func (s *Store) PutFXRate(ctx context.Context, base, quote, date string, rate float64, source string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO fx_cache (base, quote, date, rate, source) VALUES (?, ?, ?, ?, ?) ON CONFLICT(base, quote, date) DO UPDATE SET rate = excluded.rate, source = excluded.source`, base, quote, date, rate, source)
	return err
}
