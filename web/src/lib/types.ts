export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }
export interface Currency { code: string; name: string; decimals: number }

export type TripStatus = 'planning' | 'booked' | 'active' | 'completed' | 'cancelled'
export interface TripMember { user_id: number; name: string; email: string; role: string }
export interface TripCounts { places: number; visited: number; lodging: number; transport: number; documents: number; journal: number; checklist_items: number; checklist_done: number; planned: number; actual: number; total: number }
export interface Trip {
  id: number; owner_id: number; name: string; destination: string; description: string
  start_date: string; end_date: string; status: TripStatus; currency: string; budget: number
  color: string; has_cover: boolean; link: string; notes: string; lat: number | null; lng: number | null; timezone: string
  firefly_tag: string; dawarich: boolean; immich: boolean; archived: boolean; external_id: string
  created_at: string; updated_at: string; days: number; role?: string; members?: TripMember[]; counts?: TripCounts
  country_code: string; countries_manual: string[]; countries?: CountryDays[]
}
export type PlaceStatus = 'idea' | 'planned' | 'booked' | 'visited' | 'skipped'
export interface PlacePhoto { id: number; place_id: number; source: 'upload' | 'import' | 'wikipedia' | 'commons'; credit: string; license: string; source_url: string; sort_order: number; created_at: string }
export interface Place {
  id: number; trip_id: number; name: string; category: string; description: string; address: string
  lat: number | null; lng: number | null; link: string; phone: string; rating: number | null; price: number; currency: string
  day_date: string; start_time: string; duration_min: number; status: PlaceStatus; visited_at: string; booking_ref: string
  tags: string; notes: string; has_image: boolean; sort_order: number; external_id: string; created_at: string; updated_at: string
  photos: PlacePhoto[]
}
export interface Lodging {
  id: number; trip_id: number; name: string; type: string; address: string; lat: number | null; lng: number | null
  check_in: string; check_out: string; reservation_number: string; link: string; phone: string; price: number; currency: string
  paid: boolean; rating: number | null; notes: string; has_image: boolean; external_id: string; created_at: string; updated_at: string
}
export interface Transport {
  id: number; trip_id: number; type: string; name: string; carrier: string; number: string
  from_name: string; from_lat: number | null; from_lng: number | null; to_name: string; to_lat: number | null; to_lng: number | null
  depart_at: string; arrive_at: string; booking_ref: string; seat: string; link: string; price: number; currency: string; paid: boolean
  distance_km: number; notes: string; external_id: string; created_at: string; updated_at: string
}
export interface Expense {
  id: number; trip_id: number; name: string; category: string; amount: number; currency: string; amount_home: number; fx_rate: number
  date: string; kind: 'planned' | 'actual'; source: 'manual' | 'money' | 'firefly' | 'item' | 'cospend'; external_id: string; link_kind: string; link_id: number
  notes: string; created_at: string; updated_at: string
  // a spent cost is a bill: who paid, for whom (trip people ids), how; trash; Cospend sync
  payer_id: number | null; owers: number[]; payment_mode_id: number; bill_time: string; deleted: boolean; updated_ts: number
  repeat: 'n' | 'd' | 'w' | 'b' | 's' | 'm' | 'y'; repeat_all_active: boolean; repeat_until: string; repeat_freq: number
  /** this bill is an extra part of another (uneven split) */
  parent_id: number | null
}
export interface TripCategory { id: number; trip_id: number; key: string; name: string; icon: string; color: string; order: number }
export interface TripPaymentMode { id: number; trip_id: number; name: string; icon: string; color: string; order: number; old_id: string }
export interface TripCurrency { id: number; trip_id: number; name: string; exchange_rate: number }
export interface Balance { member_id: number; name: string; paid: number; spent: number; sent: number; received: number; balance: number }
export interface Transfer { from: number; to: number; amount: number }
export interface TripShare { trip_id: number; token: string; password: string; access_level: number; label: string; created_at: string }
export interface SplitInfo {
  currency: string; people: TripPerson[]; balances: Balance[]; settlement: Transfer[]; center?: number
  categories: TripCategory[]; payment_modes: TripPaymentMode[]; currencies: TripCurrency[]
  bills: number; total: number; trash: number; share?: TripShare | null
  by_category: Record<string, number>; by_payment_mode: Record<string, number>
}
export interface ChecklistItem { id: number; checklist_id: number; text: string; done: boolean; assignee_id: number | null; sort_order: number; created_at: string }
export interface Checklist { id: number; trip_id: number; name: string; kind: string; sort_order: number; created_at: string; items: ChecklistItem[] }
export interface JournalEntry { id: number; trip_id: number; user_id: number; user_name: string; date: string; title: string; body: string; mood: string; lat: number | null; lng: number | null; created_at: string; updated_at: string }
export interface Doc { id: number; trip_id: number; user_id: number; name: string; mime: string; bytes: number; kind: string; link_kind: string; link_id: number; created_at: string }
export interface Activity { id: number; trip_id: number; user_id: number | null; user_name: string; action: string; subject: string; created_at: string }

