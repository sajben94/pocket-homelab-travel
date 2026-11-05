package server

// Import a Nextcloud Cospend project into a trip through its share link,
// reading it with the same public API MoneyBuster uses. Members become trip
// people (and people in the owner's book), categories and payment methods
// are matched by name or added, bills come over with who paid and for whom.
// Re-running the import updates what came before (bills are keyed by the
// project token and Cospend bill id) and adds what is new.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// cospendSource is the expense source of imported Cospend bills.
const cospendSource = "cospend"

// parseCospendLink understands cospend://host/token/password, the
// https://host/index.php/apps/cospend/s/<token> page and a bare
// server URL with the token and password given separately.
func parseCospendLink(link, token, password string) (base, tok, pass string, err error) {
	link = strings.TrimSpace(link)
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return "", "", "", fmt.Errorf("that does not look like a Cospend link")
	}
	switch u.Scheme {
	case "cospend", "cospend+http":
		scheme := "https"
		if u.Scheme == "cospend+http" {
			scheme = "http"
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if strings.HasSuffix(u.Path, "/") || len(parts) == 1 {
			tok, pass = parts[len(parts)-1], ""
			parts = parts[:len(parts)-1]
		} else {
			tok, pass = parts[len(parts)-2], parts[len(parts)-1]
			parts = parts[:len(parts)-2]
		}
		prefix := strings.Join(parts, "/")
		if prefix != "" {
			prefix = "/" + prefix
		}
		base = scheme + "://" + u.Host + prefix + "/index.php/apps/cospend"
	case "http", "https":
		p := strings.TrimRight(u.Path, "/")
		i := strings.Index(p, "/apps/cospend")
		root := p
		if i >= 0 {
			root = p[:i]
			rest := strings.Trim(p[i+len("/apps/cospend"):], "/")
			seg := strings.Split(rest, "/")
			if len(seg) >= 2 && seg[0] == "s" {
				tok = seg[1]
			}
		}
		root = strings.TrimSuffix(root, "/index.php")
		base = u.Scheme + "://" + u.Host + root + "/index.php/apps/cospend"
	default:
		return "", "", "", fmt.Errorf("that does not look like a Cospend link")
	}
	if token != "" {
		tok = token
	}
	if password != "" {
		pass = password
	}
	if pass == "" {
		pass = "no-pass"
	}
	if tok == "" {
		return "", "", "", fmt.Errorf("the link has no project token")
	}
	return base, tok, pass, nil
}

type remoteCospend struct {
	Name         string `json:"name"`
	CurrencyName string `json:"currencyname"`
	Members      []struct {
		ID        int64           `json:"id"`
		Name      string          `json:"name"`
		Weight    float64         `json:"weight"`
		Activated bool            `json:"activated"`
		Color     json.RawMessage `json:"color"`
	} `json:"members"`
	Categories   map[string]remoteList `json:"categories"`
	PaymentModes map[string]remoteList `json:"paymentmodes"`
	Currencies   []struct {
		Name         string  `json:"name"`
		ExchangeRate float64 `json:"exchange_rate"`
	} `json:"currencies"`
}

type remoteList struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
	OldID string `json:"old_id"`
}

type remoteBill struct {
	ID            int64   `json:"id"`
	What          string  `json:"what"`
	Comment       string  `json:"comment"`
	PayerID       int64   `json:"payer_id"`
	Timestamp     int64   `json:"timestamp"`
	Date          string  `json:"date"`
	Amount        float64 `json:"amount"`
	Repeat        string  `json:"repeat"`
	CategoryID    int64   `json:"categoryid"`
	PaymentMode   string  `json:"paymentmode"`
	PaymentModeID int64   `json:"paymentmodeid"`
	Owers         []struct {
		ID int64 `json:"id"`
	} `json:"owers"`
}

// Cospend's old built-in categories, to this app's.
var cospendOldCategories = map[int64]string{-1: "groceries", -2: "food", -3: "lodging", -4: "fees", -5: "activities", -10: "shopping", -12: "food", -13: "lodging", -14: "transport", -15: "activities", -11: store.ReimbursementKey}

func (s *Server) cospendGet(ctx context.Context, u string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the Cospend server: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return fmt.Errorf("the Cospend server refused the token or password")
	case res.StatusCode == http.StatusNotFound:
		return fmt.Errorf("no Cospend project at that link")
	case res.StatusCode >= 300:
		return fmt.Errorf("the Cospend server answered HTTP %d", res.StatusCode)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("the answer was not Cospend's: %v", err)
	}
	return nil
}

// CospendImportResult says what an import did.
type CospendImportResult struct {
	TripID  int64  `json:"trip_id"`
	Name    string `json:"name"`
	Created bool   `json:"created"` // a new trip was made for it
	Members int    `json:"members"`
	Bills   int    `json:"bills"`
	New     int    `json:"new_bills"`
}

