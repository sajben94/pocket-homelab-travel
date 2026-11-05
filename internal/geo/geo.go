// Package geo is the keyless outside world: place search and reverse
// geocoding (Nominatim), nearby points of interest (Overpass), a short
// description (Wikipedia), the forecast (Open-Meteo), and the maths for
// distances and route simplification.
package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client makes the outbound requests with a proper User-Agent.
type Client struct {
	HTTP *http.Client
	UA   string
}

// New creates a client.
func New(ua string, timeout time.Duration) *Client {
	return &Client{HTTP: &http.Client{Timeout: timeout}, UA: ua}
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", res.StatusCode, req.URL.Host)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Result is a geocoded place.
type Result struct {
	Name        string  `json:"name"`
	Display     string  `json:"display"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	Type        string  `json:"type"`
	Category    string  `json:"category"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"` // ISO 3166-1 alpha-2, upper case
	City        string  `json:"city"`
	Website     string  `json:"website,omitempty"`
	Phone       string  `json:"phone,omitempty"`
	Hours       string  `json:"hours,omitempty"`
	Source      string  `json:"source"`
	OSMID       string  `json:"osm_id,omitempty"`
	Suggest     string  `json:"suggest,omitempty"` // our place category guessed from the OSM tags
}

// Search runs a free-text Nominatim search.
func (c *Client) Search(ctx context.Context, q string, limit int) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []Result{}, nil
	}
	if limit <= 0 {
		limit = 8
	}
	var raw []struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		Type        string `json:"type"`
		Class       string `json:"category"` // jsonv2 calls the OSM class "category"
		OSMType     string `json:"osm_type"`
		OSMID       int64  `json:"osm_id"`
		Address     struct {
			CountryCode string `json:"country_code"`
			Country     string `json:"country"`
			City        string `json:"city"`
			Town        string `json:"town"`
			Village     string `json:"village"`
			Road        string `json:"road"`
			HouseNumber string `json:"house_number"`
		} `json:"address"`
	}
	u := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=jsonv2&addressdetails=1&limit=%d", url.QueryEscape(q), limit)
	if err := c.getJSON(ctx, u, &raw); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(raw))
	for _, r := range raw {
		var lat, lng float64
		fmt.Sscanf(r.Lat, "%f", &lat)
		fmt.Sscanf(r.Lon, "%f", &lng)
		name := prettyName(r.Name, r.Address.Road, r.Address.HouseNumber, r.DisplayName)
		city := r.Address.City
		if city == "" {
			city = r.Address.Town
		}
		if city == "" {
			city = r.Address.Village
		}
		out = append(out, Result{Name: name, Display: r.DisplayName, Lat: lat, Lng: lng, Type: r.Type, Category: r.Class, Suggest: SuggestCategory(r.Class, r.Type), Country: r.Address.Country, CountryCode: strings.ToUpper(r.Address.CountryCode), City: city, Source: "nominatim", OSMID: fmt.Sprintf("%s/%d", r.OSMType, r.OSMID)})
	}
	return out, nil
}

// Reverse turns coordinates into an address.
func (c *Client) Reverse(ctx context.Context, lat, lng float64) (*Result, error) {
	var raw struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Type        string `json:"type"`
		Class       string `json:"category"`
		Address     struct {
			CountryCode string `json:"country_code"`
			Country     string `json:"country"`
			City        string `json:"city"`
			Town        string `json:"town"`
			Village     string `json:"village"`
			Road        string `json:"road"`
			HouseNumber string `json:"house_number"`
		} `json:"address"`
	}
	u := fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?lat=%f&lon=%f&format=jsonv2&addressdetails=1", lat, lng)
	if err := c.getJSON(ctx, u, &raw); err != nil {
		return nil, err
	}
	city := raw.Address.City
	if city == "" {
		city = raw.Address.Town
	}
	if city == "" {
		city = raw.Address.Village
	}
	return &Result{Name: prettyName(raw.Name, raw.Address.Road, raw.Address.HouseNumber, raw.DisplayName), Display: raw.DisplayName, Lat: lat, Lng: lng, Type: raw.Type, Category: raw.Class, Suggest: SuggestCategory(raw.Class, raw.Type), Country: raw.Address.Country, CountryCode: strings.ToUpper(raw.Address.CountryCode), City: city, Source: "nominatim"}, nil
}

