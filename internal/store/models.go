package store

// User is an account that can sign in. The first user is the admin; admins
// manage other users and the shared settings. Trips are shared with members.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Trip statuses.
const (
	StatusPlanning  = "planning"
	StatusBooked    = "booked"
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// ValidStatus reports whether s is a trip status.
func ValidStatus(s string) bool {
	switch s {
	case StatusPlanning, StatusBooked, StatusActive, StatusCompleted, StatusCancelled:
		return true
	}
	return false
}

// Trip is one journey.
type Trip struct {
	ID          int64    `json:"id"`
	OwnerID     int64    `json:"owner_id"`
	Name        string   `json:"name"`
	Destination string   `json:"destination"`
	Description string   `json:"description"`
	StartDate   string   `json:"start_date"`
	EndDate     string   `json:"end_date"`
	Status      string   `json:"status"`
	Currency    string   `json:"currency"`
	Budget      int64    `json:"budget"`
	Color       string   `json:"color"`
	CoverPath   string   `json:"-"`
	HasCover    bool     `json:"has_cover"`
	Link        string   `json:"link"`
	Notes       string   `json:"notes"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	Timezone    string   `json:"timezone"`
	FireflyTag  string   `json:"firefly_tag"`
	Dawarich    bool     `json:"dawarich"`
	Immich      bool     `json:"immich"`
	Archived    bool     `json:"archived"`
	ExternalID  string   `json:"external_id"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	CountryCode string   `json:"country_code"` // of the destination's coordinates (detected)
	// CountriesManual is the person's own list of the trip's countries; empty
	// means the detected ones are used.
	CountriesManual []string `json:"countries_manual"`

	// Derived
	Days    int          `json:"days"`
	Role    string       `json:"role,omitempty"` // the requesting user's role
	Members []TripMember `json:"members,omitempty"`
	Counts  *TripCounts  `json:"counts,omitempty"`
	// Countries of the trip with days (lists and search fill it)
	Countries []CountryDays `json:"countries,omitempty"`
}

// TripMember is a user with access to a trip.
type TripMember struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

// TripCounts summarises a trip for list cards.
type TripCounts struct {
	Places    int   `json:"places"`
	Visited   int   `json:"visited"`
	Lodging   int   `json:"lodging"`
	Transport int   `json:"transport"`
	Documents int   `json:"documents"`
	Journal   int   `json:"journal"`
	Checklist int   `json:"checklist_items"`
	Checked   int   `json:"checklist_done"`
	Planned   int64 `json:"planned"` // minor units, trip currency
	Actual    int64 `json:"actual"`
	Total     int64 `json:"total"` // actual + unpaid booked stays/transport
}

// Place categories.
var PlaceCategories = []string{"sight", "museum", "nature", "beach", "viewpoint", "food", "cafe", "bar", "shop", "activity", "event", "nightlife", "wellness", "transport", "other"}

// Place is somewhere to go.
type Place struct {
	ID          int64    `json:"id"`
	TripID      int64    `json:"trip_id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Address     string   `json:"address"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	Link        string   `json:"link"`
	Phone       string   `json:"phone"`
	Rating      *float64 `json:"rating"`
	Price       int64    `json:"price"`
	Currency    string   `json:"currency"`
	DayDate     string   `json:"day_date"`
	StartTime   string   `json:"start_time"`
	DurationMin int      `json:"duration_min"`
	Status      string   `json:"status"`
	VisitedAt   string   `json:"visited_at"`
	BookingRef  string   `json:"booking_ref"`
	Tags        string   `json:"tags"`
	Notes       string   `json:"notes"`
	ImagePath   string   `json:"-"`
	HasImage    bool     `json:"has_image"`
	SortOrder   int      `json:"sort_order"`
	ExternalID  string   `json:"external_id"`
	CountryCode string   `json:"country_code"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`

	Photos []PlacePhoto `json:"photos"`
}

// Lodging is where you sleep.
type Lodging struct {
	ID                int64    `json:"id"`
	TripID            int64    `json:"trip_id"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	Address           string   `json:"address"`
	Lat               *float64 `json:"lat"`
	Lng               *float64 `json:"lng"`
	CheckIn           string   `json:"check_in"`
	CheckOut          string   `json:"check_out"`
	ReservationNumber string   `json:"reservation_number"`
	Link              string   `json:"link"`
	Phone             string   `json:"phone"`
	Price             int64    `json:"price"`
	Currency          string   `json:"currency"`
	Paid              bool     `json:"paid"`
	Rating            *float64 `json:"rating"`
	Notes             string   `json:"notes"`
	ImagePath         string   `json:"-"`
	HasImage          bool     `json:"has_image"`
	ExternalID        string   `json:"external_id"`
	CountryCode       string   `json:"country_code"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

// Transport is how you get from A to B.
type Transport struct {
	ID         int64    `json:"id"`
	TripID     int64    `json:"trip_id"`
	Type       string   `json:"type"`
	Name       string   `json:"name"`
	Carrier    string   `json:"carrier"`
	Number     string   `json:"number"`
	FromName   string   `json:"from_name"`
	FromLat    *float64 `json:"from_lat"`
	FromLng    *float64 `json:"from_lng"`
	ToName     string   `json:"to_name"`
	ToLat      *float64 `json:"to_lat"`
	ToLng      *float64 `json:"to_lng"`
	DepartAt   string   `json:"depart_at"`
	ArriveAt   string   `json:"arrive_at"`
	BookingRef string   `json:"booking_ref"`
	Seat       string   `json:"seat"`
	Link       string   `json:"link"`
	Price      int64    `json:"price"`
	Currency   string   `json:"currency"`
	Paid       bool     `json:"paid"`
	DistanceKm float64  `json:"distance_km"`
	Notes      string   `json:"notes"`
	ExternalID string   `json:"external_id"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

// Expense categories.
var ExpenseCategories = []string{"transport", "lodging", "food", "activities", "shopping", "fees", "fuel", "groceries", "insurance", "other"}

// Expense is money planned or spent on a trip.
type Expense struct {
	ID         int64   `json:"id"`
	TripID     int64   `json:"trip_id"`
	Name       string  `json:"name"`
	Category   string  `json:"category"`
	Amount     int64   `json:"amount"`
	Currency   string  `json:"currency"`
	AmountHome int64   `json:"amount_home"`
	FXRate     float64 `json:"fx_rate"`
	Date       string  `json:"date"`
	Kind       string  `json:"kind"`   // planned | actual
	Source     string  `json:"source"` // manual | item | money | firefly (synced: money = Pocket Money transaction id, firefly = Firefly III journal id)
	ExternalID string  `json:"external_id"`
	LinkKind   string  `json:"link_kind"`
	LinkID     int64   `json:"link_id"`
	Notes      string  `json:"notes"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`

	// A spent cost is a bill (Cospend): who paid, for whom (trip people ids),
	// how; in the trash when deleted; updated_ts for Cospend sync.
	PayerID         *int64  `json:"payer_id"`
	Owers           []int64 `json:"owers"`
	PaymentModeID   int64   `json:"payment_mode_id"`
	BillTime        string  `json:"bill_time"` // HH:MM, "" = no time
	Deleted         bool    `json:"deleted"`
	UpdatedTS       int64   `json:"updated_ts"`
	Repeat          string  `json:"repeat"` // n | d | w | b | s | m | y
	RepeatAllActive bool    `json:"repeat_all_active"`
	RepeatUntil     string  `json:"repeat_until"`
	RepeatFreq      int     `json:"repeat_freq"`
	// ParentID: this bill is the extra part of another (uneven split)
	ParentID *int64 `json:"parent_id"`
}

// IsBill reports whether an expense is a bill (spent, not just planned).
func (e *Expense) IsBill() bool { return e.Kind == "actual" }

// Checklist groups items (packing, to-do before leaving, shopping…).
type Checklist struct {
	ID        int64           `json:"id"`
	TripID    int64           `json:"trip_id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	SortOrder int             `json:"sort_order"`
	CreatedAt string          `json:"created_at"`
	Items     []ChecklistItem `json:"items"`
}

// ChecklistItem is one line of a checklist.
type ChecklistItem struct {
	ID          int64  `json:"id"`
	ChecklistID int64  `json:"checklist_id"`
	Text        string `json:"text"`
	Done        bool   `json:"done"`
	AssigneeID  *int64 `json:"assignee_id"`
	SortOrder   int    `json:"sort_order"`
	CreatedAt   string `json:"created_at"`
}

// JournalEntry is a diary note written during (or after) the trip.
type JournalEntry struct {
	ID        int64    `json:"id"`
	TripID    int64    `json:"trip_id"`
	UserID    int64    `json:"user_id"`
	UserName  string   `json:"user_name"`
	Date      string   `json:"date"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Mood      string   `json:"mood"`
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	TripName  string   `json:"trip_name,omitempty"`
}

// Document is an uploaded file.
type Document struct {
	ID        int64  `json:"id"`
	TripID    int64  `json:"trip_id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Filename  string `json:"-"`
	Mime      string `json:"mime"`
	Bytes     int64  `json:"bytes"`
	Kind      string `json:"kind"`
	LinkKind  string `json:"link_kind"`
	LinkID    int64  `json:"link_id"`
	CreatedAt string `json:"created_at"`
}

// Activity is one line of the trip's change log.
type Activity struct {
	ID        int64  `json:"id"`
	TripID    int64  `json:"trip_id"`
	UserID    *int64 `json:"user_id"`
	UserName  string `json:"user_name"`
	Action    string `json:"action"`
	Subject   string `json:"subject"`
	CreatedAt string `json:"created_at"`
}

// Settings are preferences stored as key/value pairs, shared by everyone.
type Settings struct {
	AIProvider    string `json:"ai_provider"`
	AIPocketModel string `json:"ai_pocket_model"`
	Title         string `json:"title"`
	HomeCurrency  string `json:"home_currency"`
	DateFormat    string `json:"date_format"`
	TimeFormat    string `json:"time_format"`
	DistanceUnit  string `json:"distance_unit"` // km | mi
	FXAuto        bool   `json:"fx_auto"`
	// Pocket Money (or Firefly III): how trip costs are read. MoneyMode is
	// core | direct | off ("" = direct with a saved token, else core). The
	// firefly_* keys hold the Direct connection: any Firefly-compatible API,
	// e.g. <Money>/firefly or a real Firefly III.
	MoneyMode    string `json:"money_mode"`
	FireflyURL   string `json:"firefly_url"`
	FireflyToken string `json:"firefly_token"`
	// How Travel reaches the person's Dawarich and Immich: core (address +
	// their own key from Pocket Core) | direct (the URL + key below) | off;
	// "" = direct while a key is saved, else core when Travel is connected
	// to Core, else direct.
	DawarichMode    string `json:"dawarich_mode"`
	ImmichMode      string `json:"immich_mode"`
	DawarichURL     string `json:"dawarich_url"`
	DawarichAPIKey  string `json:"dawarich_api_key"`
	ImmichURL       string `json:"immich_url"`
	ImmichAPIKey    string `json:"immich_api_key"`
	ImmichPublicURL string `json:"immich_public_url"` // what the browser opens; defaults to the API URL
	// PhotoPrism (direct only): URL, app password, what the browser opens.
	PhotoPrismURL       string `json:"photoprism_url"`
	PhotoPrismToken     string `json:"photoprism_token"`
	PhotoPrismPublicURL string `json:"photoprism_public_url"`
	// Home, where trips start and end (finding trips in photos); unset = detected.
	HomeName string   `json:"home_name"`
	HomeLat  *float64 `json:"home_lat"`
	HomeLng  *float64 `json:"home_lng"`
	// Where trip routes, the timeline, distances and countries come from:
	// movement (Pocket Movement, through Pocket Core only) | dawarich; "" =
	// Pocket Movement while Travel is connected to Core and the link is on,
	// else Dawarich.
	LocationSource string `json:"location_source"`
	// Pocket Places on the trip page (through Pocket Core only): core | off;
	// "" = core.
	PlacesMode    string `json:"places_mode"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"` // any OpenAI-compatible endpoint
	MapTiles      string `json:"map_tiles"`       // osm | carto | topo
	AutoPhotos    bool   `json:"auto_photos"`     // look up Wikipedia/Commons pictures for new places
	CheckInTime   string `json:"checkin_time"`    // HH:MM a new stay starts with (15:00)
	CheckOutTime  string `json:"checkout_time"`   // HH:MM a new stay ends with (11:00)
	SetupComplete bool   `json:"setup_complete"`
}
