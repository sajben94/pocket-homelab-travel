package server

// A Nextcloud Cospend-compatible API for a trip's shared expenses, so
// MoneyBuster (and other Cospend clients) can use a trip as a Cospend
// project through its share link: cospend://<host>/<token>/<password>.
//
// MoneyBuster turns that link into https://<host>/index.php/apps/cospend and
// calls Cospend's public API under it (the "old" API, which it uses when it
// cannot find a Cospend version in /ocs/v2.php/cloud/capabilities — we do not
// answer that, so it does). Responses are plain JSON exactly as Cospend's
// DataResponse sends them; requests are form-encoded. Ids are this app's:
// project = the share token, member = trip person, bill = expense,
// category / payment mode / currency = the trip's own lists. Reimbursement
// is Cospend's category -11 both ways.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// Cospend access levels.
const (
	cospendViewer      = 1
	cospendParticipant = 2
	cospendMaintainer  = 3
	cospendAdmin       = 4
)

const cospendReimbursement = -11

// cospendRoutes mounts the public Cospend API.
func (s *Server) cospendRoutes(r chi.Router) {
	r.Route("/index.php/apps/cospend", func(r chi.Router) {
		r.Use(cospendCORS)
		r.Options("/*", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
		for _, prefix := range []string{"/api", "/apiv2", "/apiv3"} {
			p := prefix + "/projects/{token}/{password}"
			r.Get(p, s.cospend(cospendViewer, s.cospendGetProject))
			r.Put(p, s.cospend(cospendAdmin, s.cospendSetProject))
			r.Delete(p, s.cospend(cospendAdmin, func(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
				cospendJSON(w, http.StatusForbidden, map[string]string{"message": "a trip is deleted in Pocket Travel"})
			}))
			r.Get(p+"/members", s.cospend(cospendViewer, s.cospendGetMembers))
			r.Post(p+"/members", s.cospend(cospendMaintainer, s.cospendAddMember))
			r.Put(p+"/members/{memberid}", s.cospend(cospendMaintainer, s.cospendEditMember))
			r.Delete(p+"/members/{memberid}", s.cospend(cospendMaintainer, s.cospendDeleteMember))
			r.Get(p+"/bills", s.cospend(cospendViewer, s.cospendGetBills))
			r.Post(p+"/bills", s.cospend(cospendParticipant, s.cospendAddBill))
			r.Put(p+"/bills", s.cospend(cospendParticipant, s.cospendEditBills))
			r.Delete(p+"/bills", s.cospend(cospendParticipant, s.cospendDeleteBills))
			r.Put(p+"/bills/{billid}", s.cospend(cospendParticipant, s.cospendEditBill))
			r.Delete(p+"/bills/{billid}", s.cospend(cospendParticipant, s.cospendDeleteBill))
			r.Get(p+"/bills/{billid}/repeat", s.cospend(cospendParticipant, s.cospendRepeat))
			r.Delete(p+"/trashbin", s.cospend(cospendParticipant, s.cospendClearTrash))
			r.Get(p+"/statistics", s.cospend(cospendViewer, s.cospendStatistics))
			r.Get(p+"/settle", s.cospend(cospendViewer, s.cospendSettle))
			r.Get(p+"/autosettlement", s.cospend(cospendParticipant, s.cospendAutoSettle))
			r.Post(p+"/currency", s.cospend(cospendMaintainer, s.cospendSaveCurrency))
			r.Put(p+"/currency/{id}", s.cospend(cospendMaintainer, s.cospendSaveCurrency))
			r.Delete(p+"/currency/{id}", s.cospend(cospendMaintainer, s.cospendDeleteCurrency))
			r.Post(p+"/paymentmode", s.cospend(cospendMaintainer, s.cospendSavePaymentMode))
			r.Put(p+"/paymentmode/{id}", s.cospend(cospendMaintainer, s.cospendSavePaymentMode))
			r.Delete(p+"/paymentmode/{id}", s.cospend(cospendMaintainer, s.cospendDeletePaymentMode))
			r.Put(p+"/paymentmode-order", s.cospend(cospendMaintainer, s.cospendOrder("paymentmode")))
			r.Post(p+"/category", s.cospend(cospendMaintainer, s.cospendSaveCategory))
			r.Put(p+"/category/{id}", s.cospend(cospendMaintainer, s.cospendSaveCategory))
			r.Delete(p+"/category/{id}", s.cospend(cospendMaintainer, s.cospendDeleteCategory))
			r.Put(p+"/category-order", s.cospend(cospendMaintainer, s.cospendOrder("category")))
		}
	})
}

func cospendCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
		next.ServeHTTP(w, r)
	})
}

// cospendJSON writes a value as Cospend does: plain JSON, no envelope.
func cospendJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type cospendCtx struct {
	trip  *store.Trip
	share *store.TripShare
	// form values (body, then query), Cospend's parameter names
	form url.Values
}

