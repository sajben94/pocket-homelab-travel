package store

import "context"

// HiddenPhotos is the set of photo keys userID hid from tripID.
func (s *Store) HiddenPhotos(ctx context.Context, tripID, userID int64) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT photo FROM trip_photo_hidden WHERE trip_id = ? AND user_id = ?`, tripID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// SetPhotosHidden hides (or shows again) photos on tripID for userID.
func (s *Store) SetPhotosHidden(ctx context.Context, tripID, userID int64, photos []string, hidden bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := `DELETE FROM trip_photo_hidden WHERE trip_id = ? AND user_id = ? AND photo = ?`
	if hidden {
		q = `INSERT OR IGNORE INTO trip_photo_hidden (trip_id, user_id, photo) VALUES (?, ?, ?)`
	}
	for _, p := range photos {
		if _, err := tx.ExecContext(ctx, q, tripID, userID, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DismissedPhotoTrips are the start dates of found trips userID said no to.
func (s *Store) DismissedPhotoTrips(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT start_date FROM photo_trip_dismissed WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out[d] = true
	}
	return out, rows.Err()
}

// DismissPhotoTrip remembers that the trip found from start is not a trip.
func (s *Store) DismissPhotoTrip(ctx context.Context, userID int64, start string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO photo_trip_dismissed (user_id, start_date) VALUES (?, ?)`, userID, start)
	return err
}

// PhotoTripSplits are the dates where userID said a new trip starts inside
// one found in their photos.
func (s *Store) PhotoTripSplits(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT date FROM photo_trip_splits WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out[d] = true
	}
	return out, rows.Err()
}

// AddPhotoTripSplit remembers that a new trip starts on date.
func (s *Store) AddPhotoTripSplit(ctx context.Context, userID int64, date string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO photo_trip_splits (user_id, date) VALUES (?, ?)`, userID, date)
	return err
}

// RemovePhotoTripSplits forgets the splits on from..to (inclusive).
func (s *Store) RemovePhotoTripSplits(ctx context.Context, userID int64, from, to string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM photo_trip_splits WHERE user_id = ? AND date >= ? AND date <= ?`, userID, from, to)
	return err
}
