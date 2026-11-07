import { Link } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Activity, Lodging, Place, Transport } from '../../lib/types'
import { LODGING_ICONS, PLACE_ICONS, TRANSPORT_ICONS, WX_ICON } from '../../lib/types'
import { ago, daysBetween, fmtClock, fmtDate, fmtDateTime, fmtKm, plural, todayISO, weekday } from '../../lib/format'
import { Card, useAsync } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Money } from '../../components/Money'
import { useTrip } from './ctx'
import { RecapModal } from './ai'
import { PocketPlacesCard } from './PocketPlaces'
import { PocketWalletCard } from './PocketWallet'
import { PocketTrailsCard } from './PocketTrails'
import { useState } from 'react'

const regionNames = typeof Intl !== 'undefined' && 'DisplayNames' in Intl ? new Intl.DisplayNames(undefined, { type: 'region' }) : null
/** A country's name from its ISO code, in the browser's language. */
const countryName = (code: string) => { try { return regionNames?.of(code) || code } catch { return code } }

export function OverviewTab() {
  const { ov, trip, weather, route, timeline, photos, extErrors, refreshExternal, canEdit, edit, go } = useTrip()
  const { settings } = useApp()
  const [recap, setRecap] = useState(false)
  const { data: activity } = useAsync(() => api.get<Activity[]>(`${V1}/trips/${trip.id}/activity?limit=8`), [trip.id, trip.updated_at])
  const today = todayISO()
  const b = ov.budget
  const running = trip.start_date && trip.start_date <= today && trip.end_date >= today
  const past = trip.end_date && trip.end_date < today
  const until = trip.start_date > today ? daysBetween(today, trip.start_date) : 0
  // Next up: the next scheduled thing from now.
  const nowStamp = `${today}T${String(new Date().getHours()).padStart(2, '0')}:${String(new Date().getMinutes()).padStart(2, '0')}`
  const upcoming: { when: string; label: string; sub: string; icon: string; onClick: () => void }[] = []
  for (const p of ov.places) if (p.day_date && p.status !== 'visited' && p.status !== 'skipped') upcoming.push({ when: p.day_date + 'T' + (p.start_time || '23:59'), label: p.name, sub: `${fmtDate(p.day_date, false)}${p.start_time ? ' · ' + fmtClock(p.start_time) : ''}`, icon: PLACE_ICONS[p.category] || '📍', onClick: () => edit('place', p) })
  for (const t of ov.transport) if (t.depart_at) upcoming.push({ when: t.depart_at, label: t.name || `${t.from_name} → ${t.to_name}`, sub: fmtDateTime(t.depart_at, false), icon: TRANSPORT_ICONS[t.type] || '➡️', onClick: () => edit('transport', t) })
  for (const l of ov.lodging) if (l.check_in) upcoming.push({ when: l.check_in.slice(0, 10) + 'T15:00', label: `Check in · ${l.name}`, sub: fmtDate(l.check_in, false), icon: LODGING_ICONS[l.type] || '🛏', onClick: () => edit('lodging', l) })
  const next = upcoming.filter(u => u.when >= nowStamp).sort((a, b) => a.when.localeCompare(b.when)).slice(0, 4)
  const stay = ov.lodging.find(l => l.check_in.slice(0, 10) <= today && today < l.check_out.slice(0, 10))
  const visited = ov.places.filter(p => p.status === 'visited').length
  const ideas = ov.places.filter(p => !p.day_date).length
  const listsTotal = ov.checklists.reduce((n, c) => n + c.items.length, 0), listsDone = ov.checklists.reduce((n, c) => n + c.items.filter(i => i.done).length, 0)
  const recordedKm = route?.distance_km || 0
  const wxToday = weather.find(w => w.date === today)
  const wx = weather.slice(0, 16)
  const unit = settings?.distance_unit || 'km'
  const integ = ov.integrations
  return (
    <div className="overview-grid">
      <Card title={running ? 'Right now' : past ? 'How it went' : 'Countdown'}>
        {running && <><div className="countdown">{daysBetween(trip.start_date, today) + 1}<small>of {trip.days} days</small></div>
          {stay && <p className="mt-sm">Staying at <b>{stay.name}</b>{stay.check_out && <span className="muted"> · check-out {fmtDate(stay.check_out, false)}</span>}</p>}
          {wxToday && <p className="muted">{WX_ICON(wxToday.code)} {wxToday.label}, {Math.round(wxToday.tmin)}–{Math.round(wxToday.tmax)}°{wxToday.precip_mm > 0.5 ? ` · ${wxToday.precip_mm.toFixed(0)} mm rain` : ''}</p>}</>}
        {!running && until > 0 && <><div className="countdown">{until}<small>days to go</small></div><p className="muted mt-sm">{fmtDate(trip.start_date)} · {weekday(trip.start_date, true)}</p></>}
        {!running && !until && past && <><div className="countdown">{trip.days}<small>days</small></div><p className="muted mt-sm">{visited} of {ov.places.length} places visited{recordedKm > 0 && ` · ${fmtKm(recordedKm, unit)} recorded`}{photos.length > 0 && ` · ${plural(photos.length, 'photo')}`}</p></>}
        {!running && !until && !past && <p className="muted">Set the dates to get a countdown, weather and the recorded route.</p>}
        {next.length > 0 && <div className="mt"><div className="muted small mb-sm">Next up</div><div className="day-items">{next.map((n, i) => <div key={i} className="item" onClick={n.onClick}><span className="ico">{n.icon}</span><span className="txt"><span className="title">{n.label}</span><span className="sub">{n.sub}</span></span></div>)}</div></div>}
      </Card>
      <Card title="Budget" actions={<button type="button" className="btn ghost sm" onClick={() => go('budget')}>Details</button>}>
        <div className="row" style={{ alignItems: 'baseline', gap: 6 }}><span style={{ fontSize: 26, fontWeight: 700 }}><Money minor={b.total > 0 ? b.total : b.planned} code={b.currency} /></span><span className="muted small">{b.total > 0 ? 'total' : 'planned'}{trip.budget > 0 && <> of <Money minor={trip.budget} code={b.currency} /></>}</span></div>
        {b.total > 0 && b.bookings > 0 && <div className="muted small"><Money minor={b.actual} code={b.currency} /> spent + <Money minor={b.bookings} code={b.currency} /> booked stays & transport not marked paid</div>}
        {trip.budget > 0 && <div className={`budget-bar mt-sm ${b.actual > trip.budget ? 'over' : ''}`}><div className="actual" style={{ width: `${Math.min(100, b.actual / trip.budget * 100)}%` }} /><div className="planned" style={{ width: `${Math.max(0, Math.min(100 - b.actual / trip.budget * 100, (b.planned - b.actual) / trip.budget * 100))}%` }} /></div>}
        <div className="kv mt-sm">
          {b.actual > 0 && <><dt>Planned</dt><dd><Money minor={b.planned} code={b.currency} /></dd></>}
          {trip.budget > 0 && <><dt>Remaining</dt><dd className={b.remaining < 0 ? 'bad' : ''}><Money minor={b.remaining} code={b.currency} /></dd></>}
          {b.per_day > 0 && <><dt>Per day</dt><dd><Money minor={b.per_day} code={b.currency} />{b.days_elapsed > 0 && <span className="muted small"> over {b.days_elapsed} days</span>}</dd></>}
          {integ.firefly.configured && <><dt>Pocket Money</dt><dd>{trip.firefly_tag ? <>{b.firefly_count} transactions tagged <code>{trip.firefly_tag}</code>{integ.firefly.synced_at && <span className="muted small"> · synced {ago(integ.firefly.synced_at)}</span>}</> : <span className="muted">no tag linked — <a onClick={() => edit('trip', trip)} href="#" >set one</a></span>}</dd></>}
        </div>
        {b.by_category.length > 0 && <div className="chips mt-sm">{b.by_category.slice(0, 5).map(c => <span key={c.category} className="chip sm">{c.category} <Money minor={c.actual || c.planned} code={b.currency} compact /></span>)}</div>}
      </Card>
      <Card title="Progress" actions={canEdit && <button type="button" className="btn ghost sm" onClick={() => edit('place', null, {})}><Icon name="plus" size={14} />Place</button>}>
        <div className="stats" style={{ marginBottom: 8 }}>
          <a className="stat" onClick={() => go('places')}><div className="n">{ov.places.length}</div><div className="l">places · {ideas} unscheduled</div></a>
          <a className="stat" onClick={() => go('places')}><div className="n">{visited}</div><div className="l">visited</div></a>
          <a className="stat" onClick={() => go('lists')}><div className="n">{listsTotal ? Math.round(listsDone / listsTotal * 100) : 0}%</div><div className="l">checklists · {listsDone}/{listsTotal}</div></a>
          <a className="stat" onClick={() => go('stay')}><div className="n">{ov.lodging.length}<span className="muted" style={{ fontSize: 14 }}> / {ov.transport.length}</span></div><div className="l">stays / legs</div></a>
        </div>
        {ov.lodging.length === 0 && !past && <p className="muted small">No stay yet. <a href="#" onClick={e => { e.preventDefault(); edit('lodging', null, {}) }}>Add where you sleep</a>.</p>}
        {ov.transport.length === 0 && !past && <p className="muted small">No transport yet. <a href="#" onClick={e => { e.preventDefault(); edit('transport', null, {}) }}>Add the flight, train or drive</a>.</p>}
      </Card>
      {wx.length > 0 && <Card title={`Weather${trip.destination ? ' · ' + trip.destination : ''}`} className="span2">
        <div className="wx-strip">{wx.map(w => <div key={w.date} className={`wx-day ${w.date === today ? 'today' : ''}`} title={`${w.label}${w.precip_mm ? ` · ${w.precip_mm.toFixed(1)} mm` : ''} · wind ${Math.round(w.wind_kmh)} km/h`}><div className="muted">{weekday(w.date)} {fmtDate(w.date, false)}</div><div className="big">{WX_ICON(w.code)}</div><div><b>{Math.round(w.tmax)}°</b> <span className="muted">{Math.round(w.tmin)}°</span></div>{w.precip_mm >= 1 && <div className="muted row" style={{ justifyContent: 'center', gap: 2 }}><Icon name="droplet" size={11} />{w.precip_mm.toFixed(0)}</div>}</div>)}</div>
        <p className="muted small mt-sm" style={{ margin: '6px 0 0' }}>Open-Meteo · {wx.some(w => w.source === 'archive') ? 'recorded weather for past days, ' : ''}forecast up to 16 days ahead.</p>
      </Card>}
      {(integ.dawarich.configured && trip.dawarich) && <Card title={`Recorded by ${(route?.source || integ.dawarich.source) === 'movement' ? 'Pocket Movement' : 'Dawarich'}`} actions={<button type="button" className="btn ghost sm" onClick={refreshExternal}><Icon name="refresh" size={14} />Refresh</button>}>
        {extErrors.dawarich ? <p className="bad small">{extErrors.dawarich}</p> : !route ? <p className="muted">Loading…</p> : route.raw_count === 0 ? <p className="muted">No location points in this date range{!past && !running ? ' yet' : ''}.</p> : (
          <>
            <div className="stats" style={{ marginBottom: 8 }}>
              <div className="stat"><div className="n">{fmtKm(route.distance_km, unit)}</div><div className="l">travelled</div></div>
              <div className="stat"><div className="n">{Object.keys(route.days_km).length}</div><div className="l">days tracked</div></div>
              <div className="stat"><div className="n">{timeline.reduce((n, d) => n + d.entries.filter(e => e.type === 'visit').length, 0)}</div><div className="l">stops detected</div></div>
            </div>
            {route.countries && route.countries.length > 0 && <p className="small" style={{ margin: '0 0 8px' }}><span className="muted">Countries </span>{route.countries.map(c => `${countryName(c.code)}${route.countries!.length > 1 ? ` (${c.days} d)` : ''}`).join(' · ')}</p>}
            <div className="timeline-mini">{timeline.slice(-5).reverse().map(d => <div key={d.date} className="row"><span className="muted" style={{ width: 60 }}>{fmtDate(d.date, false)}</span><span>{fmtKm(d.summary.total_distance, unit)}</span><span className="muted">{Object.entries(d.summary.mode_distances || {}).filter(([, v]) => v >= 0.5).map(([m, v]) => `${m} ${fmtKm(v, unit)}`).join(' · ')}</span></div>)}</div>
            <button type="button" className="btn ghost sm mt-sm" onClick={() => go('map')}>See the route on the map</button>
          </>
        )}
      </Card>}
      {(integ.immich.configured && trip.immich) && <Card title="Photos" actions={<button type="button" className="btn ghost sm" onClick={() => go('photos')}>All {photos.length ? photos.length : ''}</button>}>
        {extErrors.immich ? <p className="bad small">{extErrors.immich}</p> : photos.length === 0 ? <p className="muted">No photos from Immich in this date range{!past && !running ? ' yet' : ''}.</p> : (
          <div className="photo-grid sm">{photos.slice(0, 12).map(p => <a key={p.id} href={`${integ.immich.url}/photos/${p.id}`} target="_blank" rel="noreferrer"><img src={`${V1}/trips/${trip.id}/photos/${p.id}/thumb`} loading="lazy" alt="" />{p.type === 'VIDEO' && <span className="vid"><Icon name="play" size={10} /></span>}</a>)}</div>
        )}
      </Card>}
      {integ.ai.configured && (past || running) && <Card title={<><Icon name="sparkles" size={16} />Recap</>}>
        <p className="muted small">Turn the visited places, journal entries, spending and recorded distance into a story, a factual summary or a caption — and keep it in the journal.</p>
        <button type="button" className="btn sm" onClick={() => setRecap(true)}>Write a recap</button>
        {recap && <RecapModal onClose={() => setRecap(false)} />}
      </Card>}
      {activity && activity.length > 0 && <Card title="Recent changes">
        <ul className="activity">{activity.map(a => <li key={a.id}><span className="muted" style={{ width: 70, flexShrink: 0 }}>{ago(a.created_at)}</span><span>{a.user_name && <b>{a.user_name.split(' ')[0]} </b>}{a.action} <i>{a.subject}</i></span></li>)}</ul>
      </Card>}
      {trip.description && <Card title="About this trip" className="span2"><p style={{ whiteSpace: 'pre-wrap' }}>{trip.description}</p>{trip.link && <a href={trip.link} target="_blank" rel="noreferrer">{trip.link}</a>}</Card>}
      {trip.notes && <Card title="Notes" className="span2"><p style={{ whiteSpace: 'pre-wrap' }}>{trip.notes}</p></Card>}
      {!integ.firefly.configured && !integ.dawarich.configured && !integ.immich.configured && <Card title="Connect your other apps" className="span2"><p className="muted">Connect <b>Pocket Money</b> to see what the trip really cost, <b>Pocket Movement</b> (through Pocket Core) or <b>Dawarich</b> for the route you actually took, and <b>Immich</b> for the photos — all under <Link to="/settings/integrations">Settings → Integrations</Link>.</p></Card>}
      {/* last, so they never push the cards above them while they load */}
      <PocketWalletCard />
      <PocketTrailsCard />
      <PocketPlacesCard />
    </div>
  )
}

export function placeSub(p: Place): string { return p.address || (p.lat != null ? `${p.lat.toFixed(4)}, ${p.lng?.toFixed(4)}` : '') }
export function lodgingSub(l: Lodging): string { return [l.address, l.reservation_number && `#${l.reservation_number}`].filter(Boolean).join(' · ') }
export function transportSub(t: Transport): string { return [t.carrier, t.number, t.booking_ref && `ref ${t.booking_ref}`, t.seat && `seat ${t.seat}`].filter(Boolean).join(' · ') }
