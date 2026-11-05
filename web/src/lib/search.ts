import { useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { SearchResult } from './types'

const SPECIAL: Record<string, string> = { đ: 'd', ł: 'l', ø: 'o', æ: 'ae', œ: 'oe', ß: 'ss', ı: 'i', þ: 'th', ð: 'd', ħ: 'h' }

/** Lowercase without diacritics: "Açores Đakovo" → "acores dakovo" (same as the server's search.Fold). */
export function fold(s: string): string {
  return s.toLowerCase().replace(/[đłøæœßıþðħ]/g, c => SPECIAL[c] || c).normalize('NFD').replace(/\p{M}/gu, '')
}

/** Every word of q occurs in text, ignoring case and accents. */
export function matches(q: string, text: string): boolean {
  const t = fold(text)
  return fold(q).split(/\s+/).filter(Boolean).every(w => t.includes(w))
}

/** Server-side search across trips, places and stays (debounced). */
export function useSearch(q: string, ms = 200): { result: SearchResult | null; busy: boolean } {
  const [result, setResult] = useState<SearchResult | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const term = q.trim()
    if (!term) { setResult(null); setBusy(false); return }
    let live = true
    setBusy(true)
    const t = setTimeout(() => {
      api.get<SearchResult>(`${V1}/search?q=${encodeURIComponent(term)}`)
        .then(r => { if (live) setResult(r) })
        .catch(() => { if (live) setResult(null) })
        .finally(() => { if (live) setBusy(false) })
    }, ms)
    return () => { live = false; clearTimeout(t) }
  }, [q, ms])
  return { result, busy }
}

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

/** "summer 2025 · July" — what the date words of a search were understood as. */
export function describeDates(r: SearchResult | null): string {
  if (!r) return ''
  const q = r.query
  const parts = [...(q.seasons || []), ...(q.months || []).map(m => MONTHS[m - 1]), ...(q.years || []).map(String)]
  return parts.join(' · ')
}

/** Where a search hit opens: the place or stay inside its trip. */
export function hitLink(h: { kind: string; trip_id: number; id: number }): string {
  return h.kind === 'lodging' ? `/trips/${h.trip_id}/stay?open=lodging:${h.id}` : `/trips/${h.trip_id}/places?open=place:${h.id}`
}
