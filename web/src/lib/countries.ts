// Country codes (ISO 3166-1 alpha-2) to flags and names in the browser's language.

/** "PT" → 🇵🇹 */
export function flag(code: string): string {
  if (!/^[A-Za-z]{2}$/.test(code)) return '🏳️'
  return String.fromCodePoint(...code.toUpperCase().split('').map(c => 0x1f1e6 + c.charCodeAt(0) - 65))
}

let names: Intl.DisplayNames | null = null
/** "PT" → "Portugal" */
export function countryName(code: string): string {
  try { names ??= new Intl.DisplayNames(undefined, { type: 'region' }); return names.of(code.toUpperCase()) || code } catch { return code }
}

/** Every country code the browser can name, sorted by name — for pickers. */
export function allCountries(): { code: string; name: string }[] {
  const out: { code: string; name: string }[] = []
  for (let a = 65; a <= 90; a++) for (let b = 65; b <= 90; b++) {
    const code = String.fromCharCode(a, b)
    const n = countryName(code)
    if (n && n !== code && !/^[A-Z]{2}$/.test(n)) out.push({ code, name: n })
  }
  return out.sort((x, y) => x.name.localeCompare(y.name))
}

/** "UTC+1" for a time zone right now. */
export function utcOffset(tz: string, at = new Date()): string {
  try {
    const p = new Intl.DateTimeFormat('en-US', { timeZone: tz, timeZoneName: 'shortOffset' }).formatToParts(at).find(x => x.type === 'timeZoneName')?.value || ''
    return p.replace('GMT', 'UTC') === 'UTC' ? 'UTC±0' : p.replace('GMT', 'UTC')
  } catch { return '' }
}

/** Minutes east of UTC for a time zone right now. */
function offsetMinutes(tz: string, at = new Date()): number {
  const m = utcOffset(tz, at).match(/UTC([+-])(\d+)(?::(\d+))?/)
  return m ? (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3] || 0)) : 0
}

/** The local time and offset(s) of a country: "21:45 · UTC+1", or "UTC−10 to UTC−4" for several zones. */
export function countryClock(zones: string[], at = new Date()): { time: string; offset: string } | null {
  if (!zones.length) return null
  const offs = [...new Set(zones.map(z => offsetMinutes(z, at)))].sort((a, b) => a - b)
  const fmt = (m: number) => m === 0 ? 'UTC±0' : `UTC${m < 0 ? '−' : '+'}${Math.floor(Math.abs(m) / 60)}${Math.abs(m) % 60 ? ':' + String(Math.abs(m) % 60).padStart(2, '0') : ''}`
  let time = ''
  try { time = new Intl.DateTimeFormat(undefined, { timeZone: zones[0], hour: '2-digit', minute: '2-digit' }).format(at) } catch { /* unknown zone */ }
  return { time: offs.length === 1 ? time : '', offset: offs.length === 1 ? fmt(offs[0]) : `${fmt(offs[0])} to ${fmt(offs[offs.length - 1])}` }
}

/** 58915656 → "58.9 million" */
export function bigNumber(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(n >= 1e10 ? 0 : 1)} billion`
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e7 ? 0 : 1)} million`
  return n.toLocaleString()
}
