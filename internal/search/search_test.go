package search

import (
	"testing"
	"time"
)

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Açores":           "acores",
		"São Miguel":       "sao miguel",
		"Đakovo Šibenik":   "dakovo sibenik",
		"Kraków Łódź":      "krakow lodz",
		"Ærøskøbing":       "aeroskobing",
		"Straße":           "strasse",
		"Čačak Žabljak":    "cacak zabljak",
		"plain ascii TEXT": "plain ascii text",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	q := Parse("Açores Summer 2025 july")
	if len(q.Words) != 1 || q.Words[0] != "acores" {
		t.Fatalf("words = %v", q.Words)
	}
	if len(q.Years) != 1 || q.Years[0] != 2025 {
		t.Fatalf("years = %v", q.Years)
	}
	if len(q.Seasons) != 1 || q.Seasons[0] != Summer {
		t.Fatalf("seasons = %v", q.Seasons)
	}
	if len(q.Months) != 1 || q.Months[0] != time.July {
		t.Fatalf("months = %v", q.Months)
	}
	if q := Parse("jesen"); len(q.Seasons) != 1 || q.Seasons[0] != Autumn {
		t.Fatalf("jesen → %v", q.Seasons)
	}
	if q := Parse("fall"); len(q.Seasons) != 1 || q.Seasons[0] != Autumn {
		t.Fatalf("fall → %v", q.Seasons)
	}
	if q := Parse("may"); len(q.Months) != 1 || q.Months[0] != time.May {
		t.Fatalf("may → %v", q.Months)
	}
}

func TestMatchDates(t *testing.T) {
	cases := []struct {
		q          string
		start, end string
		want       bool
	}{
		{"2025", "2025-07-01", "2025-07-10", true},
		{"2025", "2024-12-28", "2025-01-03", true}, // overlaps
		{"2024", "2025-07-01", "2025-07-10", false},
		{"july", "2023-07-01", "2023-07-10", true},
		{"july", "2023-06-01", "2023-06-30", false},
		{"july 2025", "2024-07-01", "2024-07-10", false},
		{"summer", "2022-08-20", "2022-08-25", true},
		{"summer 2025", "2025-06-30", "2025-07-02", true},
		{"summer 2025", "2024-07-01", "2024-07-10", false},
		{"winter 2025", "2026-01-10", "2026-01-15", true}, // Jan 2026 is winter 2025
		{"winter 2025", "2025-01-10", "2025-01-15", false},
		{"winter 2025", "2025-12-20", "2025-12-27", true},
		{"autumn", "2025-05-01", "2025-05-03", false},
		{"2024 2025", "2024-03-01", "2024-03-02", true},
		{"anything", "", "", true}, // no date terms
		{"2025", "", "", false},
	}
	for _, c := range cases {
		if got := Parse(c.q).MatchDates(c.start, c.end); got != c.want {
			t.Errorf("%q on %s..%s = %v, want %v", c.q, c.start, c.end, got, c.want)
		}
	}
}
