package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

func TestBalancesAndSettle(t *testing.T) {
	people := []store.TripPerson{{ID: 1, Name: "Darko", Weight: 1}, {ID: 2, Name: "Ana", Weight: 1}, {ID: 3, Name: "Marko", Weight: 2}}
	p := func(id int64) *int64 { return &id }
	bills := []store.Expense{
		{Kind: "actual", AmountHome: 12000, PayerID: p(1), Owers: []int64{1, 2, 3}}, // 30 / 30 / 60
		{Kind: "actual", AmountHome: 3000, PayerID: p(2), Owers: []int64{1, 2}},     // 15 / 15
		{Kind: "actual", AmountHome: 999, PayerID: p(3), Owers: []int64{3}, Deleted: true},
		{Kind: "planned", AmountHome: 50000, PayerID: p(3), Owers: []int64{1}},
	}
	bal := balances(people, bills)
	want := map[int64]int64{1: 12000 - 3000 - 1500, 2: 3000 - 3000 - 1500, 3: -6000}
	for _, b := range bal {
		if b.Balance != want[b.MemberID] {
			t.Fatalf("%s balance %d, want %d (%+v)", b.Name, b.Balance, want[b.MemberID], bal)
		}
	}
	tr := settle(bal)
	if len(tr) != 2 || tr[0].From != 3 || tr[0].To != 1 || tr[0].Amount != 6000 || tr[1].From != 2 || tr[1].To != 1 || tr[1].Amount != 1500 {
		t.Fatalf("settle = %+v", tr)
	}
	var sum int64
	for _, b := range bal {
		sum += b.Balance
	}
	if sum != 0 {
		t.Fatalf("balances add up to %d", sum)
	}
}

func TestBills(t *testing.T) {
	h := newIsoHarness(t)
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "Malta", "start_date": "2025-08-26", "end_date": "2025-09-02", "currency": "EUR"})
	base := fmt.Sprintf("/api/v1/trips/%d", tr)
	ana := h.create(h.ownerTok, base+"/people", map[string]any{"name": "Ana"})
	var people []store.TripPerson
	_, b := h.do(h.ownerTok, http.MethodGet, base+"/people", nil)
	_ = json.Unmarshal(b, &people)
	me := people[0].ID

	// a spent cost without payer/owers: the owner paid, for everyone
	dinner := h.create(h.ownerTok, base+"/expenses", map[string]any{"name": "Dinner", "category": "food", "amount": 6000, "currency": "EUR", "kind": "actual", "date": "2025-08-27"})
	_, b = h.do(h.ownerTok, http.MethodGet, fmt.Sprintf("%s/expenses", base), nil)
	var ex []store.Expense
	_ = json.Unmarshal(b, &ex)
	if len(ex) != 1 || ex[0].PayerID == nil || *ex[0].PayerID != me || len(ex[0].Owers) != 2 {
		t.Fatalf("defaults: %s", b)
	}
	// Ana pays the taxi for herself and me
	h.create(h.ownerTok, base+"/expenses", map[string]any{"name": "Taxi", "category": "transport", "amount": 2000, "currency": "EUR", "kind": "actual", "date": "2025-08-27", "payer_id": ana, "owers": []int64{me, ana}})
	// a payer from another trip is refused
	if code, _ := h.do(h.ownerTok, http.MethodPost, base+"/expenses", map[string]any{"name": "x", "amount": 1, "kind": "actual", "payer_id": 999999}); code != http.StatusBadRequest {
		t.Fatalf("foreign payer accepted: %d", code)
	}
	split := func() SplitInfo {
		t.Helper()
		_, b := h.do(h.ownerTok, http.MethodGet, base+"/split", nil)
		var s SplitInfo
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatalf("%s", b)
		}
		return s
	}
	s := split()
	if s.Total != 8000 || len(s.Settlement) != 1 || s.Settlement[0].From != ana || s.Settlement[0].Amount != 2000 || len(s.Categories) == 0 || len(s.PaymentModes) != 5 {
		t.Fatalf("split = %+v", s)
	}
	// Ana cannot leave while she is on bills
	if code, _ := h.do(h.ownerTok, http.MethodDelete, fmt.Sprintf("%s/people/%d", base, ana), nil); code != http.StatusConflict {
		t.Fatalf("removed someone with bills: %d", code)
	}
	// settle up: a reimbursement bill, and everyone is at zero
	h.do(h.ownerTok, http.MethodPost, base+"/split/settle", nil)
	s = split()
	if len(s.Settlement) != 0 || s.Total != 8000 {
		t.Fatalf("after settle = %+v", s)
	}
	// deleting a bill puts it in the trash; restore brings it back
	h.do(h.ownerTok, http.MethodDelete, fmt.Sprintf("%s/expenses/%d", base, dinner), nil)
	if s := split(); s.Trash != 1 || s.Total != 2000 {
		t.Fatalf("after trash = %+v", s)
	}
	h.do(h.ownerTok, http.MethodPost, fmt.Sprintf("%s/expenses/%d/restore", base, dinner), nil)
	if s := split(); s.Trash != 0 || s.Total != 8000 {
		t.Fatalf("after restore = %+v", s)
	}
	// a paid stay becomes a bill, an unpaid one stays planned
	st := h.create(h.ownerTok, base+"/lodging", map[string]any{"name": "Hotel", "price": 40000, "currency": "EUR", "check_in": "2025-08-26", "check_out": "2025-09-02"})
	if s := split(); s.Total != 8000 {
		t.Fatalf("unpaid stay counted: %+v", s)
	}
	h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("%s/lodging/%d", base, st), map[string]any{"paid": true})
	if s := split(); s.Total != 48000 {
		t.Fatalf("paid stay not a bill: %+v", s)
	}
	// the counts on the trip leave the trash out
	h.do(h.ownerTok, http.MethodDelete, fmt.Sprintf("%s/expenses/%d", base, dinner), nil)
	h.do(h.ownerTok, http.MethodDelete, base+"/split/trash", nil)
	if s := split(); s.Trash != 0 {
		t.Fatalf("trash not emptied: %+v", s)
	}
}

