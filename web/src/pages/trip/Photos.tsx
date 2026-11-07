import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, photoKey, photoThumb, V1 } from '../../lib/api'
import type { Photo, Place } from '../../lib/types'
import { fmtDate, plural, weekday } from '../../lib/format'
import { Empty, Loading, useToast } from '../../components/ui'
import { Gallery } from '../../components/Gallery'
import { Icon } from '../../components/icons'
import { useTrip } from './ctx'

const SRC_NAME = { immich: 'Immich', photoprism: 'PhotoPrism' } as const
/** Photos this far from every place, stay and transport stop of the trip look like they are not from it. */
const FAR_KM = 60

const OFF_KEY = 'pt.photos.devicesOff'
function loadOff(): Set<string> {
  try { return new Set(JSON.parse(localStorage.getItem(OFF_KEY) || '[]')) } catch { return new Set() }
}

function km(aLat: number, aLng: number, bLat: number, bLng: number) {
  const r = Math.PI / 180, x = Math.sin((bLat - aLat) * r / 2), y = Math.sin((bLng - aLng) * r / 2)
  return 12742 * Math.asin(Math.sqrt(x * x + Math.cos(aLat * r) * Math.cos(bLat * r) * y * y))
}

export function Photos() {
  const { ov, trip, allPhotos, setAllPhotos, photosReady, extErrors, refreshExternal, canEdit, edit, reload } = useTrip()
  const { toast } = useToast()
  const [openKey, setOpenKey] = useState<string | null>(null)
  // sources switched off (another phone, WhatsApp…), remembered on this device for every trip
  const [off, setOff] = useState<Set<string>>(loadOff)
  const [made, setMade] = useState<Record<string, Place>>({})
  const [notice, setNotice] = useState<{ key: string; place: Place } | null>(null)
  const [showHidden, setShowHidden] = useState(false)
  const [selecting, setSelecting] = useState(false)
  const [sel, setSel] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const im = ov.integrations.immich, pp = ov.integrations.photoprism

  // Where the trip happened: its centre, places, stays and transport ends.
  const anchors = useMemo(() => {
    const out: [number, number][] = []
    if (trip.lat != null && trip.lng != null) out.push([trip.lat, trip.lng])
    for (const p of ov.places) if (p.lat != null && p.lng != null) out.push([p.lat, p.lng])
    for (const l of ov.lodging) if (l.lat != null && l.lng != null) out.push([l.lat, l.lng])
    // every transport stop except where the first leg leaves from and the
    // last one arrives: that is usually home
    const legs = [...ov.transport].sort((a, b) => (a.depart_at || '').localeCompare(b.depart_at || ''))
    legs.forEach((t, i) => {
      if (i > 0 && t.from_lat != null && t.from_lng != null) out.push([t.from_lat, t.from_lng])
      if ((i < legs.length - 1 || legs.length === 1) && t.to_lat != null && t.to_lng != null) out.push([t.to_lat, t.to_lng])
    })
    return out
  }, [ov, trip])
  const far = useMemo(() => anchors.length === 0 ? [] : allPhotos.filter(p => !p.hidden && p.lat != null && p.lng != null && anchors.every(([a, b]) => km(a, b, p.lat!, p.lng!) > FAR_KM)), [allPhotos, anchors])

  if (!im.configured && !pp?.configured) return <Empty icon="camera" title="No photo library connected">Connect Immich or PhotoPrism under <Link to="/settings/integrations">Settings → Integrations</Link> and every photo taken during the trip shows up here, by day.</Empty>
  if (!trip.immich) return <Empty icon="camera" title="Photos are off for this trip">Turn them on under the trip's settings.</Empty>
  if (extErrors.immich) return <div className="callout danger"><Icon name="alert" size={15} /><span>{extErrors.immich}</span></div>
  if (!photosReady) return <Loading />
  if (allPhotos.length === 0) return <Empty icon="camera" title="No photos in this date range">{trip.start_date ? 'Your photo library has nothing taken between the trip dates (yet).' : 'Set the trip dates first.'}</Empty>

  const hiddenN = allPhotos.filter(p => p.hidden).length
  const pool = showHidden ? allPhotos : allPhotos.filter(p => !p.hidden)
  const devices = new Map<string, number>()
  for (const p of pool) devices.set(p.device || 'Unknown camera', (devices.get(p.device || 'Unknown camera') || 0) + 1)
  const shown = pool.filter(p => !off.has(p.device || 'Unknown camera'))
  const filtered = pool.filter(p => off.has(p.device || 'Unknown camera'))
  const toggleDevice = (d: string) => setOff(o => {
    const n = new Set(o); if (n.has(d)) n.delete(d); else n.add(d)
    try { localStorage.setItem(OFF_KEY, JSON.stringify([...n])) } catch { /* private mode */ }
    return n
  })
  const byDay = new Map<string, Photo[]>()
  for (const p of shown) { const d = p.taken_at.slice(0, 10); byDay.set(d, [...(byDay.get(d) || []), p]) }
  const libs = [...new Set(allPhotos.map(p => SRC_NAME[p.src || 'immich']))].join(' and ')
  const idx = openKey ? shown.findIndex(p => photoKey(p) === openKey) : -1

  // a visited place where the photo was taken (trips written after the fact)
  const makePlace = async (p: Photo) => {
    setBusy(true)
    try {
      const pl = await api.post<Place>(`${V1}/trips/${trip.id}/places/from-photo`, { src: p.src, id: p.id, hash: p.hash, lat: p.lat, lng: p.lng, taken_at: p.taken_at, city: p.city })
      setMade(m => ({ ...m, [photoKey(p)]: pl })); setNotice({ key: photoKey(p), place: pl })
      reload()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }

  const setHidden = async (keys: string[], hidden: boolean) => {
    if (!keys.length) return
    setBusy(true)
    try {
      await api.put(`${V1}/trips/${trip.id}/photos/hidden`, { photos: keys, hidden })
      const ks = new Set(keys)
      setAllPhotos(ps => ps.map(p => ks.has(photoKey(p)) ? { ...p, hidden } : p))
      if (keys.length > 1 || idx < 0) toast(hidden ? `${plural(keys.length, 'photo')} hidden from this trip` : `${plural(keys.length, 'photo')} back in this trip`)
      setSel(new Set()); setSelecting(false)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const toggle = (p: Photo) => setSel(s => { const n = new Set(s); const k = photoKey(p); if (n.has(k)) n.delete(k); else n.add(k); return n })
  const selDay = (ps: Photo[]) => setSel(s => { const n = new Set(s); const all = ps.every(p => n.has(photoKey(p))); ps.forEach(p => all ? n.delete(photoKey(p)) : n.add(photoKey(p))); return n })
  const selPhotos = allPhotos.filter(p => sel.has(photoKey(p)))
  const selHidden = selPhotos.filter(p => p.hidden).length

  return (
    <>
      <div className="toolbar">
        <span className="muted">{plural(shown.length, 'photo')} from {libs} across {byDay.size} days{hiddenN > 0 && !showHidden && ` · ${hiddenN} hidden`}</span>
        <span className="spacer" />
        {hiddenN > 0 && <button type="button" className={`btn ghost sm ${showHidden ? 'active' : ''}`} onClick={() => setShowHidden(v => !v)}><Icon name={showHidden ? 'eye-off' : 'eye'} size={14} />{showHidden ? 'Hide hidden' : `Show hidden (${hiddenN})`}</button>}
        <button type="button" className={`btn ghost sm ${selecting ? 'active' : ''}`} onClick={() => { setSelecting(v => !v); setSel(new Set()) }}><Icon name="check" size={14} />{selecting ? 'Done' : 'Select'}</button>
        <button type="button" className="btn ghost sm" onClick={refreshExternal}><Icon name="refresh" size={14} />Refresh</button>
      </div>
      {devices.size > 1 && (
        <div className="photo-devices" role="group" aria-label="Which phone or app">
          {[...devices.entries()].sort((a, b) => b[1] - a[1]).map(([d, n]) => (
            <button key={d} type="button" className={`chip ${off.has(d) ? 'off' : 'active'}`} aria-pressed={!off.has(d)} title={off.has(d) ? 'Show these again' : 'Leave these out'} onClick={() => toggleDevice(d)}>
              <Icon name={d === 'WhatsApp' || d === 'Viber' || d === 'Telegram' || d === 'Messenger' || d === 'Signal' ? 'send' : d === 'Screenshots' ? 'image' : 'camera'} size={13} />{d}<span className="n">{n}</span>
            </button>
          ))}
          {filtered.length > 0 && <>
            <button type="button" className="link-btn small" onClick={() => { setOff(new Set()); try { localStorage.removeItem(OFF_KEY) } catch { /* */ } }}>Show all</button>
            <button type="button" className="link-btn small" disabled={busy} onClick={() => setHidden(filtered.filter(p => !p.hidden).map(photoKey), true)} title="Hide them from this trip for good (also on the map and overview)">Hide these {filtered.filter(p => !p.hidden).length} from the trip</button>
          </>}
        </div>
      )}
      {extErrors.photos_partial && <div className="callout warn mb-sm"><Icon name="alert" size={15} /><span>{extErrors.photos_partial}</span></div>}
      {!selecting && far.length > 0 && (
        <div className="hint-strip photo-far"><Icon name="info" size={14} /><span>{plural(far.length, 'photo')} {far.length === 1 ? 'was' : 'were'} taken more than {FAR_KM} km from every place, stay and stop of this trip (at home before leaving, say).</span>
          <span className="spacer" /><button type="button" className="btn sm" onClick={() => { setSelecting(true); setSel(new Set(far.map(photoKey))) }}>Review</button></div>
      )}
      {Array.from(byDay.entries()).map(([d, ps]) => (
        <div key={d} className="photo-day">
          <h3>{weekday(d, true)}, {fmtDate(d)} <span className="muted small">· {ps.length}</span>{selecting && <button type="button" className="link-btn small photo-selday" onClick={() => selDay(ps)}>{ps.every(p => sel.has(photoKey(p))) ? 'Unselect day' : 'Select day'}</button>}</h3>
          <div className={`photo-grid ${selecting ? 'selecting' : ''}`}>{ps.map(p => {
            const k = photoKey(p), on = sel.has(k)
            return (
              <a key={k} href={p.open || '#'} className={`${p.hidden ? 'is-hidden' : ''} ${on ? 'is-sel' : ''}`} aria-pressed={selecting ? on : undefined}
                onClick={e => { e.preventDefault(); if (selecting) toggle(p); else { setNotice(null); setOpenKey(k) } }}>
                <img src={photoThumb(p)} loading="lazy" alt={p.file_name} />
                {p.type === 'VIDEO' && <span className="vid"><Icon name="play" size={9} />{p.duration?.slice(3, 8)}</span>}
                {p.favorite && <span className="vid"><Icon name="heart" size={10} /></span>}
                {p.hidden && <span className="ph-badge"><Icon name="eye-off" size={11} />hidden</span>}
                {selecting && <span className={`ph-check ${on ? 'on' : ''}`}>{on && <Icon name="check" size={13} />}</span>}
              </a>
            )
          })}</div>
        </div>
      ))}
      {selecting && (
        <div className="photo-selbar">
          <span>{sel.size ? plural(sel.size, 'photo') + ' selected' : 'Tap photos to select them'}</span>
          <span className="spacer" />
          {selHidden > 0 && <button type="button" className="btn sm" disabled={busy} onClick={() => setHidden(selPhotos.filter(p => p.hidden).map(photoKey), false)}><Icon name="eye" size={14} />Show again ({selHidden})</button>}
          <button type="button" className="btn sm danger" disabled={busy || selPhotos.length === selHidden} onClick={() => setHidden(selPhotos.filter(p => !p.hidden).map(photoKey), true)}><Icon name="eye-off" size={14} />Hide from trip{selPhotos.length - selHidden > 0 ? ` (${selPhotos.length - selHidden})` : ''}</button>
          <button type="button" className="btn ghost sm" onClick={() => { setSelecting(false); setSel(new Set()) }}>Cancel</button>
        </div>
      )}
      {idx >= 0 && (
        <Gallery photos={shown} index={idx} onIndex={i => { setNotice(null); setOpenKey(photoKey(shown[i])) }} onClose={() => setOpenKey(null)}
          notice={notice && notice.key === openKey && <><Icon name="check" size={14} />Added “{notice.place.name}” to the trip's places<button type="button" className="link-btn" onClick={() => { const pl = notice.place; setOpenKey(null); edit('place', pl) }}>Edit</button></>}
          actions={p => <>
            {canEdit && (made[photoKey(p)]
              ? <button type="button" className="btn sm" onClick={() => { const pl = made[photoKey(p)]; setOpenKey(null); edit('place', pl) }}><Icon name="check" size={14} />Place added</button>
              : <button type="button" className="btn sm" disabled={busy || p.lat == null} title={p.lat == null ? 'This photo has no location' : 'A visited place here, on this day, with this photo'} onClick={() => makePlace(p)}><Icon name="pin" size={14} />Create place</button>)}
            <button type="button" className="btn sm" disabled={busy} onClick={async () => {
              const k = photoKey(p)
              if (!p.hidden && !showHidden) { const nx = shown[idx + 1] || shown[idx - 1]; await setHidden([k], true); setOpenKey(nx && photoKey(nx) !== k ? photoKey(nx) : null) }
              else await setHidden([k], !p.hidden)
            }}><Icon name={p.hidden ? 'eye' : 'eye-off'} size={14} />{p.hidden ? 'Show in trip' : 'Hide from trip'}</button>
            {p.open && <a className="btn sm" href={p.open} target="_blank" rel="noreferrer"><Icon name="external" size={14} />{SRC_NAME[p.src || 'immich']}</a>}
          </>} />
      )}
    </>
  )
}
