package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

type tripKeyT int

const tripKey tripKeyT = 2

type tripCtx struct {
	trip *store.Trip
	role string // owner | editor | viewer
}

func tripFrom(ctx context.Context) *tripCtx {
	t, _ := ctx.Value(tripKey).(*tripCtx)
	return t
}

// tripAccess loads the trip and checks the user can see it.
func (s *Server) tripAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r.Context())
		id := idParam(r, "tripID")
		role := s.store.TripRole(r.Context(), id, u)
		if role == "" {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		t, err := s.store.GetTrip(r.Context(), id, u.ID)
		if err != nil {
			s.fail(w, err, "get trip")
			return
		}
		if t.Role == "" {
			t.Role = role
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tripKey, &tripCtx{trip: t, role: role})))
	})
}

// canEdit rejects viewers.
func (s *Server) canEdit(w http.ResponseWriter, r *http.Request) bool {
	tc := tripFrom(r.Context())
	if tc == nil || tc.role == "viewer" {
		writeErr(w, http.StatusForbidden, "you can only view this trip")
		return false
	}
	return true
}

func (s *Server) isOwner(r *http.Request) bool {
	tc := tripFrom(r.Context())
	return tc != nil && tc.role == "owner"
}

func (s *Server) handleListTrips(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	// Your own trips and the ones shared with you; admins see no more than anyone else.
	f := s.visibleTrips(r.Context(), u.ID)
	f.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	if v := r.URL.Query().Get("archived"); v != "" {
		b := queryBool(r, "archived")
		f.Archived = &b
	}
	trips, err := s.store.ListTrips(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list trips")
		return
	}
	for i := range trips {
		trips[i].Counts, _ = s.store.TripCounts(r.Context(), trips[i].ID)
		trips[i].Countries = s.tripCountries(r.Context(), &trips[i])
	}
	writeJSON(w, http.StatusOK, trips)
}