func (c *cospendCtx) has(k string) bool   { _, ok := c.form[k]; return ok }
func (c *cospendCtx) get(k string) string { return c.form.Get(k) }

// cospend checks the share token and password and the access level the
// link grants, then runs h.
func (s *Server) cospend(min int, h func(http.ResponseWriter, *http.Request, *cospendCtx)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		password, _ := url.PathUnescape(chi.URLParam(r, "password"))
		sh, err := s.store.TripShareByToken(r.Context(), token)
		if err != nil {
			cospendJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
			return
		}
		if sh.Password != "" && subtle.ConstantTimeCompare([]byte(sh.Password), []byte(password)) != 1 {
			time.Sleep(500 * time.Millisecond) // brute force
			cospendJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
			return
		}
		if sh.AccessLevel < min {
			cospendJSON(w, http.StatusForbidden, map[string]string{"message": "You are not allowed to do that"})
			return
		}
		t, err := s.store.GetTrip(r.Context(), sh.TripID, 0)
		if err != nil {
			cospendJSON(w, http.StatusNotFound, map[string]string{"message": "Project not found"})
			return
		}
		_ = s.store.EnsureBillSetup(r.Context(), t.ID)
		form := url.Values{}
		if err := r.ParseForm(); err == nil {
			form = r.Form
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && r.Body != nil {
			var raw map[string]any
			if json.NewDecoder(r.Body).Decode(&raw) == nil {
				for k, v := range raw {
					switch x := v.(type) {
					case []any:
						for _, e := range x {
							form.Add(k, fmt.Sprint(e))
						}
					case nil:
					default:
						form.Set(k, fmt.Sprint(x))
					}
				}
			}
		}
		h(w, r, &cospendCtx{trip: t, share: sh, form: form})
		s.store.TouchTrip(r.Context(), t.ID)
	}
}

// ---- project ----

type cospendColor struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

type cospendMember struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Weight      float64      `json:"weight"`
	Activated   bool         `json:"activated"`
	LastChanged int64        `json:"lastchanged"`
	UserID      *string      `json:"userid"`
	Color       cospendColor `json:"color"`
}

func toCospendMember(p store.TripPerson) cospendMember {
	col := p.Color
	if col == "" {
		col = autoColor(p.Name)
	}
	return cospendMember{ID: p.ID, Name: p.Name, Weight: p.Weight, Activated: p.Active, LastChanged: p.UpdatedTS, Color: hexColor(col)}
}