// Discover categories map to Overpass tag filters.
var discoverFilters = map[string]string{
	"sight":     `nwr["tourism"~"attraction|viewpoint|artwork"]`,
	"museum":    `nwr["tourism"~"museum|gallery"]`,
	"nature":    `nwr["natural"~"beach|peak|waterfall|cave_entrance|spring"]`,
	"beach":     `nwr["natural"="beach"]`,
	"viewpoint": `nwr["tourism"="viewpoint"]`,
	"historic":  `nwr["historic"]["name"]`,
	"food":      `nwr["amenity"="restaurant"]`,
	"cafe":      `nwr["amenity"~"cafe|ice_cream"]`,
	"bar":       `nwr["amenity"~"bar|pub|biergarten"]`,
	"shop":      `nwr["shop"~"supermarket|mall|bakery|convenience"]`,
	"lodging":   `nwr["tourism"~"hotel|guest_house|hostel|apartment|camp_site"]`,
	"wellness":  `nwr["leisure"~"spa|sauna|swimming_pool"]`,
	"activity":  `nwr["leisure"~"park|marina|water_park|nature_reserve"]`,
	"transport": `nwr["amenity"~"fuel|charging_station|parking|bus_station|ferry_terminal"]`,
}

// DiscoverCategories lists what Discover can look for.
func DiscoverCategories() []string {
	return []string{"sight", "museum", "historic", "nature", "beach", "viewpoint", "food", "cafe", "bar", "shop", "lodging", "wellness", "activity", "transport"}
}

// Discover lists OpenStreetMap points of interest around a point.
func (c *Client) Discover(ctx context.Context, lat, lng float64, radiusM int, category string) ([]Result, error) {
	f, ok := discoverFilters[category]
	if !ok {
		return nil, errors.New("unknown discover category")
	}
	if radiusM <= 0 || radiusM > 20000 {
		radiusM = 3000
	}
	ql := fmt.Sprintf(`[out:json][timeout:20];(%s(around:%d,%f,%f););out center tags 60;`, f, radiusM, lat, lng)
	var raw struct {
		Elements []struct {
			Type   string                      `json:"type"`
			ID     int64                       `json:"id"`
			Lat    float64                     `json:"lat"`
			Lon    float64                     `json:"lon"`
			Center *struct{ Lat, Lon float64 } `json:"center"`
			Tags   map[string]string           `json:"tags"`
		} `json:"elements"`
	}
	u := "https://overpass-api.de/api/interpreter?data=" + url.QueryEscape(ql)
	if err := c.getJSON(ctx, u, &raw); err != nil {
		return nil, err
	}
	out := []Result{}
	for _, e := range raw.Elements {
		name := e.Tags["name:en"]
		if name == "" {
			name = e.Tags["name"]
		}
		if name == "" {
			continue
		}
		la, lo := e.Lat, e.Lon
		if e.Center != nil {
			la, lo = e.Center.Lat, e.Center.Lon
		}
		kind := e.Tags["tourism"]
		for _, k := range []string{"amenity", "historic", "natural", "leisure", "shop"} {
			if kind == "" {
				kind = e.Tags[k]
			}
		}
		addr := strings.TrimSpace(strings.Join(filterEmpty(e.Tags["addr:street"]+" "+e.Tags["addr:housenumber"], e.Tags["addr:city"]), ", "))
		r := Result{Name: name, Display: addr, Lat: la, Lng: lo, Type: kind, Category: category, Website: firstOf(e.Tags["website"], e.Tags["contact:website"], e.Tags["url"]), Phone: firstOf(e.Tags["phone"], e.Tags["contact:phone"]), Hours: e.Tags["opening_hours"], Source: "osm", OSMID: fmt.Sprintf("%s/%d", e.Type, e.ID)}
		r.Display = fmt.Sprintf("%.1f km", Haversine(lat, lng, la, lo)/1000)
		if addr != "" {
			r.Display += " · " + addr
		}
		out = append(out, r)
	}
	// Nearest first.
	sortByDistance(out, lat, lng)
	return out, nil
}

func filterEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstOf(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func sortByDistance(rs []Result, lat, lng float64) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && Haversine(lat, lng, rs[j].Lat, rs[j].Lng) < Haversine(lat, lng, rs[j-1].Lat, rs[j-1].Lng); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// Wiki is a Wikipedia summary.
type Wiki struct {
	Title   string `json:"title"`
	Extract string `json:"extract"`
	URL     string `json:"url"`
	Thumb   string `json:"thumbnail,omitempty"`
}

// Wikipedia fetches the summary for a title (tries a search when the direct page is missing).
func (c *Client) Wikipedia(ctx context.Context, q, lang string) (*Wiki, error) {
	if lang == "" {
		lang = "en"
	}
	title := strings.TrimSpace(q)
	if title == "" {
		return nil, errors.New("empty query")
	}
	var raw struct {
		Title     string `json:"title"`
		Extract   string `json:"extract"`
		Type      string `json:"type"`
		Thumbnail *struct {
			Source string `json:"source"`
		} `json:"thumbnail"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}
	try := func(t string) error {
		return c.getJSON(ctx, fmt.Sprintf("https://%s.wikipedia.org/api/rest_v1/page/summary/%s", lang, url.PathEscape(strings.ReplaceAll(t, " ", "_"))), &raw)
	}
	if err := try(title); err != nil || raw.Type == "disambiguation" || raw.Extract == "" {
		// Fall back to the search API for the best title.
		var sr struct {
			Query struct {
				Search []struct {
					Title string `json:"title"`
				} `json:"search"`
			} `json:"query"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("https://%s.wikipedia.org/w/api.php?action=query&list=search&format=json&srlimit=1&srsearch=%s", lang, url.QueryEscape(title)), &sr); err != nil {
			return nil, err
		}
		if len(sr.Query.Search) == 0 {
			return nil, errors.New("nothing found on Wikipedia")
		}
		if err := try(sr.Query.Search[0].Title); err != nil {
			return nil, err
		}
	}
	w := &Wiki{Title: raw.Title, Extract: raw.Extract, URL: raw.ContentURLs.Desktop.Page}
	if raw.Thumbnail != nil {
		w.Thumb = raw.Thumbnail.Source
	}
	return w, nil
}

// DayWeather is one day's forecast or record.
type DayWeather struct {
	Date    string  `json:"date"`
	Code    int     `json:"code"`
	Label   string  `json:"label"`
	TMax    float64 `json:"tmax"`
	TMin    float64 `json:"tmin"`
	Precip  float64 `json:"precip_mm"`
	Wind    float64 `json:"wind_kmh"`
	Sunrise string  `json:"sunrise,omitempty"`
	Sunset  string  `json:"sunset,omitempty"`
	Source  string  `json:"source"` // forecast | archive
}

// WeatherLabel turns a WMO code into words.
func WeatherLabel(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code <= 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code <= 49:
		return "Fog"
	case code <= 59:
		return "Drizzle"
	case code <= 69:
		return "Rain"
	case code <= 79:
		return "Snow"
	case code <= 84:
		return "Showers"
	case code <= 94:
		return "Snow showers"
	default:
		return "Thunderstorm"
	}
}

