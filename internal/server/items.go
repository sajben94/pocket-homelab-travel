package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/geo"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// ---- places ----

func (s *Server) handleListPlaces(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	p, err := s.store.ListPlaces(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "list places")
		return
	}
	s.attachPhotos(r.Context(), t.ID, p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleGetPlace(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	p, err := s.store.GetPlace(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get place")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type placeInput struct {
	Name        *string  `json:"name"`
	Category    *string  `json:"category"`
	Description *string  `json:"description"`
	Address     *string  `json:"address"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	Link        *string  `json:"link"`
	Phone       *string  `json:"phone"`
	Rating      *float64 `json:"rating"`
	Price       *int64   `json:"price"`
	Currency    *string  `json:"currency"`
	DayDate     *string  `json:"day_date"`
	StartTime   *string  `json:"start_time"`
	DurationMin *int     `json:"duration_min"`
	Status      *string  `json:"status"`
	VisitedAt   *string  `json:"visited_at"`
	BookingRef  *string  `json:"booking_ref"`
	Tags        *string  `json:"tags"`
	Notes       *string  `json:"notes"`
	SortOrder   *int     `json:"sort_order"`
}

func applyPlace(p *store.Place, in placeInput, keys map[string]bool) string {
	setS := func(k string, dst *string, v *string) {
		if keys[k] && v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	setS("name", &p.Name, in.Name)
	setS("category", &p.Category, in.Category)
	setS("description", &p.Description, in.Description)
	setS("address", &p.Address, in.Address)
	setS("link", &p.Link, in.Link)
	setS("phone", &p.Phone, in.Phone)
	setS("day_date", &p.DayDate, in.DayDate)
	setS("start_time", &p.StartTime, in.StartTime)
	setS("status", &p.Status, in.Status)
	setS("visited_at", &p.VisitedAt, in.VisitedAt)
	setS("booking_ref", &p.BookingRef, in.BookingRef)
	setS("tags", &p.Tags, in.Tags)
	setS("notes", &p.Notes, in.Notes)
	if keys["lat"] {
		p.Lat = in.Lat
	}
	if keys["lng"] {
		p.Lng = in.Lng
	}
	if keys["rating"] {
		p.Rating = in.Rating
	}
	if keys["price"] && in.Price != nil {
		p.Price = *in.Price
	}
	if keys["currency"] && in.Currency != nil {
		p.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if keys["duration_min"] && in.DurationMin != nil {
		p.DurationMin = *in.DurationMin
	}
	if keys["sort_order"] && in.SortOrder != nil {
		p.SortOrder = *in.SortOrder
	}
	if p.Name == "" {
		return "name is required"
	}
	switch p.Status {
	case "", "idea", "planned", "booked", "visited", "skipped":
	default:
		return "invalid status"
	}
	if p.Status == "" {
		p.Status = "idea"
	}
	if !validDate(p.DayDate) {
		return "day must be YYYY-MM-DD"
	}
	if p.DayDate != "" && p.Status == "idea" {
		p.Status = "planned"
	}
	if p.Currency != "" && !currency.Valid(p.Currency) {
		return "unknown currency"
	}
	if p.Price < 0 {
		return "price cannot be negative"
	}
	if p.Rating != nil && (*p.Rating < 0 || *p.Rating > 5) {
		return "rating must be 0–5"
	}
	if (p.Lat == nil) != (p.Lng == nil) {
		return "latitude and longitude go together"
	}
	return ""
}

func allKeys(in any) map[string]bool {
	// For create: treat every field as sent.
	return map[string]bool{"name": true, "category": true, "description": true, "address": true, "lat": true, "lng": true, "link": true, "phone": true, "rating": true, "price": true, "currency": true, "day_date": true, "start_time": true, "duration_min": true, "status": true, "visited_at": true, "booking_ref": true, "tags": true, "notes": true, "sort_order": true,
		"type": true, "check_in": true, "check_out": true, "reservation_number": true, "paid": true, "carrier": true, "number": true, "from_name": true, "from_lat": true, "from_lng": true, "to_name": true, "to_lat": true, "to_lng": true, "depart_at": true, "arrive_at": true, "seat": true, "distance_km": true}
}

func (s *Server) handleCreatePlace(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var in placeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := &store.Place{TripID: t.ID, Category: "sight", Status: "idea", Currency: t.Currency}
	if msg := applyPlace(p, in, allKeys(in)); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreatePlace(r.Context(), p); err != nil {
		s.fail(w, err, "create place")
		return
	}
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "added place", p.Name)
	s.store.TouchTrip(r.Context(), t.ID)
	p.Photos = []store.PlacePhoto{}
	s.autoPhotos(actorID(r.Context()), t, p)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePlace(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	p, err := s.store.GetPlace(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get place")
		return
	}
	var in placeInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	wasVisited := p.Status == "visited"
	if msg := applyPlace(p, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if p.Status == "visited" && !wasVisited && p.VisitedAt == "" {
		p.VisitedAt = store.Today()
	}
	if err := s.store.UpdatePlace(r.Context(), p); err != nil {
		s.fail(w, err, "update place")
		return
	}
	s.store.TouchTrip(r.Context(), t.ID)
	if p.Status == "visited" && !wasVisited {
		s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "visited", p.Name)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePlace(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	p, err := s.store.GetPlace(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get place")
		return
	}
	paths := s.store.PlacePhotoPaths(r.Context(), p.ID)
	if err := s.store.DeletePlace(r.Context(), t.ID, p.ID); err != nil {
		s.fail(w, err, "delete place")
		return
	}
	for _, f := range paths {
		s.removeFile(f)
	}
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "removed place", p.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderPlaces(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var in struct {
		Day string  `json:"day"`
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil || !validDate(in.Day) {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderPlaces(r.Context(), t.ID, in.Day, in.IDs); err != nil {
		s.fail(w, err, "reorder")
		return
	}
	p, _ := s.store.ListPlaces(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handlePlaceImage(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	p, err := s.store.GetPlace(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get place")
		return
	}
	b, name, ct, err := s.readUpload(r, "file")
	if err != nil || len(b) == 0 || !strings.HasPrefix(ct, "image/") {
		writeErr(w, http.StatusBadRequest, "an image file is required")
		return
	}
	rel, err := s.saveFile(t.ID, extOf(name, ct), b)
	if err != nil {
		s.fail(w, err, "save image")
		return
	}
	if err := s.store.AddPlacePhoto(r.Context(), &store.PlacePhoto{PlaceID: p.ID, Path: rel, Source: "upload"}); err != nil {
		s.fail(w, err, "add photo")
		return
	}
	p, _ = s.store.GetPlace(r.Context(), t.ID, p.ID)
	p.Photos, _ = s.store.ListPlacePhotos(r.Context(), p.ID)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handlePlaceImageGet(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	p, err := s.store.GetPlace(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get place")
		return
	}
	s.serveFile(w, r, p.ImagePath, "", "")
}

// ---- lodging ----

func (s *Server) handleListLodging(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	l, err := s.store.ListLodging(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "list lodging")
		return
	}
	writeJSON(w, http.StatusOK, l)
}

type lodgingInput struct {
	Name              *string  `json:"name"`
	Type              *string  `json:"type"`
	Address           *string  `json:"address"`
	Lat               *float64 `json:"lat"`
	Lng               *float64 `json:"lng"`
	CheckIn           *string  `json:"check_in"`
	CheckOut          *string  `json:"check_out"`
	ReservationNumber *string  `json:"reservation_number"`
	Link              *string  `json:"link"`
	Phone             *string  `json:"phone"`
	Price             *int64   `json:"price"`
	Currency          *string  `json:"currency"`
	Paid              *bool    `json:"paid"`
	Rating            *float64 `json:"rating"`
	Notes             *string  `json:"notes"`
}

func applyLodging(l *store.Lodging, in lodgingInput, keys map[string]bool) string {
	setS := func(k string, dst *string, v *string) {
		if keys[k] && v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	setS("name", &l.Name, in.Name)
	setS("type", &l.Type, in.Type)
	setS("address", &l.Address, in.Address)
	setS("check_in", &l.CheckIn, in.CheckIn)
	setS("check_out", &l.CheckOut, in.CheckOut)
	setS("reservation_number", &l.ReservationNumber, in.ReservationNumber)
	setS("link", &l.Link, in.Link)
	setS("phone", &l.Phone, in.Phone)
	setS("notes", &l.Notes, in.Notes)
	if keys["lat"] {
		l.Lat = in.Lat
	}
	if keys["lng"] {
		l.Lng = in.Lng
	}
	if keys["rating"] {
		l.Rating = in.Rating
	}
	if keys["price"] && in.Price != nil {
		l.Price = *in.Price
	}
	if keys["currency"] && in.Currency != nil {
		l.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if keys["paid"] && in.Paid != nil {
		l.Paid = *in.Paid
	}
	if l.Name == "" {
		return "name is required"
	}
	if l.Type == "" {
		l.Type = "hotel"
	}
	// check-in / check-out: a day (YYYY-MM-DD) or a day and a time (YYYY-MM-DDTHH:MM)
	for _, v := range []string{l.CheckIn, l.CheckOut} {
		if !validStayTime(v) {
			return "check-in and check-out are YYYY-MM-DD or YYYY-MM-DDTHH:MM"
		}
	}
	if l.CheckIn != "" && l.CheckOut != "" && l.CheckOut < l.CheckIn {
		return "check-out is before check-in"
	}
	if l.Currency != "" && !currency.Valid(l.Currency) {
		return "unknown currency"
	}
	if (l.Lat == nil) != (l.Lng == nil) {
		return "latitude and longitude go together"
	}
	return ""
}

func (s *Server) handleCreateLodging(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var in lodgingInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	l := &store.Lodging{TripID: t.ID, Currency: t.Currency}
	if msg := applyLodging(l, in, allKeys(in)); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateLodging(r.Context(), l); err != nil {
		s.fail(w, err, "create lodging")
		return
	}
	s.syncItemCost(r.Context(), t, "lodging", l.ID, l.Name, "lodging", l.Price, l.Currency, l.CheckIn, l.Paid)
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "added stay", l.Name)
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleUpdateLodging(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	l, err := s.store.GetLodging(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get lodging")
		return
	}
	var in lodgingInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := applyLodging(l, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateLodging(r.Context(), l); err != nil {
		s.fail(w, err, "update lodging")
		return
	}
	s.syncItemCost(r.Context(), t, "lodging", l.ID, l.Name, "lodging", l.Price, l.Currency, l.CheckIn, l.Paid)
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteLodging(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	l, err := s.store.GetLodging(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get lodging")
		return
	}
	if err := s.store.DeleteLodging(r.Context(), t.ID, l.ID); err != nil {
		s.fail(w, err, "delete lodging")
		return
	}
	s.removeFile(l.ImagePath)
	s.syncItemCost(r.Context(), t, "lodging", l.ID, l.Name, "lodging", 0, "", "", false)
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "removed stay", l.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLodgingImage(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	l, err := s.store.GetLodging(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get lodging")
		return
	}
	b, name, ct, err := s.readUpload(r, "file")
	if err != nil || len(b) == 0 || !strings.HasPrefix(ct, "image/") {
		writeErr(w, http.StatusBadRequest, "an image file is required")
		return
	}
	rel, err := s.saveFile(t.ID, extOf(name, ct), b)
	if err != nil {
		s.fail(w, err, "save image")
		return
	}
	s.removeFile(l.ImagePath)
	l.ImagePath = rel
	_ = s.store.UpdateLodging(r.Context(), l)
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleLodgingImageGet(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	l, err := s.store.GetLodging(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get lodging")
		return
	}
	s.serveFile(w, r, l.ImagePath, "", "")
}

// ---- transport ----

func (s *Server) handleListTransport(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	l, err := s.store.ListTransport(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "list transport")
		return
	}
	writeJSON(w, http.StatusOK, l)
}

type transportInput struct {
	Type       *string  `json:"type"`
	Name       *string  `json:"name"`
	Carrier    *string  `json:"carrier"`
	Number     *string  `json:"number"`
	FromName   *string  `json:"from_name"`
	FromLat    *float64 `json:"from_lat"`
	FromLng    *float64 `json:"from_lng"`
	ToName     *string  `json:"to_name"`
	ToLat      *float64 `json:"to_lat"`
	ToLng      *float64 `json:"to_lng"`
	DepartAt   *string  `json:"depart_at"`
	ArriveAt   *string  `json:"arrive_at"`
	BookingRef *string  `json:"booking_ref"`
	Seat       *string  `json:"seat"`
	Link       *string  `json:"link"`
	Price      *int64   `json:"price"`
	Currency   *string  `json:"currency"`
	Paid       *bool    `json:"paid"`
	DistanceKm *float64 `json:"distance_km"`
	Notes      *string  `json:"notes"`
}

func applyTransport(t *store.Transport, in transportInput, keys map[string]bool) string {
	setS := func(k string, dst *string, v *string) {
		if keys[k] && v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	setS("type", &t.Type, in.Type)
	setS("name", &t.Name, in.Name)
	setS("carrier", &t.Carrier, in.Carrier)
	setS("number", &t.Number, in.Number)
	setS("from_name", &t.FromName, in.FromName)
	setS("to_name", &t.ToName, in.ToName)
	setS("depart_at", &t.DepartAt, in.DepartAt)
	setS("arrive_at", &t.ArriveAt, in.ArriveAt)
	setS("booking_ref", &t.BookingRef, in.BookingRef)
	setS("seat", &t.Seat, in.Seat)
	setS("link", &t.Link, in.Link)
	setS("notes", &t.Notes, in.Notes)
	if keys["from_lat"] {
		t.FromLat = in.FromLat
	}
	if keys["from_lng"] {
		t.FromLng = in.FromLng
	}
	if keys["to_lat"] {
		t.ToLat = in.ToLat
	}
	if keys["to_lng"] {
		t.ToLng = in.ToLng
	}
	if keys["price"] && in.Price != nil {
		t.Price = *in.Price
	}
	if keys["currency"] && in.Currency != nil {
		t.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if keys["paid"] && in.Paid != nil {
		t.Paid = *in.Paid
	}
	if keys["distance_km"] && in.DistanceKm != nil {
		t.DistanceKm = *in.DistanceKm
	}
	if t.Type == "" {
		t.Type = "other"
	}
	if t.Name == "" && t.FromName == "" && t.ToName == "" {
		return "give the leg a name or a from/to"
	}
	if t.Currency != "" && !currency.Valid(t.Currency) {
		return "unknown currency"
	}
	if t.DistanceKm == 0 && t.FromLat != nil && t.FromLng != nil && t.ToLat != nil && t.ToLng != nil {
		t.DistanceKm = geo.Haversine(*t.FromLat, *t.FromLng, *t.ToLat, *t.ToLng) / 1000
	}
	return ""
}

func (s *Server) handleCreateTransport(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var in transportInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	tr := &store.Transport{TripID: t.ID, Currency: t.Currency}
	if msg := applyTransport(tr, in, allKeys(in)); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateTransport(r.Context(), tr); err != nil {
		s.fail(w, err, "create transport")
		return
	}
	s.syncItemCost(r.Context(), t, "transport", tr.ID, transportLabel(tr), "transport", tr.Price, tr.Currency, tr.DepartAt, tr.Paid)
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "added transport", transportLabel(tr))
	writeJSON(w, http.StatusCreated, tr)
}

func transportLabel(t *store.Transport) string {
	if t.Name != "" {
		return t.Name
	}
	return strings.TrimSpace(t.FromName + " → " + t.ToName)
}

func (s *Server) handleUpdateTransport(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	tr, err := s.store.GetTransport(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get transport")
		return
	}
	var in transportInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := applyTransport(tr, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateTransport(r.Context(), tr); err != nil {
		s.fail(w, err, "update transport")
		return
	}
	s.syncItemCost(r.Context(), t, "transport", tr.ID, transportLabel(tr), "transport", tr.Price, tr.Currency, tr.DepartAt, tr.Paid)
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleDeleteTransport(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	tr, err := s.store.GetTransport(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get transport")
		return
	}
	if err := s.store.DeleteTransport(r.Context(), t.ID, tr.ID); err != nil {
		s.fail(w, err, "delete transport")
		return
	}
	s.syncItemCost(r.Context(), t, "transport", tr.ID, "", "transport", 0, "", "", false)
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "removed transport", transportLabel(tr))
	w.WriteHeader(http.StatusNoContent)
}

// attachPhotos fills Photos on a list of places with one query.
func (s *Server) attachPhotos(ctx context.Context, tripID int64, places []store.Place) {
	m, err := s.store.PhotosForTrip(ctx, tripID)
	if err != nil {
		return
	}
	for i := range places {
		if ph := m[places[i].ID]; ph != nil {
			places[i].Photos = ph
		} else {
			places[i].Photos = []store.PlacePhoto{}
		}
		places[i].HasImage = len(places[i].Photos) > 0
	}
}

// validClock accepts HH:MM (24 h).
func validClock(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	h, m := (int(v[0])-'0')*10+int(v[1]-'0'), (int(v[3])-'0')*10+int(v[4]-'0')
	for _, c := range []byte{v[0], v[1], v[3], v[4]} {
		if c < '0' || c > '9' {
			return false
		}
	}
	return h < 24 && m < 60
}

// validStayTime accepts "", YYYY-MM-DD and YYYY-MM-DDTHH:MM (seconds and an
// RFC 3339 zone from imports are tolerated).
func validStayTime(v string) bool {
	if v == "" {
		return true
	}
	if len(v) < 10 || !validDate(v[:10]) {
		return false
	}
	if len(v) == 10 {
		return true
	}
	return v[10] == 'T' && len(v) >= 16 && validClock(v[11:16])
}