func TestRepeatBills(t *testing.T) {
	for _, c := range []struct {
		day, rep string
		freq     int
		want     string
	}{
		{"2025-01-31", "m", 1, "2025-03-03"}, {"2025-01-10", "w", 2, "2025-01-24"}, {"2025-01-10", "s", 1, "2025-01-16"}, {"2025-01-20", "s", 1, "2025-02-01"}, {"2024-02-29", "y", 1, "2025-03-01"},
	} {
		if got, _ := nextRepeat(c.day, c.rep, c.freq); got != c.want {
			t.Errorf("%s +%s×%d = %s, want %s", c.day, c.rep, c.freq, got, c.want)
		}
	}
	h := newIsoHarness(t)
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "Flat", "start_date": "2025-01-01", "end_date": "2025-12-31", "currency": "EUR"})
	base := fmt.Sprintf("/api/v1/trips/%d", tr)
	h.create(h.ownerTok, base+"/expenses", map[string]any{"name": "Rent", "amount": 50000, "kind": "actual", "date": "2025-01-01", "repeat": "m", "repeat_until": "2025-04-15"})
	if n := h.srv.repeatBills(t.Context(), "2025-06-01"); n != 3 {
		t.Fatalf("made %d copies", n)
	}
	if n := h.srv.repeatBills(t.Context(), "2025-06-01"); n != 0 {
		t.Fatalf("made %d more", n)
	}
	_, b := h.do(h.ownerTok, http.MethodGet, base+"/expenses", nil)
	var ex []store.Expense
	_ = json.Unmarshal(b, &ex)
	repeating := 0
	for _, e := range ex {
		if e.Repeat != "n" {
			repeating++
		}
		if len(e.Owers) != 1 {
			t.Fatalf("copy lost its owers: %+v", e)
		}
	}
	if len(ex) != 4 || repeating != 1 || ex[0].Date != "2025-04-01" {
		t.Fatalf("bills = %s", b)
	}
}

