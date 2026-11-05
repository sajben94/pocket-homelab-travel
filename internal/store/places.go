package store

import "context"

const placeCols = `id, trip_id, name, category, description, address, lat, lng, link, phone, rating, price, currency, day_date, start_time, duration_min, status, visited_at, booking_ref, tags, notes, image_path, sort_order, external_id, created_at, updated_at, country_code`

func scanPlace(row interface{ Scan(...any) error }) (*Place, error) {
	var p Place
	if err := row.Scan(&p.ID, &p.TripID, &p.Name, &p.Category, &p.Description, &p.Address, &p.Lat, &p.Lng, &p.Link, &p.Phone, &p.Rating, &p.Price, &p.Currency, &p.DayDate, &p.StartTime, &p.DurationMin, &p.Status, &p.VisitedAt, &p.BookingRef, &p.Tags, &p.Notes, &p.ImagePath, &p.SortOrder, &p.ExternalID, &p.CreatedAt, &p.UpdatedAt, &p.CountryCode); err != nil {
		return nil, notFound(err)
	}
	p.HasImage = p.ImagePath != ""
	return &p, nil
}

// ListPlaces returns a trip's places, scheduled ones first in day/time order.
func (s *Store) ListPlaces(ctx context.Context, tripID int64) ([]Place, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+placeCols+` FROM places WHERE trip_id = ? ORDER BY CASE WHEN day_date = '' THEN 1 ELSE 0 END, day_date, sort_order, start_time, id`, tripID)
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

// GetPlace loads one place.
func (s *Store) GetPlace(ctx context.Context, tripID, id int64) (*Place, error) {
	return scanPlace(s.DB.QueryRowContext(ctx, `SELECT `+placeCols+` FROM places WHERE id = ? AND trip_id = ?`, id, tripID))
}

// CreatePlace inserts a place at the end of its day.
func (s *Store) CreatePlace(ctx context.Context, p *Place) error {
	if p.Status == "" {
		p.Status = "idea"
	}
	if p.Category == "" {
		p.Category = "other"
	}
	if p.SortOrder == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM places WHERE trip_id = ? AND day_date = ?`, p.TripID, p.DayDate).Scan(&p.SortOrder)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO places (trip_id, name, category, description, address, lat, lng, link, phone, rating, price, currency, day_date, start_time, duration_min, status, visited_at, booking_ref, tags, notes, image_path, sort_order, external_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.TripID, p.Name, p.Category, p.Description, p.Address, p.Lat, p.Lng, p.Link, p.Phone, p.Rating, p.Price, p.Currency, p.DayDate, p.StartTime, p.DurationMin, p.Status, p.VisitedAt, p.BookingRef, p.Tags, p.Notes, p.ImagePath, p.SortOrder, p.ExternalID)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	p.HasImage = p.ImagePath != ""
	return nil
}

// UpdatePlace saves a place.
func (s *Store) UpdatePlace(ctx context.Context, p *Place) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE places SET country_code = CASE WHEN lat IS ? AND lng IS ? THEN country_code ELSE '' END, name = ?, category = ?, description = ?, address = ?, lat = ?, lng = ?, link = ?, phone = ?, rating = ?, price = ?, currency = ?, day_date = ?, start_time = ?, duration_min = ?, status = ?, visited_at = ?, booking_ref = ?, tags = ?, notes = ?, image_path = ?, sort_order = ?, updated_at = ? WHERE id = ? AND trip_id = ?`,
		nullF(p.Lat), nullF(p.Lng), p.Name, p.Category, p.Description, p.Address, p.Lat, p.Lng, p.Link, p.Phone, p.Rating, p.Price, p.Currency, p.DayDate, p.StartTime, p.DurationMin, p.Status, p.VisitedAt, p.BookingRef, p.Tags, p.Notes, p.ImagePath, p.SortOrder, Now(), p.ID, p.TripID)
	p.HasImage = p.ImagePath != ""
	return err
}