// handleCospendImport answers POST /api/v1/import/cospend
// {link, token?, password?, trip_id?}: into that trip, or a new one.
func (s *Server) handleCospendImport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var in struct {
		Link     string `json:"link"`
		Token    string `json:"token"`
		Password string `json:"password"`
		TripID   int64  `json:"trip_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	base, tok, pass, err := parseCospendLink(in.Link, in.Token, in.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.importCospend(r.Context(), u, base, tok, pass, in.TripID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) importCospend(ctx context.Context, u *store.User, base, tok, pass string, tripID int64) (*CospendImportResult, error) {
	api := fmt.Sprintf("%s/api/projects/%s/%s", base, url.PathEscape(tok), url.PathEscape(pass))
	var proj remoteCospend
	if err := s.cospendGet(ctx, api, &proj); err != nil {
		return nil, err
	}
	var bills struct {
		Bills []remoteBill `json:"bills"`
	}
	if err := s.cospendGet(ctx, fmt.Sprintf("%s/apiv2/projects/%s/%s/bills?lastchanged=0", base, url.PathEscape(tok), url.PathEscape(pass)), &bills); err != nil {
		return nil, err
	}
	cur := strings.ToUpper(strings.TrimSpace(proj.CurrencyName))
	if !currency.Valid(cur) {
		cur = ""
	}
	res := &CospendImportResult{Name: proj.Name}

	// the trip: the one picked (that the person may edit), or a new one spanning the bills
	var t *store.Trip
	if tripID > 0 {
		role := s.store.TripRole(ctx, tripID, u)
		if role != "owner" && role != "editor" {
			return nil, fmt.Errorf("you cannot change that trip")
		}
		var err error
		if t, err = s.store.GetTrip(ctx, tripID, u.ID); err != nil {
			return nil, err
		}
	} else {
		first, last := "", ""
		for _, b := range bills.Bills {
			d := cospendBillDate(b)
			if first == "" || d < first {
				first = d
			}
			if d > last {
				last = d
			}
		}
		st, _ := s.store.GetSettings(ctx, u.ID)
		t = &store.Trip{OwnerID: u.ID, Name: proj.Name, StartDate: first, EndDate: last, Status: store.StatusCompleted, Currency: st.HomeCurrency, Dawarich: true, Immich: true, ExternalID: "cospend:" + tok}
		if cur != "" {
			t.Currency = cur
		}
		if t.Name == "" {
			t.Name = "Cospend " + tok
		}
		if last >= store.Today() {
			t.Status = store.StatusPlanning
		}
		if err := s.store.CreateTrip(ctx, t); err != nil {
			return nil, err
		}
		res.Created = true
	}
	res.TripID = t.ID
	if err := s.store.EnsureBillSetup(ctx, t.ID); err != nil {
		return nil, err
	}
	if cur == "" {
		cur = t.Currency
	}

	// members → trip people (by name; the trip owner's book learns new names)
	people, _ := s.store.ListTripPeople(ctx, t.ID)
	memberMap := map[int64]int64{}
	for _, m := range proj.Members {
		var tp *store.TripPerson
		for i := range people {
			if strings.EqualFold(people[i].Name, m.Name) {
				tp = &people[i]
				break
			}
		}
		// you, under your Cospend name: match the owner when the names agree on the first word
		if tp == nil {
			for i := range people {
				if people[i].Role == "owner" && strings.EqualFold(firstWord(people[i].Name), firstWord(m.Name)) {
					tp = &people[i]
					break
				}
			}
		}
		if tp == nil {
			p, err := s.store.PersonByName(ctx, t.OwnerID, m.Name)
			if err != nil {
				return nil, err
			}
			np := &store.TripPerson{TripID: t.ID, PersonID: &p.ID, Name: p.Name, Color: rgbColor(m.Color), Weight: m.Weight, Active: m.Activated}
			if np.Weight <= 0 {
				np.Weight = 1
			}
			if err := s.store.AddTripPerson(ctx, np); err != nil {
				return nil, err
			}
			tp = np
			people = append(people, *np)
		}
		memberMap[m.ID] = tp.ID
		res.Members++
	}

	// categories and payment methods: by Cospend's old ids, by name, or added
	cats, _ := s.store.ListTripCategories(ctx, t.ID)
	catMap := map[int64]string{}
	for idStr, c := range proj.Categories {
		id, _ := strconv.ParseInt(idStr, 10, 64)
		if c.ID != 0 {
			id = c.ID
		}
		key := ""
		for _, mine := range cats {
			if strings.EqualFold(mine.Name, c.Name) || strings.EqualFold(mine.Key, c.Name) {
				key = mine.Key
			}
		}
		if key == "" {
			nc := &store.TripCategory{TripID: t.ID, Name: c.Name, Icon: c.Icon, Color: c.Color}
			if err := s.store.SaveTripCategory(ctx, nc); err != nil {
				return nil, err
			}
			cats = append(cats, *nc)
			key = nc.Key
		}
		catMap[id] = key
	}
	pms, _ := s.store.ListTripPaymentModes(ctx, t.ID)
	pmMap := map[int64]int64{}
	pmOld := map[string]int64{}
	for _, m := range pms {
		if m.OldID != "" {
			pmOld[m.OldID] = m.ID
		}
	}
	for idStr, m := range proj.PaymentModes {
		id, _ := strconv.ParseInt(idStr, 10, 64)
		if m.ID != 0 {
			id = m.ID
		}
		var mine int64
		for _, x := range pms {
			if strings.EqualFold(x.Name, m.Name) || (m.OldID != "" && x.OldID == m.OldID) {
				mine = x.ID
			}
		}
		if mine == 0 {
			nm := &store.TripPaymentMode{TripID: t.ID, Name: m.Name, Icon: m.Icon, Color: m.Color, OldID: m.OldID}
			if err := s.store.SaveTripPaymentMode(ctx, nm); err != nil {
				return nil, err
			}
			pms = append(pms, *nm)
			mine = nm.ID
		}
		pmMap[id] = mine
	}
	if existing, _ := s.store.ListTripCurrencies(ctx, t.ID); len(existing) == 0 {
		for _, c := range proj.Currencies {
			_ = s.store.SaveTripCurrency(ctx, &store.TripCurrency{TripID: t.ID, Name: c.Name, ExchangeRate: c.ExchangeRate})
		}
	}

	// bills
	sort.Slice(bills.Bills, func(i, j int) bool { return bills.Bills[i].Timestamp < bills.Bills[j].Timestamp })
	dec := math.Pow10(currency.Decimals(cur))
	for _, b := range bills.Bills {
		payer, ok := memberMap[b.PayerID]
		if !ok {
			continue
		}
		owers := []int64{}
		for _, o := range b.Owers {
			if id, ok := memberMap[o.ID]; ok {
				owers = append(owers, id)
			}
		}
		if len(owers) == 0 {
			continue
		}
		date := cospendBillDate(b)
		amount := int64(math.Round(b.Amount * dec))
		rate := 1.0
		if cur != t.Currency {
			rate, _ = s.rate(ctx, cur, t.Currency, date)
		}
		cat := catMap[b.CategoryID]
		if cat == "" {
			cat = cospendOldCategories[b.CategoryID]
		}
		if cat == "" {
			cat = "other"
		}
		pm := pmMap[b.PaymentModeID]
		if pm == 0 && b.PaymentMode != "" {
			pm = pmOld[b.PaymentMode]
		}
		billTime := ""
		if b.Timestamp > 0 {
			if tt := time.Unix(b.Timestamp, 0).UTC(); tt.Hour() != 12 || tt.Minute() != 0 {
				billTime = tt.Format("15:04")
			}
		}
		e := &store.Expense{TripID: t.ID, Name: strings.TrimSpace(b.What), Category: cat, Amount: amount, Currency: cur, AmountHome: currency.Convert(amount, cur, t.Currency, rate), FXRate: rate,
			Date: date, Kind: "actual", Source: cospendSource, ExternalID: fmt.Sprintf("%s:%d", tok, b.ID), Notes: b.Comment,
			PayerID: &payer, Owers: owers, PaymentModeID: pm, BillTime: billTime, Repeat: "n", RepeatFreq: 1}
		if e.Name == "" {
			e.Name = "?"
		}
		created, err := s.upsertImportedBill(ctx, e)
		if err != nil {
			return nil, err
		}
		res.Bills++
		if created {
			res.New++
		}
	}
	s.store.LogActivity(ctx, t.ID, u.ID, "imported from Cospend", fmt.Sprintf("%d bills", res.Bills))
	return res, nil
}

// upsertImportedBill adds an imported bill, or updates the one imported before.
func (s *Server) upsertImportedBill(ctx context.Context, e *store.Expense) (bool, error) {
	var id int64
	err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM expenses WHERE trip_id = ? AND source = ? AND external_id = ?`, e.TripID, e.Source, e.ExternalID).Scan(&id)
	if err == nil {
		old, err := s.store.GetExpense(ctx, e.TripID, id)
		if err != nil {
			return false, err
		}
		e.ID, e.Deleted = id, old.Deleted
		return false, s.store.UpdateExpense(ctx, e)
	}
	return true, s.store.CreateExpense(ctx, e)
}

func cospendBillDate(b remoteBill) string {
	if b.Timestamp > 0 {
		return time.Unix(b.Timestamp, 0).UTC().Format("2006-01-02")
	}
	if validDate(b.Date) && b.Date != "" {
		return b.Date
	}
	return store.Today()
}

// rgbColor turns Cospend's {"r":…,"g":…,"b":…} (or "#rrggbb") into "#rrggbb".
func rgbColor(raw json.RawMessage) string {
	var c cospendColor
	if json.Unmarshal(raw, &c) == nil && (c.R|c.G|c.B) != 0 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return "#" + strings.TrimPrefix(s, "#")
	}
	return ""
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return s
}