export interface CategoryTotal { category: string; planned: number; actual: number }
export interface Budget {
  currency: string; budget: number; planned: number; actual: number; remaining: number; per_day: number; total: number; bookings: number
  by_category: CategoryTotal[]; by_day: { date: string; actual: number; count: number }[]; by_currency: Record<string, number>
  top: Expense[]; firefly_count: number; days: number; days_elapsed: number; sources: Record<string, number>
}
export interface Overview {
  trip: Trip; places: Place[]; lodging: Lodging[]; transport: Transport[]; expenses: Expense[]; checklists: Checklist[]
  journal: JournalEntry[]; documents: Doc[]; budget: Budget
  countries: CountryDays[]
  people: TripPerson[]
  split: SplitInfo | null
  day_costs: DayCost[]
  budget_plan: PlanLine[]
  integrations: { firefly: { configured: boolean; via: '' | 'core' | 'direct'; tag: string; synced_at: string }; dawarich: { configured: boolean; enabled: boolean; source?: LocationSource }; immich: { configured: boolean; enabled: boolean; url: string }; ai: { configured: boolean; model: string } }
}
export interface DayWeather { date: string; code: number; label: string; tmax: number; tmin: number; precip_mm: number; wind_kmh: number; sunrise?: string; sunset?: string; source: 'forecast' | 'archive' }
export interface RouteDay { date: string; km: number; segments: [number, number][][]; start?: string; end?: string; countries?: string[] }
export interface Route { points: [number, number, number][]; raw_count: number; distance_km: number; days_km: Record<string, number>; days: RouteDay[]; bounds?: [number, number, number, number]; source?: LocationSource; countries?: { code: string; days: number }[] }
export const DAY_COLORS = ['#0ea5e9', '#f97316', '#22c55e', '#a855f7', '#ef4444', '#eab308', '#14b8a6', '#ec4899', '#6366f1', '#84cc16', '#f43f5e', '#06b6d4', '#d946ef', '#10b981']
export interface TimelineEntry { type: 'journey' | 'visit'; track_id?: number; visit_id?: number; name?: string; status?: string; started_at: string; ended_at: string; duration: number; distance: number; dominant_mode?: string; avg_speed?: number; elevation_gain?: number; latitude?: number; longitude?: number; point_count?: number }
export interface TimelineDay { date: string; summary: { total_distance: number; distance_unit: string; places_visited: number; time_moving_minutes: number; time_stationary_minutes: number; mode_distances: Record<string, number> }; entries: TimelineEntry[] }
export interface Photo { id: string; type: 'IMAGE' | 'VIDEO'; file_name: string; taken_at: string; lat: number | null; lng: number | null; city: string; country: string; thumbhash: string; favorite: boolean; duration?: string }
export interface GeoResult { name: string; display: string; lat: number; lng: number; type: string; category: string; country: string; city: string; website?: string; phone?: string; hours?: string; source: string; osm_id?: string; suggest?: string }
export type ConnMode = 'core' | 'direct' | 'off'
/** Where the recorded route comes from: Pocket Movement (through Pocket Core only) or Dawarich. */
export type LocationSource = 'movement' | 'dawarich'
export interface Settings {
  ai_provider: string; ai_pocket_model: string; ai_status: { provider: string; selected: string; ready: boolean; detail: string }