func hexColor(h string) cospendColor {
	h = strings.TrimPrefix(h, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil || len(h) != 6 {
		return cospendColor{R: 0, G: 130, B: 201}
	}
	return cospendColor{R: int(v >> 16 & 0xff), G: int(v >> 8 & 0xff), B: int(v & 0xff)}
}

var autoColors = []string{"0ea5e9", "f97316", "22c55e", "a855f7", "ef4444", "eab308", "14b8a6", "ec4899", "6366f1", "84cc16"}

// autoColor matches the colour the web app picks for someone without one.
func autoColor(name string) string {
	var h int32
	for _, c := range name {
		h = h*31 + int32(c)
	}
	if h < 0 {
		h = -h
	}
	return autoColors[int(h)%len(autoColors)]
}

func (s *Server) cospendMoney(t *store.Trip, minor int64) float64 {
	d := currency.Decimals(t.Currency)
	return float64(minor) / math.Pow10(d)
}

func (s *Server) cospendMinor(t *store.Trip, major float64) int64 {
	d := currency.Decimals(t.Currency)
	return int64(math.Round(major * math.Pow10(d)))
}

func (s *Server) cospendProject(ctx context.Context, c *cospendCtx) (map[string]any, error) {
	t := c.trip
	info, err := s.splitInfo(ctx, t, false)
	if err != nil {
		return nil, err
	}
	members := []cospendMember{}
	active := []cospendMember{}
	last := int64(0)
	for _, p := range info.People {
		m := toCospendMember(p)
		members = append(members, m)
		if m.Activated {
			active = append(active, m)
		}
		if p.UpdatedTS > last {
			last = p.UpdatedTS
		}
	}
	balance := map[string]float64{}
	for _, b := range info.Balances {
		balance[strconv.FormatInt(b.MemberID, 10)] = s.cospendMoney(t, b.Balance)
	}
	cats := map[string]any{}
	for _, cat := range info.Categories {
		id := cat.ID
		if cat.Key == store.ReimbursementKey {
			continue // Cospend's built-in -11
		}
		cats[strconv.FormatInt(id, 10)] = map[string]any{"id": id, "projectid": c.share.Token, "name": cat.Name, "color": cat.Color, "icon": cat.Icon, "order": cat.SortOrder}
	}
	pms := map[string]any{}
	for _, m := range info.PaymentModes {
		pms[strconv.FormatInt(m.ID, 10)] = map[string]any{"id": m.ID, "projectid": c.share.Token, "name": m.Name, "color": m.Color, "icon": m.Icon, "order": m.SortOrder, "old_id": m.OldID}
	}
	curs := []any{}
	for _, cu := range info.Currencies {
		curs = append(curs, map[string]any{"id": cu.ID, "projectid": c.share.Token, "name": cu.Name, "exchange_rate": cu.ExchangeRate})
	}
	if ts, err := time.Parse(time.RFC3339Nano, t.UpdatedAt); err == nil && ts.Unix() > last {
		last = ts.Unix()
	}
	name := t.Name
	return map[string]any{
		"id": c.share.Token, "name": name, "email": "", "contact_email": "", "autoexport": "n", "lastchanged": last,
		"deletiondisabled": true, "categorysort": "a", "paymentmodesort": "a", "currencyname": t.Currency, "archived_ts": nil,
		"active_members": active, "members": members, "balance": balance,
		"nb_bills": info.Bills, "total_spent": s.cospendMoney(t, info.Total), "nb_trashbin_bills": info.Trash,
		"shares": []any{}, "currencies": curs, "categories": cats, "paymentmodes": pms,
		"myaccesslevel": c.share.AccessLevel,
	}, nil
}

func (s *Server) cospendGetProject(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	p, err := s.cospendProject(r.Context(), c)
	if err != nil {
		cospendJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	cospendJSON(w, http.StatusOK, p)
}

// cospendSetProject renames the trip (other Cospend project options have no meaning here).
func (s *Server) cospendSetProject(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	if n := strings.TrimSpace(c.get("name")); n != "" && n != c.trip.Name {
		c.trip.Name = n
		if err := s.store.UpdateTrip(r.Context(), c.trip); err != nil {
			cospendJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
	}
	cospendJSON(w, http.StatusOK, "UPDATED")
}

// ---- members ----

func (s *Server) cospendGetMembers(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	people, err := s.store.ListTripPeople(r.Context(), c.trip.ID)
	if err != nil {
		cospendJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	since, _ := strconv.ParseInt(c.get("lastchanged"), 10, 64)
	out := []cospendMember{}
	for _, p := range people {
		if p.UpdatedTS >= since {
			out = append(out, toCospendMember(p))
		}
	}
	cospendJSON(w, http.StatusOK, out)
}

// cospendAddMember adds someone: a name the trip owner knows becomes that
// person, a new one is added to their address book.
func (s *Server) cospendAddMember(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	name := strings.TrimSpace(c.get("name"))
	if name == "" {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"name": "This field is required."})
		return
	}
	people, _ := s.store.ListTripPeople(ctx, c.trip.ID)
	for _, p := range people {
		if strings.EqualFold(p.Name, name) {
			cospendJSON(w, http.StatusBadRequest, map[string]string{"name": "This project already has this member"})
			return
		}
	}
	p, err := s.store.PersonByName(ctx, c.trip.OwnerID, name)
	if err != nil {
		cospendJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	tp := &store.TripPerson{TripID: c.trip.ID, PersonID: &p.ID, Name: p.Name, Color: p.Color, Weight: 1, Active: true}
	if v, err := strconv.ParseFloat(c.get("weight"), 64); err == nil && v > 0 {
		tp.Weight = v
	}
	if c.has("active") && c.get("active") == "0" {
		tp.Active = false
	}
	if col := strings.TrimSpace(c.get("color")); col != "" {
		tp.Color = "#" + strings.TrimPrefix(col, "#")
	}
	if err := s.store.AddTripPerson(ctx, tp); err != nil {
		cospendJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.HasPrefix(r.URL.Path, "/index.php/apps/cospend/apiv2/") {
		cospendJSON(w, http.StatusOK, toCospendMember(*tp))
		return
	}
	cospendJSON(w, http.StatusOK, tp.ID)
}

func (s *Server) cospendEditMember(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "memberid"), 10, 64)
	tp, err := s.store.GetTripPerson(ctx, c.trip.ID, id)
	if err != nil {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "This project have no such member"})
		return
	}
	if n := strings.TrimSpace(c.get("name")); n != "" {
		tp.Name = n
	}
	if v, err := strconv.ParseFloat(c.get("weight"), 64); err == nil && v > 0 {
		tp.Weight = v
	}
	if c.has("activated") {
		v := strings.ToLower(c.get("activated"))
		tp.Active = v == "1" || v == "true"
	}
	if col := strings.TrimSpace(c.get("color")); c.has("color") {
		if col == "" {
			tp.Color = ""
		} else {
			tp.Color = "#" + strings.TrimPrefix(col, "#")
		}
	}
	if err := s.store.UpdateTripPerson(ctx, tp); err != nil {
		cospendJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	tp, _ = s.store.GetTripPerson(ctx, c.trip.ID, id)
	cospendJSON(w, http.StatusOK, toCospendMember(*tp))
}

// cospendDeleteMember removes someone who is on no bill, and only switches
// off someone who is (as Cospend does); the trip owner always stays.
func (s *Server) cospendDeleteMember(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "memberid"), 10, 64)
	tp, err := s.store.GetTripPerson(ctx, c.trip.ID, id)
	if err != nil {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	n, _ := s.store.MemberBills(ctx, c.trip.ID, id)
	if n > 0 || tp.Role != "" {
		tp.Active = false
		_ = s.store.UpdateTripPerson(ctx, tp)
	} else {
		_ = s.store.DeleteTripPerson(ctx, c.trip.ID, id)
	}
	cospendJSON(w, http.StatusOK, "OK")
}

