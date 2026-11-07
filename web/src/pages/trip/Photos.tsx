import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, photoKey, photoThumb, V1 } from '../../lib/api'
import type { Photo } from '../../lib/types'
import { fmtDate, fmtISOTime, plural, weekday } from '../../lib/format'
import { Empty, Loading, Modal, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { useTrip } from './ctx'

const SRC_NAME = { immich: 'Immich', photoprism: 'PhotoPrism' } as const
/** Photos this far from every place, stay and transport stop of the trip look like they are not from it. */
const FAR_KM = 60

function km(aLat: number, aLng: number, bLat: number, bLng: number) {
  const r = Math.PI / 180, x = Math.sin((bLat - aLat) * r / 2), y = Math.sin((bLng - aLng) * r / 2)
  return 12742 * Math.asin(Math.sqrt(x * x + Math.cos(aLat * r) * Math.cos(bLat * r) * y * y))
}

export function Photos() {
  const { ov, trip, allPhotos, setAllPhotos, photosReady, extErrors, refreshExternal } = useTrip()
  const { toast } = useToast()
  const [open, setOpen] = useState<Photo | null>(null)
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
  const shown = showHidden ? allPhotos : allPhotos.filter(p => !p.hidden)
  const byDay = new Map<string, Photo[]>()
  for (const p of shown) { const d = p.taken_at.slice(0, 10); byDay.set(d, [...(byDay.get(d) || []), p]) }
  const libs = [...new Set(allPhotos.map(p => SRC_NAME[p.src || 'immich']))].join(' and ')
  const idx = open ? shown.indexOf(open) : -1
  const step = (n: number) => { const p = shown[idx + n]; if (p) setOpen(p) }

  const setHidden = async (keys: string[], hidden: boolean) => {
    if (!keys.length) return
    setBusy(true)
    try {
      await api.put(`${V1}/trips/${trip.id}/photos/hidden`, { photos: keys, hidden })
      const ks = new Set(keys)
      setAllPhotos(ps => ps.map(p => ks.has(photoKey(p)) ? { ...p, hidden } : p))
      toast(hidden ? `${plural(keys.length, 'photo')} hidden from this trip` : `${plural(keys.length, 'photo')} back in this trip`)
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
                onClick={e => { e.preventDefault(); if (selecting) toggle(p); else setOpen(p) }}>
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
      {open && (
        <Modal title={<span>{fmtDate(open.taken_at.slice(0, 10))} {fmtISOTime(open.taken_at)} <span className="muted small">{open.city && `· ${open.city}`}</span></span>} onClose={() => setOpen(null)} size="lg">
          <div onKeyDown={e => { if (e.key === 'ArrowRight') step(1); if (e.key === 'ArrowLeft') step(-1) }} tabIndex={0}>
            <img className="lightbox-img" src={photoThumb(open, 'preview')} alt={open.file_name} />
            <div className="row wrap mt-sm">
              <button type="button" className="btn sm" onClick={() => step(-1)} disabled={idx <= 0}><Icon name="arrow-left" size={14} />Previous</button>
              <button type="button" className="btn sm" onClick={() => step(1)} disabled={idx >= shown.length - 1}>Next<Icon name="arrow-right" size={14} /></button>
              <span className="spacer" /><span className="muted small">{open.file_name}</span>
              <button type="button" className="btn sm" disabled={busy} onClick={async () => { const p = open; await setHidden([photoKey(p)], !p.hidden); if (!p.hidden && !showHidden) { const nx = shown[idx + 1] || shown[idx - 1]; setOpen(nx && nx !== p ? nx : null) } else setOpen({ ...p, hidden: !p.hidden }) }}>
                <Icon name={open.hidden ? 'eye' : 'eye-off'} size={14} />{open.hidden ? 'Show in trip' : 'Hide from trip'}</button>
              {open.open && <a className="btn sm" href={open.open} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open in {SRC_NAME[open.src || 'immich']}</a>}
            </div>
          </div>
        </Modal>
      )}
    </>
  )
}