  title: string; home_currency: string; date_format: string; time_format: string; distance_unit: 'km' | 'mi'; fx_auto: boolean; fx_provider: string; map_tiles: string; auto_photos: boolean; checkin_time: string; checkout_time: string
  core_configured: boolean
  /** connected to Pocket Core and registered: Core-only choices are offered only then */
  core_ready: boolean
  location_source: '' | LocationSource; location_effective: LocationSource; location_detail: string; movement_available: boolean
  money_mode: ConnMode; money_via: '' | 'core' | 'direct'; money_detail: string
  dawarich_mode: ConnMode; dawarich_via: '' | 'core' | 'direct'; dawarich_detail: string; dawarich_source: '' | 'personal' | 'box'
  immich_mode: ConnMode; immich_via: '' | 'core' | 'direct'; immich_detail: string; immich_source: '' | 'personal' | 'box'
  /** Pocket Places on the trip page: through Pocket Core only */
  places_mode: 'core' | 'off'; places_via: '' | 'core'; places_detail: string
  firefly_url: string; firefly_token: string; firefly_configured: boolean
  dawarich_url: string; dawarich_api_key: string; dawarich_configured: boolean
  immich_url: string; immich_public_url: string; immich_api_key: string; immich_configured: boolean
  openai_api_key: string; openai_model: string; openai_base_url: string; openai_configured: boolean; ai_configured: boolean
  max_upload_bytes: number; setup_complete: boolean; can_edit_global?: boolean
}
/** Pocket Places, the person's own list of places, as the trip page shows it. */
export interface PPPlace { id: number; name: string; kind: string; cuisine: string; city: string; status: 'want' | 'been'; favourite: boolean; rating: number; price: number; lat: number | null; lng: number | null; notes: string }
export interface PPList { id: number; name: string; emoji: string; count: number; been: number }
export interface PPVisit { id: number; place_id: number; date: string; time: string; planned: boolean; rating: number; with_whom: string; spent: number; currency: string; place_name: string; place_kind: string }
export interface PocketPlaces {
  /** ok, or why there is nothing: no_core (no Pocket Core), off (switched off in Settings), unavailable (Core refused the link), error (no answer) */
  status: 'ok' | 'no_core' | 'off' | 'unavailable' | 'error'; reason?: string; detail?: string
  places_url?: string; trip_url?: string
  lists: PPList[]; want: PPPlace[]; been: PPPlace[]; visits: PPVisit[]; spent: { currency: string; total: number; visits: number }[]
}
export interface Meta { place_categories: string[]; expense_categories: string[]; discover_categories: string[]; packing_templates: string[]; lodging_types: string[]; transport_types: string[] }
export interface HomeData {
  stats: { trips: number; upcoming: number; days: number; places: number; visited: number; destinations: string[]; next_trip_id: number }
  current: Trip | null; next: Trip | null; upcoming: Trip[]; recent: Trip[]
  today?: { date: string; day_number: number; places: Place[]; stay: Lodging | null; budget: Budget }
  recorded_km: number; photos: number; journal: (JournalEntry & { trip_name?: string })[]
  pins: { id: number; name: string; status: TripStatus; start_date: string; end_date: string; lat: number; lng: number; has_cover: boolean; stay?: string; stay_id?: number; stay_from?: string; stay_to?: string }[]
  hero_weather: DayWeather | null
  on_this_day: OnThisDay[]
}

export const PLACE_ICONS: Record<string, string> = { sight: '🏛', museum: '🖼', nature: '🌿', beach: '🏖', viewpoint: '🌄', food: '🍽', cafe: '☕', bar: '🍸', shop: '🛍', activity: '🎟', event: '🎭', nightlife: '🌙', wellness: '🧖', transport: '🚏', other: '📍', historic: '🏰', lodging: '🛏' }
export const TRANSPORT_ICONS: Record<string, string> = { flight: '✈️', train: '🚆', bus: '🚌', car: '🚗', ferry: '⛴', taxi: '🚕', bike: '🚲', walk: '🚶', other: '➡️' }
export const LODGING_ICONS: Record<string, string> = { hotel: '🏨', apartment: '🏢', hostel: '🛏', house: '🏡', camping: '⛺', friends: '🫶', other: '🛏' }
export const EXPENSE_ICONS: Record<string, string> = { transport: '🚗', lodging: '🛏', food: '🍽', activities: '🎟', shopping: '🛍', fees: '🏧', fuel: '⛽', groceries: '🛒', insurance: '🛡', other: '💶' }
export const STATUS_LABEL: Record<string, string> = { planning: 'Planning', booked: 'Booked', active: 'On the road', completed: 'Completed', cancelled: 'Cancelled' }
export const PLACE_STATUS_LABEL: Record<string, string> = { idea: 'Idea', planned: 'Planned', booked: 'Booked', visited: 'Visited', skipped: 'Skipped' }
export const WX_ICON = (code: number) => code === 0 ? '☀️' : code <= 2 ? '🌤' : code === 3 ? '☁️' : code <= 49 ? '🌫' : code <= 59 ? '🌦' : code <= 69 ? '🌧' : code <= 79 ? '🌨' : code <= 84 ? '🌦' : code <= 94 ? '🌨' : '⛈'