// ---- bills ----

type cospendOwer struct {
	ID        int64   `json:"id"`
	Weight    float64 `json:"weight"`
	Name      string  `json:"name"`
	Activated bool    `json:"activated"`
}

func (s *Server) cospendBill(c *cospendCtx, e store.Expense, people map[int64]store.TripPerson, cats map[string]int64, pms map[int64]store.TripPaymentMode) map[string]any {
	ts := billTimestamp(e)
	owers := []cospendOwer{}
	ids := []int64{}
	for _, m := range e.Owers {
		p := people[m]
		owers = append(owers, cospendOwer{ID: m, Weight: p.Weight, Name: p.Name, Activated: p.Active})
		ids = append(ids, m)
	}
	payer := int64(0)
	if e.PayerID != nil {
		payer = *e.PayerID
	}
	catID := cats[e.Category]
	if e.Category == store.ReimbursementKey {
		catID = cospendReimbursement
	}
	pmOld := "n"
	if pm, ok := pms[e.PaymentModeID]; ok && pm.OldID != "" {
		pmOld = pm.OldID
	}
	comment := e.Notes
	if e.Currency != c.trip.Currency {
		// Cospend keeps amounts in the project currency; say what it was
		orig := fmt.Sprintf("%s %s", strconv.FormatFloat(float64(e.Amount)/math.Pow10(currency.Decimals(e.Currency)), 'f', -1, 64), e.Currency)
		if !strings.Contains(comment, orig) {
			comment = strings.TrimSpace(orig + " " + comment)
		}
	}
	deleted := 0
	if e.Deleted {
		deleted = 1
	}
	return map[string]any{
		"id": e.ID, "projectid": c.share.Token, "what": e.Name, "comment": comment, "payer_id": payer,
		"timestamp": ts, "date": time.Unix(ts, 0).UTC().Format("2006-01-02"),
		"amount": s.cospendMoney(c.trip, e.AmountHome), "repeat": e.Repeat, "repeatallactive": boolInt(e.RepeatAllActive),
		"repeatuntil": nilIfEmpty(e.RepeatUntil), "repeatfreq": e.RepeatFreq, "categoryid": catID,
		"paymentmode": pmOld, "paymentmodeid": e.PaymentModeID, "deleted": deleted, "lastchanged": e.UpdatedTS,
		"owers": owers, "owerIds": ids,
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// billTimestamp is the bill's date (and time) as unix seconds, read in UTC as Cospend does.
func billTimestamp(e store.Expense) int64 {
	layout, v := "2006-01-02", e.Date
	if e.BillTime != "" {
		layout, v = "2006-01-02 15:04", e.Date+" "+e.BillTime
	}
	if t, err := time.Parse(layout, v); err == nil {
		if e.BillTime == "" {
			return t.Add(12 * time.Hour).Unix() // midday: the same date in every time zone near ours
		}
		return t.Unix()
	}
	return 0
}

func (s *Server) cospendLookups(ctx context.Context, tripID int64) (map[int64]store.TripPerson, map[string]int64, map[int64]string, map[int64]store.TripPaymentMode, map[string]int64) {
	people := map[int64]store.TripPerson{}
	if list, err := s.store.ListTripPeople(ctx, tripID); err == nil {
		for _, p := range list {
			people[p.ID] = p
		}
	}
	cats, catKeys := map[string]int64{}, map[int64]string{}
	if list, err := s.store.ListTripCategories(ctx, tripID); err == nil {
		for _, c := range list {
			cats[c.Key], catKeys[c.ID] = c.ID, c.Key
		}
	}
	pms, pmOld := map[int64]store.TripPaymentMode{}, map[string]int64{}
	if list, err := s.store.ListTripPaymentModes(ctx, tripID); err == nil {
		for _, m := range list {
			pms[m.ID] = m
			if m.OldID != "" {
				pmOld[m.OldID] = m.ID
			}
		}
	}
	return people, cats, catKeys, pms, pmOld
}

func (s *Server) cospendGetBills(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	f := store.ExpenseFilter{BillsOnly: true}
	if v, err := strconv.ParseInt(c.get("lastchanged"), 10, 64); err == nil {
		f.ChangedFrom = v
	} else if v, err := strconv.ParseInt(c.get("lastChanged"), 10, 64); err == nil {
		f.ChangedFrom = v
	}
	if c.get("deleted") == "1" {
		yes := true
		f.Deleted = &yes
	}
	now := time.Now().Unix()
	bills, err := s.store.ListExpensesWith(ctx, c.trip.ID, f)
	if err != nil {
		cospendJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	people, cats, _, pms, _ := s.cospendLookups(ctx, c.trip.ID)
	out := make([]map[string]any, 0, len(bills))
	for i := len(bills) - 1; i >= 0; i-- { // oldest first, as Cospend
		out = append(out, s.cospendBill(c, bills[i], people, cats, pms))
	}
	path := r.URL.Path
	if strings.Contains(path, "/apiv2/") || strings.Contains(path, "/apiv3/") {
		ids, _ := s.store.BillIDs(ctx, c.trip.ID)
		resp := map[string]any{"bills": out, "allBillIds": ids, "timestamp": now}
		if strings.Contains(path, "/apiv3/") {
			resp["nb_bills"] = len(ids)
		}
		cospendJSON(w, http.StatusOK, resp)
		return
	}
	cospendJSON(w, http.StatusOK, out)
}

// applyCospendBill reads Cospend's bill parameters into an expense (only
// those sent, so an edit changes what it names).
func (s *Server) applyCospendBill(ctx context.Context, c *cospendCtx, e *store.Expense, creating bool) string {
	people, _, catKeys, _, pmOld := s.cospendLookups(ctx, c.trip.ID)
	get := func(keys ...string) (string, bool) {
		for _, k := range keys {
			if c.has(k) {
				return c.get(k), true
			}
		}
		return "", false
	}
	if v, ok := get("what"); ok {
		e.Name = strings.TrimSpace(v)
	}
	if v, ok := get("comment"); ok {
		e.Notes = v
	}
	if v, ok := get("amount"); ok {
		a, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
		if err != nil {
			return "amount: this field is invalid"
		}
		// amounts are in the project (trip) currency
		e.Amount, e.Currency, e.FXRate = s.cospendMinor(c.trip, a), c.trip.Currency, 1
		e.AmountHome = e.Amount
	}
	if v, ok := get("timestamp"); ok && v != "" {
		ts, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "timestamp: this field is invalid"
		}
		t := time.Unix(ts, 0).UTC()
		e.Date = t.Format("2006-01-02")
		if t.Hour() != 12 || t.Minute() != 0 {
			e.BillTime = t.Format("15:04")
		} else {
			e.BillTime = ""
		}
	} else if v, ok := get("date"); ok && v != "" {
		if !validDate(v) {
			return "date: this field is invalid"
		}
		e.Date = v
	}
	if v, ok := get("payer"); ok {
		id, err := strconv.ParseInt(v, 10, 64)
		if _, on := people[id]; err != nil || !on {
			return "payer: not a member of this project"
		}
		e.PayerID = &id
	}
	if v, ok := get("payed_for", "payedFor"); ok {
		e.Owers = []int64{}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			id, err := strconv.ParseInt(p, 10, 64)
			if _, on := people[id]; err != nil || !on {
				return "payed_for: not a member of this project"
			}
			e.Owers = append(e.Owers, id)
		}
		if len(e.Owers) == 0 {
			return "payed_for: this field is required"
		}
	}
	if v, ok := get("categoryid", "categoryId"); ok {
		id, _ := strconv.ParseInt(v, 10, 64)
		switch {
		case id == cospendReimbursement:
			e.Category = store.ReimbursementKey
		case catKeys[id] != "":
			e.Category = catKeys[id]
		default:
			e.Category = "other"
		}
	}
	pmSet := false
	if v, ok := get("paymentmodeid", "paymentModeId"); ok {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			e.PaymentModeID, pmSet = id, true
		} else if v == "0" {
			e.PaymentModeID = 0
		}
	}
	if v, ok := get("paymentmode", "paymentMode"); ok && !pmSet {
		e.PaymentModeID = pmOld[v]
	}
	if v, ok := get("repeat"); ok && v != "" {
		switch v {
		case "n", "d", "w", "b", "s", "m", "y":
			e.Repeat = v
		default:
			return "repeat: this field is invalid"
		}
	}
	if v, ok := get("repeatallactive"); ok {
		e.RepeatAllActive = v == "1" || v == "true"
	}
	if v, ok := get("repeatuntil"); ok {
		if v != "" && !validDate(v) {
			return "repeatuntil: this field is invalid"
		}
		e.RepeatUntil = v
	}
	if v, ok := get("repeatfreq"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			e.RepeatFreq = n
		}
	}
	if v, ok := get("deleted"); ok {
		e.Deleted = v == "1" || v == "true"
	}
	if e.Name == "" {
		e.Name = "?"
	}
	if creating && (e.PayerID == nil || len(e.Owers) == 0) {
		return "payer and payed_for are required"
	}
	return ""
}

