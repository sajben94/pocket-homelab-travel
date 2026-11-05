package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/firefly"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// rate returns how many units of `to` one unit of `from` is worth on a date,
// from the cache or the FX providers. Falls back to 1 (and reports it) when
// nothing is available so an expense is never lost.
func (s *Server) rate(ctx context.Context, from, to, date string) (float64, string) {
	from, to = strings.ToUpper(from), strings.ToUpper(to)
	if from == to || from == "" || to == "" {
		return 1, "same"
	}
	if date == "" || date > store.Today() {
		date = store.Today()
	}
	if r, src, ok := s.store.GetFXRate(ctx, from, to, date); ok {
		return r, src
	}
	st, _ := s.store.GetSettings(ctx, 0) // fx_auto is box-wide
	if !st.FXAuto {
		return 1, "disabled"
	}
	r, src, err := s.fx.Rate(ctx, from, to, date)
	if err != nil {
		s.log.Warn("fx lookup failed", "from", from, "to", to, "date", date, "err", err)
		return 1, "unavailable"
	}
	_ = s.store.PutFXRate(ctx, from, to, date, r, src)
	return r, src
}

func (s *Server) handleFX(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, date := strings.ToUpper(q.Get("from")), strings.ToUpper(q.Get("to")), q.Get("date")
	if !currency.Valid(from) || !currency.Valid(to) {
		writeErr(w, http.StatusBadRequest, "unknown currency")
		return
	}
	rate, src := s.rate(r.Context(), from, to, date)
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "date": date, "rate": rate, "source": src})
}

func (s *Server) handleCurrencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currency.All())
}

// syncItemCost keeps a planned expense in step with an item's price
// (lodging/transport): price 0 removes it.
func (s *Server) syncItemCost(ctx context.Context, t *store.Trip, kind string, id int64, name, category string, price int64, cur, date string, paid bool) {
	expenses, err := s.store.ListExpenses(ctx, t.ID)
	if err != nil {
		return
	}
	var existing *store.Expense
	for i := range expenses {
		if expenses[i].Source == "item" && expenses[i].LinkKind == kind && expenses[i].LinkID == id {
			existing = &expenses[i]
			break
		}
	}
	if price <= 0 {
		if existing != nil {
			_ = s.store.DeleteExpense(ctx, t.ID, existing.ID)
		}
		return
	}
	if cur == "" {
		cur = t.Currency
	}
	if len(date) > 10 {
		date = date[:10]
	}
	rate, _ := s.rate(ctx, cur, t.Currency, date)
	ek := "planned"
	if paid {
		ek = "actual"
	}
	e := store.Expense{TripID: t.ID, Name: name, Category: category, Amount: price, Currency: cur, AmountHome: currency.Convert(price, cur, t.Currency, rate), FXRate: rate, Date: date, Kind: ek, Source: "item", LinkKind: kind, LinkID: id}
	if existing != nil {
		// once paid it is a bill: keep whoever paid and whoever it is for
		e.ID, e.PayerID, e.PaymentModeID, e.BillTime, e.Notes = existing.ID, existing.PayerID, existing.PaymentModeID, existing.BillTime, existing.Notes
		e.Deleted = existing.Deleted
		if e.IsBill() && existing.PayerID == nil {
			_ = s.billDefaults(ctx, t, &e)
		}
		_ = s.store.UpdateExpense(ctx, &e)
		return
	}
	_ = s.billDefaults(ctx, t, &e)
	_ = s.store.CreateExpense(ctx, &e)
}

