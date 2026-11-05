import { useEffect, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { GeoResult } from '../lib/types'
import { GeoSearch } from './GeoSearch'
import { MapView } from './Map'
import { Icon } from './icons'

/** via: search = picked from the search box (its name and address describe the place); pin = map click, GPS or typed coordinates (the address is only a nearby guess). */
export interface PickedLocation { lat: number | null; lng: number | null; via: 'search' | 'pin'; result?: GeoResult; address?: string }

/** "41.38570, 2.17950" → [41.3857, 2.1795]; null when it is not a coordinate pair. */
export function parseCoords(s: string): [number, number] | null {
  const m = s.trim().match(/^(-?\d{1,2}(?:[.,]\d+)?)\s*[,;\s]\s*(-?\d{1,3}(?:[.,]\d+)?)$/)
  if (!m) return null
  const lat = Number(m[1].replace(',', '.')), lng = Number(m[2].replace(',', '.'))
  if (!isFinite(lat) || !isFinite(lng) || Math.abs(lat) > 90 || Math.abs(lng) > 180) return null
  return [lat, lng]
}

const fmt = (lat: number | null, lng: number | null) => lat != null && lng != null ? `${lat.toFixed(6)}, ${lng.toFixed(6)}` : ''

/**
 * Where something is, without needing an address: search OpenStreetMap, paste a
 * Google/Apple Maps link, type GPS coordinates, click the map, or use the
 * phone's current position. Many rentals have no findable address — the pin is
 * what counts. onChange gets the address only when one was looked up.
 */
export function LocationPicker({ lat, lng, onChange, center, icon = '📍', searchPlaceholder = 'Search an address or place, or paste a maps link…', lookupAddress = true }: {
  lat: number | null; lng: number | null; onChange: (p: PickedLocation) => void; center?: [number, number]; icon?: string; searchPlaceholder?: string; lookupAddress?: boolean
}) {
  const [coords, setCoords] = useState(fmt(lat, lng))
  const [bad, setBad] = useState(false)
  const [locating, setLocating] = useState(false)
  const typing = useRef(false)
  useEffect(() => { if (!typing.current) { setCoords(fmt(lat, lng)); setBad(false) } }, [lat, lng])
  const drop = async (la: number, ln: number) => {
    onChange({ lat: la, lng: ln, via: 'pin' })
    if (!lookupAddress) return
    try { const r = await api.get<GeoResult>(`${V1}/geo/reverse?lat=${la}&lng=${ln}`); onChange({ lat: la, lng: ln, via: 'pin', result: r, address: r.display }) } catch { /* the pin is enough */ }
  }
  const typed = (v: string) => {
    setCoords(v)
    if (!v.trim()) { setBad(false); return }
    const c = parseCoords(v)
    setBad(!c)
    if (c) onChange({ lat: c[0], lng: c[1], via: 'pin' })
  }
  const here = () => {
    if (!navigator.geolocation) return
    setLocating(true)
    navigator.geolocation.getCurrentPosition(p => { setLocating(false); drop(p.coords.latitude, p.coords.longitude) }, () => setLocating(false), { enableHighAccuracy: true, timeout: 15000 })
  }
  const marker = lat != null && lng != null ? [{ id: 'x', lat, lng, label: '', icon }] : []
  return (
    <div className="grid" style={{ gap: 8 }}>
      <GeoSearch onPick={r => onChange({ lat: r.lat, lng: r.lng, via: 'search', result: r, address: r.display })} placeholder={searchPlaceholder} />
      <div className="row" style={{ gap: 8 }}>
        <div className="search grow"><Icon name="pin" size={15} /><input value={coords} onChange={e => typed(e.target.value)} onFocus={() => { typing.current = true }} onBlur={() => { typing.current = false; if (!bad) setCoords(fmt(lat, lng)) }} placeholder="GPS: 38.71667, -9.13333" aria-label="GPS coordinates" aria-invalid={bad} inputMode="decimal" /></div>
        {'geolocation' in navigator && <button type="button" className="btn sm" onClick={here} disabled={locating} title="Use where I am now"><Icon name="navigation" size={14} />{locating ? 'Locating…' : 'Here'}</button>}
        {lat != null && <button type="button" className="btn ghost sm" onClick={() => onChange({ lat: null, lng: null, via: 'pin' })}>Clear</button>}
      </div>
      {bad && <div className="small bad">Type latitude, longitude — e.g. 38.71667, -9.13333</div>}
      <MapView className="sm" markers={marker} center={lat != null ? [lat, lng!] : center} zoom={lat != null ? 16 : 10} onClick={drop} fitKey={`${lat}:${lng}`} cluster={false} />
      <div className="small muted">Click the map to drop the pin exactly where it is.</div>
    </div>
  )
}