// DeletePlace removes a place.
func (s *Store) DeletePlace(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM places WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderPlaces assigns day + sort order to ids in sequence (drag and drop).
func (s *Store) ReorderPlaces(ctx context.Context, tripID int64, day string, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE places SET day_date = ?, sort_order = ?, status = CASE WHEN status = 'idea' AND ? <> '' THEN 'planned' ELSE status END, updated_at = ? WHERE id = ? AND trip_id = ?`, day, i+1, day, Now(), id, tripID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ---- lodging ----

const lodgingCols = `id, trip_id, name, type, address, lat, lng, check_in, check_out, reservation_number, link, phone, price, currency, paid, rating, notes, image_path, external_id, created_at, updated_at, country_code`

func scanLodging(row interface{ Scan(...any) error }) (*Lodging, error) {
	var l Lodging
	var paid int
	if err := row.Scan(&l.ID, &l.TripID, &l.Name, &l.Type, &l.Address, &l.Lat, &l.Lng, &l.CheckIn, &l.CheckOut, &l.ReservationNumber, &l.Link, &l.Phone, &l.Price, &l.Currency, &paid, &l.Rating, &l.Notes, &l.ImagePath, &l.ExternalID, &l.CreatedAt, &l.UpdatedAt, &l.CountryCode); err != nil {
		return nil, notFound(err)
	}
	l.Paid = paid == 1
	l.HasImage = l.ImagePath != ""
	return &l, nil
}

// ListLodging returns stays in check-in order.
func (s *Store) ListLodging(ctx context.Context, tripID int64) ([]Lodging, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+lodgingCols+` FROM lodging WHERE trip_id = ? ORDER BY check_in, id`, tripID)
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

// GetLodging loads one stay.
func (s *Store) GetLodging(ctx context.Context, tripID, id int64) (*Lodging, error) {
	return scanLodging(s.DB.QueryRowContext(ctx, `SELECT `+lodgingCols+` FROM lodging WHERE id = ? AND trip_id = ?`, id, tripID))
}

// CreateLodging inserts a stay.
func (s *Store) CreateLodging(ctx context.Context, l *Lodging) error {
	if l.Type == "" {
		l.Type = "hotel"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO lodging (trip_id, name, type, address, lat, lng, check_in, check_out, reservation_number, link, phone, price, currency, paid, rating, notes, image_path, external_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.TripID, l.Name, l.Type, l.Address, l.Lat, l.Lng, l.CheckIn, l.CheckOut, l.ReservationNumber, l.Link, l.Phone, l.Price, l.Currency, boolInt(l.Paid), l.Rating, l.Notes, l.ImagePath, l.ExternalID)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	l.HasImage = l.ImagePath != ""
	return nil
}

// UpdateLodging saves a stay.
func (s *Store) UpdateLodging(ctx context.Context, l *Lodging) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE lodging SET country_code = CASE WHEN lat IS ? AND lng IS ? THEN country_code ELSE '' END, name = ?, type = ?, address = ?, lat = ?, lng = ?, check_in = ?, check_out = ?, reservation_number = ?, link = ?, phone = ?, price = ?, currency = ?, paid = ?, rating = ?, notes = ?, image_path = ?, updated_at = ? WHERE id = ? AND trip_id = ?`,
		nullF(l.Lat), nullF(l.Lng), l.Name, l.Type, l.Address, l.Lat, l.Lng, l.CheckIn, l.CheckOut, l.ReservationNumber, l.Link, l.Phone, l.Price, l.Currency, boolInt(l.Paid), l.Rating, l.Notes, l.ImagePath, Now(), l.ID, l.TripID)
	l.HasImage = l.ImagePath != ""
	return err
}

// DeleteLodging removes a stay.
func (s *Store) DeleteLodging(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM lodging WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- transport ----

const transportCols = `id, trip_id, type, name, carrier, number, from_name, from_lat, from_lng, to_name, to_lat, to_lng, depart_at, arrive_at, booking_ref, seat, link, price, currency, paid, distance_km, notes, external_id, created_at, updated_at`

func scanTransport(row interface{ Scan(...any) error }) (*Transport, error) {
	var t Transport
	var paid int
	if err := row.Scan(&t.ID, &t.TripID, &t.Type, &t.Name, &t.Carrier, &t.Number, &t.FromName, &t.FromLat, &t.FromLng, &t.ToName, &t.ToLat, &t.ToLng, &t.DepartAt, &t.ArriveAt, &t.BookingRef, &t.Seat, &t.Link, &t.Price, &t.Currency, &paid, &t.DistanceKm, &t.Notes, &t.ExternalID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	t.Paid = paid == 1
	return &t, nil
}

// ListTransport returns legs in departure order.
func (s *Store) ListTransport(ctx context.Context, tripID int64) ([]Transport, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+transportCols+` FROM transport WHERE trip_id = ? ORDER BY depart_at, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Transport{}
	for rows.Next() {
		t, err := scanTransport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTransport loads one leg.
func (s *Store) GetTransport(ctx context.Context, tripID, id int64) (*Transport, error) {
	return scanTransport(s.DB.QueryRowContext(ctx, `SELECT `+transportCols+` FROM transport WHERE id = ? AND trip_id = ?`, id, tripID))
}

// CreateTransport inserts a leg.
func (s *Store) CreateTransport(ctx context.Context, t *Transport) error {
	if t.Type == "" {
		t.Type = "other"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO transport (trip_id, type, name, carrier, number, from_name, from_lat, from_lng, to_name, to_lat, to_lng, depart_at, arrive_at, booking_ref, seat, link, price, currency, paid, distance_km, notes, external_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TripID, t.Type, t.Name, t.Carrier, t.Number, t.FromName, t.FromLat, t.FromLng, t.ToName, t.ToLat, t.ToLng, t.DepartAt, t.ArriveAt, t.BookingRef, t.Seat, t.Link, t.Price, t.Currency, boolInt(t.Paid), t.DistanceKm, t.Notes, t.ExternalID)
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	return nil
}

// UpdateTransport saves a leg.
func (s *Store) UpdateTransport(ctx context.Context, t *Transport) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE transport SET type = ?, name = ?, carrier = ?, number = ?, from_name = ?, from_lat = ?, from_lng = ?, to_name = ?, to_lat = ?, to_lng = ?, depart_at = ?, arrive_at = ?, booking_ref = ?, seat = ?, link = ?, price = ?, currency = ?, paid = ?, distance_km = ?, notes = ?, updated_at = ? WHERE id = ? AND trip_id = ?`,
		t.Type, t.Name, t.Carrier, t.Number, t.FromName, t.FromLat, t.FromLng, t.ToName, t.ToLat, t.ToLng, t.DepartAt, t.ArriveAt, t.BookingRef, t.Seat, t.Link, t.Price, t.Currency, boolInt(t.Paid), t.DistanceKm, t.Notes, Now(), t.ID, t.TripID)
	return err
}

// DeleteTransport removes a leg.
func (s *Store) DeleteTransport(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM transport WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
