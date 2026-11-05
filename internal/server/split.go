package server

import (
	"context"
	"database/sql"

	"fmt"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/currency"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// Shared expenses work like Nextcloud Cospend: every spent cost is a bill
// paid by someone on the trip for some of its people, each owing a share by
// their weight. Balances say who is up or down; the settlement says who pays
// whom to get everyone to zero.

// billDefaults fills who paid (the trip owner) and who it was for (everyone
// still taking part) on a bill that does not say, and checks the ids belong
// to the trip.
func (s *Server) billDefaults(ctx context.Context, t *store.Trip, e *store.Expense) string {
	if !e.IsBill() {
		return ""
	}
	people, err := s.store.ListTripPeople(ctx, t.ID)
	if err != nil {
		return "could not load the trip's people"
	}
	on := map[int64]bool{}
	var owner *int64
	for i := range people {
		on[people[i].ID] = true
		if people[i].Role == "owner" && owner == nil {
			owner = &people[i].ID
		}
	}
	if e.PayerID == nil {
		if owner == nil && len(people) > 0 {
			owner = &people[0].ID
		}
		e.PayerID = owner
	} else if !on[*e.PayerID] {
		return "the payer is not on this trip"
	}
	if len(e.Owers) == 0 {
		e.Owers = []int64{}
		for _, p := range people {
			if p.Active {
				e.Owers = append(e.Owers, p.ID)
			}
		}
		if len(e.Owers) == 0 && e.PayerID != nil {
			e.Owers = []int64{*e.PayerID}
		}
	}
	for _, m := range e.Owers {
		if !on[m] {
			return "someone the bill is for is not on this trip"
		}
	}
	return ""
}

// Balance is where someone on the trip stands, in the trip currency (minor units).
type Balance struct {
	MemberID int64  `json:"member_id"`
	Name     string `json:"name"`
	Paid     int64  `json:"paid"`     // bills they paid (for anyone)
	Spent    int64  `json:"spent"`    // their share of all bills
	Sent     int64  `json:"sent"`     // money they gave others (reimbursements, advances)
	Received int64  `json:"received"` // money others gave them
	Balance  int64  `json:"balance"`  // paid + sent − spent − received: > 0 they get money back, < 0 they owe
}

// Transfer is one payment of the settlement.
type Transfer struct {
	From   int64 `json:"from"`
	To     int64 `json:"to"`
	Amount int64 `json:"amount"` // minor units, trip currency
}

// balances computes everyone's balance from the bills (as Cospend does: the
// payer is credited the amount, each ower debited amount / Σweights × weight,
// a weight of 0 counting as 1). Shares are kept in fractions of a minor unit
// and rounded at the end.
func balances(people []store.TripPerson, bills []store.Expense) []Balance {
	weight := map[int64]float64{}
	idx := map[int64]int{}
	out := make([]Balance, len(people))
	paid := make([]float64, len(people))
	owed := make([]float64, len(people))
	spent := make([]float64, len(people))
	sent := make([]float64, len(people))
	received := make([]float64, len(people))
	for i, p := range people {
		w := p.Weight
		if w == 0 {
			w = 1
		}
		weight[p.ID], idx[p.ID] = w, i
		out[i] = Balance{MemberID: p.ID, Name: p.Name}
	}
	for _, b := range bills {
		if !b.IsBill() || b.Deleted || b.PayerID == nil {
			continue
		}
		amount := float64(b.AmountHome)
		reimb := b.Category == store.ReimbursementKey
		if i, ok := idx[*b.PayerID]; ok {
			if reimb {
				sent[i] += amount
			} else {
				paid[i] += amount
			}
		}
		var total float64
		for _, m := range b.Owers {
			total += weight[m]
		}
		if total == 0 {
			continue
		}
		for _, m := range b.Owers {
			i, ok := idx[m]
			if !ok {
				continue
			}
			share := amount / total * weight[m]
			owed[i] += share
			if reimb {
				received[i] += share
			} else {
				spent[i] += share
			}
		}
	}
	for i := range out {
		out[i].Paid = int64(math.Round(paid[i]))
		out[i].Spent = int64(math.Round(spent[i]))
		out[i].Sent = int64(math.Round(sent[i]))
		out[i].Received = int64(math.Round(received[i]))
		out[i].Balance = int64(math.Round(paid[i] + sent[i] - owed[i]))
	}
	return out
}

// settle turns balances into payments: the one owing most pays the one owed
// most, as much as settles one of them, until nobody is left (Cospend's plan).
func settle(bal []Balance) []Transfer {
	type acc struct {
		id  int64
		amt int64
	}
	var debit, credit []acc
	for _, b := range bal {
		if b.Balance < 0 {
			debit = append(debit, acc{b.MemberID, -b.Balance})
		} else if b.Balance > 0 {
			credit = append(credit, acc{b.MemberID, b.Balance})
		}
	}
	out := []Transfer{}
	for len(debit) > 0 && len(credit) > 0 {
		sort.Slice(debit, func(i, j int) bool {
			return debit[i].amt > debit[j].amt || (debit[i].amt == debit[j].amt && debit[i].id < debit[j].id)
		})
		sort.Slice(credit, func(i, j int) bool {
			return credit[i].amt > credit[j].amt || (credit[i].amt == credit[j].amt && credit[i].id < credit[j].id)
		})
		d, c := &debit[0], &credit[0]
		amt := d.amt
		if c.amt < amt {
			amt = c.amt
		}
		if amt > 0 {
			out = append(out, Transfer{From: d.id, To: c.id, Amount: amt})
		}
		d.amt -= amt
		c.amt -= amt
		if d.amt <= 0 {
			debit = debit[1:]
		}
		if c.amt <= 0 {
			credit = credit[1:]
		}
	}
	return out
}

// settleThrough settles everyone with one person (Cospend's "centred on"):
// whoever owes pays them, whoever is owed gets paid by them.
func settleThrough(bal []Balance, center int64) []Transfer {
	out := []Transfer{}
	for _, b := range bal {
		if b.MemberID == center || b.Balance == 0 {
			continue
		}
		if b.Balance > 0 {
			out = append(out, Transfer{From: center, To: b.MemberID, Amount: b.Balance})
		} else {
			out = append(out, Transfer{From: b.MemberID, To: center, Amount: -b.Balance})
		}
	}
	return out
}

// SplitInfo is everything about the trip's shared expenses.
type SplitInfo struct {
	Currency     string                  `json:"currency"`
	People       []store.TripPerson      `json:"people"`
	Balances     []Balance               `json:"balances"`
	Settlement   []Transfer              `json:"settlement"`
	Center       int64                   `json:"center,omitempty"` // settlement through this person (?center=)
	Categories   []store.TripCategory    `json:"categories"`
	PaymentModes []store.TripPaymentMode `json:"payment_modes"`
	Currencies   []store.TripCurrency    `json:"currencies"`
	Bills        int                     `json:"bills"`
	Total        int64                   `json:"total"` // spent, reimbursements left out
	Trash        int                     `json:"trash"`
	Share        *store.TripShare        `json:"share,omitempty"` // the Cospend link (owner and editors)
	ByCategory   map[string]int64        `json:"by_category"`
	ByPayment    map[int64]int64         `json:"by_payment_mode"`
}

func (s *Server) splitInfo(ctx context.Context, t *store.Trip, withShare bool) (*SplitInfo, error) {
	if err := s.store.EnsureBillSetup(ctx, t.ID); err != nil {
		return nil, err
	}
	people, err := s.store.ListTripPeople(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	bills, err := s.store.ListExpensesWith(ctx, t.ID, store.ExpenseFilter{BillsOnly: true})
	if err != nil {
		return nil, err
	}
	trashed := true
	trash, err := s.store.ListExpensesWith(ctx, t.ID, store.ExpenseFilter{Deleted: &trashed, BillsOnly: true})
	if err != nil {
		return nil, err
	}
	info := &SplitInfo{Currency: t.Currency, People: people, Bills: len(bills), Trash: len(trash), ByCategory: map[string]int64{}, ByPayment: map[int64]int64{}}
	info.Balances = balances(people, bills)
	info.Settlement = settle(info.Balances)
	for _, b := range bills {
		if b.Category == store.ReimbursementKey {
			continue
		}
		info.Total += b.AmountHome
		info.ByCategory[b.Category] += b.AmountHome
		info.ByPayment[b.PaymentModeID] += b.AmountHome
	}
	if info.Categories, err = s.store.ListTripCategories(ctx, t.ID); err != nil {
		return nil, err
	}
	if info.PaymentModes, err = s.store.ListTripPaymentModes(ctx, t.ID); err != nil {
		return nil, err
	}
	if info.Currencies, err = s.store.ListTripCurrencies(ctx, t.ID); err != nil {
		return nil, err
	}
	if withShare {
		info.Share, _ = s.store.GetTripShare(ctx, t.ID)
	}
	return info, nil
}

// handleSplit answers GET /trips/{id}/split.
func (s *Server) handleSplit(w http.ResponseWriter, r *http.Request) {
	tc := tripFrom(r.Context())
	info, err := s.splitInfo(r.Context(), tc.trip, tc.role != "viewer")
	if err != nil {
		s.fail(w, err, "split")
		return
	}
	if c := idParamQuery(r, "center"); c > 0 {
		for _, b := range info.Balances {
			if b.MemberID == c {
				info.Settlement, info.Center = settleThrough(info.Balances, c), c
			}
		}
	}
	writeJSON(w, http.StatusOK, info)
}

// handleSettle records the settlement as reimbursement bills: each transfer
// becomes a bill paid by the one who owed, for the one who is owed.
func (s *Server) handleSettle(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	ctx := r.Context()
	t := tripFrom(ctx).trip
	info, err := s.splitInfo(ctx, t, false)
	if err != nil {
		s.fail(w, err, "settle")
		return
	}
	names := map[int64]string{}
	for _, p := range info.People {
		names[p.ID] = p.Name
	}
	today := store.Today()
	for _, tr := range info.Settlement {
		from, to := tr.From, tr.To
		e := &store.Expense{TripID: t.ID, Name: fmt.Sprintf("%s → %s", names[from], names[to]), Category: store.ReimbursementKey,
			Amount: tr.Amount, Currency: t.Currency, AmountHome: tr.Amount, FXRate: 1, Date: today, Kind: "actual", Source: "manual",
			PayerID: &from, Owers: []int64{to}}
		if err := s.store.CreateExpense(ctx, e); err != nil {
			s.fail(w, err, "settle")
			return
		}
	}
	s.store.LogActivity(ctx, t.ID, userFrom(ctx).ID, "settled", fmt.Sprintf("%d payments", len(info.Settlement)))
	info, _ = s.splitInfo(ctx, t, true)
	writeJSON(w, http.StatusOK, info)
}

// ---- trash ----

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	t := tripFrom(r.Context()).trip
	yes := true
	bills, err := s.store.ListExpensesWith(r.Context(), t.ID, store.ExpenseFilter{Deleted: &yes})
	if err != nil {
		s.fail(w, err, "trash")
		return
	}
	writeJSON(w, http.StatusOK, bills)
}

func (s *Server) handleRestoreExpense(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	if err := s.store.TrashExpense(r.Context(), t.ID, idParam(r, "id"), false); err != nil {
		s.fail(w, err, "restore")
		return
	}
	e, _ := s.store.GetExpense(r.Context(), t.ID, idParam(r, "id"))
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	n, err := s.store.EmptyTrash(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "empty trash")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

// ---- categories, payment modes, currencies ----

func (s *Server) handleSaveCategory(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var c store.TripCategory
	if err := decode(r, &c); err != nil || strings.TrimSpace(c.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a name is required")
		return
	}
	_ = s.store.EnsureBillSetup(r.Context(), t.ID)
	c.TripID, c.Name, c.Key = t.ID, strings.TrimSpace(c.Name), ""
	if id := idParam(r, "id"); id > 0 {
		c.ID = id
	} else {
		c.ID = 0
	}
	if err := s.store.SaveTripCategory(r.Context(), &c); err != nil {
		s.fail(w, err, "category")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	if err := s.store.DeleteTripCategory(r.Context(), tripFrom(r.Context()).trip.ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "category")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSavePaymentMode(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var m store.TripPaymentMode
	if err := decode(r, &m); err != nil || strings.TrimSpace(m.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a name is required")
		return
	}
	_ = s.store.EnsureBillSetup(r.Context(), t.ID)
	m.TripID, m.Name, m.ID = t.ID, strings.TrimSpace(m.Name), idParam(r, "id")
	if err := s.store.SaveTripPaymentMode(r.Context(), &m); err != nil {
		s.fail(w, err, "payment mode")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeletePaymentMode(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	if err := s.store.DeleteTripPaymentMode(r.Context(), tripFrom(r.Context()).trip.ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "payment mode")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSaveCurrency(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	t := tripFrom(r.Context()).trip
	var c store.TripCurrency
	if err := decode(r, &c); err != nil || strings.TrimSpace(c.Name) == "" || c.ExchangeRate <= 0 {
		writeErr(w, http.StatusBadRequest, "a name and an exchange rate above 0 are required")
		return
	}
	c.TripID, c.Name, c.ID = t.ID, strings.TrimSpace(c.Name), idParam(r, "id")
	if err := s.store.SaveTripCurrency(r.Context(), &c); err != nil {
		s.fail(w, err, "currency")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteCurrency(w http.ResponseWriter, r *http.Request) {
	if !s.canEdit(w, r) {
		return
	}
	if err := s.store.DeleteTripCurrency(r.Context(), tripFrom(r.Context()).trip.ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "currency")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- people and their bills ----

// memberInUse says why someone cannot leave a trip: they paid or owe a bill.
func (s *Server) memberInUseImpl(ctx context.Context, tripID, memberID int64) string {
	n, err := s.store.MemberBills(ctx, tripID, memberID)
	if err != nil || n == 0 {
		return ""
	}
	return fmt.Sprintf("they are on %d bill(s) — take them off those first, or keep them and switch them off for new bills", n)
}

// moveMemberRefsImpl moves the bills of one trip member to another.
func moveMemberRefsImpl(tx *sql.Tx, tripID, from, into int64) error {
	return store.MoveMemberBills(context.Background(), tx, tripID, from, into)
}

// nextRepeat is the day after day a bill repeating every freq periods comes back.
func nextRepeat(day, repeat string, freq int) (string, bool) {
	d, err := store.ParseDate(day)
	if err != nil {
		return "", false
	}
	if freq <= 0 {
		freq = 1
	}
	switch repeat {
	case "d":
		d = d.AddDate(0, 0, freq)
	case "w":
		d = d.AddDate(0, 0, 7*freq)
	case "b":
		d = d.AddDate(0, 0, 14*freq)
	case "s": // twice a month: the 1st and the 16th
		for i := 0; i < freq; i++ {
			if d.Day() < 16 {
				d = d.AddDate(0, 0, 16-d.Day())
			} else {
				d = d.AddDate(0, 1, 1-d.Day())
			}
		}
	case "m":
		d = d.AddDate(0, freq, 0)
	case "y":
		d = d.AddDate(freq, 0, 0)
	default:
		return "", false
	}
	return d.Format("2006-01-02"), true
}

// repeatBills creates the copies of repeating bills that are due by today,
// as Cospend does: the newest copy carries the repetition on, the earlier
// ones stop repeating. With "all active" a copy is for everyone taking part
// at that moment.
func (s *Server) repeatBills(ctx context.Context, today string) int {
	bills, err := s.store.RepeatingBills(ctx)
	if err != nil {
		return 0
	}
	made := 0
	for _, b := range bills {
		cur := b
		for i := 0; i < 400; i++ {
			next, ok := nextRepeat(cur.Date, cur.Repeat, cur.RepeatFreq)
			if !ok || next > today || (cur.RepeatUntil != "" && next > cur.RepeatUntil) {
				break
			}
			cp := cur
			cp.ID, cp.Date, cp.Source, cp.ExternalID, cp.LinkKind, cp.LinkID = 0, next, "manual", "", "", 0
			cp.Owers = append([]int64{}, cur.Owers...)
			if cur.RepeatAllActive {
				if people, err := s.store.ListTripPeople(ctx, cur.TripID); err == nil {
					cp.Owers = cp.Owers[:0]
					for _, p := range people {
						if p.Active {
							cp.Owers = append(cp.Owers, p.ID)
						}
					}
				}
			}
			if err := s.store.CreateExpense(ctx, &cp); err != nil {
				break
			}
			cur.Repeat = "n"
			cur.Owers = nil // keep who it was for
			if err := s.store.UpdateExpense(ctx, &cur); err != nil {
				break
			}
			made++
			cur = cp
		}
	}
	return made
}

// splitExtras reads an uneven split: amount is the whole bill, each extra is
// owed on top by one person, and the main bill keeps the rest, shared equally
// (by weight). Returns nil when the request does not touch extras; an empty
// list removes them.
func (s *Server) splitExtras(ctx context.Context, t *store.Trip, e *store.Expense, in expenseInput, keys map[string]bool) ([]billExtra, string) {
	if !keys["extras"] {
		return nil, ""
	}
	out := []billExtra{}
	if !e.IsBill() {
		return out, ""
	}
	if e.Source == "item" || store.IsSynced(e.Source) {
		for _, x := range in.Extras {
			if x.Amount != 0 {
				return nil, "the amount of this bill comes from elsewhere — it can only be split evenly"
			}
		}
		return out, ""
	}
	people, err := s.store.ListTripPeople(ctx, t.ID)
	if err != nil {
		return nil, "could not load the trip's people"
	}
	on := map[int64]bool{}
	for _, p := range people {
		on[p.ID] = true
	}
	var sum int64
	seen := map[int64]bool{}
	for _, x := range in.Extras {
		if x.Amount == 0 {
			continue
		}
		if x.Amount < 0 {
			return nil, "an extra part cannot be negative"
		}
		if !on[x.MemberID] {
			return nil, "an extra part is for someone not on this trip"
		}
		if seen[x.MemberID] {
			return nil, "one extra part per person"
		}
		seen[x.MemberID] = true
		sum += x.Amount
		out = append(out, x)
	}
	if sum > e.Amount {
		return nil, "the extra parts add up to more than the bill"
	}
	// the main bill keeps what is shared
	e.Amount -= sum
	e.AmountHome = currency.Convert(e.Amount, e.Currency, t.Currency, e.FXRate)
	return out, ""
}

// writeBillParts replaces the extra parts of a bill: one small bill per
// person, paid by the same payer, for that person alone.
func (s *Server) writeBillParts(ctx context.Context, t *store.Trip, main *store.Expense, extras []billExtra) error {
	old, err := s.store.BillParts(ctx, t.ID, main.ID)
	if err != nil {
		return err
	}
	for _, p := range old {
		if err := s.store.DeleteExpense(ctx, t.ID, p.ID); err != nil {
			return err
		}
	}
	if !main.IsBill() {
		return nil
	}
	names := map[int64]string{}
	if people, err := s.store.ListTripPeople(ctx, t.ID); err == nil {
		for _, p := range people {
			names[p.ID] = p.Name
		}
	}
	for _, x := range extras {
		member := x.MemberID
		part := &store.Expense{TripID: t.ID, Name: main.Name + " · " + names[member], Category: main.Category, Amount: x.Amount, Currency: main.Currency,
			FXRate: main.FXRate, AmountHome: currency.Convert(x.Amount, main.Currency, t.Currency, main.FXRate), Date: main.Date, BillTime: main.BillTime,
			Kind: "actual", Source: "manual", PayerID: main.PayerID, Owers: []int64{member}, PaymentModeID: main.PaymentModeID, ParentID: &main.ID, Deleted: main.Deleted}
		if err := s.store.CreateExpense(ctx, part); err != nil {
			return err
		}
	}
	return nil
}

func idParamQuery(r *http.Request, k string) int64 {
	var id int64
	fmt.Sscan(r.URL.Query().Get(k), &id)
	return id
}

// DayCost is what a day of the trip cost, by category (trip currency).
type DayCost struct {
	Date    string           `json:"date"` // YYYY-MM-DD, or "before" / "after" the trip
	Actual  map[string]int64 `json:"actual"`
	Planned map[string]int64 `json:"planned"`
	// People is each member's share of the day's bills, by category.
	People map[int64]map[string]int64 `json:"people"`
}

// spreadOverTrip says whether a cost belongs to the whole trip rather than
// one day: stays, tickets (a leg, or transport booked before the trip) and
// insurance are averaged over every day; a meal or a taxi stays on its day.
func spreadOverTrip(t *store.Trip, e store.Expense) bool {
	switch {
	case e.LinkKind == "lodging" || e.LinkKind == "transport":
		return true
	case e.Category == "lodging" || e.Category == "insurance":
		return true
	case e.Category == "transport":
		return e.Date == "" || (t.StartDate != "" && e.Date < t.StartDate) || (t.EndDate != "" && e.Date > t.EndDate)
	}
	return false
}

// dayCosts spreads a trip's costs over its days. Whole-trip costs (stays,
// flights, insurance) are shared out equally over every day of the trip;
// everything else sits on its own date (dinner on the evening it was eaten).
// Other costs dated outside the trip land in "before" or "after";
// reimbursements are not costs. Each bill's split between the people is
// kept per day too, so one person's days can be shown.
func dayCosts(t *store.Trip, expenses []store.Expense, people []store.TripPerson) []DayCost {
	weight := map[int64]float64{}
	for _, p := range people {
		w := p.Weight
		if w == 0 {
			w = 1
		}
		weight[p.ID] = w
	}
	var tripDays []string
	if s, err := store.ParseDate(t.StartDate); err == nil {
		if e, err := store.ParseDate(t.EndDate); err == nil && !e.Before(s) {
			for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
				tripDays = append(tripDays, d.Format("2006-01-02"))
			}
		}
	}
	days := map[string]*DayCost{}
	get := func(key string) *DayCost {
		d := days[key]
		if d == nil {
			d = &DayCost{Date: key, Actual: map[string]int64{}, Planned: map[string]int64{}, People: map[int64]map[string]int64{}}
			days[key] = d
		}
		return d
	}
	keyOf := func(date string) string {
		switch {
		case date == "":
			if t.StartDate != "" {
				return t.StartDate
			}
			return "before"
		case t.StartDate != "" && date < t.StartDate:
			return "before"
		case t.EndDate != "" && date > t.EndDate:
			return "after"
		}
		return date
	}
	// add puts an amount on a day, and each ower's share of it
	add := func(key string, e store.Expense, amount int64) {
		d := get(key)
		if !e.IsBill() {
			d.Planned[e.Category] += amount
			return
		}
		d.Actual[e.Category] += amount
		var total float64
		for _, m := range e.Owers {
			total += weight[m]
		}
		if total == 0 {
			return
		}
		for _, m := range e.Owers {
			if _, ok := weight[m]; !ok {
				continue
			}
			if d.People[m] == nil {
				d.People[m] = map[string]int64{}
			}
			d.People[m][e.Category] += int64(math.Round(float64(amount) * weight[m] / total))
		}
	}
	for _, e := range expenses {
		if e.Deleted || e.Category == store.ReimbursementKey {
			continue
		}
		if n := int64(len(tripDays)); n > 1 && spreadOverTrip(t, e) {
			per, rest := e.AmountHome/n, e.AmountHome%n
			for i, day := range tripDays {
				amt := per
				if i == 0 {
					amt += rest
				}
				add(day, e, amt)
			}
			continue
		}
		add(keyOf(e.Date), e, e.AmountHome)
	}
	// every day of the trip, even the free ones
	for _, k := range tripDays {
		get(k)
	}
	out := make([]DayCost, 0, len(days))
	for _, d := range days {
		out = append(out, *d)
	}
	rank := func(k string) string {
		switch k {
		case "before":
			return "0"
		case "after":
			return "9"
		}
		return "5" + k
	}
	sort.Slice(out, func(i, j int) bool { return rank(out[i].Date) < rank(out[j].Date) })
	return out
}
