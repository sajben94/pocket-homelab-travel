package store

import "context"

const visibleTrips = `(SELECT id FROM trips WHERE owner_id = ? OR id IN (SELECT trip_id FROM trip_members WHERE user_id = ?))`

// PlacesForUser returns every place on the trips userID can see.
func (s *Store) PlacesForUser(ctx context.Context, userID int64) ([]Place, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+placeCols+` FROM places WHERE trip_id IN `+visibleTrips+` ORDER BY trip_id, CASE WHEN day_date = '' THEN 1 ELSE 0 END, day_date, sort_order, id`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Place{}
	for rows.Next() {
		p, err := scanPlace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// LodgingForUser returns every stay on the trips userID can see.
func (s *Store) LodgingForUser(ctx context.Context, userID int64) ([]Lodging, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+lodgingCols+` FROM lodging WHERE trip_id IN `+visibleTrips+` ORDER BY trip_id, check_in, id`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lodging{}
	for rows.Next() {
		l, err := scanLodging(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}