func TestUnevenSplit(t *testing.T) {
	h := newIsoHarness(t)
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "Gozo", "start_date": "2025-08-26", "end_date": "2025-09-02", "currency": "EUR"})
	base := fmt.Sprintf("/api/v1/trips/%d", tr)
	ana := h.create(h.ownerTok, base+"/people", map[string]any{"name": "Ana"})
	marko := h.create(h.ownerTok, base+"/people", map[string]any{"name": "Marko"})
	var people []store.TripPerson
	_, b := h.do(h.ownerTok, http.MethodGet, base+"/people", nil)
	_ = json.Unmarshal(b, &people)
	me := people[0].ID
	// dinner 100 paid by me: Ana had wine for 20, Marko a dessert for 5, the rest (75) shared by all three
	id := h.create(h.ownerTok, base+"/expenses", map[string]any{"name": "Dinner", "amount": 10000, "kind": "actual", "date": "2025-08-27", "owers": []int64{me, ana, marko},
		"extras": []map[string]any{{"member_id": ana, "amount": 2000}, {"member_id": marko, "amount": 500}}})
	split := func() SplitInfo {
		_, b := h.do(h.ownerTok, http.MethodGet, base+"/split", nil)
		var s SplitInfo
		_ = json.Unmarshal(b, &s)
		return s
	}
	s := split()
	owes := map[int64]int64{}
	for _, tr := range s.Settlement {
		owes[tr.From] = tr.Amount
	}
	if s.Total != 10000 || owes[ana] != 2000+2500 || owes[marko] != 500+2500 {
		t.Fatalf("uneven = %+v", s)
	}
	// edit: no more extras → even again; parts are gone
	h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("%s/expenses/%d", base, id), map[string]any{"amount": 9000, "extras": []any{}})
	s = split()
	if s.Total != 9000 || s.Bills != 1 {
		t.Fatalf("even again = %+v", s)
	}
	// fully uneven: everyone's exact part, nothing left to share
	h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("%s/expenses/%d", base, id), map[string]any{"amount": 9000, "extras": []map[string]any{{"member_id": me, "amount": 1000}, {"member_id": ana, "amount": 3000}, {"member_id": marko, "amount": 5000}}})
	s = split()
	owes = map[int64]int64{}
	for _, tr := range s.Settlement {
		owes[tr.From] = tr.Amount
	}
	if s.Total != 9000 || owes[ana] != 3000 || owes[marko] != 5000 {
		t.Fatalf("exact parts = %+v", s)
	}
	// too much
	if code, _ := h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("%s/expenses/%d", base, id), map[string]any{"amount": 1000, "extras": []map[string]any{{"member_id": ana, "amount": 2000}}}); code != http.StatusBadRequest {
		t.Fatalf("extras over the total accepted: %d", code)
	}
	// the trash takes the parts along, and gives them back
	h.do(h.ownerTok, http.MethodDelete, fmt.Sprintf("%s/expenses/%d", base, id), nil)
	if s := split(); s.Total != 0 || s.Trash != 4 {
		t.Fatalf("trashed = %+v", s)
	}
	h.do(h.ownerTok, http.MethodPost, fmt.Sprintf("%s/expenses/%d/restore", base, id), nil)
	if s := split(); s.Total != 9000 {
		t.Fatalf("restored = %+v", s)
	}
}

