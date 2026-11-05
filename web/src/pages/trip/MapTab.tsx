import { useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { GeoResult } from '../../lib/types'
import { DAY_COLORS, LODGING_ICONS, PLACE_ICONS } from '../../lib/types'
import { cap, fmtDate, fmtKm, itineraryOrder, weekday } from '../../lib/format'
import { Card, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select } from '../../components/Select'
import { MapView, type Line, type Marker } from '../../components/Map'
import { useTrip } from './ctx'

export function MapTab() {
  const { ov, trip, route, timeline, photos, canEdit, edit, extErrors } = useTrip()
  const { meta, settings } = useApp()
  const { toast } = useToast()
  const [layers, setLayers] = useState({ places: true, lodging: true, route: true, visits: false, photos: false, numbers: true })
  const [discover, setDiscover] = useState<{ cat: string; radius: number; results: GeoResult[]; busy: boolean; open: boolean }>({ cat: 'sight', radius: 3000, results: [], busy: false, open: false })
  const [center, setCenter] = useState<[number, number] | null>(null)
  const [day, setDay] = useState<string>('all')
  const routeDays = route?.days || []
  const dayIndex = (d: string) => routeDays.findIndex(x => x.date === d)
  const dayColor = (d: string) => DAY_COLORS[Math.max(0, dayIndex(d)) % DAY_COLORS.length]
  const unit = settings?.distance_unit || 'km'
  const markers = useMemo(() => {
    const out: Marker[] = []
    if (layers.places) {
      const scheduled = ov.places.filter(p => p.lat != null && p.day_date).sort(itineraryOrder)
      scheduled.filter(p => day === 'all' || p.day_date === day).forEach((p, i) => out.push({ id: `p${p.id}`, lat: p.lat!, lng: p.lng!, label: p.name, icon: PLACE_ICONS[p.category], kind: p.status, number: layers.numbers ? (day === 'all' ? i + 1 : i + 1) : undefined, popup: `<b>${esc(p.name)}</b><br>${cap(p.category)} · ${fmtDate(p.day_date, false)}${p.address ? '<br>' + esc(p.address) : ''}`, onClick: () => edit('place', p) }))
      if (day === 'all') ov.places.filter(p => p.lat != null && !p.day_date).forEach(p => out.push({ id: `p${p.id}`, lat: p.lat!, lng: p.lng!, label: p.name, icon: PLACE_ICONS[p.category], kind: p.status === 'visited' ? 'visited' : 'idea', popup: `<b>${esc(p.name)}</b><br>${cap(p.category)} · idea`, onClick: () => edit('place', p) }))
    }
    if (layers.lodging) ov.lodging.filter(l => l.lat != null).forEach(l => out.push({ id: `l${l.id}`, lat: l.lat!, lng: l.lng!, label: l.name, icon: LODGING_ICONS[l.type] || '🛏', kind: 'lodging', popup: `<b>${esc(l.name)}</b><br>${fmtDate(l.check_in, false)} – ${fmtDate(l.check_out, false)}`, onClick: () => edit('lodging', l) }))
    if (layers.visits) for (const d of timeline) for (const e of d.entries) if (day === 'all' || d.date === day) if (e.type === 'visit' && e.latitude != null && e.longitude != null) out.push({ id: `v${e.visit_id}`, lat: e.latitude, lng: e.longitude, label: e.name || 'stop', icon: '📌', kind: 'visit', popup: `<b>${esc(e.name || 'Stop')}</b><br>${fmtDate(d.date, false)} · ${Math.round(e.duration / 60)} min` })
    if (layers.photos) photos.filter(p => p.lat != null && (day === 'all' || p.taken_at.slice(0, 10) === day)).forEach(p => out.push({ id: `ph${p.id}`, lat: p.lat!, lng: p.lng!, label: p.file_name, icon: '📷', kind: 'photo', popup: `<img src="${V1}/trips/${trip.id}/photos/${p.id}/thumb" style="width:160px;border-radius:6px"><br>${fmtDate(p.taken_at.slice(0, 10), false)}` }))
    discover.results.forEach((r, i) => out.push({ id: `d${i}`, lat: r.lat, lng: r.lng, label: r.name, icon: '✚', kind: 'idea', popup: `<b>${esc(r.name)}</b><br>${esc(r.type || '')}${r.hours ? '<br>' + esc(r.hours) : ''}`, onClick: () => canEdit && edit('place', null, { name: r.name, category: discover.cat === 'historic' ? 'sight' : discover.cat === 'lodging' ? 'other' : discover.cat, lat: r.lat, lng: r.lng, link: r.website || '', phone: r.phone || '', address: r.display.replace(/^[\d.]+ km( · )?/, '') }) }))
    return out
  }, [ov, layers, timeline, photos, discover.results, discover.cat, trip.id, canEdit, edit, day])
  const lines = useMemo(() => {
    const out: Line[] = []
    if (layers.route && route) for (const rd of route.days) {
      if (day !== 'all' && rd.date !== day) continue
      const i = dayIndex(rd.date)
      for (const seg of rd.segments) out.push({ points: seg, color: DAY_COLORS[i % DAY_COLORS.length], weight: day === 'all' ? 3 : 4, opacity: day === 'all' ? .7 : .9, tooltip: `Day ${i + 1} · ${fmtDate(rd.date, false)} · ${fmtKm(rd.km, unit)}` })
    }
    if (layers.places && layers.numbers) {
      const scheduled = ov.places.filter(p => p.lat != null && p.day_date && (day === 'all' || p.day_date === day)).sort(itineraryOrder)
      if (scheduled.length > 1) out.push({ points: scheduled.map(p => [p.lat!, p.lng!] as [number, number]), color: '#64748b', dashed: true, weight: 2 })
    }
    if (day === 'all') for (const t of ov.transport) if (t.from_lat != null && t.to_lat != null) out.push({ points: [[t.from_lat, t.from_lng!], [t.to_lat, t.to_lng!]], color: '#7c3aed', dashed: true, weight: 2, noFit: true })
    return out
  }, [ov, layers, route, day, unit]) // eslint-disable-line react-hooks/exhaustive-deps
  const runDiscover = async (lat?: number, lng?: number) => {
    const c = lat != null ? [lat, lng!] : center || (trip.lat != null ? [trip.lat, trip.lng!] : ov.lodging.find(l => l.lat != null) ? [ov.lodging.find(l => l.lat != null)!.lat!, ov.lodging.find(l => l.lat != null)!.lng!] : null)
    if (!c) { toast('Set the trip destination or click the map to choose where to look', 'err'); return }
    setDiscover(d => ({ ...d, busy: true }))
    try { const r = await api.get<GeoResult[]>(`${V1}/geo/discover?lat=${c[0]}&lng=${c[1]}&radius=${discover.radius}&category=${discover.cat}`); setDiscover(d => ({ ...d, results: r, busy: false })); if (r.length === 0) toast('Nothing found there — try a bigger radius') } catch (e) { toast((e as Error).message, 'err'); setDiscover(d => ({ ...d, busy: false })) }
  }
  const tog = (k: keyof typeof layers) => setLayers(l => ({ ...l, [k]: !l[k] }))
  const fitKey = `${day}:${markers.length}:${lines.length}:${discover.results.length}`
  const selected = routeDays.find(d => d.date === day)
  return (
    <>
      <div className="toolbar">
        <div className="chips">
          <button type="button" className={`chip ${layers.places ? 'active' : ''}`} onClick={() => tog('places')}><Icon name="pin" size={13} />Places</button>
          <button type="button" className={`chip ${layers.numbers ? 'active' : ''}`} onClick={() => tog('numbers')}><Icon name="list" size={13} />Order</button>
          <button type="button" className={`chip ${layers.lodging ? 'active' : ''}`} onClick={() => tog('lodging')}><Icon name="bed" size={13} />Stays</button>
          {route && route.raw_count > 0 && <button type="button" className={`chip ${layers.route ? 'active' : ''}`} onClick={() => tog('route')}><Icon name="route" size={13} />Route {fmtKm(route.distance_km, unit)}</button>}
          {timeline.length > 0 && <button type="button" className={`chip ${layers.visits ? 'active' : ''}`} onClick={() => tog('visits')}><Icon name="flag" size={13} />Stops</button>}
          {photos.some(p => p.lat != null) && <button type="button" className={`chip ${layers.photos ? 'active' : ''}`} onClick={() => tog('photos')}><Icon name="camera" size={13} />Photos</button>}
        </div>
        <span className="spacer" />
        <button type="button" className={`btn sm ${discover.open ? 'primary' : ''}`} onClick={() => setDiscover(d => ({ ...d, open: !d.open }))}><Icon name="compass" size={14} />Discover nearby</button>
      </div>
      {discover.open && (
        <Card className="mb">
          <div className="row wrap">
            <Select value={discover.cat} onChange={v => setDiscover(d => ({ ...d, cat: v }))} options={(meta?.discover_categories || []).map(c => ({ value: c, label: `${PLACE_ICONS[c] || '📍'} ${cap(c)}` }))} />
            <Select value={String(discover.radius)} onChange={v => setDiscover(d => ({ ...d, radius: Number(v) }))} options={[1000, 2000, 3000, 5000, 10000, 20000].map(r => ({ value: String(r), label: `within ${r / 1000} km` }))} />
            <button type="button" className="btn primary sm" onClick={() => runDiscover()} disabled={discover.busy}>{discover.busy ? 'Searching…' : 'Search'}</button>
            {discover.results.length > 0 && <button type="button" className="btn ghost sm" onClick={() => setDiscover(d => ({ ...d, results: [] }))}>Clear {discover.results.length}</button>}
            <span className="muted small">OpenStreetMap points of interest around {center ? 'the clicked spot' : 'the trip destination'}. Click the map to search elsewhere, click a ✚ pin to add it.</span>
          </div>
        </Card>
      )}
      {extErrors.dawarich && <div className="callout danger mb"><Icon name="alert" size={15} /><span>Recorded route: {extErrors.dawarich}</span></div>}
      {routeDays.length > 0 && layers.route && (
        <div className="day-strip">
          <button type="button" className={`chip sm ${day === 'all' ? 'active' : ''}`} onClick={() => setDay('all')}>All days · {fmtKm(route!.distance_km, unit)}</button>
          {routeDays.map((d, i) => <button type="button" key={d.date} className={`chip sm ${day === d.date ? 'active' : ''}`} style={{ '--c': DAY_COLORS[i % DAY_COLORS.length] } as React.CSSProperties} onClick={() => setDay(day === d.date ? 'all' : d.date)} title={`${weekday(d.date, true)} ${fmtDate(d.date)}${d.start ? ` · ${d.start}–${d.end}` : ''}`}><span className="swatch" style={{ background: DAY_COLORS[i % DAY_COLORS.length] }} /> {i + 1} <span className="muted">{fmtKm(d.km, unit)}</span></button>)}
          <span className="spacer" />
          <button type="button" className="btn ghost sm" onClick={() => setDay(d => { const i = dayIndex(d); return routeDays[(i - 1 + routeDays.length) % routeDays.length].date })} title="Previous day" aria-label="Previous day"><Icon name="chevron-left" size={15} /></button>
          <button type="button" className="btn ghost sm" onClick={() => setDay(d => { const i = dayIndex(d); return routeDays[(i + 1) % routeDays.length].date })} title="Next day" aria-label="Next day"><Icon name="chevron-right" size={15} /></button>
        </div>
      )}
      {selected && <div className="hint-strip"><b>Day {dayIndex(selected.date) + 1}</b> · {weekday(selected.date, true)}, {fmtDate(selected.date)}{selected.start && ` · recorded ${selected.start}–${selected.end}`} · {fmtKm(selected.km, unit)}{(() => { const tl = timeline.find(x => x.date === selected.date); return tl ? ` · ${Object.entries(tl.summary.mode_distances || {}).filter(([, v]) => v >= 0.5).map(([m, v]) => `${m} ${fmtKm(v, unit)}`).join(', ')} · ${tl.entries.filter(e => e.type === 'visit').length} stops` : '' })()}<span className="spacer" /><span className="muted">Only this day's places{layers.photos ? ', photos' : ''}{layers.visits ? ', stops' : ''} are shown.</span></div>}
      <MapView className="tall" markers={markers} lines={lines} center={trip.lat != null ? [trip.lat, trip.lng!] : undefined} zoom={10} fitKey={fitKey} onClick={(lat, lng) => { setCenter([lat, lng]); if (discover.open) runDiscover(lat, lng) }} />
      <div className="legend"><span style={{ '--d': '#0284c7' } as React.CSSProperties}>planned</span><span style={{ '--d': '#d97706' } as React.CSSProperties}>booked</span><span style={{ '--d': '#16a34a' } as React.CSSProperties}>visited</span><span style={{ '--d': '#94a3b8' } as React.CSSProperties}>idea</span><span style={{ '--d': '#7c3aed' } as React.CSSProperties}>stay</span><span style={{ '--d': '#0ea5e9' } as React.CSSProperties}>recorded route (one colour per day)</span>{layers.visits && <span style={{ '--d': '#0ea5e9' } as React.CSSProperties}>stops</span>}{layers.photos && <span style={{ '--d': '#db2777' } as React.CSSProperties}>photos</span>}</div>
    </>
  )
}

function esc(s: string): string { return s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] || c)) }
