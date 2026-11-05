package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

func TestPeople(t *testing.T) {
	h := newIsoHarness(t)
	trip := func(name, start, end string) int64 {
		return h.create(h.ownerTok, "/api/v1/trips", map[string]any{"name": name, "start_date": start, "end_date": end, "currency": "EUR"})
	}
	rome := trip("Roma 2019", "2019-03-17", "2019-03-20")
	malta := trip("Malta 2025", "2025-08-26", "2025-09-02")
	sofia := trip("Sofia 2025", "2025-05-01", "2025-05-04")

	people := func(tr int64) []store.TripPerson {
		t.Helper()
		_, b := h.do(h.ownerTok, http.MethodGet, fmt.Sprintf("/api/v1/trips/%d/people", tr), nil)
		var out []store.TripPerson
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%s", b)
		}
		return out
	}
	// the owner is on every new trip
	if p := people(rome); len(p) != 1 || p[0].Role != "owner" || p[0].PersonID == nil {
		t.Fatalf("new trip people = %+v", p)
	}
	// a new name lands in the address book and can come along next time
	ana := h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/people", rome), map[string]any{"name": "Ana"})
	if ana == 0 {
		t.Fatal("no member id")
	}
	var book []store.Person
	_, b := h.do(h.ownerTok, http.MethodGet, "/api/v1/people", nil)
	_ = json.Unmarshal(b, &book)
	if len(book) != 2 || !book[0].IsSelf || book[1].Name != "Ana" || book[1].Trips != 1 {
		t.Fatalf("book = %s", b)
	}
	anaID := book[1].ID
	h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/people", malta), map[string]any{"person_id": anaID})
	// adding the same name again reuses the person
	h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/people", malta), map[string]any{"name": "ana"})
	if p := people(malta); len(p) != 2 {
		t.Fatalf("malta people = %+v", p)
	}
	// Marko has an account: linking gives him access to the trips he is on
	marko := h.create(h.ownerTok, "/api/v1/people", map[string]any{"name": "Marko", "user_id": h.member.ID})
	h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/people", malta), map[string]any{"person_id": marko})
	if code, _ := h.do(h.memberTok, http.MethodGet, fmt.Sprintf("/api/v1/trips/%d", malta), nil); code != http.StatusOK {
		t.Fatalf("member has no access to malta: %d", code)
	}
	if code, _ := h.do(h.memberTok, http.MethodGet, fmt.Sprintf("/api/v1/trips/%d", rome), nil); code == http.StatusOK {
		t.Fatal("member sees rome")
	}

	// travelling with: all of them
	list := func(q string) []int64 {
		t.Helper()
		_, b := h.do(h.ownerTok, http.MethodGet, "/api/v1/trips"+q, nil)
		var trips []struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(b, &trips)
		ids := []int64{}
		for _, x := range trips {
			ids = append(ids, x.ID)
		}
		return ids
	}
	if ids := list(fmt.Sprintf("?with=%d", anaID)); len(ids) != 2 {
		t.Fatalf("with ana = %v", ids)
	}
	if ids := list(fmt.Sprintf("?with=%d,%d", anaID, marko)); len(ids) != 1 || ids[0] != malta {
		t.Fatalf("with ana+marko = %v", ids)
	}
	if ids := list(""); len(ids) != 3 {
		t.Fatalf("all = %v", ids)
	}
	// search: "with ana"
	_, b = h.do(h.ownerTok, http.MethodGet, "/api/v1/search?q=with+ana+2025", nil)
	var sr struct {
		Trips []struct{ ID int64 } `json:"trips"`
	}
	_ = json.Unmarshal(b, &sr)
	if len(sr.Trips) != 1 || sr.Trips[0].ID != malta {
		t.Fatalf("search with ana 2025 = %s", b)
	}
	// the person page
	_, b = h.do(h.ownerTok, http.MethodGet, fmt.Sprintf("/api/v1/people/%d", anaID), nil)
	var pp struct {
		Trips  []PersonTrip   `json:"trips"`
		Totals map[string]int `json:"totals"`
	}
	_ = json.Unmarshal(b, &pp)
	if len(pp.Trips) != 2 || pp.Totals["days"] != 4+8 {
		t.Fatalf("ana's page = %s", b)
	}
	// renaming carries over to the trips
	h.do(h.ownerTok, http.MethodPut, fmt.Sprintf("/api/v1/people/%d", anaID), map[string]any{"name": "Ana K."})
	for _, p := range people(rome) {
		if p.PersonID != nil && *p.PersonID == anaID && p.Name != "Ana K." {
			t.Fatalf("rename not carried: %+v", p)
		}
	}
	// merge a duplicate into Ana
	dup := h.create(h.ownerTok, fmt.Sprintf("/api/v1/trips/%d/people", sofia), map[string]any{"name": "Anka"})
	_ = dup
	_, b = h.do(h.ownerTok, http.MethodGet, "/api/v1/people", nil)
	_ = json.Unmarshal(b, &book)
	var ankaID int64
	for _, p := range book {
		if p.Name == "Anka" {
			ankaID = p.ID
		}
	}
	if code, b := h.do(h.ownerTok, http.MethodPost, fmt.Sprintf("/api/v1/people/%d/merge", ankaID), map[string]any{"into": anaID}); code != http.StatusOK {
		t.Fatalf("merge: %d %s", code, b)
	}
	if ids := list(fmt.Sprintf("?with=%d", anaID)); len(ids) != 3 {
		t.Fatalf("after merge with ana = %v", ids)
	}
	// the owner cannot be removed or merged away
	self := book[0]
	if code, _ := h.do(h.ownerTok, http.MethodDelete, fmt.Sprintf("/api/v1/people/%d", self.ID), nil); code != http.StatusBadRequest {
		t.Fatalf("deleted self: %d", code)
	}
	owner := people(rome)[0]
	if code, _ := h.do(h.ownerTok, http.MethodDelete, fmt.Sprintf("/api/v1/trips/%d/people/%d", rome, owner.ID), nil); code != http.StatusBadRequest {
		t.Fatalf("removed owner: %d", code)
	}
	// someone else's book is private
	if code, _ := h.do(h.memberTok, http.MethodGet, fmt.Sprintf("/api/v1/people/%d", anaID), nil); code != http.StatusNotFound {
		t.Fatalf("member read owner's person: %d", code)
	}
}