export interface SearchHit { kind: 'place' | 'lodging'; id: number; trip_id: number; trip_name: string; trip_start: string; trip_end: string; name: string; category: string; address: string; date: string; date_end: string; status: string }
export interface SearchTrip extends Trip { direct: boolean; hits: SearchHit[] }
export interface SearchResult { query: { words: string[] | null; years: number[] | null; months: number[] | null; seasons: string[] | null }; trips: SearchTrip[]; places: SearchHit[]; stays: SearchHit[] }

export interface CountryDays { code: string; days: number; first: string }
export interface StatsCountry { code: string; name: string; days: number; planned_days: number; trips: number[]; first: string; last: string }
export interface StatsTrip { id: number; name: string; color: string; status: TripStatus; start_date: string; end_date: string; days: number; countries: CountryDays[]; places: number; visited: number; upcoming: boolean }
export interface StatsYear { year: number; trips: number; days: number; countries: string[]; new_countries: string[] }
export interface Stats { countries: StatsCountry[]; trips: StatsTrip[]; years: StatsYear[]; totals: { trips: number; days: number; countries: number; places_visited: number; pending_lookups: number } }

export interface CountryFacts {
  code: string; name: string; official: string; capital: string[]; region: string; subregion: string
  population: number; population_year: number; area_km2: number; languages: string[]
  currencies: { code: string; name: string; symbol: string }[]; calling_code: string; tld: string; drives: '' | 'left' | 'right'
  timezones: string[]; borders: string[]; landlocked: boolean; latlng: number[]; demonym: string; independent: boolean
}
export interface CountryNote { code: string; facts: Record<string, string>; extra: { label: string; value: string }[]; notes: string; updated_at: string }
export interface CountryTrip { id: number; name: string; color: string; status: TripStatus; start_date: string; end_date: string; days: number; trip_days: number; others: string[]; has_cover: boolean; upcoming: boolean; updated_at: string }
export interface CountryPin { kind: 'place' | 'lodging'; id: number; trip_id: number; trip_name: string; name: string; category: string; status: string; lat: number; lng: number }
export interface CountryPage {
  country: CountryFacts; note: CountryNote
  trips?: CountryTrip[]; pins?: CountryPin[]; totals?: { days: number; planned_days: number; trips: number; first: string; last: string }
  wiki?: { title: string; extract: string; url: string; thumbnail?: string } | null
}

export interface Person { id: number; owner_id: number; name: string; color: string; user_id: number | null; email: string; notes: string; is_self: boolean; created_at: string; updated_at: string; trips: number; last_trip?: string }
export interface TripPerson { id: number; trip_id: number; person_id: number | null; name: string; weight: number; active: boolean; color: string; updated_ts: number; user_id: number | null; is_self: boolean; role: '' | 'owner' | 'editor' | 'viewer' }
export interface PersonTrip { id: number; name: string; color: string; status: TripStatus; start_date: string; end_date: string; days: number; has_cover: boolean; updated_at: string; upcoming: boolean; countries: CountryDays[]; others: string[] }
export interface PersonPage { person: Person; trips: PersonTrip[]; totals: { trips: number; upcoming: number; days: number; countries: number }; countries: CountryDays[]; company: { name: string; n: number }[] }

export interface DayCost { date: string; actual: Record<string, number>; planned: Record<string, number>; people: Record<string, Record<string, number>> }
export interface PlanLine { category: string; amount: number; per_day: boolean }
export interface OnThisDay { trip_id: number; trip_name: string; color: string; has_cover: boolean; updated_at: string; date: string; year: number; years_ago: number; day_number: number; days: number; stay: string; places: string[]; countries: string[] }