func (s *Server) cospendAddBill(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	e := &store.Expense{TripID: c.trip.ID, Kind: "actual", Source: "manual", Currency: c.trip.Currency, FXRate: 1, Category: "other", Date: store.Today(), Repeat: "n", RepeatFreq: 1}
	if msg := s.applyCospendBill(ctx, c, e, true); msg != "" {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": msg})
		return
	}
	if err := s.store.CreateExpense(ctx, e); err != nil {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	s.store.LogActivity(ctx, c.trip.ID, 0, "added bill (Cospend)", e.Name)
	cospendJSON(w, http.StatusOK, e.ID)
}

func (s *Server) cospendLoadBill(ctx context.Context, c *cospendCtx, id int64) (*store.Expense, bool) {
	e, err := s.store.GetExpense(ctx, c.trip.ID, id)
	if err != nil || !e.IsBill() {
		return nil, false
	}
	return e, true
}

func (s *Server) cospendEditBill(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "billid"), 10, 64)
	e, ok := s.cospendLoadBill(ctx, c, id)
	if !ok {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "There is no such bill"})
		return
	}
	if store.IsSynced(e.Source) || e.Source == "item" {
		// the amount and date belong to Pocket Money / the stay or leg
		amount, cur, home, date := e.Amount, e.Currency, e.AmountHome, e.Date
		if msg := s.applyCospendBill(ctx, c, e, false); msg != "" {
			cospendJSON(w, http.StatusBadRequest, map[string]string{"message": msg})
			return
		}
		e.Amount, e.Currency, e.AmountHome, e.Date = amount, cur, home, date
	} else if msg := s.applyCospendBill(ctx, c, e, false); msg != "" {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": msg})
		return
	}
	if err := s.store.UpdateExpense(ctx, e); err != nil {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	cospendJSON(w, http.StatusOK, e.ID)
}

