package store

import (
	"context"
	"database/sql"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"strings"
	"time"
)

const tripCols = `t.id, t.owner_id, t.name, t.destination, t.description, t.start_date, t.end_date, t.status, t.currency, t.budget, t.color, t.cover_path, t.link, t.notes, t.lat, t.lng, t.timezone, t.firefly_tag, t.dawarich, t.immich, t.archived, t.external_id, t.created_at, t.updated_at, t.country_code, t.countries`

func scanTrip(row interface{ Scan(...any) error }) (*Trip, error) {
	var t Trip
	var dw, im, ar int
	var manual string
	if err := row.Scan(&t.ID, &t.OwnerID, &t.Name, &t.Destination, &t.Description, &t.StartDate, &t.EndDate, &t.Status, &t.Currency, &t.Budget, &t.Color, &t.CoverPath, &t.Link, &t.Notes, &t.Lat, &t.Lng, &t.Timezone, &t.FireflyTag, &dw, &im, &ar, &t.ExternalID, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode, &manual); err != nil {
		return nil, notFound(err)
	}
	t.CountriesManual = SplitCodes(manual)
	t.Dawarich, t.Immich, t.Archived = dw == 1, im == 1, ar == 1
	t.HasCover = t.CoverPath != ""
	t.Days = DaysBetween(t.StartDate, t.EndDate)
	return &t, nil
}

// DaysBetween counts inclusive days between two YYYY-MM-DD dates (0 when unset).
func DaysBetween(a, b string) int {
	s, err1 := ParseDate(a)
	e, err2 := ParseDate(b)
	if err1 != nil || err2 != nil || e.Before(s) {
		return 0
	}
	return int(e.Sub(s).Hours()/24) + 1
}

// TripFilter narrows ListTrips.
type TripFilter struct {
	UserID   int64 // visible to this user (0 = every trip; background jobs only, never a request)
	Owned    bool  // only trips UserID owns (not ones shared with them)
	Archived *bool
	Status   string
	Query    string
	// WithPeople keeps trips on which every one of these people (ids in the
	// requesting user's address book) was.
	WithPeople []int64
}

