package store

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// TripCategory is a bill category of a trip (Cospend category). Key ties it
// to an expense category of this app (food, lodging…); categories made in a
// Cospend client get "c<id>".
type TripCategory struct {
	ID        int64  `json:"id"`
	TripID    int64  `json:"trip_id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Color     string `json:"color"`
	SortOrder int    `json:"order"`
}

// TripPaymentMode is how a bill was paid (Cospend payment mode).
type TripPaymentMode struct {
	ID        int64  `json:"id"`
	TripID    int64  `json:"trip_id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Color     string `json:"color"`
	SortOrder int    `json:"order"`
	OldID     string `json:"old_id"` // Cospend's one-letter id from before payment modes were per project
}

// TripCurrency is an extra currency of a trip; ExchangeRate trip-currency units make one of it.
type TripCurrency struct {
	ID           int64   `json:"id"`
	TripID       int64   `json:"trip_id"`
	Name         string  `json:"name"`
	ExchangeRate float64 `json:"exchange_rate"`
}

// ReimbursementKey is Cospend's special category for paying each other back:
// such bills settle debts and are left out of spending statistics.
const ReimbursementKey = "reimbursement"

// defaultCategories follow this app's expense categories, in Cospend's style.
var defaultCategories = []TripCategory{
	{Key: "food", Name: "Restaurant", Icon: "🍴", Color: "#d0d5e1"},
	{Key: "groceries", Name: "Groceries", Icon: "🛒", Color: "#ffaa00"},
	{Key: "lodging", Name: "Accommodation", Icon: "🛌", Color: "#5de1a3"},
	{Key: "transport", Name: "Transport", Icon: "🚌", Color: "#6f2ee1"},
	{Key: "fuel", Name: "Fuel", Icon: "⛽", Color: "#e1513c"},
	{Key: "activities", Name: "Excursion / Culture", Icon: "🎭", Color: "#0055ff"},
	{Key: "shopping", Name: "Shopping", Icon: "🛍", Color: "#e167d1"},
	{Key: "fees", Name: "Fees", Icon: "💸", Color: "#7990ff"},
	{Key: "insurance", Name: "Insurance", Icon: "🛡", Color: "#00b9a1"},
	{Key: "other", Name: "Other", Icon: "📌", Color: "#a0a0a0"},
	{Key: ReimbursementKey, Name: "Reimbursement", Icon: "💰", Color: "#22c55e"},
}

// defaultPaymentModes are Cospend's.
var defaultPaymentModes = []TripPaymentMode{
	{Name: "Credit card", Icon: "💳", Color: "#FF7F50", OldID: "c"},
	{Name: "Cash", Icon: "💵", Color: "#556B2F", OldID: "b"},
	{Name: "Check", Icon: "🎫", Color: "#A9A9A9", OldID: "f"},
	{Name: "Transfer", Icon: "⇄", Color: "#00CED1", OldID: "t"},
	{Name: "Online service", Icon: "🌎", Color: "#9932CC", OldID: "o"},
}