// cospendEditBills applies the same change to several bills (billIds[]).
func (s *Server) cospendEditBills(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	ids := c.form["billIds[]"]
	if len(ids) == 0 {
		ids = c.form["billIds"]
	}
	done := []int64{}
	for _, v := range ids {
		id, _ := strconv.ParseInt(v, 10, 64)
		e, ok := s.cospendLoadBill(ctx, c, id)
		if !ok {
			continue
		}
		if msg := s.applyCospendBill(ctx, c, e, false); msg != "" {
			cospendJSON(w, http.StatusBadRequest, map[string]string{"message": msg})
			return
		}
		if err := s.store.UpdateExpense(ctx, e); err == nil {
			done = append(done, id)
		}
	}
	cospendJSON(w, http.StatusOK, done)
}

func (s *Server) cospendDeleteOne(ctx context.Context, c *cospendCtx, id int64, toTrash bool) bool {
	e, ok := s.cospendLoadBill(ctx, c, id)
	if !ok {
		return false
	}
	if toTrash && !e.Deleted {
		return s.store.TrashExpense(ctx, c.trip.ID, id, true) == nil
	}
	if e.Source == "item" {
		// the price of a stay or leg: it comes back if deleted for good, so it goes to the trash
		return s.store.TrashExpense(ctx, c.trip.ID, id, true) == nil
	}
	return s.store.DeleteExpense(ctx, c.trip.ID, id) == nil
}

func (s *Server) cospendDeleteBill(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "billid"), 10, 64)
	toTrash := c.get("moveToTrash") != "false" && c.get("moveToTrash") != "0"
	if !s.cospendDeleteOne(r.Context(), c, id, toTrash) {
		cospendJSON(w, http.StatusNotFound, "")
		return
	}
	cospendJSON(w, http.StatusOK, "OK")
}

func (s *Server) cospendDeleteBills(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ids := c.form["billIds[]"]
	if len(ids) == 0 {
		ids = c.form["billIds"]
	}
	toTrash := c.get("moveToTrash") != "false" && c.get("moveToTrash") != "0"
	for _, v := range ids {
		id, _ := strconv.ParseInt(v, 10, 64)
		s.cospendDeleteOne(r.Context(), c, id, toTrash)
	}
	cospendJSON(w, http.StatusOK, "OK")
}

