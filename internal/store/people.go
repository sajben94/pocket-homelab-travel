package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Person is someone in a person's address book of travel companions.
type Person struct {
	ID        int64  `json:"id"`
	OwnerID   int64  `json:"owner_id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	UserID    *int64 `json:"user_id"` // linked account on this box
	Email     string `json:"email"`
	Notes     string `json:"notes"`
	IsSelf    bool   `json:"is_self"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// Derived
	Trips    int    `json:"trips"`               // trips together
	LastTrip string `json:"last_trip,omitempty"` // start of the latest one
}

// TripPerson is someone on a trip; also a member of its shared expenses.
type TripPerson struct {
	ID        int64   `json:"id"`
	TripID    int64   `json:"trip_id"`
	PersonID  *int64  `json:"person_id"`
	Name      string  `json:"name"`
	Weight    float64 `json:"weight"`
	Active    bool    `json:"active"`
	Color     string  `json:"color"`
	UpdatedTS int64   `json:"updated_ts"`

	// Derived from the person and the trip's access list
	UserID *int64 `json:"user_id"`
	IsSelf bool   `json:"is_self"` // the trip owner
	Role   string `json:"role"`    // owner | editor | viewer | "" (no account access)
}

// ErrSelf is returned when someone tries to delete their own entry.
var ErrSelf = errors.New("that is you")

const personCols = `id, owner_id, name, color, user_id, email, notes, is_self, created_at, updated_at`

func scanPerson(row interface{ Scan(...any) error }) (*Person, error) {
	var p Person
	var self int
	var uid sql.NullInt64
	if err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Color, &uid, &p.Email, &p.Notes, &self, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	if uid.Valid {
		p.UserID = &uid.Int64
	}
	p.IsSelf = self == 1
	return &p, nil
}

// EnsureSelf returns the user's own entry in their address book, creating it.
func (s *Store) EnsureSelf(ctx context.Context, u *User) (*Person, error) {
	p, err := scanPerson(s.DB.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE owner_id = ? AND is_self = 1`, u.ID))
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	name := u.Name
	if name == "" {
		name = u.Email
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO people (owner_id, name, user_id, email, is_self) VALUES (?, ?, ?, ?, 1)`, u.ID, name, u.ID, u.Email); err != nil {
		return nil, err
	}
	return scanPerson(s.DB.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE owner_id = ? AND is_self = 1`, u.ID))
}

// ListPeople returns an address book, the owner first, then by name, with
// how many of the owner's visible trips each person was on.
func (s *Store) ListPeople(ctx context.Context, ownerID int64) ([]Person, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+personCols+`,
		(SELECT COUNT(DISTINCT tp.trip_id) FROM trip_people tp JOIN trips t ON t.id = tp.trip_id WHERE tp.person_id = people.id AND t.status <> 'cancelled'),
		COALESCE((SELECT MAX(t.start_date) FROM trip_people tp JOIN trips t ON t.id = tp.trip_id WHERE tp.person_id = people.id AND t.status <> 'cancelled'), '')
		FROM people WHERE owner_id = ? ORDER BY is_self DESC, name COLLATE NOCASE`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		var self int
		var uid sql.NullInt64
		if err := rows.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Color, &uid, &p.Email, &p.Notes, &self, &p.CreatedAt, &p.UpdatedAt, &p.Trips, &p.LastTrip); err != nil {
			return nil, err
		}
		if uid.Valid {
			p.UserID = &uid.Int64
		}
		p.IsSelf = self == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPerson loads one person from ownerID's book.
func (s *Store) GetPerson(ctx context.Context, ownerID, id int64) (*Person, error) {
	return scanPerson(s.DB.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE id = ? AND owner_id = ?`, id, ownerID))
}

// PersonByUser finds (or adds) the entry for an account in ownerID's book.
func (s *Store) PersonByUser(ctx context.Context, ownerID int64, u *User) (*Person, error) {
	if u.ID == ownerID {
		return s.EnsureSelf(ctx, u)
	}
	p, err := scanPerson(s.DB.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE owner_id = ? AND user_id = ? ORDER BY id LIMIT 1`, ownerID, u.ID))
	if err == nil || !errors.Is(err, ErrNotFound) {
		return p, err
	}
	name := u.Name
	if name == "" {
		name = u.Email
	}
	np := &Person{OwnerID: ownerID, Name: name, UserID: &u.ID, Email: u.Email}
	return np, s.CreatePerson(ctx, np)
}

// PersonByName finds a person by name (ignoring case) in ownerID's book, or adds one.
func (s *Store) PersonByName(ctx context.Context, ownerID int64, name string) (*Person, error) {
	name = strings.TrimSpace(name)
	p, err := scanPerson(s.DB.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE owner_id = ? AND name = ? COLLATE NOCASE ORDER BY is_self DESC, id LIMIT 1`, ownerID, name))
	if err == nil || !errors.Is(err, ErrNotFound) {
		return p, err
	}
	np := &Person{OwnerID: ownerID, Name: name}
	return np, s.CreatePerson(ctx, np)
}

