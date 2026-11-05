package server

import (
	"context"
	"strconv"

	"github.com/abjelosevic88/pocket-homelab-travel/internal/store"
)

// OnThisDay is a past trip that was running on today's date in an earlier year.
type OnThisDay struct {
	TripID    int64    `json:"trip_id"`
	TripName  string   `json:"trip_name"`
	Color     string   `json:"color"`
	HasCover  bool     `json:"has_cover"`
	UpdatedAt string   `json:"updated_at"`
	Date      string   `json:"date"` // that day, YYYY-MM-DD
	Year      int      `json:"year"`
	YearsAgo  int      `json:"years_ago"`
	DayNumber int      `json:"day_number"` // 1-based
	Days      int      `json:"days"`
	Stay      string   `json:"stay"`      // where you slept that night
	Places    []string `json:"places"`    // what was on the plan that day
	Countries []string `json:"countries"` // where it was
}

// onThisDay finds every earlier trip that covered today's month and day.
func (s *Server) onThisDay(ctx context.Context, trips []store.Trip, today string) []OnThisDay {
	out := []OnThisDay{}
	if len(today) < 10 {
		return out
	}
	year, _ := strconv.Atoi(today[:4])
	md := today[4:] // "-MM-DD"
	for _, t := range trips {
		if t.Archived || t.Status == store.StatusCancelled || len(t.StartDate) < 10 || len(t.EndDate) < 10 {
			continue
		}
		y0, _ := strconv.Atoi(t.StartDate[:4])
		y1, _ := strconv.Atoi(t.EndDate[:4])
		for y := y0; y <= y1 && y < year; y++ {
			day := strconv.Itoa(y) + md
			if day < t.StartDate || day > t.EndDate {
				continue
			}
			if _, err := store.ParseDate(day); err != nil { // 29 February
				continue
			}
			o := OnThisDay{TripID: t.ID, TripName: t.Name, Color: t.Color, HasCover: t.HasCover, UpdatedAt: t.UpdatedAt, Date: day, Year: y, YearsAgo: year - y,
				DayNumber: store.DaysBetween(t.StartDate, day), Days: t.Days, Places: []string{}, Countries: []string{}}
			seen := map[string]bool{}
			addCountry := func(c string) {
				if c != "" && !seen[c] {
					seen[c] = true
					o.Countries = append(o.Countries, c)
				}
			}
			if stays, err := s.store.ListLodging(ctx, t.ID); err == nil {
				for _, l := range stays {
					in, outD := dateOnly(l.CheckIn), dateOnly(l.CheckOut)
					if in != "" && in <= day && (day < outD || (outD == "" && in == day)) {
						o.Stay = l.Name
						addCountry(l.CountryCode)
					}
				}
			}
			if places, err := s.store.ListPlaces(ctx, t.ID); err == nil {
				for _, p := range places {
					if p.DayDate == day && p.Status != "skipped" {
						o.Places = append(o.Places, p.Name)
						addCountry(p.CountryCode)
					}
				}
			}
			if len(o.Countries) == 0 {
				for _, c := range t.Countries {
					addCountry(c.Code)
				}
			}
			out = append(out, o)
		}
	}
	return out
}