func (s *Server) cospendClearTrash(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	if _, err := s.store.EmptyTrash(r.Context(), c.trip.ID); err != nil {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	cospendJSON(w, http.StatusOK, "")
}

// cospendRepeat brings repeating bills up to date now.
func (s *Server) cospendRepeat(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	s.repeatBills(r.Context(), store.Today())
	cospendJSON(w, http.StatusOK, []any{})
}

// ---- statistics and settlement ----

func (s *Server) cospendStatistics(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	info, err := s.splitInfo(ctx, c.trip, false)
	if err != nil {
		cospendJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	people := map[int64]store.TripPerson{}
	for _, p := range info.People {
		people[p.ID] = p
	}
	stats := []map[string]any{}
	for _, b := range info.Balances {
		stats = append(stats, map[string]any{
			"balance": s.cospendMoney(c.trip, b.Balance), "filtered_balance": s.cospendMoney(c.trip, b.Balance),
			"paid": s.cospendMoney(c.trip, b.Paid), "spent": s.cospendMoney(c.trip, b.Spent),
			"member": toCospendMember(people[b.MemberID]),
		})
	}
	cats := map[string]float64{}
	_, catIDs, _, _, _ := s.cospendLookups(ctx, c.trip.ID)
	for key, v := range info.ByCategory {
		id := catIDs[key]
		cats[strconv.FormatInt(id, 10)] = s.cospendMoney(c.trip, v)
	}
	pms := map[string]float64{}
	for id, v := range info.ByPayment {
		pms[strconv.FormatInt(id, 10)] = s.cospendMoney(c.trip, v)
	}
	cospendJSON(w, http.StatusOK, map[string]any{"stats": stats, "categoryStats": cats, "paymentModeStats": pms, "memberIds": func() []int64 {
		ids := []int64{}
		for _, p := range info.People {
			ids = append(ids, p.ID)
		}
		return ids
	}()})
}

func (s *Server) cospendTransfers(t *store.Trip, tr []Transfer) []map[string]any {
	out := []map[string]any{}
	for _, x := range tr {
		out = append(out, map[string]any{"from": x.From, "to": x.To, "amount": s.cospendMoney(t, x.Amount)})
	}
	return out
}

func (s *Server) cospendSettle(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	info, err := s.splitInfo(r.Context(), c.trip, false)
	if err != nil {
		cospendJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	bal := map[string]float64{}
	for _, b := range info.Balances {
		bal[strconv.FormatInt(b.MemberID, 10)] = s.cospendMoney(c.trip, b.Balance)
	}
	cospendJSON(w, http.StatusOK, map[string]any{"transactions": s.cospendTransfers(c.trip, info.Settlement), "balances": bal})
}

// cospendAutoSettle records the settlement as reimbursement bills.
func (s *Server) cospendAutoSettle(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	info, err := s.splitInfo(ctx, c.trip, false)
	if err != nil {
		cospendJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	names := map[int64]string{}
	for _, p := range info.People {
		names[p.ID] = p.Name
	}
	for _, tr := range info.Settlement {
		from, to := tr.From, tr.To
		e := &store.Expense{TripID: c.trip.ID, Name: fmt.Sprintf("%s → %s", names[from], names[to]), Category: store.ReimbursementKey,
			Amount: tr.Amount, Currency: c.trip.Currency, AmountHome: tr.Amount, FXRate: 1, Date: store.Today(), Kind: "actual", Source: "manual",
			PayerID: &from, Owers: []int64{to}}
		_ = s.store.CreateExpense(ctx, e)
	}
	cospendJSON(w, http.StatusOK, "")
}

// ---- currencies, payment modes, categories ----

func (s *Server) cospendSaveCurrency(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rate, err := strconv.ParseFloat(c.get("rate"), 64)
	if err != nil {
		rate, err = strconv.ParseFloat(c.get("exchange_rate"), 64)
	}
	name := strings.TrimSpace(c.get("name"))
	if name == "" || err != nil || rate <= 0 {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": "Incorrect field values"})
		return
	}
	cu := &store.TripCurrency{ID: id, TripID: c.trip.ID, Name: name, ExchangeRate: rate}
	if err := s.store.SaveTripCurrency(r.Context(), cu); err != nil {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "This project have no such currency"})
		return
	}
	cospendJSON(w, http.StatusOK, cu.ID)
}

func (s *Server) cospendDeleteCurrency(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := s.store.DeleteTripCurrency(r.Context(), c.trip.ID, id); err != nil {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
		return
	}
	cospendJSON(w, http.StatusOK, id)
}

func (s *Server) cospendSavePaymentMode(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	m := &store.TripPaymentMode{ID: id, TripID: c.trip.ID}
	if id > 0 {
		list, _ := s.store.ListTripPaymentModes(ctx, c.trip.ID)
		for _, x := range list {
			if x.ID == id {
				*m = x
			}
		}
	}
	if c.has("name") {
		m.Name = strings.TrimSpace(c.get("name"))
	}
	if c.has("icon") {
		m.Icon = c.get("icon")
	}
	if c.has("color") {
		m.Color = c.get("color")
	}
	if m.Name == "" {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": "Incorrect field values"})
		return
	}
	if err := s.store.SaveTripPaymentMode(ctx, m); err != nil {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "This project have no such payment mode"})
		return
	}
	cospendJSON(w, http.StatusOK, m.ID)
}

func (s *Server) cospendDeletePaymentMode(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := s.store.DeleteTripPaymentMode(r.Context(), c.trip.ID, id); err != nil {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
		return
	}
	cospendJSON(w, http.StatusOK, id)
}