// CreatePerson adds someone to an address book.
func (s *Store) CreatePerson(ctx context.Context, p *Person) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO people (owner_id, name, color, user_id, email, notes) VALUES (?, ?, ?, ?, ?, ?)`, p.OwnerID, p.Name, p.Color, p.UserID, p.Email, p.Notes)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	p.CreatedAt, p.UpdatedAt = Now(), Now()
	return nil
}

// UpdatePerson saves a person; a new name or colour carries over to the
// trips they are on (and their shared-expense member entries).
func (s *Store) UpdatePerson(ctx context.Context, p *Person) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE people SET name = ?, color = ?, user_id = ?, email = ?, notes = ?, updated_at = ? WHERE id = ? AND owner_id = ?`, p.Name, p.Color, p.UserID, p.Email, p.Notes, Now(), p.ID, p.OwnerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE trip_people SET name = ?, color = ?, updated_ts = ? WHERE person_id = ? AND (name <> ? OR color <> ?)`, p.Name, p.Color, time.Now().Unix(), p.ID, p.Name, p.Color); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePerson removes someone from the book; on past trips they stay as a
// plain name (their bills keep adding up).
func (s *Store) DeletePerson(ctx context.Context, ownerID, id int64) error {
	p, err := s.GetPerson(ctx, ownerID, id)
	if err != nil {
		return err
	}
	if p.IsSelf {
		return ErrSelf
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM people WHERE id = ? AND owner_id = ?`, id, ownerID)
	return err
}

// MergePeople moves everything of person from into person into (both in
// ownerID's book) and deletes from. On a trip with both, from's entry folds
// into into's: whatever referenced it (bills) is moved by moveMember.
func (s *Store) MergePeople(ctx context.Context, ownerID, from, into int64, moveMember func(tx *sql.Tx, tripID, fromMember, intoMember int64) error) error {
	if from == into {
		return nil
	}
	pf, err := s.GetPerson(ctx, ownerID, from)
	if err != nil {
		return err
	}
	if pf.IsSelf {
		return ErrSelf
	}
	pi, err := s.GetPerson(ctx, ownerID, into)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type pair struct{ trip, fromM, intoM int64 }
	var both []pair
	rows, err := tx.QueryContext(ctx, `SELECT a.trip_id, a.id, b.id FROM trip_people a JOIN trip_people b ON b.trip_id = a.trip_id AND b.person_id = ? WHERE a.person_id = ?`, into, from)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.trip, &p.fromM, &p.intoM); err != nil {
			rows.Close()
			return err
		}
		both = append(both, p)
	}
	rows.Close()
	for _, p := range both {
		if moveMember != nil {
			if err := moveMember(tx, p.trip, p.fromM, p.intoM); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM trip_people WHERE id = ?`, p.fromM); err != nil {
			return err
		}
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `UPDATE trip_people SET person_id = ?, name = ?, color = ?, updated_ts = ? WHERE person_id = ?`, into, pi.Name, pi.Color, now, from); err != nil {
		return err
	}
	// the merged entry keeps the account link and notes it had
	if pi.UserID == nil && pf.UserID != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE people SET user_id = ? WHERE id = ?`, *pf.UserID, into); err != nil {
			return err
		}
	}
	if pf.Notes != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE people SET notes = TRIM(notes || char(10) || ?) WHERE id = ?`, pf.Notes, into); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM people WHERE id = ?`, from); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- trip people ----

const tripPersonCols = `tp.id, tp.trip_id, tp.person_id, tp.name, tp.weight, tp.active, tp.color, tp.updated_ts, p.user_id, COALESCE(p.is_self, 0), t.owner_id, COALESCE(m.role, '')`
const tripPersonFrom = ` FROM trip_people tp JOIN trips t ON t.id = tp.trip_id LEFT JOIN people p ON p.id = tp.person_id LEFT JOIN trip_members m ON m.trip_id = tp.trip_id AND m.user_id = p.user_id`