func TestBreakdownSettleThroughAndDayCosts(t *testing.T) {
	p := func(id int64) *int64 { return &id }
	people := []store.TripPerson{{ID: 1, Name: "Darko", Weight: 1}, {ID: 2, Name: "Ana", Weight: 1}, {ID: 3, Name: "Marko", Weight: 1}}
	bills := []store.Expense{
		{Kind: "actual", AmountHome: 20000, PayerID: p(2), Owers: []int64{1}, Category: store.ReimbursementKey}, // Ana gives me 200 up front
		{Kind: "actual", AmountHome: 30000, PayerID: p(1), Owers: []int64{1, 2, 3}, Category: "lodging"},        // I pay the flat
	}
	bal := balances(people, bills)
	me, ana, marko := bal[0], bal[1], bal[2]
	if me.Paid != 30000 || me.Received != 20000 || me.Spent != 10000 || me.Balance != 0 || ana.Sent != 20000 || ana.Balance != 10000 || marko.Balance != -10000 {
		t.Fatalf("breakdown = %+v", bal)
	}
	// everyone settles with Marko… or through me
	tr := settleThrough(bal, 1)
	if len(tr) != 2 || tr[0].From != 1 || tr[0].To != 2 || tr[0].Amount != 10000 || tr[1].From != 3 || tr[1].To != 1 || tr[1].Amount != 10000 {
		t.Fatalf("through me = %+v", tr)
	}
	// the stay and the flight are spread over every day, dinner sits on its day, an early museum ticket before the trip
	trip := &store.Trip{StartDate: "2025-07-01", EndDate: "2025-07-04"}
	ex := []store.Expense{
		{Kind: "actual", AmountHome: 40001, Category: "lodging", LinkKind: "lodging", LinkID: 7, Date: "2025-07-01", PayerID: p(1), Owers: []int64{1, 2}},
		{Kind: "planned", AmountHome: 8000, Category: "transport", LinkKind: "transport", Date: "2025-07-04"},
		{Kind: "actual", AmountHome: 500, Category: "activities", Date: "2025-06-20"},
		{Kind: "actual", AmountHome: 3000, Category: "food", Date: "2025-07-02", PayerID: p(2), Owers: []int64{1, 2, 3}},
		{Kind: "actual", AmountHome: 999, Category: store.ReimbursementKey, Date: "2025-07-02"},
	}
	dc := dayCosts(trip, ex, people)
	got := map[string]DayCost{}
	for _, d := range dc {
		got[d.Date] = d
	}
	if len(dc) != 5 || dc[0].Date != "before" || got["before"].Actual["activities"] != 500 || got["2025-07-01"].Actual["lodging"] != 10001 || got["2025-07-04"].Actual["lodging"] != 10000 ||
		got["2025-07-01"].Planned["transport"] != 2000 || got["2025-07-02"].Actual["food"] != 3000 || len(got["2025-07-02"].Actual) != 2 ||
		got["2025-07-02"].People[3]["food"] != 1000 || got["2025-07-02"].People[2]["lodging"] != 5000 || got["2025-07-02"].People[3]["lodging"] != 0 {
		t.Fatalf("day costs = %+v", dc)
	}
}

func TestBudgetPlanAndOnThisDay(t *testing.T) {
	h := newIsoHarness(t)
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "London", "start_date": "2025-11-03", "end_date": "2025-11-07", "currency": "EUR"})
	base := fmt.Sprintf("/api/v1/trips/%d", tr)
	// 400 for the flat in all, 50 a day for food over 5 days: a budget of 650
	code, b := h.do(h.ownerTok, http.MethodPut, base+"/budget/plan", map[string]any{"lines": []map[string]any{
		{"category": "lodging", "amount": 40000}, {"category": "food", "amount": 5000, "per_day": true}, {"category": "fun", "amount": 0}}})
	if code != http.StatusOK {
		t.Fatalf("plan: %d %s", code, b)
	}
	var ov struct {
		Trip       store.Trip       `json:"trip"`
		BudgetPlan []store.PlanLine `json:"budget_plan"`
	}
	_, b = h.do(h.ownerTok, http.MethodGet, base+"/overview", nil)
	_ = json.Unmarshal(b, &ov)
	if len(ov.BudgetPlan) != 2 || ov.Trip.Budget != 65000 || !ov.BudgetPlan[1].PerDay {
		t.Fatalf("overview plan = %+v budget %d", ov.BudgetPlan, ov.Trip.Budget)
	}
	// a viewer can't plan
	if code, _ := h.do(h.memberTok, http.MethodPut, base+"/budget/plan", map[string]any{"lines": []any{}}); code == http.StatusOK {
		t.Fatalf("a stranger planned the budget")
	}

	// on 5 November 2026, this was its 3rd day a year before
	s := h.srv
	trips, _ := s.store.ListTrips(t.Context(), store.TripFilter{UserID: h.owner.ID})
	got := s.onThisDay(t.Context(), trips, "2026-11-05")
	if len(got) != 1 || got[0].DayNumber != 3 || got[0].YearsAgo != 1 || got[0].Date != "2025-11-05" {
		t.Fatalf("on this day = %+v", got)
	}
	if got := s.onThisDay(t.Context(), trips, "2026-11-09"); len(got) != 0 {
		t.Fatalf("nothing on 9 Nov, got %+v", got)
	}
	if got := s.onThisDay(t.Context(), trips, "2025-11-05"); len(got) != 0 {
		t.Fatalf("the trip itself is not an earlier year: %+v", got)
	}
}