// EnsureBillSetup gives a trip the default categories and payment modes once.
func (s *Store) EnsureBillSetup(ctx context.Context, tripID int64) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(1) FROM trip_categories WHERE trip_id = ?) + (SELECT COUNT(1) FROM trip_payment_modes WHERE trip_id = ?)`, tripID, tripID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, c := range defaultCategories {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO trip_categories (trip_id, key, name, icon, color, sort_order) VALUES (?, ?, ?, ?, ?, ?)`, tripID, c.Key, c.Name, c.Icon, c.Color, i); err != nil {
			return err
		}
	}
	for i, m := range defaultPaymentModes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO trip_payment_modes (trip_id, name, icon, color, sort_order, old_id) VALUES (?, ?, ?, ?, ?, ?)`, tripID, m.Name, m.Icon, m.Color, i, m.OldID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTripCategories returns a trip's bill categories in order.
func (s *Store) ListTripCategories(ctx context.Context, tripID int64) ([]TripCategory, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, trip_id, key, name, icon, color, sort_order FROM trip_categories WHERE trip_id = ? ORDER BY sort_order, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TripCategory{}
	for rows.Next() {
		var c TripCategory
		if err := rows.Scan(&c.ID, &c.TripID, &c.Key, &c.Name, &c.Icon, &c.Color, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveTripCategory adds (ID 0) or updates a category; a new one gets the key "c<id>".
func (s *Store) SaveTripCategory(ctx context.Context, c *TripCategory) error {
	if c.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO trip_categories (trip_id, key, name, icon, color, sort_order) VALUES (?, ?, ?, ?, ?, COALESCE((SELECT MAX(sort_order) + 1 FROM trip_categories WHERE trip_id = ?), 0))`,
			c.TripID, "tmp-"+time.Now().Format("150405.000000"), c.Name, c.Icon, c.Color, c.TripID)
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
		if c.Key == "" || strings.HasPrefix(c.Key, "tmp-") {
			c.Key = "c" + strconv.FormatInt(c.ID, 10)
		}
		_, err = s.DB.ExecContext(ctx, `UPDATE trip_categories SET key = ? WHERE id = ?`, c.Key, c.ID)
		return conflict(err)
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE trip_categories SET name = ?, icon = ?, color = ?, sort_order = ? WHERE id = ? AND trip_id = ?`, c.Name, c.Icon, c.Color, c.SortOrder, c.ID, c.TripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTripCategory removes a category; its bills fall back to "other".
func (s *Store) DeleteTripCategory(ctx context.Context, tripID, id int64) error {
	var key string
	if err := s.DB.QueryRowContext(ctx, `SELECT key FROM trip_categories WHERE id = ? AND trip_id = ?`, id, tripID).Scan(&key); err != nil {
		return notFound(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE expenses SET category = 'other', updated_ts = ? WHERE trip_id = ? AND category = ?`, time.Now().Unix(), tripID, key); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM trip_categories WHERE id = ? AND trip_id = ?`, id, tripID)
	return err
}

// ListTripPaymentModes returns a trip's payment modes in order.
func (s *Store) ListTripPaymentModes(ctx context.Context, tripID int64) ([]TripPaymentMode, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, trip_id, name, icon, color, sort_order, old_id FROM trip_payment_modes WHERE trip_id = ? ORDER BY sort_order, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TripPaymentMode{}
	for rows.Next() {
		var m TripPaymentMode
		if err := rows.Scan(&m.ID, &m.TripID, &m.Name, &m.Icon, &m.Color, &m.SortOrder, &m.OldID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveTripPaymentMode adds (ID 0) or updates a payment mode.
func (s *Store) SaveTripPaymentMode(ctx context.Context, m *TripPaymentMode) error {
	if m.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO trip_payment_modes (trip_id, name, icon, color, sort_order, old_id) VALUES (?, ?, ?, ?, COALESCE((SELECT MAX(sort_order) + 1 FROM trip_payment_modes WHERE trip_id = ?), 0), ?)`,
			m.TripID, m.Name, m.Icon, m.Color, m.TripID, m.OldID)
		if err != nil {
			return err
		}
		m.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE trip_payment_modes SET name = ?, icon = ?, color = ?, sort_order = ? WHERE id = ? AND trip_id = ?`, m.Name, m.Icon, m.Color, m.SortOrder, m.ID, m.TripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTripPaymentMode removes a payment mode from a trip and its bills.
func (s *Store) DeleteTripPaymentMode(ctx context.Context, tripID, id int64) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE expenses SET payment_mode_id = 0, updated_ts = ? WHERE trip_id = ? AND payment_mode_id = ?`, time.Now().Unix(), tripID, id); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM trip_payment_modes WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTripCurrencies returns a trip's extra currencies.
func (s *Store) ListTripCurrencies(ctx context.Context, tripID int64) ([]TripCurrency, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, trip_id, name, exchange_rate FROM trip_currencies WHERE trip_id = ? ORDER BY id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TripCurrency{}
	for rows.Next() {
		var c TripCurrency
		if err := rows.Scan(&c.ID, &c.TripID, &c.Name, &c.ExchangeRate); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveTripCurrency adds (ID 0) or updates an extra currency.
func (s *Store) SaveTripCurrency(ctx context.Context, c *TripCurrency) error {
	if c.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO trip_currencies (trip_id, name, exchange_rate) VALUES (?, ?, ?)`, c.TripID, c.Name, c.ExchangeRate)
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE trip_currencies SET name = ?, exchange_rate = ? WHERE id = ? AND trip_id = ?`, c.Name, c.ExchangeRate, c.ID, c.TripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTripCurrency removes an extra currency.
func (s *Store) DeleteTripCurrency(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM trip_currencies WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- the Cospend share link ----

// TripShare is the Cospend public link of a trip.
type TripShare struct {
	TripID      int64  `json:"trip_id"`
	Token       string `json:"token"`
	Password    string `json:"password"`
	AccessLevel int    `json:"access_level"`
	Label       string `json:"label"`
	CreatedAt   string `json:"created_at"`
}

// GetTripShare returns a trip's share link (ErrNotFound when off).
func (s *Store) GetTripShare(ctx context.Context, tripID int64) (*TripShare, error) {
	var sh TripShare
	err := s.DB.QueryRowContext(ctx, `SELECT trip_id, token, password, access_level, label, created_at FROM trip_shares WHERE trip_id = ?`, tripID).Scan(&sh.TripID, &sh.Token, &sh.Password, &sh.AccessLevel, &sh.Label, &sh.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &sh, nil
}

// TripShareByToken finds the share link with a token.
func (s *Store) TripShareByToken(ctx context.Context, token string) (*TripShare, error) {
	var sh TripShare
	err := s.DB.QueryRowContext(ctx, `SELECT trip_id, token, password, access_level, label, created_at FROM trip_shares WHERE token = ?`, token).Scan(&sh.TripID, &sh.Token, &sh.Password, &sh.AccessLevel, &sh.Label, &sh.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &sh, nil
}

// SaveTripShare creates or replaces a trip's share link.
func (s *Store) SaveTripShare(ctx context.Context, sh *TripShare) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO trip_shares (trip_id, token, password, access_level, label) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(trip_id) DO UPDATE SET token = excluded.token, password = excluded.password, access_level = excluded.access_level, label = excluded.label`,
		sh.TripID, sh.Token, sh.Password, sh.AccessLevel, sh.Label)
	return conflict(err)
}

// DeleteTripShare turns a trip's share link off.
func (s *Store) DeleteTripShare(ctx context.Context, tripID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM trip_shares WHERE trip_id = ?`, tripID)
	return err
}