// Weather returns daily weather for a date range: Open-Meteo's forecast for
// the next 16 days, the archive for the past. Dates outside both are skipped.
func (c *Client) Weather(ctx context.Context, lat, lng float64, start, end string) ([]DayWeather, error) {
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return nil, err
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return nil, err
	}
	if e.Before(s) {
		return nil, errors.New("end before start")
	}
	if e.Sub(s) > 120*24*time.Hour {
		e = s.Add(120 * 24 * time.Hour)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	horizon := today.Add(15 * 24 * time.Hour)
	archiveEnd := today.Add(-6 * 24 * time.Hour) // the archive lags a few days
	var out []DayWeather
	daily := "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,wind_speed_10m_max,sunrise,sunset"
	fetch := func(u, source string) error {
		var raw struct {
			Daily struct {
				Time    []string   `json:"time"`
				Code    []*int     `json:"weather_code"`
				TMax    []*float64 `json:"temperature_2m_max"`
				TMin    []*float64 `json:"temperature_2m_min"`
				Precip  []*float64 `json:"precipitation_sum"`
				Wind    []*float64 `json:"wind_speed_10m_max"`
				Sunrise []string   `json:"sunrise"`
				Sunset  []string   `json:"sunset"`
			} `json:"daily"`
		}
		if err := c.getJSON(ctx, u, &raw); err != nil {
			return err
		}
		for i, d := range raw.Daily.Time {
			if i >= len(raw.Daily.Code) || raw.Daily.Code[i] == nil || raw.Daily.TMax[i] == nil {
				continue
			}
			w := DayWeather{Date: d, Code: *raw.Daily.Code[i], TMax: *raw.Daily.TMax[i], Source: source}
			w.Label = WeatherLabel(w.Code)
			if raw.Daily.TMin[i] != nil {
				w.TMin = *raw.Daily.TMin[i]
			}
			if i < len(raw.Daily.Precip) && raw.Daily.Precip[i] != nil {
				w.Precip = *raw.Daily.Precip[i]
			}
			if i < len(raw.Daily.Wind) && raw.Daily.Wind[i] != nil {
				w.Wind = *raw.Daily.Wind[i]
			}
			if i < len(raw.Daily.Sunrise) {
				w.Sunrise = raw.Daily.Sunrise[i]
			}
			if i < len(raw.Daily.Sunset) {
				w.Sunset = raw.Daily.Sunset[i]
			}
			out = append(out, w)
		}
		return nil
	}
	// Archive part.
	if as, ae := s, minT(e, archiveEnd); !ae.Before(as) {
		u := fmt.Sprintf("https://archive-api.open-meteo.com/v1/archive?latitude=%f&longitude=%f&start_date=%s&end_date=%s&daily=%s&timezone=auto", lat, lng, as.Format("2006-01-02"), ae.Format("2006-01-02"), daily)
		if err := fetch(u, "archive"); err != nil {
			return nil, err
		}
	}
	// Forecast part (covers the last few days too).
	if fs, fe := maxT(s, archiveEnd.Add(24*time.Hour)), minT(e, horizon); !fe.Before(fs) {
		u := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&start_date=%s&end_date=%s&daily=%s&timezone=auto", lat, lng, fs.Format("2006-01-02"), fe.Format("2006-01-02"), daily)
		if err := fetch(u, "forecast"); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []DayWeather{}
	}
	return out, nil
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxT(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// Haversine returns metres between two coordinates.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp := (lat2 - lat1) * math.Pi / 180
	dl := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// Pt is a coordinate for simplification.
type Pt struct {
	Lat, Lng float64
	TS       int64
}

// Simplify runs Douglas–Peucker with a tolerance in metres (approximate planar).
func Simplify(pts []Pt, toleranceM float64) []Pt {
	if len(pts) <= 2 {
		return pts
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	type seg struct{ a, b int }
	stack := []seg{{0, len(pts) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		maxD, idx := 0.0, -1
		for i := s.a + 1; i < s.b; i++ {
			if d := perpDist(pts[i], pts[s.a], pts[s.b]); d > maxD {
				maxD, idx = d, i
			}
		}
		if idx >= 0 && maxD > toleranceM {
			keep[idx] = true
			stack = append(stack, seg{s.a, idx}, seg{idx, s.b})
		}
	}
	out := make([]Pt, 0, len(pts)/4)
	for i, k := range keep {
		if k {
			out = append(out, pts[i])
		}
	}
	return out
}

func perpDist(p, a, b Pt) float64 {
	// Planar approximation in metres around the segment's latitude.
	k := math.Cos((a.Lat + b.Lat) / 2 * math.Pi / 180)
	ax, ay := a.Lng*k*111320, a.Lat*110540
	bx, by := b.Lng*k*111320, b.Lat*110540
	px, py := p.Lng*k*111320, p.Lat*110540
	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// PathLength sums the distance along a path in metres.
func PathLength(pts []Pt) float64 {
	var d float64
	for i := 1; i < len(pts); i++ {
		d += Haversine(pts[i-1].Lat, pts[i-1].Lng, pts[i].Lat, pts[i].Lng)
	}
	return d
}

// prettyName turns Nominatim's name (often just a house number for address
// hits) into something readable: "Zmaj Ognjenog Vuka 238" rather than "238".
func prettyName(name, road, house, display string) string {
	n := strings.TrimSpace(name)
	isNumber := n != "" && strings.Trim(n, "0123456789abcdefABCDEF/- ") == ""
	if n == "" || isNumber || n == house {
		if road != "" {
			if house != "" {
				return road + " " + house
			}
			return road
		}
		if n != "" {
			return n
		}
		return strings.TrimSpace(strings.SplitN(display, ",", 2)[0])
	}
	return n
}

// SuggestCategory maps OSM class/type to one of the app's place categories.
func SuggestCategory(class, typ string) string {
	switch class {
	case "amenity":
		switch typ {
		case "restaurant", "fast_food", "food_court":
			return "food"
		case "cafe", "ice_cream":
			return "cafe"
		case "bar", "pub", "biergarten", "nightclub":
			if typ == "nightclub" {
				return "nightlife"
			}
			return "bar"
		case "theatre", "cinema", "arts_centre":
			return "event"
		case "marketplace":
			return "shop"
		case "fuel", "parking", "bus_station", "ferry_terminal", "charging_station", "taxi":
			return "transport"
		case "place_of_worship":
			return "sight"
		}
	case "tourism":
		switch typ {
		case "museum", "gallery":
			return "museum"
		case "viewpoint":
			return "viewpoint"
		case "attraction", "artwork", "theme_park", "zoo", "aquarium":
			if typ == "theme_park" || typ == "zoo" || typ == "aquarium" {
				return "activity"
			}
			return "sight"
		case "hotel", "guest_house", "hostel", "apartment", "camp_site":
			return "other"
		}
	case "natural":
		if typ == "beach" {
			return "beach"
		}
		if typ == "peak" {
			return "viewpoint"
		}
		return "nature"
	case "leisure":
		switch typ {
		case "park", "nature_reserve", "garden":
			return "nature"
		case "spa", "sauna", "swimming_pool":
			return "wellness"
		}
		return "activity"
	case "historic":
		return "sight"
	case "shop":
		return "shop"
	case "aeroway", "railway", "highway":
		return "transport"
	}
	return ""
}
