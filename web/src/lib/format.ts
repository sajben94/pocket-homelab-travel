import type { Currency } from './types'

/** Decimals for a currency code; 2 when the list is not loaded yet. */
export function decimalsOf(code: string, currencies: Currency[]): number {
  const c = currencies.find(x => x.code === code)
  return c ? c.decimals : 2
}

const fmtCache = new Map<string, Intl.NumberFormat>()
function formatter(code: string, decimals: number, display: 'symbol' | 'code' = 'symbol'): Intl.NumberFormat {
  const key = `${code}:${decimals}:${display}`
  let f = fmtCache.get(key)
  if (!f) {
    try {
      f = new Intl.NumberFormat(undefined, { style: 'currency', currency: code, currencyDisplay: display === 'symbol' ? 'narrowSymbol' : 'code', minimumFractionDigits: decimals, maximumFractionDigits: decimals })
    } catch {
      f = new Intl.NumberFormat(undefined, { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
    }
    fmtCache.set(key, f)
  }
  return f
}

/** Minor units → "12.50 €", "¥1,500". Unknown codes fall back to "12.50 XYZ". */
export function fmtMoney(minor: number, code: string, decimals = 2, opts: { sign?: boolean; code?: boolean; compact?: boolean } = {}): string {
  const major = minor / Math.pow(10, decimals)
  let s: string
  try {
    if (opts.compact && Math.abs(major) >= 10000) {
      s = formatter(code, 0, opts.code ? 'code' : 'symbol').format(Math.round(Math.abs(major)))
    } else {
      s = formatter(code, decimals, opts.code ? 'code' : 'symbol').format(Math.abs(major))
    }
    if (!/[^\d.,\s ]/.test(s)) s = `${s} ${code}`
  } catch {
    s = `${Math.abs(major).toFixed(decimals)} ${code}`
  }
  if (minor < 0) return `−${s}`
  if (opts.sign && minor > 0) return `+${s}`
  return s
}

export function toMajor(minor: number, decimals: number): string { return (minor / Math.pow(10, decimals)).toFixed(decimals) }
export function toMinor(s: string, decimals: number): number | null {
  const t = s.replace(/\s/g, '').replace(',', '.')
  if (t === '' || t === '-' || t === '.') return null
  if (!/^-?\d*(\.\d*)?$/.test(t)) return null
  const n = Number(t)
  if (!Number.isFinite(n)) return null
  return Math.round(n * Math.pow(10, decimals))
}

export function todayISO(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
export function addDays(iso: string, n: number): string {
  const [y, m, d] = iso.split('-').map(Number)
  const t = new Date(y, m - 1, d + n)
  return `${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, '0')}-${String(t.getDate()).padStart(2, '0')}`
}
export function daysBetween(a: string, b: string): number {
  if (!a || !b) return 0
  const [y1, m1, d1] = a.split('-').map(Number), [y2, m2, d2] = b.split('-').map(Number)
  return Math.round((Date.UTC(y2, m2 - 1, d2) - Date.UTC(y1, m1 - 1, d1)) / 86400000)
}
/** Every date from a to b inclusive. */
export function dateRange(a: string, b: string): string[] {
  const out: string[] = []
  if (!a || !b || b < a) return out
  for (let d = a, i = 0; d <= b && i < 400; d = addDays(d, 1), i++) out.push(d)
  return out
}
export function weekday(iso: string, long = false): string {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { weekday: long ? 'long' : 'short' })
}

// ---- dates: the format comes from Settings → General and is set once by the app context.
export type DateFormat = 'auto' | 'dmy-dot' | 'dmy-slash' | 'mdy-slash' | 'ymd' | 'dmmmy' | 'mmmdy'
export type TimeFormat = 'auto' | '24h' | '12h'
let DATE_FMT: DateFormat = 'auto'
let TIME_FMT: TimeFormat = 'auto'
export function setDateFormats(d: DateFormat, t: TimeFormat) { DATE_FMT = d; TIME_FMT = t }
export const DATE_FORMATS: { id: DateFormat; label: string }[] = [
  { id: 'auto', label: 'Browser locale' }, { id: 'dmy-dot', label: '31.12.2026' }, { id: 'dmy-slash', label: '31/12/2026' },
  { id: 'mdy-slash', label: '12/31/2026' }, { id: 'ymd', label: '2026-12-31' }, { id: 'dmmmy', label: '31 Dec 2026' }, { id: 'mmmdy', label: 'Dec 31, 2026' },
]
export const TIME_FORMATS: { id: TimeFormat; label: string }[] = [{ id: 'auto', label: 'Browser locale' }, { id: '24h', label: '17:05' }, { id: '12h', label: '5:05 PM' }]
const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
export function fmtYMD(y: number, m: number, d: number, withYear = true, fmt: DateFormat = DATE_FMT): string {
  const dd = String(d).padStart(2, '0'), mm = String(m).padStart(2, '0')
  switch (fmt) {
    case 'dmy-dot': return withYear ? `${dd}.${mm}.${y}` : `${dd}.${mm}.`
    case 'dmy-slash': return withYear ? `${dd}/${mm}/${y}` : `${dd}/${mm}`
    case 'mdy-slash': return withYear ? `${mm}/${dd}/${y}` : `${mm}/${dd}`
    case 'ymd': return withYear ? `${y}-${mm}-${dd}` : `${mm}-${dd}`
    case 'dmmmy': return withYear ? `${d} ${MON[m - 1]} ${y}` : `${d} ${MON[m - 1]}`
    case 'mmmdy': return withYear ? `${MON[m - 1]} ${d}, ${y}` : `${MON[m - 1]} ${d}`
    default: return new Date(y, m - 1, d).toLocaleDateString(undefined, withYear ? { day: 'numeric', month: 'short', year: 'numeric' } : { day: 'numeric', month: 'short' })
  }
}
export function fmtDate(d: string, withYear = true): string {
  if (!d) return ''
  const [y, m, day] = d.slice(0, 10).split('-').map(Number)
  return fmtYMD(y, m, day, withYear)
}
/** "14 Aug – 28 Aug 2026" */
export function fmtRange(a: string, b: string): string {
  if (!a && !b) return 'no dates yet'
  if (!b || a === b) return fmtDate(a || b)
  const sameYear = a.slice(0, 4) === b.slice(0, 4)
  return `${fmtDate(a, !sameYear)} – ${fmtDate(b)}`
}
export function fmtTime(t: Date, fmt: TimeFormat = TIME_FMT): string {
  const h = t.getHours(), mi = String(t.getMinutes()).padStart(2, '0')
  switch (fmt) {
    case '24h': return `${String(h).padStart(2, '0')}:${mi}`
    case '12h': return `${h % 12 || 12}:${mi} ${h < 12 ? 'AM' : 'PM'}`
    default: return t.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  }
}
/** "HH:MM" wall time → formatted. */
export function fmtClock(hhmm: string): string {
  if (!hhmm || hhmm.length < 5) return ''
  const t = new Date(2000, 0, 1, Number(hhmm.slice(0, 2)), Number(hhmm.slice(3, 5)))
  return fmtTime(t)
}
/** "2026-08-28T07:30" (wall) or ISO → "28 Aug 2026 07:30". */
export function fmtDateTime(s: string, withYear = true): string {
  if (!s) return ''
  if (s.length === 16 && s[10] === 'T') return `${fmtDate(s, withYear)} ${fmtClock(s.slice(11))}`
  const t = new Date(s)
  if (Number.isNaN(t.getTime())) return s
  return `${fmtYMD(t.getFullYear(), t.getMonth() + 1, t.getDate(), withYear)} ${fmtTime(t)}`
}
export function fmtISOTime(iso: string): string {
  const t = new Date(iso)
  return Number.isNaN(t.getTime()) ? '' : fmtTime(t)
}
export function ago(iso: string): string {
  if (!iso) return 'never'
  const t = new Date(iso).getTime()
  const s = Math.max(0, (Date.now() - t) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  if (s < 86400 * 7) return `${Math.floor(s / 86400)} d ago`
  return fmtDateTime(iso)
}
export function fmtDuration(min: number): string {
  if (!min) return ''
  const h = Math.floor(min / 60), m = min % 60
  return h ? (m ? `${h} h ${m} min` : `${h} h`) : `${m} min`
}
export function fmtKm(km: number, unit: 'km' | 'mi' = 'km'): string {
  if (unit === 'mi') return `${(km * 0.621371).toFixed(km < 10 ? 1 : 0)} mi`
  return `${km.toFixed(km < 10 ? 1 : 0)} km`
}
export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
export function plural(n: number, one: string, many = one + 's'): string { return `${n.toLocaleString()} ${n === 1 ? one : many}` }
export function initials(name: string): string {
  const p = name.replace(/@.*/, '').split(/[\s._-]+/).filter(Boolean)
  return ((p[0]?.[0] || '?') + (p[1]?.[0] || '')).toUpperCase()
}
const palette = ['#0284c7', '#7c3aed', '#d97706', '#db2777', '#2563eb', '#16a34a', '#0d9488', '#ea580c']
export function colorFor(id: number): string { return palette[Math.abs(id) % palette.length] }
export const TRIP_COLORS = ['#0284c7', '#7c3aed', '#d97706', '#db2777', '#16a34a', '#0d9488', '#ea580c', '#dc2626', '#475569']
export function stars(r: number | null | undefined): string { return r ? '★'.repeat(Math.round(r)) + '☆'.repeat(5 - Math.round(r)) : '' }
export function cap(s: string): string { return s ? s[0].toUpperCase() + s.slice(1).replace(/[-_]/g, ' ') : '' }
export function gmaps(lat: number, lng: number): string { return `https://www.google.com/maps/dir/?api=1&destination=${lat},${lng}` }
export function appleMaps(lat: number, lng: number): string { return `https://maps.apple.com/?daddr=${lat},${lng}` }
export function osmLink(lat: number, lng: number): string { return `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}#map=16/${lat}/${lng}` }

/** Where a place sits within its day on the itinerary: timed places by their
 * time, untimed ones after them in the order they were dragged into. */
export function placeWhen(p: { start_time: string; sort_order: number }): string {
  return p.start_time || '99:' + String(p.sort_order).padStart(3, '0')
}

/** Itinerary order: by day, then as the day lists them (see placeWhen). */
export function itineraryOrder(a: { day_date: string; start_time: string; sort_order: number; id: number }, b: { day_date: string; start_time: string; sort_order: number; id: number }): number {
  return a.day_date.localeCompare(b.day_date) || placeWhen(a).localeCompare(placeWhen(b)) || a.id - b.id
}

/** The time part of a stay's check-in/out ("2026-07-05T15:00" → "15:00"), "" when only a day is stored. */
export function stayClock(v?: string): string {
  return v && v.length >= 16 && v[10] === 'T' ? v.slice(11, 16) : ''
}

/** A day plus an optional time, as stays store them: "2026-07-05" + "15:00" → "2026-07-05T15:00". */
export function withClock(day?: string, time?: string): string {
  const d = (day || '').slice(0, 10)
  return d && time ? `${d}T${time}` : d
}

/** When a stay's check-in or check-out happens: its own time, else the person's default. */
export function stayTime(v: string, which: 'in' | 'out', s?: { checkin_time?: string; checkout_time?: string } | null): string {
  return stayClock(v) || (which === 'in' ? s?.checkin_time || '15:00' : s?.checkout_time || '11:00')
}

/** A bill with the extra parts of its uneven split folded in. */
export type GroupedBill<E> = E & { total: number; total_home: number; parts: E[] }

/** Folds the extra parts of uneven splits into their bills (parts whose bill is not in the list stay on their own). */
export function groupBills<E extends { id: number; parent_id: number | null; amount: number; amount_home: number }>(list: E[]): GroupedBill<E>[] {
  const ids = new Set(list.map(e => e.id))
  const parts = new Map<number, E[]>()
  for (const e of list) if (e.parent_id != null && ids.has(e.parent_id)) parts.set(e.parent_id, [...(parts.get(e.parent_id) || []), e])
  return list.filter(e => e.parent_id == null || !ids.has(e.parent_id)).map(e => {
    const p = parts.get(e.id) || []
    return { ...e, parts: p, total: e.amount + p.reduce((n, x) => n + x.amount, 0), total_home: e.amount_home + p.reduce((n, x) => n + x.amount_home, 0) }
  })
}