type tripInput struct {
	Name        string   `json:"name"`
	Destination string   `json:"destination"`
	Description string   `json:"description"`
	StartDate   string   `json:"start_date"`
	EndDate     string   `json:"end_date"`
	Status      string   `json:"status"`
	Currency    string   `json:"currency"`
	Budget      int64    `json:"budget"`
	Color       string   `json:"color"`
	Link        string   `json:"link"`
	Notes       string   `json:"notes"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	Timezone    string   `json:"timezone"`
	FireflyTag  string   `json:"firefly_tag"`
	Dawarich    *bool    `json:"dawarich"`
	Immich      *bool    `json:"immich"`
	Archived    *bool    `json:"archived"`
	// CountriesManual replaces the detected countries ([] = detect again)
	CountriesManual []string `json:"countries_manual"`
}

func applyTripInput(t *store.Trip, in tripInput, keys map[string]bool) string {
	has := func(k string) bool { return keys == nil || keys[k] }
	if has("name") {
		t.Name = strings.TrimSpace(in.Name)
	}
	if has("destination") {
		t.Destination = strings.TrimSpace(in.Destination)
	}
	if has("description") {
		t.Description = in.Description
	}
	if has("start_date") {
		t.StartDate = in.StartDate
	}
	if has("end_date") {
		t.EndDate = in.EndDate
	}
	if has("status") && in.Status != "" {
		if !store.ValidStatus(in.Status) {
			return "invalid status"
		}
		t.Status = in.Status
	}
	if has("currency") && in.Currency != "" {
		if !currency.Valid(in.Currency) {
			return "unknown currency"
		}
		t.Currency = strings.ToUpper(in.Currency)
	}
	if has("budget") {
		t.Budget = in.Budget
	}
	if has("color") {
		t.Color = in.Color
	}
	if has("link") {
		t.Link = strings.TrimSpace(in.Link)
	}
	if has("notes") {
		t.Notes = in.Notes
	}
	if has("lat") {
		t.Lat = in.Lat
	}
	if has("lng") {
		t.Lng = in.Lng
	}
	if has("timezone") {
		t.Timezone = in.Timezone
	}
	if has("firefly_tag") {
		t.FireflyTag = strings.TrimSpace(in.FireflyTag)
	}
	if has("dawarich") && in.Dawarich != nil {
		t.Dawarich = *in.Dawarich
	}
	if has("immich") && in.Immich != nil {
		t.Immich = *in.Immich
	}
	if has("archived") && in.Archived != nil {
		t.Archived = *in.Archived
	}
	if has("countries_manual") {
		t.CountriesManual = store.SplitCodes(strings.Join(in.CountriesManual, ","))
		if len(t.CountriesManual) != len(in.CountriesManual) {
			for _, c := range in.CountriesManual {
				if len(strings.TrimSpace(c)) != 2 {
					return "countries are two-letter codes (PT, ES)"
				}
			}
		}
	}
	if t.Name == "" {
		return "name is required"
	}
	if !validDate(t.StartDate) || !validDate(t.EndDate) {
		return "dates must be YYYY-MM-DD"
	}
	if t.StartDate != "" && t.EndDate != "" && t.EndDate < t.StartDate {
		return "the end date is before the start date"
	}
	if t.Budget < 0 {
		return "budget cannot be negative"
	}
	return ""
}

func (s *Server) handleCreateTrip(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var in tripInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), u.ID)
	t := &store.Trip{OwnerID: u.ID, Currency: st.HomeCurrency, Status: store.StatusPlanning, Dawarich: true, Immich: true}
	if msg := applyTripInput(t, in, nil); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateTrip(r.Context(), t, s.ownerEvent(r.Context(), t.OwnerID, func() (string, string, map[string]any) {
		return "travel.trip_created", fmt.Sprintf("trip:%d", t.ID), map[string]any{
			"trip_id": t.ID, "name": t.Name, "destination": t.Destination, "start_date": t.StartDate, "end_date": t.EndDate,
			"status": t.Status, "currency": t.Currency, "budget": t.Budget, "firefly_tag": t.FireflyTag,
		}
	})); err != nil {
		s.fail(w, err, "create trip")
		return
	}
	s.store.LogActivity(r.Context(), t.ID, u.ID, "created", t.Name)
	// Default checklists so the trip is useful from the first minute.
	for _, cl := range []struct{ name, kind string }{{"Packing", "packing"}, {"Before we leave", "todo"}} {
		_ = s.store.CreateChecklist(r.Context(), &store.Checklist{TripID: t.ID, Name: cl.name, Kind: cl.kind})
	}
	t, _ = s.store.GetTrip(r.Context(), t.ID, u.ID)

	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleGetTrip(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	t.Counts, _ = s.store.TripCounts(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleUpdateTrip(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var in tripInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	oldTag, oldStart, oldEnd := t.FireflyTag, t.StartDate, t.EndDate
	// The Money tag pulls spending from the owner's Pocket Money, so only
	// the owner may point it somewhere else.
	if keys["firefly_tag"] && strings.TrimSpace(in.FireflyTag) != t.FireflyTag && !s.isOwner(r) {
		writeErr(w, http.StatusForbidden, "only the owner can change the Money tag")
		return
	}
	if msg := applyTripInput(t, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateTrip(r.Context(), t, s.ownerEvent(r.Context(), t.OwnerID, func() (string, string, map[string]any) {
		return "travel.trip_updated", fmt.Sprintf("trip:%d", t.ID), map[string]any{
			"trip_id": t.ID, "name": t.Name, "destination": t.Destination, "start_date": t.StartDate, "end_date": t.EndDate,
			"status": t.Status, "currency": t.Currency, "budget": t.Budget, "firefly_tag": t.FireflyTag, "archived": t.Archived,
		}
	})); err != nil {
		s.fail(w, err, "update trip")
		return
	}
	if oldStart != t.StartDate || oldEnd != t.EndDate || oldTag != t.FireflyTag {
		_ = s.store.CacheDelete(r.Context(), cacheKey(t.ID, ""))
		if t.FireflyTag != "" {
			go s.syncMoney(context.Background(), t.ID)
		}
	}
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "updated", "trip details")
	t.Counts, _ = s.store.TripCounts(r.Context(), t.ID)

	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTrip(w http.ResponseWriter, r *http.Request) {
	if !s.isOwner(r) {
		writeErr(w, http.StatusForbidden, "only the owner can delete a trip")
		return
	}
	t := tripFrom(r.Context()).trip
	files := s.store.TripFilenames(r.Context(), t.ID)
	if err := s.store.DeleteTrip(r.Context(), t.ID); err != nil {
		s.fail(w, err, "delete trip")
		return
	}
	for _, f := range files {
		s.removeFile(f)
	}
	_ = s.store.CacheDelete(r.Context(), cacheKey(t.ID, ""))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTripActivity(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	a, err := s.store.ListActivity(r.Context(), t.ID, queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "activity")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleTripMembers(w http.ResponseWriter, r *http.Request) {
	if !s.isOwner(r) {
		writeErr(w, http.StatusForbidden, "only the owner can change who has access")
		return
	}
	t := tripFrom(r.Context()).trip
	var in struct {
		Members []struct {
			UserID int64  `json:"user_id"`
			Role   string `json:"role"`
		} `json:"members"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Replace the member list (owner is implicit).
	cur, _ := s.store.TripMembers(r.Context(), t.ID)
	keep := map[int64]bool{}
	for _, m := range in.Members {
		if m.UserID == t.OwnerID {
			continue
		}
		if m.Role != "viewer" {
			m.Role = "editor"
		}
		keep[m.UserID] = true
		if err := s.store.SetTripMember(r.Context(), t.ID, m.UserID, m.Role); err != nil {
			s.fail(w, err, "set member")
			return
		}
		// a companion with access is on the trip (and shares its bills)
		if mu, err := s.store.GetUser(r.Context(), m.UserID); err == nil {
			if p, err := s.store.PersonByUser(r.Context(), t.OwnerID, mu); err == nil {
				_ = s.store.AddTripPerson(r.Context(), &store.TripPerson{TripID: t.ID, PersonID: &p.ID, Name: p.Name, Color: p.Color, Weight: 1, Active: true})
			}
		}
	}
	for _, m := range cur {
		if m.UserID != t.OwnerID && !keep[m.UserID] {
			_ = s.store.SetTripMember(r.Context(), t.ID, m.UserID, "")
		}
	}
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "updated", "members")
	members, _ := s.store.TripMembers(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleTripCover(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	b, name, ct, err := s.readUpload(r, "file")
	if err != nil || len(b) == 0 {
		writeErr(w, http.StatusBadRequest, "no file")
		return
	}
	if !strings.HasPrefix(ct, "image/") {
		writeErr(w, http.StatusBadRequest, "cover must be an image")
		return
	}
	rel, err := s.saveFile(t.ID, extOf(name, ct), b)
	if err != nil {
		s.fail(w, err, "save cover")
		return
	}
	s.removeFile(t.CoverPath)
	t.CoverPath = rel
	if err := s.store.UpdateTrip(r.Context(), t); err != nil {
		s.fail(w, err, "update trip")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTripCoverImage(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	p := t.CoverPath
	if p == "" {
		p = s.store.CoverFallback(r.Context(), t.ID)
	}
	s.serveFile(w, r, p, "", "")
}

func (s *Server) handleTripCoverDelete(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	s.removeFile(t.CoverPath)
	t.CoverPath = ""
	_ = s.store.UpdateTrip(r.Context(), t)
	w.WriteHeader(http.StatusNoContent)
}

func extOf(name, ct string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && len(name)-i <= 6 {
		return name[i:]
	}
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	}
	return ""
}

// handleDuplicateTrip copies a trip (places, stays, transport, planned costs,
// checklists) as a new planning trip — "same as last year".
func (s *Server) handleDuplicateTrip(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	src := tripFrom(r.Context()).trip
	ctx := r.Context()
	var in struct {
		Name      string `json:"name"`
		StartDate string `json:"start_date"`
	}
	_ = decode(r, &in)
	nt := *src
	nt.ID, nt.OwnerID, nt.CoverPath, nt.Status, nt.Archived, nt.FireflyTag, nt.ExternalID = 0, u.ID, "", store.StatusPlanning, false, "", ""
	nt.Name = strings.TrimSpace(in.Name)
	if nt.Name == "" {
		nt.Name = src.Name + " (copy)"
	}
	shift := 0
	if validDate(in.StartDate) && in.StartDate != "" && src.StartDate != "" {
		a, _ := time.Parse("2006-01-02", src.StartDate)
		b, _ := time.Parse("2006-01-02", in.StartDate)
		shift = int(b.Sub(a).Hours() / 24)
	}
	shiftDate := func(d string) string {
		if shift == 0 || len(d) < 10 {
			return d
		}
		t, err := time.Parse("2006-01-02", d[:10])
		if err != nil {
			return d
		}
		return t.AddDate(0, 0, shift).Format("2006-01-02") + d[10:]
	}
	nt.StartDate, nt.EndDate = shiftDate(src.StartDate), shiftDate(src.EndDate)
	if err := s.store.CreateTrip(ctx, &nt); err != nil {
		s.fail(w, err, "create trip")
		return
	}
	// the same people come along (the owner is already on it)
	if people, err := s.store.ListTripPeople(ctx, src.ID); err == nil {
		for _, tp := range people {
			if tp.Role == "owner" {
				continue
			}
			np := tp
			np.ID, np.TripID = 0, nt.ID
			_ = s.store.AddTripPerson(ctx, &np)
		}
	}
	places, _ := s.store.ListPlaces(ctx, src.ID)
	for _, p := range places {
		p.ID, p.TripID, p.ImagePath, p.VisitedAt, p.ExternalID = 0, nt.ID, "", "", ""
		if p.Status == "visited" || p.Status == "skipped" {
			p.Status = "planned"
		}
		p.DayDate = shiftDate(p.DayDate)
		_ = s.store.CreatePlace(ctx, &p)
	}
	lodging, _ := s.store.ListLodging(ctx, src.ID)
	for _, l := range lodging {
		l.ID, l.TripID, l.ImagePath, l.ExternalID, l.Paid = 0, nt.ID, "", "", false
		l.CheckIn, l.CheckOut = shiftDate(l.CheckIn), shiftDate(l.CheckOut)
		_ = s.store.CreateLodging(ctx, &l)
	}
	transport, _ := s.store.ListTransport(ctx, src.ID)
	for _, t := range transport {
		t.ID, t.TripID, t.ExternalID, t.Paid = 0, nt.ID, "", false
		t.DepartAt, t.ArriveAt = shiftDate(t.DepartAt), shiftDate(t.ArriveAt)
		_ = s.store.CreateTransport(ctx, &t)
	}
	expenses, _ := s.store.ListExpenses(ctx, src.ID)
	for _, e := range expenses {
		if e.Kind != "planned" || store.IsSynced(e.Source) {
			continue
		}
		e.ID, e.TripID, e.ExternalID, e.LinkKind, e.LinkID = 0, nt.ID, "", "", 0
		e.Date = shiftDate(e.Date)
		_ = s.store.CreateExpense(ctx, &e)
	}
	lists, _ := s.store.ListChecklists(ctx, src.ID)
	for _, c := range lists {
		nc := store.Checklist{TripID: nt.ID, Name: c.Name, Kind: c.Kind}
		if err := s.store.CreateChecklist(ctx, &nc); err == nil {
			for _, it := range c.Items {
				_ = s.store.AddChecklistItem(ctx, nt.ID, &store.ChecklistItem{ChecklistID: nc.ID, Text: it.Text})
			}
		}
	}
	s.store.LogActivity(ctx, nt.ID, u.ID, "created", "copied from "+src.Name)
	out, _ := s.store.GetTrip(ctx, nt.ID, u.ID)
	writeJSON(w, http.StatusCreated, out)
}

// ---- overview: everything the trip page needs in one request ----

func (s *Server) handleTripOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := tripFrom(ctx).trip
	places, err := s.store.ListPlaces(ctx, t.ID)
	if err != nil {
		s.fail(w, err, "places")
		return
	}
	s.attachPhotos(ctx, t.ID, places)
	lodging, _ := s.store.ListLodging(ctx, t.ID)
	transport, _ := s.store.ListTransport(ctx, t.ID)
	expenses, _ := s.store.ListExpenses(ctx, t.ID)
	lists, _ := s.store.ListChecklists(ctx, t.ID)
	journal, _ := s.store.ListJournal(ctx, t.ID)
	docs, _ := s.store.ListDocuments(ctx, t.ID)
	t.Counts, _ = s.store.TripCounts(ctx, t.ID)
	// Spending is synced from the trip owner's Pocket Money; route, photos
	// and AI use the viewer's own accounts.
	moneyVia := s.moneyVia(ctx, t.OwnerID)
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	_, aiStatus := s.inferenceFor(ctx, actorID(ctx), st.AIProvider, st.OpenAIKey != "")
	aiModel := firstNonEmpty(st.OpenAIModel, "gpt-4o-mini")
	if aiStatus.Selected == "pocket" {
		aiModel = "pocket-ai"
	}
	locSrc, locOK := s.locationStatus(ctx, actorID(ctx))
	imURL := ""
	_, imConn, imErr := s.immichFor(ctx, actorID(ctx))
	if imErr == nil {
		imURL = imConn.public
	}
	_, ppURL, ppErr := s.photoprismFor(ctx, actorID(ctx))
	budget := s.budgetSummary(t, expenses)
	tripPeople, _ := s.store.ListTripPeople(ctx, t.ID)
	budgetPlan, _ := s.store.BudgetPlan(ctx, t.ID)
	if budgetPlan == nil {
		budgetPlan = []store.PlanLine{}
	}
	split, _ := s.splitInfo(ctx, t, tripFrom(ctx).role != "viewer")
	// Freshness of the cost sync.
	_, syncedAt, _ := s.store.CacheGet(ctx, cacheKey(t.ID, "firefly-synced"))
	writeJSON(w, http.StatusOK, map[string]any{
		"trip": t, "places": places, "lodging": lodging, "transport": transport, "expenses": expenses,
		"checklists": lists, "journal": journal, "documents": docs, "budget": budget,
		"countries":   countryDays(t, lodging, places),
		"people":      tripPeople,
		"split":       split,
		"day_costs":   dayCosts(t, expenses, tripPeople),
		"budget_plan": budgetPlan,
		"integrations": map[string]any{
			"firefly": map[string]any{"configured": moneyVia != "", "via": moneyVia, "tag": t.FireflyTag, "synced_at": syncedAt},
			// "dawarich" is the recorded route, from Pocket Movement or Dawarich (source)
			"dawarich":   map[string]any{"configured": locOK, "enabled": t.Dawarich, "source": locSrc},
			"immich":     map[string]any{"configured": imErr == nil, "enabled": t.Immich, "url": imURL},
			"photoprism": map[string]any{"configured": ppErr == nil, "url": ppURL},
			"ai":         map[string]any{"configured": aiStatus.Ready, "model": aiModel, "provider": aiStatus.Selected},
		},
	})
}

// handleTripRefresh drops caches and re-syncs the costs from Pocket Money.
func (s *Server) handleTripRefresh(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	_ = s.store.CacheDelete(r.Context(), cacheKey(t.ID, ""))
	res := map[string]any{"ok": true}
	if t.FireflyTag != "" {
		n, err := s.syncMoney(r.Context(), t.ID)
		res["firefly_rows"] = n
		if err != nil {
			res["firefly_error"] = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func cacheKey(tripID int64, what string) string {
	return "trip:" + strconv.FormatInt(tripID, 10) + ":" + what
}

// userCacheKey keys what came from one person's own accounts (their Dawarich
// track, their Immich photos) so another member of the trip never gets it.
func userCacheKey(tripID, userID int64, what string) string {
	return cacheKey(tripID, "u"+strconv.FormatInt(userID, 10)+":"+what)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
