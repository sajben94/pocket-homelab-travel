import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, photoKey, photoThumb, V1 } from '../lib/api'
import type { Photo, PhotoHome, PhotoTrip, Settings, Trip } from '../lib/types'
import { daysBetween, fmtDate, fmtRange, plural, todayISO, weekday } from '../lib/format'
import { flag, countryName } from '../lib/countries'
import { Icon } from '../components/icons'
import { Card, Field, Loading, Modal, useToast } from '../components/ui'
import { MapView } from '../components/Map'
import { Gallery } from '../components/Gallery'
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
  // the scan runs on the server in the background; this is how far it got
  const [scan, setScan] = useState<{ state: 'scanning' | 'done' | 'error'; seen?: number; located?: number; scanned_at?: string } | null>(null)
  const [all, setAll] = useState(false)
  const [busy, setBusy] = useState('')
  const [home, setHome] = useState<PhotoHome | null>(null)
  const [editHome, setEditHome] = useState(false)
  const [review, setReview] = useState<PhotoTrip | null>(null)

  type Answer = { state: 'scanning' | 'done' | 'error'; trips?: PhotoTrip[]; home?: PhotoHome; seen?: number; located?: number; scanned_at?: string; error?: string; connected: boolean }
  const load = async (refresh = false) => {
    setLoading(true); setErr('')
    try {
      const r = await api.get<Answer>(`${V1}/photo-trips${refresh ? '?refresh=1' : ''}`)
      setScan({ state: r.state, seen: r.seen, located: r.located, scanned_at: r.scanned_at })
      if (r.state === 'error') setErr(r.error || 'the scan failed')
      if (r.state === 'done') { setTrips(r.trips || []); setHome(r.home || null) }
    } catch (e) { setErr((e as Error).message); setScan(s => s && { ...s, state: 'error' }) } finally { setLoading(false) }
  }
  useEffect(() => { if (connected) load() }, [connected]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (scan?.state !== 'scanning') return
    const t = setTimeout(() => load(), 2000)
    return () => clearTimeout(t)
  }, [scan]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!connected) return null
  const scanning = scan?.state === 'scanning'

  const add = async (f: PhotoTrip) => {
    setBusy(f.start_date)
    try {
      const t = await api.post<Trip>(`${V1}/trips`, { name: f.name, destination: f.destination, start_date: f.start_date, end_date: f.end_date, lat: f.lat, lng: f.lng, status: f.end_date < todayISO() ? 'completed' : 'active' })
      setReview(null); reloadTrips(); onAdded(); toast(`${f.name} added`)
      navigate(`/trips/${t.id}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const dismiss = async (f: PhotoTrip) => {
    setBusy(f.start_date)
    try { await api.post(`${V1}/photo-trips/dismiss`, { start_date: f.start_date }); setReview(null); setTrips(ts => (ts || []).filter(x => x.start_date !== f.start_date)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const saveHome = async (h: { name: string; lat: number; lng: number } | null) => {
    try {
      const s = await api.put<Settings>(`${V1}/settings`, h ? { home_name: h.name, home_lat: h.lat, home_lng: h.lng } : { home_name: '', home_lat: null, home_lng: null })
      setSettings(s); setEditHome(false); setTrips(null); await load()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  // a found trip that is really two: a new one starts on date
  const split = async (date: string) => {
    setBusy(date)
    try { await api.post(`${V1}/photo-trips/split`, { date }); setReview(null); toast('Split in two: review or add each one'); await load() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const join = async (f: PhotoTrip) => {
    setBusy(f.start_date)
    try { await api.post(`${V1}/photo-trips/join`, { date: f.start_date }); toast('Joined back into one'); await load() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const shown = all ? trips || [] : (trips || []).slice(0, SHOW)
  return (
    <Card className="found-card mb-sm" title={<><Icon name="camera" size={16} />Found in your photos{trips && trips.length > 0 && <span className="muted small"> · {trips.length}</span>}</>}
      actions={<button type="button" className="btn ghost sm" onClick={() => { setTrips(null); load(true) }} disabled={loading || scanning} title="Look through the whole library again"><Icon name="refresh" size={14} />{scanning ? 'Looking…' : 'Rescan'}</button>}>
        {err ? <div className="callout danger"><Icon name="alert" size={15} /><span>Could not look through your photos: {err}. <button type="button" className="link-btn" onClick={() => load(true)}>Try again</button></span></div>
        : scanning || !trips ? <div className="scan-progress">
            <span className="spinner" aria-hidden="true" />
            <span><b>Looking through your photo library{scan?.seen ? ` · ${scan.seen.toLocaleString()} photos so far` : '…'}</b><br />
              <span className="muted small">It runs on the server and can take a few minutes on a big library; you can leave this page and come back.</span></span>
          </div>
        : trips.length === 0 ? <p className="muted small">No new trips found{scan?.seen ? ` in ${scan.seen.toLocaleString()} photos (${(scan.located || 0).toLocaleString()} with a location)` : ''}: every stretch away from home already has a trip{scan?.located === 0 ? ' — or none of the photos has a location (GPS) to go by' : ''}.
            {home && home.lat !== 0 && <> Home: <b>{home.name || `${home.lat.toFixed(3)}, ${home.lng.toFixed(3)}`}</b> ({home.set ? 'set by you' : 'detected'}) · <button type="button" className="link-btn" onClick={() => setEditHome(true)}>Change</button></>}</p>
        : <>
          <p className="muted small mb-sm">Days your photos were taken far from home, with no trip on them yet{scan?.seen ? ` (from ${scan.seen.toLocaleString()} photos)` : ''}. A trip runs from leaving home to coming back, through every place in between.
            {home && home.lat !== 0 && <> Home: <b>{home.name || `${home.lat.toFixed(3)}, ${home.lng.toFixed(3)}`}</b> ({home.set ? 'set by you' : 'detected'}) · <button type="button" className="link-btn" onClick={() => setEditHome(true)}>Change</button></>}</p>
          <div className="found-trips">{shown.map(f => (
            <div key={f.start_date} className="found-trip">
              {f.cover ? <img src={photoThumb(f.cover)} alt="" loading="lazy" /> : <span className="ft-noimg"><Icon name="camera" size={20} /></span>}
              <div style={{ minWidth: 0 }}>
                <div className="ft-name">{f.name} {(f.countries || []).map(c => <span key={c} title={countryName(c)}>{flag(c)}</span>)}</div>
                <div className="ft-meta">{f.split_before && <><button type="button" className="link-btn" disabled={!!busy} onClick={() => join(f)} title="It was split from the trip before it: make it one again">Join with the trip before</button> · </>}{fmtRange(f.start_date, f.end_date)} · {plural(f.days, 'day')} · {plural(f.photos, 'photo')}{f.km_from_home > 0 && ` · ${f.km_from_home.toLocaleString()} km from home`}</div>
              </div>
              <div className="ft-actions">
                <button type="button" className="btn ghost sm" disabled={!!busy} onClick={() => dismiss(f)}>Not a trip</button>
                <button type="button" className="btn sm" disabled={!!busy} onClick={() => setReview(f)}><Icon name="eye" size={14} />Review</button>
                <button type="button" className="btn primary sm" disabled={!!busy} onClick={() => add(f)}><Icon name="plus" size={14} />Add trip</button>
              </div>
            </div>
          ))}</div>
          {trips.length > SHOW && <button type="button" className="btn ghost sm mt-sm" onClick={() => setAll(v => !v)}>{all ? 'Show fewer' : `Show all ${trips.length}`}</button>}
        </>}
      {review && <ReviewTrip f={review} busy={!!busy} onClose={() => setReview(null)} onAdd={add} onDismiss={dismiss} onSplit={split} />}
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

interface PreviewDay { date: string; photos: number; located: number; away: boolean; km: number; cities: string[]; countries: string[]; lat?: number; lng?: number; sample: Photo[]; all: Photo[] }

/** A found trip, day by day with three days on each side: check it, move its start or end, then add it. */
function ReviewTrip({ f, onClose, onAdd, onDismiss, onSplit, busy }: { f: PhotoTrip; onClose: () => void; onAdd: (f: PhotoTrip) => void; onDismiss: (f: PhotoTrip) => void; onSplit: (date: string) => void; busy: boolean }) {
  const [days, setDays] = useState<PreviewDay[] | null>(null)
  const [home, setHome] = useState<PhotoHome | null>(null)
  const [err, setErr] = useState('')
  const [start, setStart] = useState(f.start_date)
  const [end, setEnd] = useState(f.end_date)
  const [name, setName] = useState(f.name)
  const [dest, setDest] = useState(f.destination)
  const [view, setView] = useState<{ photos: Photo[]; i: number } | null>(null)
  useEffect(() => {
    api.get<{ days: PreviewDay[]; home: PhotoHome | null; warning?: string }>(`${V1}/photo-trips/preview?start=${f.start_date}&end=${f.end_date}`)
      .then(r => { setDays(r.days); setHome(r.home); if (r.warning) setErr(r.warning) }).catch(e => setErr((e as Error).message))
  }, [f.start_date, f.end_date])
  const inside = (d: string) => d >= start && d <= end
  const chosen = (days || []).filter(d => inside(d.date))
  const nDays = chosen.length, nPhotos = chosen.reduce((a, d) => a + d.photos, 0)
  // the chosen days' spots, joined in order: roughly the way the trip went
  const route = (days || []).filter(d => inside(d.date) && d.lat != null).map(d => [d.lat!, d.lng!] as [number, number])
  const markers = [
    ...(days || []).filter(d => d.lat != null && (inside(d.date) || d.away)).map((d, i) => ({ id: d.date, lat: d.lat!, lng: d.lng!, label: `${fmtDate(d.date, false)} · ${d.cities[0] || ''}`, number: inside(d.date) ? daysBetween(start, d.date) + 1 : undefined, icon: inside(d.date) ? undefined : '·', kind: `d${i}` })),
    ...(home ? [{ id: 'home', lat: home.lat, lng: home.lng, label: `Home${home.name ? ' · ' + home.name : ''}`, icon: '🏠' }] : []),
  ]
  const bad = start > end
  return (
    <Modal title={<>Review: {f.name}</>} onClose={onClose} size="xl"
      footer={<>
        <button type="button" className="btn ghost" disabled={busy} onClick={() => onDismiss(f)}>Not a trip</button>
        <span className="spacer" />
        <span className="muted small">{bad ? 'The start is after the end' : `${fmtRange(start, end)} · ${plural(nDays, 'day')} · ${plural(nPhotos, 'photo')}`}</span>
        <button type="button" className="btn primary" disabled={busy || bad || !name.trim()} onClick={() => onAdd({ ...f, name: name.trim(), destination: dest.trim(), start_date: start, end_date: end })}><Icon name="plus" size={15} />Add trip</button>
      </>}>
      <div className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Name"><input value={name} onChange={e => setName(e.target.value)} maxLength={120} /></Field>
          <Field label="Destination"><input value={dest} onChange={e => setDest(e.target.value)} /></Field>
        </div>
        <div className="row wrap" style={{ gap: 12 }}>
          <Field label="Starts"><input type="date" value={start} onChange={e => setStart(e.target.value)} /></Field>
          <Field label="Ends"><input type="date" value={end} onChange={e => setEnd(e.target.value)} /></Field>
          {(start !== f.start_date || end !== f.end_date) && <button type="button" className="link-btn small" style={{ alignSelf: 'end', marginBottom: 10 }} onClick={() => { setStart(f.start_date); setEnd(f.end_date) }}>Back to {fmtRange(f.start_date, f.end_date)}</button>}
        </div>
        {err && <div className="callout warn"><Icon name="alert" size={15} /><span>{err}</span></div>}
        {!days ? (!err && <Loading />) : <>
          {markers.length > 0 && <MapView className="sm" markers={markers} lines={route.length > 1 ? [{ points: route, color: '#0284c7', dashed: true }] : []} cluster={false} fitKey={`${start}:${end}`} />}
          <p className="muted small">Three days on each side are shown, so a trip found a day off can be moved: use <b>Start here</b> / <b>End here</b>, or the dates above. Two trips run together? <b>Split here</b> on the day the second one starts. A day counts as away when most of its photos are more than 80 km from home{home?.name ? ` (${home.name})` : ''}.</p>
          <div className="review-days">
            {days.map(d => {
              const inTrip = inside(d.date)
              return (
                <div key={d.date} className={`review-day ${inTrip ? 'in' : 'out'}`}>
                  <div className="rd-date">
                    <b>{weekday(d.date, true)}</b> {fmtDate(d.date, false)}
                    <span className={`rd-pill ${d.photos === 0 ? 'none' : d.away ? 'away' : 'home'}`}>{d.photos === 0 ? 'no photos' : d.away ? `away · ${d.km.toLocaleString()} km` : d.located === 0 ? 'no location' : 'home'}</span>
                  </div>
                  <div className="rd-where">
                    {d.photos > 0 ? <>{d.cities.join(', ') || <span className="muted">—</span>}<span className="muted"> · {plural(d.photos, 'photo')}{d.located < d.photos ? ` (${d.located} located)` : ''}</span></> : <span className="muted">Nothing taken this day</span>}
                  </div>
                  <div className="rd-thumbs">{d.sample.map((p, i) => <button key={photoKey(p)} type="button" onClick={() => setView({ photos: d.all, i: d.all.findIndex(x => photoKey(x) === photoKey(p)) })}><img src={photoThumb(p)} alt="" loading="lazy" /></button>)}{d.photos > d.sample.length && <button type="button" className="rd-more" onClick={() => setView({ photos: d.all, i: 0 })}>+{d.photos - d.sample.length}</button>}</div>
                  <div className="rd-actions">
                    {d.date !== start && d.date <= end && <button type="button" className="btn ghost sm" onClick={() => setStart(d.date)}>Start here</button>}
                    {d.date !== end && d.date >= start && <button type="button" className="btn ghost sm" onClick={() => setEnd(d.date)}>End here</button>}
                    {d.date > f.start_date && d.date <= f.end_date && <button type="button" className="btn ghost sm" disabled={busy} title="Two trips: a new one starts this day" onClick={() => onSplit(d.date)}><Icon name="scissors" size={14} />Split here</button>}
                    {inTrip && (d.date === start || d.date === end) && <span className="muted small">{d.date === start && d.date === end ? 'only day' : d.date === start ? 'first day' : 'last day'}</span>}
                  </div>
                </div>
              )
            })}
          </div>
        </>}
      </div>
      {view && <Gallery overModal photos={view.photos} index={Math.max(0, view.i)} onIndex={i => setView(v => v && { ...v, i })} onClose={() => setView(null)} />}
    </Modal>
  )
}