func (s *Server) handleListExpenses(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	e, err := s.store.ListExpenses(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "list expenses")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

type expenseInput struct {
	Name     *string  `json:"name"`
	Category *string  `json:"category"`
	Amount   *int64   `json:"amount"`
	Currency *string  `json:"currency"`
	FXRate   *float64 `json:"fx_rate"`
	Date     *string  `json:"date"`
	Kind     *string  `json:"kind"`
	LinkKind *string  `json:"link_kind"`
	LinkID   *int64   `json:"link_id"`
	Notes    *string  `json:"notes"`

	// bills
	PayerID         *int64  `json:"payer_id"`
	Owers           []int64 `json:"owers"`
	PaymentModeID   *int64  `json:"payment_mode_id"`
	BillTime        *string `json:"bill_time"`
	Repeat          *string `json:"repeat"`
	RepeatAllActive *bool   `json:"repeat_all_active"`
	RepeatUntil     *string `json:"repeat_until"`
	RepeatFreq      *int    `json:"repeat_freq"`
	// Extras: what someone owes on top of an equal share (uneven split), in
	// the bill's currency; amount is then the whole bill
	Extras []billExtra `json:"extras"`
}

type billExtra struct {
	MemberID int64 `json:"member_id"`
	Amount   int64 `json:"amount"`
}

// applyBill sets who paid, for whom, how and whether it repeats.
func applyBill(e *store.Expense, in expenseInput, keys map[string]bool) string {
	if keys["payer_id"] {
		e.PayerID = in.PayerID
	}
	if keys["owers"] {
		e.Owers = append([]int64{}, in.Owers...)
	}
	if keys["payment_mode_id"] && in.PaymentModeID != nil {
		e.PaymentModeID = *in.PaymentModeID
	}
	if keys["bill_time"] && in.BillTime != nil {
		e.BillTime = strings.TrimSpace(*in.BillTime)
		if e.BillTime != "" && !validClock(e.BillTime) {
			return "time is HH:MM"
		}
	}
	if keys["repeat"] && in.Repeat != nil {
		switch *in.Repeat {
		case "n", "d", "w", "b", "s", "m", "y":
			e.Repeat = *in.Repeat
		default:
			return "repeat is n, d, w, b, s, m or y"
		}
	}
	if keys["repeat_all_active"] && in.RepeatAllActive != nil {
		e.RepeatAllActive = *in.RepeatAllActive
	}
	if keys["repeat_until"] && in.RepeatUntil != nil {
		e.RepeatUntil = *in.RepeatUntil
		if !validDate(e.RepeatUntil) {
			return "repeat until must be YYYY-MM-DD"
		}
	}
	if keys["repeat_freq"] && in.RepeatFreq != nil && *in.RepeatFreq > 0 {
		e.RepeatFreq = *in.RepeatFreq
	}
	return ""
}

func (s *Server) applyExpense(ctx context.Context, t *store.Trip, e *store.Expense, in expenseInput, keys map[string]bool) string {
	if keys["name"] && in.Name != nil {
		e.Name = strings.TrimSpace(*in.Name)
	}
	if keys["category"] && in.Category != nil {
		e.Category = strings.TrimSpace(*in.Category)
	}
	if keys["amount"] && in.Amount != nil {
		e.Amount = *in.Amount
	}
	if keys["currency"] && in.Currency != nil {
		e.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if keys["date"] && in.Date != nil {
		e.Date = *in.Date
	}
	if keys["kind"] && in.Kind != nil {
		e.Kind = *in.Kind
	}
	if keys["link_kind"] && in.LinkKind != nil {
		e.LinkKind = *in.LinkKind
	}
	if keys["link_id"] && in.LinkID != nil {
		e.LinkID = *in.LinkID
	}
	if keys["notes"] && in.Notes != nil {
		e.Notes = *in.Notes
	}
	if e.Name == "" {
		return "name is required"
	}
	if e.Amount < 0 {
		return "amount cannot be negative"
	}
	if e.Currency == "" {
		e.Currency = t.Currency
	}
	if !currency.Valid(e.Currency) {
		return "unknown currency"
	}
	if e.Category == "" {
		e.Category = "other"
	}
	if e.Kind != "planned" && e.Kind != "actual" {
		e.Kind = "planned"
	}
	if !validDate(e.Date) {
		return "date must be YYYY-MM-DD"
	}
	if keys["fx_rate"] && in.FXRate != nil && *in.FXRate > 0 {
		e.FXRate = *in.FXRate
	} else if keys["amount"] || keys["currency"] || keys["date"] || e.FXRate == 0 {
		e.FXRate, _ = s.rate(ctx, e.Currency, t.Currency, e.Date)
	}
	e.AmountHome = currency.Convert(e.Amount, e.Currency, t.Currency, e.FXRate)
	if msg := applyBill(e, in, keys); msg != "" {
		return msg
	}
	return s.billDefaults(ctx, t, e)
}

func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var in expenseInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	e := &store.Expense{TripID: t.ID, Currency: t.Currency, Kind: "planned", Source: "manual"}
	if msg := s.applyExpense(r.Context(), t, e, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	extras, msg := s.splitExtras(r.Context(), t, e, in, keys)
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateExpense(r.Context(), e); err != nil {
		s.fail(w, err, "create expense")
		return
	}
	if extras != nil {
		if err := s.writeBillParts(r.Context(), t, e, extras); err != nil {
			s.fail(w, err, "split")
			return
		}
	}
	s.store.LogActivity(r.Context(), t.ID, userFrom(r.Context()).ID, "added cost", e.Name)
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleUpdateExpense(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	e, err := s.store.GetExpense(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get expense")
		return
	}
	if store.IsSynced(e.Source) {
		// Only the category and notes may be changed locally; amounts come from Pocket Money.
		var in expenseInput
		keys, err := decodeKeys(r, &in)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		if keys["category"] && in.Category != nil {
			e.Category = strings.TrimSpace(*in.Category)
		}
		if keys["notes"] && in.Notes != nil {
			e.Notes = *in.Notes
		}
		// who paid and for whom are this app's to say
		if msg := applyBill(e, in, keys); msg != "" {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		if msg := s.billDefaults(r.Context(), t, e); msg != "" {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		if err := s.store.UpdateExpense(r.Context(), e); err != nil {
			s.fail(w, err, "update expense")
			return
		}
		writeJSON(w, http.StatusOK, e)
		return
	}
	var in expenseInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := s.applyExpense(r.Context(), t, e, in, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	extras, msg := s.splitExtras(r.Context(), t, e, in, keys)
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateExpense(r.Context(), e); err != nil {
		s.fail(w, err, "update expense")
		return
	}
	if extras != nil || !e.IsBill() {
		if err := s.writeBillParts(r.Context(), t, e, extras); err != nil {
			s.fail(w, err, "split")
			return
		}
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	e, err := s.store.GetExpense(r.Context(), t.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete expense")
		return
	}
	// a bill goes to the trash (Cospend clients see it go); a planned cost, or
	// one already in the trash, is deleted for good
	if e.IsBill() && !e.Deleted && !queryBool(r, "purge") {
		err = s.store.TrashExpense(r.Context(), t.ID, e.ID, true)
	} else {
		err = s.store.DeleteExpense(r.Context(), t.ID, e.ID)
	}
	if err != nil {
		s.fail(w, err, "delete expense")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// BudgetSummary is the money view of a trip, in the trip currency.
type BudgetSummary struct {
	Currency     string            `json:"currency"`
	Budget       int64             `json:"budget"`
	Planned      int64             `json:"planned"`
	Actual       int64             `json:"actual"`
	Remaining    int64             `json:"remaining"` // budget - actual (or - planned when no actuals yet)
	Total        int64             `json:"total"`     // actual + booked but unpaid stays/transport
	Bookings     int64             `json:"bookings"`  // the unpaid stays/transport part of Total
	PerDay       int64             `json:"per_day"`
	ByCategory   []CategoryTotal   `json:"by_category"`
	ByDay        []DayTotal        `json:"by_day"`
	ByCurrency   map[string]int64  `json:"by_currency"` // actual, in original currencies
	Top          []store.Expense   `json:"top"`
	FireflyCount int               `json:"firefly_count"` // actual costs synced from Pocket Money (or Firefly)
	Days         int               `json:"days"`
	DaysElapsed  int               `json:"days_elapsed"`
	Sources      map[string]int    `json:"sources"`
	Status       map[string]string `json:"status"`
}

// CategoryTotal is planned vs actual per category.
type CategoryTotal struct {
	Category string `json:"category"`
	Planned  int64  `json:"planned"`
	Actual   int64  `json:"actual"`
}

// DayTotal is spending on one day.
type DayTotal struct {
	Date   string `json:"date"`
	Actual int64  `json:"actual"`
	Count  int    `json:"count"`
}

func (s *Server) budgetSummary(t *store.Trip, expenses []store.Expense) BudgetSummary {
	b := BudgetSummary{Currency: t.Currency, Budget: t.Budget, ByCurrency: map[string]int64{}, Sources: map[string]int{}, Days: t.Days, Status: map[string]string{}}
	cats := map[string]*CategoryTotal{}
	days := map[string]*DayTotal{}
	var actuals []store.Expense
	for _, e := range expenses {
		if e.Category == store.ReimbursementKey || e.Deleted {
			continue // paying each other back is not spending
		}
		c := cats[e.Category]
		if c == nil {
			c = &CategoryTotal{Category: e.Category}
			cats[e.Category] = c
		}
		b.Sources[e.Source]++
		if e.Kind == "actual" {
			b.Actual += e.AmountHome
			c.Actual += e.AmountHome
			b.ByCurrency[e.Currency] += e.Amount
			if store.IsSynced(e.Source) {
				b.FireflyCount++
			}
			if e.Date != "" {
				d := days[e.Date]
				if d == nil {
					d = &DayTotal{Date: e.Date}
					days[e.Date] = d
				}
				d.Actual += e.AmountHome
				d.Count++
			}
			actuals = append(actuals, e)
		} else {
			b.Planned += e.AmountHome
			c.Planned += e.AmountHome
			if e.Source == "item" {
				b.Bookings += e.AmountHome
			}
		}
	}
	for _, c := range cats {
		b.ByCategory = append(b.ByCategory, *c)
	}
	sort.Slice(b.ByCategory, func(i, j int) bool {
		return b.ByCategory[i].Actual+b.ByCategory[i].Planned > b.ByCategory[j].Actual+b.ByCategory[j].Planned
	})
	for _, d := range days {
		b.ByDay = append(b.ByDay, *d)
	}
	sort.Slice(b.ByDay, func(i, j int) bool { return b.ByDay[i].Date < b.ByDay[j].Date })
	sort.Slice(actuals, func(i, j int) bool { return actuals[i].AmountHome > actuals[j].AmountHome })
	if len(actuals) > 5 {
		actuals = actuals[:5]
	}
	if actuals == nil {
		actuals = []store.Expense{}
	}
	b.Top = actuals
	if b.ByCategory == nil {
		b.ByCategory = []CategoryTotal{}
	}
	if b.ByDay == nil {
		b.ByDay = []DayTotal{}
	}
	b.Total = b.Actual + b.Bookings
	if t.Budget > 0 {
		if b.Actual > 0 {
			b.Remaining = t.Budget - b.Actual
		} else {
			b.Remaining = t.Budget - b.Planned
		}
	}
	// Days elapsed so far (for the burn rate).
	today := store.Today()
	if t.StartDate != "" {
		if today >= t.StartDate {
			end := today
			if t.EndDate != "" && t.EndDate < today {
				end = t.EndDate
			}
			b.DaysElapsed = store.DaysBetween(t.StartDate, end)
		}
	}
	if b.DaysElapsed > 0 {
		b.PerDay = b.Actual / int64(b.DaysElapsed)
	} else if t.Days > 0 {
		b.PerDay = b.Planned / int64(t.Days)
	}
	return b
}

func (s *Server) handleBudget(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	e, err := s.store.ListExpenses(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "expenses")
		return
	}
	writeJSON(w, http.StatusOK, s.budgetSummary(t, e))
}

// ---- Pocket Money (trip costs) ----

// fireflyCategory maps a Money/Firefly category / description to one of ours.
func fireflyCategory(cat, desc, dest string) string {
	s := strings.ToLower(cat + " " + desc + " " + dest)
	switch {
	case containsAny(s, "hotel", "apartman", "apartment", "booking", "airbnb", "smještaj", "smestaj", "lodging", "hostel", "nocenje", "noćenje"):
		return "lodging"
	case containsAny(s, "gorivo", "fuel", "benzin", "petrol", "diesel", "dizel", "ina ", "petrol ", "pumpa", "nis "):
		return "fuel"
	case containsAny(s, "restoran", "restaurant", "hrana", "food", "pekara", "bakery", "kafa", "coffee", "cafe", "kafić", "pizza", "burek", "pica", "ćevap", "cevap", "konoba", "lunch", "dinner", "ručak", "večera", "rucak", "vecera", "dorucak", "doručak"):
		return "food"
	case containsAny(s, "market", "supermarket", "fasunga", "groceries", "mercator", "konzum", "lidl", "idea", "voli", "bingo", "roda", "maxi", "tropik", "prodavnica", "shop"):
		return "groceries"
	case containsAny(s, "transport", "bus", "autobus", "voz", "train", "taxi", "taksi", "let ", "flight", "avion", "ferry", "trajekt", "parking", "putarina", "toll", "rent", "uber", "bolt"):
		return "transport"
	case containsAny(s, "ulaznic", "ticket", "muzej", "museum", "tura", "tour", "izlet", "excursion", "boat", "brod", "kayak", "kajak", "spa", "activity", "aktivnost", "ulaz"):
		return "activities"
	case containsAny(s, "suvenir", "souvenir", "odjeća", "odjeca", "clothes", "shopping", "kupovina", "poklon", "gift"):
		return "shopping"
	case containsAny(s, "osiguranje", "insurance"):
		return "insurance"
	case containsAny(s, "naknada", "fee", "provizija", "bankomat", "atm", "mjenjačnica", "exchange"):
		return "fees"
	}
	return "other"
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// syncedTx is one tagged withdrawal, ready to become a trip expense.
type syncedTx struct {
	tx     firefly.Transaction
	source string // money | firefly: whose id tx.ID is
	minor  int64
	cur    string
	row    *store.Expense // the existing row it updates (nil = new)
}

// matchSynced pairs incoming transactions with the trip's synced rows so a
// re-sync updates instead of duplicating, also across a switch between
// Firefly III and Pocket Money (whose transaction ids differ):
//
//  1. same source and id: the row this transaction made before;
//  2. a Money transaction mirrored from Firefly ("firefly:<id>" in its
//     external_id) takes the row synced from that Firefly journal;
//  3. otherwise a row from the other source with the same date, amount and
//     currency (the same description preferred), then one with the same
//     date and description.
//
// Rows are taken at most once; what is left over is pruned by the caller.
func matchSynced(in []*syncedTx, existing []store.Expense) {
	taken := map[int64]bool{}
	byKey := map[string]*store.Expense{}
	for i := range existing {
		e := &existing[i]
		if store.IsSynced(e.Source) {
			byKey[e.Source+"\x00"+e.ExternalID] = e
		}
	}
	take := func(x *syncedTx, e *store.Expense) {
		if e != nil && !taken[e.ID] {
			x.row, taken[e.ID] = e, true
		}
	}
	for _, x := range in {
		take(x, byKey[x.source+"\x00"+x.tx.ID])
	}
	for _, x := range in {
		if x.row == nil && x.source == "money" {
			if id := x.tx.FireflyOrigin(); id != "" {
				take(x, byKey["firefly\x00"+id])
			}
		}
	}
	norm := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	other := func(x *syncedTx, ok func(e *store.Expense) bool) *store.Expense {
		var first *store.Expense
		for i := range existing {
			e := &existing[i]
			if taken[e.ID] || !store.IsSynced(e.Source) || e.Source == x.source || !ok(e) {
				continue
			}
			if norm(e.Name) == norm(x.tx.Description) {
				return e
			}
			if first == nil {
				first = e
			}
		}
		return first
	}
	for _, x := range in {
		if x.row == nil {
			take(x, other(x, func(e *store.Expense) bool {
				return e.Date == x.tx.Date && e.Amount == x.minor && strings.EqualFold(e.Currency, x.cur)
			}))
		}
	}
	for _, x := range in {
		if x.row == nil {
			take(x, other(x, func(e *store.Expense) bool {
				return e.Date == x.tx.Date && norm(e.Name) == norm(x.tx.Description) && norm(e.Name) != ""
			}))
		}
	}
}

// syncMoney pulls the trip's tagged withdrawals into expenses (kind actual,
// source money, or firefly for a real Firefly III) from the trip owner's
// Pocket Money, connected the way the owner chose (Pocket Core, direct or
// off). Returns the number of synced rows.
func (s *Server) syncMoney(ctx context.Context, tripID int64) (int, error) {
	s.syncMu.Lock()
	if s.syncing[tripID] {
		s.syncMu.Unlock()
		return 0, errors.New("sync already running")
	}
	s.syncing[tripID] = true
	s.syncMu.Unlock()
	defer func() {
		s.syncMu.Lock()
		delete(s.syncing, tripID)
		s.syncMu.Unlock()
	}()

	t, err := s.store.GetTrip(ctx, tripID, 0)
	if err != nil {
		return 0, err
	}
	if t.FireflyTag == "" {
		return 0, errors.New("the trip has no Money tag")
	}
	// The trip's spending comes from its owner's Pocket Money, whoever
	// presses sync (an editor of a shared trip) or the scheduler.
	conn, err := s.moneyFor(ctx, t.OwnerID)
	if err != nil {
		return 0, err
	}
	ff := conn.client
	tag, err := ff.TagByName(ctx, t.FireflyTag)
	if err != nil {
		return 0, err
	}
	if tag == nil {
		return 0, fmt.Errorf("tag %q does not exist in Pocket Money", t.FireflyTag)
	}
	// Every withdrawal with the tag counts, also outside the trip's dates
	// (flights are paid before, the last dinner is charged after): the tag
	// is the person's explicit decision.
	txs, err := ff.TagTransactions(ctx, tag.ID, "", "")
	if err != nil {
		return 0, err
	}
	in := make([]*syncedTx, 0, len(txs))
	for _, tx := range txs {
		x := &syncedTx{tx: tx, source: "firefly"}
		if conn.via == modeCore || tx.FromMoney {
			x.source = "money"
		}
		dec := tx.Decimals
		if dec == 0 && currency.Decimals(tx.Currency) != 0 {
			dec = currency.Decimals(tx.Currency)
		}
		x.minor = int64(math.Round(tx.Amount * math.Pow10(dec)))
		x.cur = tx.Currency
		// Prefer the foreign amount when the card was charged in the trip currency.
		if tx.ForeignCur != "" && strings.EqualFold(tx.ForeignCur, t.Currency) {
			x.cur = tx.ForeignCur
			x.minor = int64(math.Round(tx.ForeignAmt * math.Pow10(currency.Decimals(x.cur))))
		}
		in = append(in, x)
	}
	existing, err := s.store.ListExpenses(ctx, t.ID)
	if err != nil {
		return 0, err
	}
	matchSynced(in, existing)
	keep := make([]int64, 0, len(in))
	n := 0
	for _, x := range in {
		tx := x.tx
		rate, _ := s.rate(ctx, x.cur, t.Currency, tx.Date)
		cat := ""
		if x.row != nil {
			cat = x.row.Category // keep what the person may have re-categorised
			if x.row.Source != x.source || x.row.ExternalID != tx.ID {
				if err := s.store.RekeyExternalExpense(ctx, t.ID, x.row.ID, x.source, tx.ID); err != nil {
					s.log.Warn("money rekey", "err", err)
					continue
				}
			}
		}
		if cat == "" {
			cat = fireflyCategory(tx.Category, tx.Description, tx.Destination)
		}
		notes := strings.TrimSpace(strings.Join(nonEmpty(tx.Category, tx.Destination, tx.Notes), " · "))
		e := &store.Expense{TripID: t.ID, Name: tx.Description, Category: cat, Amount: x.minor, Currency: x.cur, AmountHome: currency.Convert(x.minor, x.cur, t.Currency, rate), FXRate: rate, Date: tx.Date, Kind: "actual", Source: x.source, ExternalID: tx.ID, Notes: notes}
		_ = s.billDefaults(ctx, t, e) // a new synced cost: paid by the owner, for everyone
		_, err := s.store.UpsertExternalExpense(ctx, e, s.ownerEvent(ctx, t.OwnerID, func() (string, string, map[string]any) {
			return "travel.expense_linked", fmt.Sprintf("expense:%d", e.ID), map[string]any{
				"expense_id": e.ID, "trip_id": t.ID, "trip": t.Name, "name": e.Name, "category": e.Category,
				"amount": e.Amount, "currency": e.Currency, "amount_home": e.AmountHome, "home_currency": t.Currency,
				"date": e.Date, "source": e.Source, "external_id": e.ExternalID, "tag": t.FireflyTag, "firefly_tag": t.FireflyTag,
			}
		}))
		if err != nil {
			s.log.Warn("money upsert", "err", err)
			continue
		}
		keep = append(keep, e.ID)
		n++
	}
	// Synced rows (from either source) that no tagged transaction claimed
	// are gone upstream, or were the Firefly copy of a cost now read from
	// Money: either way they must not count twice.
	if _, err := s.store.DeleteSyncedExpensesExcept(ctx, t.ID, store.SyncedSources, keep); err != nil {
		s.log.Warn("money prune", "err", err)
	}
	_ = s.store.CachePut(ctx, cacheKey(t.ID, "firefly-synced"), fmt.Sprintf("%d", n))
	s.log.Info("trip costs synced", "trip", t.Name, "tag", t.FireflyTag, "via", conn.via, "rows", n)
	return n, nil
}

func nonEmpty(v ...string) []string {
	var out []string
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func (s *Server) handleMoneySync(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	n, err := s.syncMoney(r.Context(), t.ID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	e, _ := s.store.ListExpenses(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, map[string]any{"synced": n, "budget": s.budgetSummary(t, e), "expenses": e})
}

// handleMoneyTags lists the caller's own Pocket Money tags.
func (s *Server) handleMoneyTags(w http.ResponseWriter, r *http.Request) {
	conn, err := s.moneyFor(r.Context(), actorID(r.Context()))
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	tags, err := conn.client.Tags(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.Slice(tags, func(i, j int) bool { return strings.ToLower(tags[i].Name) < strings.ToLower(tags[j].Name) })
	writeJSON(w, http.StatusOK, tags)
}

// autoSync refreshes trip costs for trips that are running, upcoming or
// recently finished. Each trip syncs with its own owner's Pocket Money
// connection (see syncMoney), so the job only ever touches that owner's
// spending; an owner who switched Money off is skipped.
func (s *Server) autoSync(ctx context.Context) {
	trips, err := s.store.ListTrips(ctx, store.TripFilter{})
	if err != nil {
		return
	}
	today := store.Today()
	cutoff := time.Now().AddDate(0, 0, -14).Format("2006-01-02")
	for _, t := range trips {
		if t.FireflyTag == "" || t.Archived {
			continue
		}
		recent := t.EndDate == "" || t.EndDate >= cutoff
		upcoming := t.StartDate != "" && t.StartDate > today
		if recent || upcoming {
			if _, err := s.syncMoney(ctx, t.ID); err != nil && !errors.Is(err, errMoneyOff) {
				s.log.Warn("auto sync", "trip", t.Name, "err", err)
			}
		}
	}
}

// handlePutBudgetPlan saves the trip's budget plan (per category, a total or
// per day); the trip budget becomes its sum.
func (s *Server) handlePutBudgetPlan(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	var in struct {
		Lines []store.PlanLine `json:"lines"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	if err := s.store.SetBudgetPlan(ctx, t.ID, t.Days, in.Lines); err != nil {
		s.fail(w, err, "budget plan")
		return
	}
	plan, _ := s.store.BudgetPlan(ctx, t.ID)
	writeJSON(w, http.StatusOK, plan)
}
