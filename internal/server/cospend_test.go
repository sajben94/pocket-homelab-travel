package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// TestCospendAPI talks to a trip the way MoneyBuster does with a Cospend
// share link: form-encoded requests to /index.php/apps/cospend/api…
func TestCospendAPI(t *testing.T) {
	h := newIsoHarness(t)
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "Malta 2025", "start_date": "2025-08-26", "end_date": "2025-09-02", "currency": "EUR"})
	base := fmt.Sprintf("/api/v1/trips/%d", tr)
	h.create(h.ownerTok, base+"/people", map[string]any{"name": "Ana"})
	// the app's own bill, in another currency
	h.create(h.ownerTok, base+"/expenses", map[string]any{"name": "Pastizzi", "amount": 450, "currency": "EUR", "kind": "actual", "date": "2025-08-27", "category": "food"})

	// no link yet: nothing answers
	code, _ := h.do("", http.MethodGet, "/index.php/apps/cospend/api/projects/nope/x", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", code)
	}
	_, b := h.do(h.ownerTok, http.MethodPut, base+"/split/share", map[string]any{})
	var sh ShareLink
	_ = json.Unmarshal(b, &sh)
	if sh.Token == "" || sh.Password == "" || !strings.HasPrefix(sh.Link, "cospend+http://") || !strings.HasSuffix(sh.Link, "/"+sh.Token+"/"+sh.Password) {
		t.Fatalf("share = %s", b)
	}
	// viewers of the trip cannot see the link
	if code, _ := h.do(h.memberTok, http.MethodGet, base+"/split/share", nil); code != http.StatusNotFound {
		t.Fatalf("stranger saw the link: %d", code)
	}
	api := "/index.php/apps/cospend/api/projects/" + sh.Token + "/" + sh.Password
	api2 := "/index.php/apps/cospend/apiv2/projects/" + sh.Token + "/" + sh.Password
	form := func(method, path string, v url.Values) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, h.ts.URL+path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "MoneyBuster/1.0")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, body
	}
	// wrong password
	if code, _ := form(http.MethodGet, "/index.php/apps/cospend/api/projects/"+sh.Token+"/wrong", nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
	// project info, as MoneyBuster reads it
	code, b = form(http.MethodGet, api, nil)
	var proj struct {
		Name         string                     `json:"name"`
		CurrencyName string                     `json:"currencyname"`
		Members      []cospendMember            `json:"members"`
		Categories   map[string]json.RawMessage `json:"categories"`
		PaymentModes map[string]json.RawMessage `json:"paymentmodes"`
		Balance      map[string]float64         `json:"balance"`
		Access       int                        `json:"myaccesslevel"`
	}
	if code != http.StatusOK || json.Unmarshal(b, &proj) != nil || proj.Name != "Malta 2025" || proj.CurrencyName != "EUR" || len(proj.Members) != 2 || len(proj.Categories) == 0 || len(proj.PaymentModes) != 5 || proj.Access != 3 {
		t.Fatalf("project = %d %s", code, b)
	}
	me, ana := proj.Members[0].ID, proj.Members[1].ID
	// MoneyBuster adds a member: a plain id comes back, and the person is in the owner's book
	code, b = form(http.MethodPost, api+"/members", url.Values{"name": {"Marko"}, "weight": {"2"}})
	if code != http.StatusOK {
		t.Fatalf("add member: %d %s", code, b)
	}
	var marko int64
	if err := json.Unmarshal(b, &marko); err != nil || marko == 0 {
		t.Fatalf("member id = %s", b)
	}
	_, b = h.do(h.ownerTok, http.MethodGet, "/api/v1/people", nil)
	if !strings.Contains(string(b), `"Marko"`) {
		t.Fatalf("Marko not in the address book: %s", b)
	}
	// and a bill: Ana paid 30.50 for everyone, by card, restaurant
	var catID string
	for id, raw := range proj.Categories {
		if strings.Contains(string(raw), "Restaurant") {
			catID = id
		}
	}
	code, b = form(http.MethodPost, api+"/bills", url.Values{
		"what": {"Dinner"}, "payer": {fmt.Sprint(ana)}, "payed_for": {fmt.Sprintf("%d,%d,%d", me, ana, marko)}, "amount": {"30.50"},
		"timestamp": {"1756321200"}, "comment": {"Gozo"}, "repeat": {"n"}, "paymentmode": {"c"}, "categoryid": {catID}, "paymentmodeid": {"0"},
	})
	var billID int64
	if code != http.StatusOK || json.Unmarshal(b, &billID) != nil {
		t.Fatalf("add bill: %d %s", code, b)
	}
	// the app sees it as a bill with payer, owers, card and restaurant
	_, b = h.do(h.ownerTok, http.MethodGet, base+"/expenses", nil)
	var ex []store.Expense
	_ = json.Unmarshal(b, &ex)
	var dinner store.Expense
	for _, e := range ex {
		if e.ID == billID {
			dinner = e
		}
	}
	if dinner.AmountHome != 3050 || dinner.Category != "food" || dinner.PayerID == nil || *dinner.PayerID != ana || len(dinner.Owers) != 3 || dinner.PaymentModeID == 0 || dinner.Date != "2025-08-27" {
		t.Fatalf("dinner = %+v", dinner)
	}
	// sync: bills changed since a moment, with every bill id still there
	code, b = form(http.MethodGet, api2+"/bills?lastchanged=0", nil)
	var sync struct {
		Bills      []map[string]any `json:"bills"`
		AllBillIDs []int64          `json:"allBillIds"`
		Timestamp  int64            `json:"timestamp"`
	}
	if code != http.StatusOK || json.Unmarshal(b, &sync) != nil || len(sync.Bills) != 2 || len(sync.AllBillIDs) != 2 || sync.Timestamp == 0 {
		t.Fatalf("apiv2 bills = %d %s", code, b)
	}
	for _, bl := range sync.Bills {
		if bl["what"] == "Dinner" && (bl["amount"] != 30.5 || bl["paymentmode"] != "c" || len(bl["owers"].([]any)) != 3) {
			t.Fatalf("bill json = %v", bl)
		}
	}
	// nothing changed since now
	code, b = form(http.MethodGet, fmt.Sprintf("%s/bills?lastchanged=%d", api2, sync.Timestamp+1), nil)
	_ = json.Unmarshal(b, &sync)
	if len(sync.Bills) != 0 || len(sync.AllBillIDs) != 2 {
		t.Fatalf("since now = %s", b)
	}
	// edit: only what is sent changes
	if code, b := form(http.MethodPut, fmt.Sprintf("%s/bills/%d", api, billID), url.Values{"amount": {"40"}, "payed_for": {fmt.Sprintf("%d,%d", me, ana)}}); code != http.StatusOK {
		t.Fatalf("edit bill: %d %s", code, b)
	}
	// balances follow: Ana paid 40 for two (I owe 20), the pastizzi I paid are shared (she owes 2.25)
	_, b = h.do(h.ownerTok, http.MethodGet, base+"/split", nil)
	var sp SplitInfo
	_ = json.Unmarshal(b, &sp)
	if len(sp.Settlement) != 1 || sp.Settlement[0].From != me || sp.Settlement[0].To != ana || sp.Settlement[0].Amount != 1775 {
		t.Fatalf("settlement = %s", b)
	}
	// MoneyBuster's settle view agrees
	_, b = form(http.MethodGet, api+"/settle", nil)
	if !strings.Contains(string(b), `"amount":17.75`) {
		t.Fatalf("cospend settle = %s", b)
	}
	// delete goes to the trash and leaves the ids
	if code, b := form(http.MethodDelete, fmt.Sprintf("%s/bills/%d", api, billID), nil); code != http.StatusOK || strings.TrimSpace(string(b)) != `"OK"` {
		t.Fatalf("delete: %d %s", code, b)
	}
	_, b = form(http.MethodGet, api2+"/bills?lastchanged=0", nil)
	_ = json.Unmarshal(b, &sync)
	if len(sync.AllBillIDs) != 1 {
		t.Fatalf("after delete = %s", b)
	}
	// someone on bills is switched off, not removed
	form(http.MethodDelete, fmt.Sprintf("%s/members/%d", api, ana), nil)
	_, b = h.do(h.ownerTok, http.MethodGet, base+"/people", nil)
	if !strings.Contains(string(b), `"name":"Ana","weight":1,"active":false`) {
		t.Fatalf("ana after delete = %s", b)
	}
	// a viewer link cannot add bills
	h.do(h.ownerTok, http.MethodPut, base+"/split/share", map[string]any{"access_level": 1})
	if code, _ := form(http.MethodPost, api+"/bills", url.Values{"what": {"x"}, "payer": {fmt.Sprint(me)}, "payed_for": {fmt.Sprint(me)}, "amount": {"1"}}); code != http.StatusForbidden {
		t.Fatalf("viewer added a bill: %d", code)
	}
	// turning the link off closes it
	h.do(h.ownerTok, http.MethodDelete, base+"/split/share", nil)
	if code, _ := form(http.MethodGet, api, nil); code != http.StatusUnauthorized {
		t.Fatalf("closed link answered: %d", code)
	}
}