// ListTrips returns trips the user can see, newest start date first.
func (s *Store) ListTrips(ctx context.Context, f TripFilter) ([]Trip, error) {
	q := `SELECT ` + tripCols + `, COALESCE(m.role, CASE WHEN t.owner_id = ? THEN 'owner' ELSE '' END) FROM trips t LEFT JOIN trip_members m ON m.trip_id = t.id AND m.user_id = ? WHERE 1=1`
	args := []any{f.UserID, f.UserID}
	if f.UserID > 0 {
		if f.Owned {
			q += ` AND t.owner_id = ?`
		} else {
			q += ` AND (t.owner_id = ? OR m.user_id IS NOT NULL)`
		}
		args = append(args, f.UserID)
	}
	if f.Archived != nil {
		q += ` AND t.archived = ?`
		args = append(args, boolInt(*f.Archived))
	}
	if f.Status != "" {
		q += ` AND t.status = ?`
		args = append(args, f.Status)
	}
	if len(f.WithPeople) > 0 {
		ph, ids := inInts(f.WithPeople)
		q += ` AND t.id IN (SELECT trip_id FROM trip_people WHERE person_id IN (` + ph + `) GROUP BY trip_id HAVING COUNT(DISTINCT person_id) = ?)`
		args = append(args, ids...)
		args = append(args, len(f.WithPeople))
	}
	if f.Query != "" {
		q += ` AND (t.name LIKE ? OR t.destination LIKE ?)`
		like := "%" + f.Query + "%"
		args = append(args, like, like)
	}
	q += ` ORDER BY CASE WHEN t.start_date = '' THEN 1 ELSE 0 END, t.start_date DESC, t.id DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Trip{}
	for rows.Next() {
		var t Trip
		var dw, im, ar int
		var manual string
		if err := rows.Scan(&t.ID, &t.OwnerID, &t.Name, &t.Destination, &t.Description, &t.StartDate, &t.EndDate, &t.Status, &t.Currency, &t.Budget, &t.Color, &t.CoverPath, &t.Link, &t.Notes, &t.Lat, &t.Lng, &t.Timezone, &t.FireflyTag, &dw, &im, &ar, &t.ExternalID, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode, &manual, &t.Role); err != nil {
			return nil, err
		}
		t.CountriesManual = SplitCodes(manual)
		t.Dawarich, t.Immich, t.Archived = dw == 1, im == 1, ar == 1
		t.HasCover = t.CoverPath != ""
		t.Days = DaysBetween(t.StartDate, t.EndDate)
		if t.OwnerID == f.UserID {
			t.Role = "owner"
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // the pool has one connection; release it before the lookups below
	for i := range out {
		if !out[i].HasCover {
			out[i].HasCover = s.CoverFallback(ctx, out[i].ID) != ""
		}
	}
	return out, nil
}

// GetTrip loads one trip with members; role is filled for userID.
func (s *Store) GetTrip(ctx context.Context, id, userID int64) (*Trip, error) {
	t, err := scanTrip(s.DB.QueryRowContext(ctx, `SELECT `+tripCols+` FROM trips t WHERE t.id = ?`, id))
	if err != nil {
		return nil, err
	}
	t.Members, err = s.TripMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	if !t.HasCover {
		t.HasCover = s.CoverFallback(ctx, t.ID) != ""
	}
	if t.OwnerID == userID {
		t.Role = "owner"
	} else {
		for _, m := range t.Members {
			if m.UserID == userID {
				t.Role = m.Role
			}
		}
	}
	return t, nil
}

// TripMembers lists members with names.
func (s *Store) TripMembers(ctx context.Context, tripID int64) ([]TripMember, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, u.name, u.email, CASE WHEN t.owner_id = u.id THEN 'owner' ELSE m.role END
		FROM trips t JOIN users u ON (u.id = t.owner_id OR u.id IN (SELECT user_id FROM trip_members WHERE trip_id = t.id))
		LEFT JOIN trip_members m ON m.trip_id = t.id AND m.user_id = u.id WHERE t.id = ? ORDER BY CASE WHEN t.owner_id = u.id THEN 0 ELSE 1 END, u.name`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TripMember{}
	for rows.Next() {
		var m TripMember
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetTripMember adds or updates a member; role "" removes.
func (s *Store) SetTripMember(ctx context.Context, tripID, userID int64, role string) error {
	if role == "" {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM trip_members WHERE trip_id = ? AND user_id = ?`, tripID, userID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO trip_members (trip_id, user_id, role) VALUES (?, ?, ?) ON CONFLICT(trip_id, user_id) DO UPDATE SET role = excluded.role`, tripID, userID, role)
	return err
}

// TripRole returns the user's role on a trip: owner, or the role the owner
// gave them as a member (editor | viewer). "" means no access, and callers
// answer 404 so trip ids cannot be probed. Admins get no special access:
// sharing through trip_members is the only way into someone else's trip.
func (s *Store) TripRole(ctx context.Context, tripID int64, u *User) string {
	if u == nil || u.ID <= 0 {
		return ""
	}
	var owner int64
	if err := s.DB.QueryRowContext(ctx, `SELECT owner_id FROM trips WHERE id = ?`, tripID).Scan(&owner); err != nil {
		return ""
	}
	if owner == u.ID {
		return "owner"
	}
	var role string
	if err := s.DB.QueryRowContext(ctx, `SELECT role FROM trip_members WHERE trip_id = ? AND user_id = ?`, tripID, u.ID).Scan(&role); err == nil {
		return role
	}
	return ""
}

// FirstAdminID returns the owner of the box: the first admin account (the
// one that configured the integrations before settings were per person).
func (s *Store) FirstAdminID(ctx context.Context) int64 {
	var id int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE((SELECT MIN(id) FROM users WHERE role = 'admin'), (SELECT MIN(id) FROM users), 0)`).Scan(&id); err != nil {
		return 0
	}
	return id
}

// CreateTrip inserts a trip.
func (s *Store) CreateTrip(ctx context.Context, t *Trip, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if t.Status == "" {
		t.Status = StatusPlanning
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO trips (owner_id, name, destination, description, start_date, end_date, status, currency, budget, color, cover_path, link, notes, lat, lng, timezone, firefly_tag, dawarich, immich, archived, external_id, country_code, countries)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.OwnerID, t.Name, t.Destination, t.Description, t.StartDate, t.EndDate, t.Status, t.Currency, t.Budget, t.Color, t.CoverPath, t.Link, t.Notes, t.Lat, t.Lng, t.Timezone, t.FireflyTag, boolInt(t.Dawarich), boolInt(t.Immich), boolInt(t.Archived), t.ExternalID, t.CountryCode, JoinCodes(t.CountriesManual))
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	t.Days = DaysBetween(t.StartDate, t.EndDate)
	if err := core.Commit(tx, after...); err != nil {
		return err
	}
	// the owner is on their own trip (and in its shared expenses)
	return s.AddOwnerToTrip(ctx, t)
}

// UpdateTrip saves all editable columns.
func (s *Store) UpdateTrip(ctx context.Context, t *Trip, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE trips SET country_code = CASE WHEN lat IS ? AND lng IS ? THEN country_code ELSE '' END, countries = ?,
		name = ?, destination = ?, description = ?, start_date = ?, end_date = ?, status = ?, currency = ?, budget = ?, color = ?, cover_path = ?, link = ?, notes = ?, lat = ?, lng = ?, timezone = ?, firefly_tag = ?, dawarich = ?, immich = ?, archived = ?, updated_at = ? WHERE id = ?`,
		nullF(t.Lat), nullF(t.Lng), JoinCodes(t.CountriesManual),
		t.Name, t.Destination, t.Description, t.StartDate, t.EndDate, t.Status, t.Currency, t.Budget, t.Color, t.CoverPath, t.Link, t.Notes, t.Lat, t.Lng, t.Timezone, t.FireflyTag, boolInt(t.Dawarich), boolInt(t.Immich), boolInt(t.Archived), Now(), t.ID)
	if err != nil {
		return err
	}
	t.Days = DaysBetween(t.StartDate, t.EndDate)
	t.HasCover = t.CoverPath != ""
	return core.Commit(tx, after...)
}

// DeleteTrip removes a trip and everything in it.
func (s *Store) DeleteTrip(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM trips WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchTrip bumps updated_at.
func (s *Store) TouchTrip(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE trips SET updated_at = ? WHERE id = ?`, Now(), id)
}

// Counts summarises a trip.
func (s *Store) TripCounts(ctx context.Context, id int64) (*TripCounts, error) {
	var c TripCounts
	row := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(1) FROM places WHERE trip_id = ?),
		(SELECT COUNT(1) FROM places WHERE trip_id = ? AND status = 'visited'),
		(SELECT COUNT(1) FROM lodging WHERE trip_id = ?),
		(SELECT COUNT(1) FROM transport WHERE trip_id = ?),
		(SELECT COUNT(1) FROM documents WHERE trip_id = ?),
		(SELECT COUNT(1) FROM journal WHERE trip_id = ?),
		(SELECT COUNT(1) FROM checklist_items i JOIN checklists c ON c.id = i.checklist_id WHERE c.trip_id = ?),
		(SELECT COUNT(1) FROM checklist_items i JOIN checklists c ON c.id = i.checklist_id WHERE c.trip_id = ? AND i.done = 1),
		(SELECT COALESCE(SUM(amount_home), 0) FROM expenses WHERE trip_id = ? AND deleted = 0 AND kind = 'planned'),
		(SELECT COALESCE(SUM(amount_home), 0) FROM expenses WHERE trip_id = ? AND deleted = 0 AND kind = 'actual' AND category <> 'reimbursement'),
		(SELECT COALESCE(SUM(amount_home), 0) FROM expenses WHERE trip_id = ? AND deleted = 0 AND kind = 'planned' AND source = 'item')`,
		id, id, id, id, id, id, id, id, id, id, id)
	var bookings int64
	if err := row.Scan(&c.Places, &c.Visited, &c.Lodging, &c.Transport, &c.Documents, &c.Journal, &c.Checklist, &c.Checked, &c.Planned, &c.Actual, &bookings); err != nil {
		return nil, err
	}
	c.Total = c.Actual + bookings
	return &c, nil
}

// CoverFallback returns an image to use as the banner when no cover was
// uploaded: the stay's photo, then the first place photo.
func (s *Store) CoverFallback(ctx context.Context, tripID int64) string {
	var p string
	if err := s.DB.QueryRowContext(ctx, `SELECT image_path FROM lodging WHERE trip_id = ? AND image_path <> '' ORDER BY check_in, id LIMIT 1`, tripID).Scan(&p); err == nil && p != "" {
		return p
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT image_path FROM places WHERE trip_id = ? AND image_path <> '' ORDER BY CASE WHEN day_date = '' THEN 1 ELSE 0 END, day_date, sort_order, id LIMIT 1`, tripID).Scan(&p); err == nil {
		return p
	}
	return ""
}

// LogActivity appends to the trip's change log.
func (s *Store) LogActivity(ctx context.Context, tripID int64, userID int64, action, subject string) {
	var uid any
	if userID > 0 {
		uid = userID
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO activity (trip_id, user_id, action, subject) VALUES (?, ?, ?, ?)`, tripID, uid, action, strings.TrimSpace(subject))
}

// ListActivity returns the latest entries.
func (s *Store) ListActivity(ctx context.Context, tripID int64, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, a.trip_id, a.user_id, COALESCE(u.name, ''), a.action, a.subject, a.created_at FROM activity a LEFT JOIN users u ON u.id = a.user_id WHERE a.trip_id = ? ORDER BY a.id DESC LIMIT ?`, tripID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.TripID, &a.UserID, &a.UserName, &a.Action, &a.Subject, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Stats for the home page: totals across the user's trips.
type HomeStats struct {
	Trips      int      `json:"trips"`
	Upcoming   int      `json:"upcoming"`
	Days       int      `json:"days"`
	Places     int      `json:"places"`
	Visited    int      `json:"visited"`
	Countries  []string `json:"destinations"`
	SpentHome  int64    `json:"spent_home"`
	NextTripID int64    `json:"next_trip_id"`
}

// HomeStats aggregates the user's trips.
func (s *Store) HomeStats(ctx context.Context, userID int64, withPeople ...int64) (*HomeStats, error) {
	trips, err := s.ListTrips(ctx, TripFilter{UserID: userID, WithPeople: withPeople})
	if err != nil {
		return nil, err
	}
	st := &HomeStats{Countries: []string{}}
	today := Today()
	seen := map[string]bool{}
	var next *Trip
	for i := range trips {
		t := &trips[i]
		if t.Archived || t.Status == StatusCancelled {
			continue
		}
		st.Trips++
		if t.EndDate != "" && t.EndDate < today {
			st.Days += t.Days
		} else if t.StartDate != "" && t.StartDate <= today && t.EndDate >= today {
			st.Days += DaysBetween(t.StartDate, today)
		}
		if t.StartDate > today {
			st.Upcoming++
			if next == nil || t.StartDate < next.StartDate {
				next = t
			}
		}
		if d := strings.TrimSpace(t.Destination); d != "" && !seen[d] {
			seen[d] = true
			st.Countries = append(st.Countries, d)
		}
	}
	if next != nil {
		st.NextTripID = next.ID
	}
	ids := make([]int64, 0, len(trips))
	for i := range trips {
		if !trips[i].Archived {
			ids = append(ids, trips[i].ID)
		}
	}
	if len(ids) > 0 {
		ph, args := inInts(ids)
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(status = 'visited'), 0) FROM places WHERE trip_id IN (`+ph+`)`, args...).Scan(&st.Places, &st.Visited)
	}
	return st, nil
}

// nullF is a helper for optional floats in queries.
func nullF(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

var _ = time.Now