func (s *Server) cospendSaveCategory(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	cat := &store.TripCategory{ID: id, TripID: c.trip.ID}
	if id > 0 {
		list, _ := s.store.ListTripCategories(ctx, c.trip.ID)
		found := false
		for _, x := range list {
			if x.ID == id {
				*cat, found = x, true
			}
		}
		if !found {
			cospendJSON(w, http.StatusNotFound, map[string]string{"message": "This project have no such category"})
			return
		}
	}
	if c.has("name") {
		cat.Name = strings.TrimSpace(c.get("name"))
	}
	if c.has("icon") {
		cat.Icon = c.get("icon")
	}
	if c.has("color") {
		cat.Color = c.get("color")
	}
	if cat.Name == "" {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": "Incorrect field values"})
		return
	}
	if err := s.store.SaveTripCategory(ctx, cat); err != nil {
		cospendJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	cospendJSON(w, http.StatusOK, cat.ID)
}

func (s *Server) cospendDeleteCategory(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := s.store.DeleteTripCategory(r.Context(), c.trip.ID, id); err != nil {
		cospendJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
		return
	}
	cospendJSON(w, http.StatusOK, id)
}

// cospendOrder saves a new order: order[] = [{"id": 3, "order": 0}, …] (JSON) .
func (s *Server) cospendOrder(kind string) func(http.ResponseWriter, *http.Request, *cospendCtx) {
	return func(w http.ResponseWriter, r *http.Request, c *cospendCtx) {
		var order []struct {
			ID    int64 `json:"id"`
			Order int   `json:"order"`
		}
		_ = json.Unmarshal([]byte(c.get("order")), &order)
		table := "trip_categories"
		if kind == "paymentmode" {
			table = "trip_payment_modes"
		}
		for _, o := range order {
			_, _ = s.store.DB.ExecContext(r.Context(), `UPDATE `+table+` SET sort_order = ? WHERE id = ? AND trip_id = ?`, o.Order, o.ID, c.trip.ID)
		}
		cospendJSON(w, http.StatusOK, true)
	}
}

// ---- managing the link (from the app) ----

const tokenAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randomString(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(tokenAlphabet)))
	for i := range b {
		v, _ := rand.Int(rand.Reader, max)
		b[i] = tokenAlphabet[v.Int64()]
	}
	return string(b)
}

// ShareLink is a trip's Cospend link as the app shows it.
type ShareLink struct {
	*store.TripShare
	Link    string `json:"link"`     // cospend://… for MoneyBuster (also the QR code)
	BaseURL string `json:"base_url"` // https://…/index.php/apps/cospend, for clients asking for a server
}

func (s *Server) shareLink(r *http.Request, sh *store.TripShare) ShareLink {
	base := s.cfg.BaseURL
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		base = scheme + "://" + host
	}
	u, err := url.Parse(base)
	if err != nil {
		return ShareLink{TripShare: sh}
	}
	scheme := "cospend"
	if u.Scheme == "http" {
		scheme = "cospend+http"
	}
	path := strings.TrimRight(u.Path, "/")
	link := fmt.Sprintf("%s://%s%s/%s/%s", scheme, u.Host, path, sh.Token, url.PathEscape(sh.Password))
	return ShareLink{TripShare: sh, Link: link, BaseURL: strings.TrimRight(base, "/") + "/index.php/apps/cospend"}
}

// handleGetShare answers GET /trips/{id}/split/share (404 when off).
func (s *Server) handleGetShare(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	sh, err := s.store.GetTripShare(r.Context(), tripFrom(r.Context()).trip.ID)
	if err != nil {
		s.fail(w, err, "share")
		return
	}
	writeJSON(w, http.StatusOK, s.shareLink(r, sh))
}

// handlePutShare turns the link on, or changes it: {access_level, new: true
// for a new token and password}.
func (s *Server) handlePutShare(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	var in struct {
		AccessLevel int  `json:"access_level"`
		New         bool `json:"new"`
	}
	_ = decode(r, &in)
	sh, err := s.store.GetTripShare(ctx, t.ID)
	if err != nil || in.New {
		sh = &store.TripShare{TripID: t.ID, Token: randomString(24), Password: randomString(12), AccessLevel: cospendMaintainer}
	}
	if in.AccessLevel >= cospendViewer && in.AccessLevel <= cospendAdmin {
		sh.AccessLevel = in.AccessLevel
	}
	if err := s.store.EnsureBillSetup(ctx, t.ID); err != nil {
		s.fail(w, err, "share")
		return
	}
	if err := s.store.SaveTripShare(ctx, sh); err != nil {
		s.fail(w, err, "share")
		return
	}
	sh, _ = s.store.GetTripShare(ctx, t.ID)
	s.store.LogActivity(ctx, t.ID, userFrom(ctx).ID, "shared", "Cospend link")
	writeJSON(w, http.StatusOK, s.shareLink(r, sh))
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	if err := s.store.DeleteTripShare(r.Context(), tripFrom(r.Context()).trip.ID); err != nil {
		s.fail(w, err, "share")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