func TestParseCospendLink(t *testing.T) {
	for _, c := range []struct{ in, base, tok, pass string }{
		{"cospend://cloud.example.com/abc123/s3cret", "https://cloud.example.com/index.php/apps/cospend", "abc123", "s3cret"},
		{"cospend+http://192.168.1.5:8080/nc/abc123/pw", "http://192.168.1.5:8080/nc/index.php/apps/cospend", "abc123", "pw"},
		{"cospend://cloud.example.com/abc123/", "https://cloud.example.com/index.php/apps/cospend", "abc123", "no-pass"},
		{"https://cloud.example.com/index.php/apps/cospend/s/abc123", "https://cloud.example.com/index.php/apps/cospend", "abc123", "no-pass"},
		{"https://cloud.example.com/apps/cospend/s/abc123", "https://cloud.example.com/index.php/apps/cospend", "abc123", "no-pass"},
	} {
		base, tok, pass, err := parseCospendLink(c.in, "", "")
		if err != nil || base != c.base || tok != c.tok || pass != c.pass {
			t.Errorf("%s → %s %s %s %v", c.in, base, tok, pass, err)
		}
	}
	if _, _, _, err := parseCospendLink("hello", "", ""); err == nil {
		t.Error("garbage accepted")
	}
}

// TestCospendImport imports one trip's Cospend link into a new trip of
// another person — this app talking to itself as if it were Nextcloud.
func TestCospendImport(t *testing.T) {
	h := newIsoHarness(t)
	tr := h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": "Bosna Road Trip", "start_date": "2025-07-28", "end_date": "2025-07-31", "currency": "EUR"})
	base := fmt.Sprintf("/api/v1/trips/%d", tr)
	ana := h.create(h.ownerTok, base+"/people", map[string]any{"name": "Ana"})
	h.create(h.ownerTok, base+"/expenses", map[string]any{"name": "Ćevapi", "category": "food", "amount": 1800, "kind": "actual", "date": "2025-07-29", "payer_id": ana, "bill_time": "13:10"})
	h.create(h.ownerTok, base+"/expenses", map[string]any{"name": "Fuel", "category": "fuel", "amount": 6000, "kind": "actual", "date": "2025-07-30"})
	_, b := h.do(h.ownerTok, http.MethodPut, base+"/split/share", map[string]any{})
	var sh ShareLink
	_ = json.Unmarshal(b, &sh)

	imp := func() CospendImportResult {
		t.Helper()
		code, b := h.do(h.memberTok, http.MethodPost, "/api/v1/import/cospend", map[string]any{"link": sh.Link})
		var r CospendImportResult
		if code != http.StatusOK || json.Unmarshal(b, &r) != nil {
			t.Fatalf("import: %d %s", code, b)
		}
		return r
	}
	r := imp()
	if !r.Created || r.Bills != 2 || r.New != 2 || r.Members != 2 {
		t.Fatalf("import = %+v", r)
	}
	nb := fmt.Sprintf("/api/v1/trips/%d", r.TripID)
	_, b = h.do(h.memberTok, http.MethodGet, nb+"/split", nil)
	var s SplitInfo
	_ = json.Unmarshal(b, &s)
	if s.Total != 7800 || len(s.People) != 3 || len(s.Settlement) != 1 || s.Settlement[0].Amount != 2100 {
		t.Fatalf("imported split = %s", b)
	}
	_, b = h.do(h.memberTok, http.MethodGet, nb, nil)
	if !strings.Contains(string(b), `"start_date":"2025-07-29"`) {
		t.Fatalf("trip dates = %s", b)
	}
	// into the same trip again: updated, nothing doubled
	code, b := h.do(h.memberTok, http.MethodPost, "/api/v1/import/cospend", map[string]any{"link": sh.Link, "trip_id": r.TripID})
	var r3 CospendImportResult
	_ = json.Unmarshal(b, &r3)
	if code != http.StatusOK || r3.New != 0 || r3.Bills != 2 {
		t.Fatalf("re-import = %d %s", code, b)
	}
	// someone else's trip cannot be the target
	if code, _ := h.do(h.memberTok, http.MethodPost, "/api/v1/import/cospend", map[string]any{"link": sh.Link, "trip_id": tr}); code == http.StatusOK {
		t.Fatal("imported into someone else's trip")
	}
	// a wrong password says so
	if code, b := h.do(h.memberTok, http.MethodPost, "/api/v1/import/cospend", map[string]any{"link": strings.Replace(sh.Link, sh.Password, "nope", 1)}); code != http.StatusBadGateway || !strings.Contains(string(b), "refused") {
		t.Fatalf("wrong password: %d %s", code, b)
	}
}