func scanTripPerson(row interface{ Scan(...any) error }) (*TripPerson, error) {
	var tp TripPerson
	var pid, uid sql.NullInt64
	var active, self int
	var owner int64
	if err := row.Scan(&tp.ID, &tp.TripID, &pid, &tp.Name, &tp.Weight, &active, &tp.Color, &tp.UpdatedTS, &uid, &self, &owner, &tp.Role); err != nil {
		return nil, notFound(err)
	}
	if pid.Valid {
		tp.PersonID = &pid.Int64
	}
	if uid.Valid {
		tp.UserID = &uid.Int64
		if uid.Int64 == owner {
			tp.Role = "owner"
		}
	}
	tp.Active, tp.IsSelf = active == 1, self == 1 && uid.Valid && uid.Int64 == owner
	return &tp, nil
}

// ListTripPeople returns who is on a trip, the owner first.
func (s *Store) ListTripPeople(ctx context.Context, tripID int64) ([]TripPerson, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+tripPersonCols+tripPersonFrom+` WHERE tp.trip_id = ? ORDER BY CASE WHEN p.user_id = t.owner_id THEN 0 ELSE 1 END, tp.name COLLATE NOCASE, tp.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TripPerson{}
	for rows.Next() {
		tp, err := scanTripPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *tp)
	}
	return out, rows.Err()
}

// GetTripPerson loads one person of a trip.
func (s *Store) GetTripPerson(ctx context.Context, tripID, id int64) (*TripPerson, error) {
	return scanTripPerson(s.DB.QueryRowContext(ctx, `SELECT `+tripPersonCols+tripPersonFrom+` WHERE tp.trip_id = ? AND tp.id = ?`, tripID, id))
}

// AddTripPerson puts someone on a trip (a person already there is returned as is).
func (s *Store) AddTripPerson(ctx context.Context, tp *TripPerson) error {
	if tp.PersonID != nil {
		var id int64
		if err := s.DB.QueryRowContext(ctx, `SELECT id FROM trip_people WHERE trip_id = ? AND person_id = ?`, tp.TripID, *tp.PersonID).Scan(&id); err == nil {
			got, err := s.GetTripPerson(ctx, tp.TripID, id)
			if err == nil {
				*tp = *got
			}
			return err
		}
	}
	if tp.Weight <= 0 {
		tp.Weight = 1
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO trip_people (trip_id, person_id, name, weight, active, color, updated_ts) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tp.TripID, tp.PersonID, tp.Name, tp.Weight, boolInt(tp.Active), tp.Color, time.Now().Unix())
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	got, err := s.GetTripPerson(ctx, tp.TripID, id)
	if err == nil {
		*tp = *got
	}
	return err
}

// UpdateTripPerson saves name, weight, active and colour of someone on a trip.
func (s *Store) UpdateTripPerson(ctx context.Context, tp *TripPerson) error {
	tp.UpdatedTS = time.Now().Unix()
	_, err := s.DB.ExecContext(ctx, `UPDATE trip_people SET name = ?, weight = ?, active = ?, color = ?, updated_ts = ? WHERE id = ? AND trip_id = ?`,
		tp.Name, tp.Weight, boolInt(tp.Active), tp.Color, tp.UpdatedTS, tp.ID, tp.TripID)
	return err
}

// DeleteTripPerson takes someone off a trip.
func (s *Store) DeleteTripPerson(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM trip_people WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddOwnerToTrip puts the trip owner's own entry on the trip.
func (s *Store) AddOwnerToTrip(ctx context.Context, t *Trip) error {
	u, err := s.GetUser(ctx, t.OwnerID)
	if err != nil {
		return err
	}
	self, err := s.EnsureSelf(ctx, u)
	if err != nil {
		return err
	}
	return s.AddTripPerson(ctx, &TripPerson{TripID: t.ID, PersonID: &self.ID, Name: self.Name, Color: self.Color, Weight: 1, Active: true})
}

// TripPeopleNames returns the names of everyone on each trip (for search).
func (s *Store) TripPeopleNames(ctx context.Context, tripIDs []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(tripIDs) == 0 {
		return out, nil
	}
	q, args := inInts(tripIDs)
	rows, err := s.DB.QueryContext(ctx, `SELECT trip_id, name FROM trip_people WHERE trip_id IN (`+q+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = append(out[id], n)
	}
	return out, rows.Err()
}

func inInts(ids []int64) (string, []any) {
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i], args[i] = "?", id
	}
	return strings.Join(ph, ","), args
}
