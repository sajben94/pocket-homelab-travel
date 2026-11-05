package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// ---- "travelling with": ?with=3,7 narrows every trip list to trips with all of them ----

type withKey struct{}

// withPeopleFromQuery reads ?with=<person ids> into the request context.
func withPeopleFromQuery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.URL.Query().Get("with"); v != "" {
			var ids []int64
			for _, p := range strings.Split(v, ",") {
				if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && id > 0 {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				r = r.WithContext(context.WithValue(r.Context(), withKey{}, ids))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// withPeople returns the people the request is narrowed to (nil = everyone).
func withPeople(ctx context.Context) []int64 {
	ids, _ := ctx.Value(withKey{}).([]int64)
	return ids
}

// visibleTrips is the filter for "the trips this person sees", narrowed to
// the people picked in "travelling with".
func (s *Server) visibleTrips(ctx context.Context, userID int64) store.TripFilter {
	return store.TripFilter{UserID: userID, WithPeople: withPeople(ctx)}
}

// ---- the address book ----

type personInput struct {
	Name   *string `json:"name"`
	Color  *string `json:"color"`
	UserID *int64  `json:"user_id"`
	Email  *string `json:"email"`
	Notes  *string `json:"notes"`
}

func (s *Server) applyPerson(ctx context.Context, p *store.Person, in personInput, keys map[string]bool) string {
	if keys["name"] && in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if keys["color"] && in.Color != nil {
		p.Color = strings.TrimSpace(*in.Color)
	}
	if keys["email"] && in.Email != nil {
		p.Email = strings.TrimSpace(*in.Email)
	}
	if keys["notes"] && in.Notes != nil {
		p.Notes = strings.TrimSpace(*in.Notes)
	}
	if keys["user_id"] && !p.IsSelf {
		if in.UserID == nil || *in.UserID <= 0 {
			p.UserID = nil
		} else {
			if _, err := s.store.GetUser(ctx, *in.UserID); err != nil {
				return "no such account"
			}
			p.UserID = in.UserID
		}
	}
	if p.Name == "" {
		return "name is required"
	}
	if len(p.Name) > 80 {
		return "name is too long"
	}
	return ""
}

func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if _, err := s.store.EnsureSelf(r.Context(), u); err != nil {
		s.fail(w, err, "people")
		return
	}
	people, err := s.store.ListPeople(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err, "people")
		return
	}
	writeJSON(w, http.StatusOK, people)
}

func (s *Server) handleCreatePerson(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var in personInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := &store.Person{OwnerID: u.ID}
	if msg := s.applyPerson(r.Context(), p, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreatePerson(r.Context(), p); err != nil {
		s.fail(w, err, "create person")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// PersonTrip is a trip someone was on, with the days and countries.
type PersonTrip struct {
	ID        int64               `json:"id"`
	Name      string              `json:"name"`
	Color     string              `json:"color"`
	Status    string              `json:"status"`
	StartDate string              `json:"start_date"`
	EndDate   string              `json:"end_date"`
	Days      int                 `json:"days"`
	HasCover  bool                `json:"has_cover"`
	UpdatedAt string              `json:"updated_at"`
	Upcoming  bool                `json:"upcoming"`
	Countries []store.CountryDays `json:"countries"`
	Others    []string            `json:"others"` // everyone else on it
}

// handleGetPerson answers GET /api/v1/people/{id}: the person, every trip
// together, days and countries together, and who else came along.
func (s *Server) handleGetPerson(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ctx := r.Context()
	p, err := s.store.GetPerson(ctx, u.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "person")
		return
	}
	trips, err := s.store.ListTrips(ctx, store.TripFilter{UserID: u.ID, WithPeople: []int64{p.ID}})
	if err != nil {
		s.fail(w, err, "person")
		return
	}
	today := store.Today()
	out := []PersonTrip{}
	days, upcoming := 0, 0
	countries := map[string]int{}
	company := map[string]int{}
	for i := range trips {
		t := &trips[i]
		if t.Status == store.StatusCancelled {
			continue
		}
		cds := s.tripCountries(ctx, t)
		people, _ := s.store.ListTripPeople(ctx, t.ID)
		others := []string{}
		for _, tp := range people {
			if tp.PersonID != nil && *tp.PersonID == p.ID {
				continue
			}
			if !(tp.IsSelf && p.IsSelf) && !tp.IsSelf {
				others = append(others, tp.Name)
				company[tp.Name]++
			}
		}
		pt := PersonTrip{ID: t.ID, Name: t.Name, Color: t.Color, Status: t.Status, StartDate: t.StartDate, EndDate: t.EndDate, Days: t.Days, HasCover: t.HasCover, UpdatedAt: t.UpdatedAt, Upcoming: t.StartDate > today, Countries: cds, Others: others}
		out = append(out, pt)
		if pt.Upcoming {
			upcoming++
			continue
		}
		d := t.Days
		if t.EndDate > today {
			d = store.DaysBetween(t.StartDate, today)
		}
		days += d
		for _, c := range cds {
			if c.Days > 0 {
				countries[c.Code] += c.Days
			}
		}
	}
	type kv struct {
		Key string `json:"name"`
		N   int    `json:"n"`
	}
	often := []kv{}
	for k, n := range company {
		often = append(often, kv{k, n})
	}
	sort.Slice(often, func(i, j int) bool {
		return often[i].N > often[j].N || (often[i].N == often[j].N && often[i].Key < often[j].Key)
	})
	cl := []store.CountryDays{}
	for c, d := range countries {
		cl = append(cl, store.CountryDays{Code: c, Days: d})
	}
	sort.Slice(cl, func(i, j int) bool { return cl[i].Days > cl[j].Days })
	writeJSON(w, http.StatusOK, map[string]any{
		"person": p, "trips": out,
		"totals":    map[string]int{"trips": len(out) - upcoming, "upcoming": upcoming, "days": days, "countries": len(cl)},
		"countries": cl, "company": often,
	})
}

func (s *Server) handleUpdatePerson(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ctx := r.Context()
	p, err := s.store.GetPerson(ctx, u.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "person")
		return
	}
	var in personInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	before := p.UserID
	if msg := s.applyPerson(ctx, p, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdatePerson(ctx, p); err != nil {
		s.fail(w, err, "update person")
		return
	}
	// linking an account opens your trips with them to it; unlinking closes them
	if !sameID(before, p.UserID) {
		s.syncPersonAccess(ctx, u.ID, p, before)
	}
	writeJSON(w, http.StatusOK, p)
}

// syncPersonAccess gives a newly linked account editor access to the owner's
// trips the person is on, and takes it from the account that was linked before.
func (s *Server) syncPersonAccess(ctx context.Context, ownerID int64, p *store.Person, before *int64) {
	trips, _ := s.store.ListTrips(ctx, store.TripFilter{UserID: ownerID, Owned: true, WithPeople: []int64{p.ID}})
	for _, t := range trips {
		if before != nil && *before != ownerID {
			_ = s.store.SetTripMember(ctx, t.ID, *before, "")
		}
		if p.UserID != nil && *p.UserID != ownerID {
			if s.store.TripRole(ctx, t.ID, &store.User{ID: *p.UserID}) == "" {
				_ = s.store.SetTripMember(ctx, t.ID, *p.UserID, "editor")
			}
		}
	}
}

func sameID(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (s *Server) handleDeletePerson(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	err := s.store.DeletePerson(r.Context(), u.ID, idParam(r, "id"))
	if errors.Is(err, store.ErrSelf) {
		writeErr(w, http.StatusBadRequest, "you cannot remove yourself")
		return
	}
	if err != nil {
		s.fail(w, err, "delete person")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMergePerson folds this person into another (duplicates, an imported
// name and the real entry): their trips and bills move over.
func (s *Server) handleMergePerson(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var in struct {
		Into int64 `json:"into"`
	}
	if err := decode(r, &in); err != nil || in.Into <= 0 {
		writeErr(w, http.StatusBadRequest, "into is required")
		return
	}
	err := s.store.MergePeople(r.Context(), u.ID, idParam(r, "id"), in.Into, s.moveMemberRefs)
	if errors.Is(err, store.ErrSelf) {
		writeErr(w, http.StatusBadRequest, "you cannot merge yourself into someone else — merge them into you")
		return
	}
	if err != nil {
		s.fail(w, err, "merge")
		return
	}
	p, err := s.store.GetPerson(r.Context(), u.ID, in.Into)
	if err != nil {
		s.fail(w, err, "merge")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// moveMemberRefs moves what references one trip member to another before the
// first is folded away (bills come with the shared expenses).
func (s *Server) moveMemberRefs(tx *sql.Tx, tripID, from, into int64) error {
	return moveMemberRefsImpl(tx, tripID, from, into)
}

// ---- people on a trip ----

func (s *Server) handleListTripPeople(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	people, err := s.store.ListTripPeople(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "trip people")
		return
	}
	writeJSON(w, http.StatusOK, people)
}

type tripPersonInput struct {
	PersonID *int64   `json:"person_id"`
	Name     *string  `json:"name"`
	Weight   *float64 `json:"weight"`
	Active   *bool    `json:"active"`
	Color    *string  `json:"color"`
	Role     *string  `json:"role"` // editor | viewer | "" — access for a linked account (owner only)
}

// handleAddTripPerson puts someone on the trip: a person from the trip
// owner's book (person_id) or a new name, which is added to the book so they
// can come along next time.
func (s *Server) handleAddTripPerson(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	var in tripPersonInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var p *store.Person
	var err error
	switch {
	case in.PersonID != nil:
		p, err = s.store.GetPerson(ctx, t.OwnerID, *in.PersonID)
	case in.Name != nil && strings.TrimSpace(*in.Name) != "":
		p, err = s.store.PersonByName(ctx, t.OwnerID, *in.Name)
	default:
		writeErr(w, http.StatusBadRequest, "person_id or name is required")
		return
	}
	if err != nil {
		s.fail(w, err, "person")
		return
	}
	tp := &store.TripPerson{TripID: t.ID, PersonID: &p.ID, Name: p.Name, Color: p.Color, Weight: 1, Active: true}
	if in.Weight != nil && *in.Weight > 0 {
		tp.Weight = *in.Weight
	}
	if err := s.store.AddTripPerson(ctx, tp); err != nil {
		s.fail(w, err, "add person")
		return
	}
	// someone with an account gets in (the owner decides how far)
	if p.UserID != nil && *p.UserID != t.OwnerID && s.isOwner(r) {
		role := "editor"
		if in.Role != nil && *in.Role == "viewer" {
			role = "viewer"
		}
		if in.Role == nil || *in.Role != "" {
			_ = s.store.SetTripMember(ctx, t.ID, *p.UserID, role)
		}
	}
	s.store.LogActivity(ctx, t.ID, userFrom(ctx).ID, "added", p.Name)
	tp, _ = s.store.GetTripPerson(ctx, t.ID, tp.ID)
	writeJSON(w, http.StatusCreated, tp)
}

func (s *Server) handleUpdateTripPerson(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	tp, err := s.store.GetTripPerson(ctx, t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "trip person")
		return
	}
	var in tripPersonInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		tp.Name = strings.TrimSpace(*in.Name)
	}
	if in.Weight != nil {
		if *in.Weight <= 0 {
			writeErr(w, http.StatusBadRequest, "weight must be more than 0")
			return
		}
		tp.Weight = *in.Weight
	}
	if in.Active != nil {
		tp.Active = *in.Active
	}
	if in.Color != nil {
		tp.Color = strings.TrimSpace(*in.Color)
	}
	if in.Role != nil && tp.UserID != nil && tp.Role != "owner" {
		if !s.isOwner(r) {
			writeErr(w, http.StatusForbidden, "only the owner can change who has access")
			return
		}
		role := *in.Role
		if role != "" && role != "viewer" {
			role = "editor"
		}
		if err := s.store.SetTripMember(ctx, t.ID, *tp.UserID, role); err != nil {
			s.fail(w, err, "access")
			return
		}
	}
	if err := s.store.UpdateTripPerson(ctx, tp); err != nil {
		s.fail(w, err, "update trip person")
		return
	}
	tp, _ = s.store.GetTripPerson(ctx, t.ID, tp.ID)
	writeJSON(w, http.StatusOK, tp)
}

func (s *Server) handleDeleteTripPerson(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	tp, err := s.store.GetTripPerson(ctx, t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "trip person")
		return
	}
	if tp.Role == "owner" {
		writeErr(w, http.StatusBadRequest, "the owner stays on their trip")
		return
	}
	if msg := s.memberInUse(ctx, t.ID, tp.ID); msg != "" {
		writeErr(w, http.StatusConflict, msg)
		return
	}
	if err := s.store.DeleteTripPerson(ctx, t.ID, tp.ID); err != nil {
		s.fail(w, err, "remove person")
		return
	}
	if tp.UserID != nil && s.isOwner(r) {
		_ = s.store.SetTripMember(ctx, t.ID, *tp.UserID, "")
	}
	s.store.LogActivity(ctx, t.ID, userFrom(ctx).ID, "removed", tp.Name)
	w.WriteHeader(http.StatusNoContent)
}

// memberInUse says why someone cannot leave a trip (they paid or owe a bill).
func (s *Server) memberInUse(ctx context.Context, tripID, memberID int64) string {
	return s.memberInUseImpl(ctx, tripID, memberID)
}
