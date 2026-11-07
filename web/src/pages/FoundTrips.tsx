import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, photoThumb, V1 } from '../lib/api'
import type { PhotoHome, PhotoTrip, Settings, Trip } from '../lib/types'
import { fmtRange, plural, todayISO } from '../lib/format'
import { flag, countryName } from '../lib/countries'
import { Icon } from '../components/icons'
import { Card, Modal, useToast } from '../components/ui'
import { GeoSearch } from '../components/GeoSearch'
import { useApp } from '../lib/app-context'

const SHOW = 4

/** Trips found in where and when the photos were taken (Immich / PhotoPrism), that no trip covers yet. */
export function FoundTrips({ onAdded }: { onAdded: () => void }) {
  const { settings, setSettings, reloadTrips } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const connected = !!(settings?.photoprism_configured || settings?.immich_configured)
  const [trips, setTrips] = useState<PhotoTrip[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [all, setAll] = useState(false)
  const [busy, setBusy] = useState('')
  const [home, setHome] = useState<PhotoHome | null>(null)
  const [editHome, setEditHome] = useState(false)

  const load = async (refresh = false) => {
    setLoading(true); setErr('')
    try { const r = await api.get<{ trips: PhotoTrip[]; home?: PhotoHome }>(`${V1}/photo-trips${refresh ? '?refresh=1' : ''}`); setTrips(r.trips || []); setHome(r.home || null) } catch (e) { setErr((e as Error).message) } finally { setLoading(false) }
  }
  useEffect(() => { if (connected) load() }, [connected]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!connected || (!loading && !err && trips && trips.length === 0)) return null

  const add = async (f: PhotoTrip) => {
    setBusy(f.start_date)
    try {
      const t = await api.post<Trip>(`${V1}/trips`, { name: f.name, destination: f.destination, start_date: f.start_date, end_date: f.end_date, lat: f.lat, lng: f.lng, status: f.end_date < todayISO() ? 'completed' : 'active' })
      reloadTrips(); onAdded(); toast(`${f.name} added`)
      navigate(`/trips/${t.id}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const dismiss = async (f: PhotoTrip) => {
    setBusy(f.start_date)
    try { await api.post(`${V1}/photo-trips/dismiss`, { start_date: f.start_date }); setTrips(ts => (ts || []).filter(x => x.start_date !== f.start_date)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const saveHome = async (h: { name: string; lat: number; lng: number } | null) => {
    try {
      const s = await api.put<Settings>(`${V1}/settings`, h ? { home_name: h.name, home_lat: h.lat, home_lng: h.lng } : { home_name: '', home_lat: null, home_lng: null })
      setSettings(s); setEditHome(false); setTrips(null); await load()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const shown = all ? trips || [] : (trips || []).slice(0, SHOW)
  return (
    <Card className="found-card mb-sm" title={<><Icon name="camera" size={16} />Found in your photos{trips && trips.length > 0 && <span className="muted small"> · {trips.length}</span>}</>}
      actions={<button type="button" className="btn ghost sm" onClick={() => load(true)} disabled={loading} title="Look through the whole library again"><Icon name="refresh" size={14} />{loading ? 'Looking…' : 'Rescan'}</button>}>
        {err ? <p className="bad small">{err}</p> : !trips ? <p className="muted small">Looking through where and when you took photos… the first time can take a minute on a big library.</p> : <>
          <p className="muted small mb-sm">Days your photos were taken far from home, with no trip on them yet. A trip runs from leaving home to coming back, through every place in between.
            {home && home.lat !== 0 && <> Home: <b>{home.name || `${home.lat.toFixed(3)}, ${home.lng.toFixed(3)}`}</b> ({home.set ? 'set by you' : 'detected'}) · <button type="button" className="link-btn" onClick={() => setEditHome(true)}>Change</button></>}</p>
          <div className="found-trips">{shown.map(f => (
            <div key={f.start_date} className="found-trip">
              {f.cover ? <img src={photoThumb(f.cover)} alt="" loading="lazy" /> : <span className="ft-noimg"><Icon name="camera" size={20} /></span>}
              <div style={{ minWidth: 0 }}>
                <div className="ft-name">{f.name} {(f.countries || []).map(c => <span key={c} title={countryName(c)}>{flag(c)}</span>)}</div>
                <div className="ft-meta">{fmtRange(f.start_date, f.end_date)} · {plural(f.days, 'day')} · {plural(f.photos, 'photo')}{f.km_from_home > 0 && ` · ${f.km_from_home.toLocaleString()} km from home`}</div>
              </div>
              <div className="ft-actions">
                <button type="button" className="btn ghost sm" disabled={!!busy} onClick={() => dismiss(f)}>Not a trip</button>
                <button type="button" className="btn primary sm" disabled={!!busy} onClick={() => add(f)}><Icon name="plus" size={14} />Add trip</button>
              </div>
            </div>
          ))}</div>
          {trips.length > SHOW && <button type="button" className="btn ghost sm mt-sm" onClick={() => setAll(v => !v)}>{all ? 'Show fewer' : `Show all ${trips.length}`}</button>}
        </>}
      {editHome && (
        <Modal title="Home" onClose={() => setEditHome(false)} size="sm">
          <p className="muted small">Where your trips start and end. Days with photos more than 80 km from here count as away.{home && !home.set && home.name ? ` Detected: ${home.name}, where you took photos on the most days.` : ''}</p>
          <GeoSearch autoFocus onPick={r => saveHome({ name: r.city || r.name, lat: r.lat, lng: r.lng })} placeholder="Nitra, your street…" />
          {settings?.home_lat != null && <button type="button" className="btn ghost sm mt-sm" onClick={() => saveHome(null)}>Detect it from my photos again</button>}
        </Modal>
      )}
    </Card>
  )
}
