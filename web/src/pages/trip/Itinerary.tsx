import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Place } from '../../lib/types'
import { LODGING_ICONS, PLACE_ICONS, TRANSPORT_ICONS, WX_ICON } from '../../lib/types'
import { dateRange, fmtClock, fmtDate, fmtDateTime, fmtDuration, fmtKm, itineraryOrder, placeWhen, stayTime, todayISO, weekday } from '../../lib/format'
import { Card, Empty, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { useTrip } from './ctx'
import { lodgingSub, placeSub, transportSub } from './Overview'
import { PlanModal } from './ai'

type Row = { key: string; when: string; kind: 'place' | 'lodging-in' | 'lodging-out' | 'transport' | 'journey' | 'visit'; icon: string; title: string; sub: string; place?: Place; onClick?: () => void; cls?: string }

export function Itinerary() {
  const { ov, trip, weather, timeline, route, canEdit, edit, patch } = useTrip()
  const { settings } = useApp()
  const { toast } = useToast()
  const [dragId, setDragId] = useState<number | null>(null)
  const [over, setOver] = useState<string | null>(null)
  const [showRecorded, setShowRecorded] = useState(true)
  const [plan, setPlan] = useState(false)
  const today = todayISO()
  const unit = settings?.distance_unit || 'km'
  const days = dateRange(trip.start_date, trip.end_date)
  if (days.length === 0) return <Empty title="Set the trip dates first">The itinerary is one column per day; places, stays and transport fall into place by date.</Empty>
  const ideas = ov.places.filter(p => !p.day_date)
  const rowsFor = (d: string): Row[] => {
    const rows: Row[] = []
    for (const p of ov.places) if (p.day_date === d) rows.push({ key: `p${p.id}`, when: placeWhen(p), kind: 'place', icon: PLACE_ICONS[p.category] || '📍', title: p.name, sub: [p.duration_min ? fmtDuration(p.duration_min) : '', placeSub(p)].filter(Boolean).join(' · '), place: p, onClick: () => edit('place', p) })
    for (const t of ov.transport) if (t.depart_at.slice(0, 10) === d) rows.push({ key: `t${t.id}`, when: t.depart_at.slice(11, 16) || '00:00', kind: 'transport', icon: TRANSPORT_ICONS[t.type] || '➡️', title: t.name || `${t.from_name} → ${t.to_name}`, sub: [t.arrive_at && `arrives ${fmtDateTime(t.arrive_at, false)}`, transportSub(t)].filter(Boolean).join(' · '), onClick: () => edit('transport', t), cls: 'transport' })
    for (const l of ov.lodging) {
      if (l.check_in.slice(0, 10) === d) rows.push({ key: `li${l.id}`, when: stayTime(l.check_in, 'in', settings), kind: 'lodging-in', icon: LODGING_ICONS[l.type] || '🛏', title: `Check in · ${l.name}`, sub: lodgingSub(l), onClick: () => edit('lodging', l), cls: 'lodging' })
      if (l.check_out.slice(0, 10) === d) rows.push({ key: `lo${l.id}`, when: stayTime(l.check_out, 'out', settings), kind: 'lodging-out', icon: '🧳', title: `Check out · ${l.name}`, sub: l.address, onClick: () => edit('lodging', l), cls: 'lodging' })
    }
    if (showRecorded) {
      const tl = timeline.find(x => x.date === d)
      for (const e of tl?.entries || []) {
        // Dawarich stamps carry the trip's own offset; show that wall time rather than the browser's.
        const at = fmtClock(e.started_at.slice(11, 16)), end = fmtClock(e.ended_at.slice(11, 16))
        if (e.type === 'journey') rows.push({ key: `j${e.track_id}`, when: e.started_at.slice(11, 16), kind: 'journey', icon: e.dominant_mode === 'walking' ? '🚶' : e.dominant_mode === 'cycling' ? '🚲' : e.dominant_mode === 'driving' ? '🚗' : '🛰', title: `${e.dominant_mode ? e.dominant_mode[0].toUpperCase() + e.dominant_mode.slice(1) : 'Moved'} ${fmtKm(e.distance, unit)}`, sub: `${at}–${end} · ${fmtDuration(Math.round(e.duration / 60))}${e.elevation_gain ? ` · ↑${Math.round(e.elevation_gain)} m` : ''}`, cls: 'journey' })
        else rows.push({ key: `v${e.visit_id}`, when: e.started_at.slice(11, 16), kind: 'visit', icon: '📌', title: e.name && e.name !== 'Unknown Location' ? e.name : 'Stopped somewhere', sub: `${at}–${end} · ${fmtDuration(Math.round(e.duration / 60))}${e.status === 'suggested' ? ' · suggested' : ''}`, cls: 'visit' })
      }
    }
    return rows.sort((a, b) => a.when.localeCompare(b.when))
  }
  const drop = async (day: string, beforeKey?: string) => {
    if (dragId == null) return
    const current = ov.places.filter(p => p.day_date === day && p.id !== dragId).sort(itineraryOrder).map(p => p.id)
    const idx = beforeKey ? current.indexOf(Number(beforeKey.slice(1))) : -1
    if (idx >= 0) current.splice(idx, 0, dragId); else current.push(dragId)
    setDragId(null); setOver(null)
    try { patch({ places: await api.put<Place[]>(`${V1}/trips/${trip.id}/places/reorder`, { day, ids: current }) }) } catch (e) { toast((e as Error).message, 'err') }
  }
  const toggleVisited = async (p: Place) => {
    const status = p.status === 'visited' ? 'planned' : 'visited'
    try { const np = await api.put<Place>(`${V1}/trips/${trip.id}/places/${p.id}`, { status }); patch({ places: ov.places.map(x => x.id === p.id ? np : x) }) } catch (e) { toast((e as Error).message, 'err') }
  }
  const hasRecorded = timeline.length > 0
  return (
    <div className="itin-grid">
      {plan && <PlanModal onClose={() => setPlan(false)} />}
      <Card flush>
        <div className="pad" style={{ paddingBottom: 0 }}>
          <div className="row wrap" style={{ marginBottom: 6 }}>
            <span className="muted small">{days.length} days · drag places between days to reschedule · tick when visited</span>
            <span className="spacer" />
            {canEdit && ov.integrations.ai.configured && <button type="button" className="btn sm" onClick={() => setPlan(true)}><Icon name="sparkles" size={14} />Plan my days</button>}
            {hasRecorded && <label className="row small muted" style={{ gap: 4 }}><input type="checkbox" checked={showRecorded} onChange={e => setShowRecorded(e.target.checked)} style={{ width: 'auto' }} /> show Dawarich</label>}
          </div>
        </div>
        <div className="pad" style={{ paddingTop: 0 }}>
          {days.map((d, i) => {
            const rows = rowsFor(d)
            const wx = weather.find(w => w.date === d)
            const km = route?.days_km?.[d]
            return (
              <div key={d} className={`day ${d === today ? 'today' : ''}`}>
                <div className="day-head">
                  <h3>Day {i + 1}{d === today && ' · today'}</h3>
                  <div className="muted">{weekday(d, true)}, {fmtDate(d, false)}</div>
                  {(wx || km) && <div className="wx">{wx && <span><span className="big">{WX_ICON(wx.code)}</span> {Math.round(wx.tmax)}° <span className="muted">/ {Math.round(wx.tmin)}°</span></span>}{km != null && km > 0 && <span className="muted" title="Recorded by Dawarich"><Icon name="route" size={13} />{fmtKm(km, unit)}</span>}</div>}
                </div>
                <div className={`day-items ${over === d ? 'over' : ''}`} onDragOver={e => { if (dragId != null) { e.preventDefault(); setOver(d) } }} onDragLeave={() => setOver(o => o === d ? null : o)} onDrop={e => { e.preventDefault(); drop(d) }}>
                  {rows.length === 0 && <div className="item ghost" onClick={() => canEdit && edit('place', null, { day_date: d })}>Free day{canEdit && ' — click to add something or drop a place here'}</div>}
                  {rows.map(r => (
                    <div key={r.key} className={`item ${r.kind === 'journey' || r.kind === 'visit' ? 'recorded' : ''} ${dragId != null && r.place?.id === dragId ? 'dragging' : ''}`} draggable={canEdit && !!r.place} onDragStart={() => r.place && setDragId(r.place.id)} onDragEnd={() => { setDragId(null); setOver(null) }}
                      onDragOver={e => { if (dragId != null && r.place) { e.preventDefault(); e.stopPropagation(); setOver(d) } }} onDrop={e => { if (r.place) { e.preventDefault(); e.stopPropagation(); drop(d, r.key) } }} onClick={r.onClick}>
                      <span className="time">{r.when.startsWith('99:') ? '' : fmtClock(r.when)}</span>
                      <span className={`ico ${r.cls || ''} ${r.place?.status === 'visited' ? 'visited' : ''}`}>{r.icon}</span>
                      <span className="txt"><span className="title">{r.title}{r.place?.status === 'booked' && <span className="badge warn">booked</span>}{r.place?.status === 'skipped' && <span className="badge">skipped</span>}</span>{r.sub && <span className="sub">{r.sub}</span>}</span>
                      {r.place && canEdit && <span className={`check ${r.place.status === 'visited' ? 'on' : ''}`} title={r.place.status === 'visited' ? 'Visited' : 'Mark visited'} onClick={e => { e.stopPropagation(); toggleVisited(r.place!) }}>{r.place.status === 'visited' ? <Icon name="check" size={14} strokeWidth={2.6} /> : null}</span>}
                    </div>
                  ))}
                  {rows.length > 0 && canEdit && <div className="drop" />}
                </div>
              </div>
            )
          })}
        </div>
      </Card>
      <div>
        <Card title={`Ideas · ${ideas.length}`} actions={canEdit && <button type="button" className="btn ghost sm" onClick={() => edit('place', null, {})}><Icon name="plus" size={14} />Add</button>}>
          {ideas.length === 0 ? <p className="muted small">Every place has a day. Add more ideas from the Places tab or Discover on the map.</p> : (
            <div className="day-items" onDragOver={e => { if (dragId != null) { e.preventDefault(); setOver('') } }} onDrop={e => { e.preventDefault(); drop('') }}>
              {ideas.map(p => <div key={p.id} className={`item ${dragId === p.id ? 'dragging' : ''}`} draggable={canEdit} onDragStart={() => setDragId(p.id)} onDragEnd={() => { setDragId(null); setOver(null) }} onClick={() => edit('place', p)}><span className="ico">{PLACE_ICONS[p.category] || '📍'}</span><span className="txt"><span className="title">{p.name}</span><span className="sub">{placeSub(p) || p.category}</span></span>{canEdit && <span className="drag-handle"><Icon name="grip" size={16} strokeWidth={2.8} /></span>}</div>)}
            </div>
          )}
          <p className="muted small mt-sm">Drag an idea onto a day to schedule it; drop a scheduled place here to unschedule.</p>
        </Card>
      </div>
    </div>
  )
}
