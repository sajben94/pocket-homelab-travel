import { Link } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { PocketTrails, PTOuting } from '../../lib/types'
import { fmtClock, fmtDate, fmtKm } from '../../lib/format'
import { Card, useAsync } from '../../components/ui'
import { Icon } from '../../components/icons'
import { NeedsCore } from '../../components/NeedsCore'
import { useTrip } from './ctx'

const STATUS: Record<string, string> = { planned: 'Planned', out: 'Out now', done: 'Done', cancelled: 'Called off' }

/**
 * Outdoors: the trip's hikes and rides from Pocket Trails — the ones filed
 * with the trip there and the ones during its dates, by date and time, each
 * opening in Trails. Loads on its own, so the rest of the page never waits
 * for it; without Pocket Core or with the link off it stays in place,
 * greyed, and says what it needs.
 */
export function PocketTrailsCard() {
  const { trip } = useTrip()
  const { settings } = useApp()
  const unit = settings?.distance_unit || 'km'
  const { data, error, loading, reload } = useAsync(() => api.get<PocketTrails>(`${V1}/trips/${trip.id}/pocket-trails`),
    [trip.id, trip.start_date, trip.end_date])
  const pt = data
  const ok = pt?.status === 'ok'
  const base = pt?.trails_url || ''
  const open = ok && pt.outings.length > 0 ? <a className="btn ghost sm" href={`${base}/outings`} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open in Pocket Trails</a> : null
  return (
    <Card title={<><Icon name="mountain" size={16} />Outdoors</>} className={`span2 pp-card ${pt && !ok ? 'pp-dim' : ''}`} actions={open}
      sub="From your Pocket Trails: the hikes and rides filed with this trip or planned during it.">
      {!pt && loading ? <p className="muted pp-wait">Looking in Pocket Trails…</p>
        : !pt ? <p className="muted pp-wait">Pocket Trails could not be reached{error ? `: ${error}` : ''}. <button type="button" className="link-btn" onClick={() => reload()}>Try again</button></p>
        : pt.status === 'no_core' ? <NeedsCore what="Pocket Trails" />
        : pt.status === 'off' ? <p className="muted pp-wait">Pocket Trails is switched off for you. Connect Pocket Trails in <Link to="/settings/integrations">Settings → Integrations</Link>.</p>
        : pt.status === 'unavailable' ? <p className="muted pp-wait">Pocket Trails is not connected{pt.detail ? `: ${pt.detail}` : ''}. Connect Pocket Trails in <Link to="/settings/integrations">Settings → Integrations</Link>.</p>
        : pt.status === 'error' ? <p className="muted pp-wait">Pocket Trails did not answer{pt.detail ? `: ${pt.detail}` : ''}. <button type="button" className="link-btn" onClick={() => reload()} disabled={loading}>Try again</button></p>
        : pt.outings.length === 0 ? <p className="muted pp-wait">No hikes or rides for this trip yet. <a href={`${base}/outings`} target="_blank" rel="noreferrer">Plan one in Pocket Trails</a></p>
        : <div className="pp-rows">{pt.outings.map(o => <OutingRow key={o.id} o={o} base={base} unit={unit} />)}</div>}
    </Card>
  )
}

function OutingRow({ o, base, unit }: { o: PTOuting; base: string; unit: 'km' | 'mi' }) {
  const time = o.start_time && o.start_time.length >= 5 ? fmtClock(o.start_time.slice(0, 5)) : ''
  const climb = o.gain_m >= 1 ? (unit === 'mi' ? `${Math.round(o.gain_m * 3.28084)} ft up` : `${Math.round(o.gain_m)} m up`) : ''
  const sub = [o.activity ? o.activity[0].toUpperCase() + o.activity.slice(1) : '', o.route_name && o.route_name !== o.title ? o.route_name : ''].filter(Boolean).join(' · ')
  const icon = ['hike', 'walk', 'climb', 'ski', 'snowshoe', 'trail'].some(a => o.activity?.includes(a)) || !o.activity ? 'mountain' : 'route'
  const cls = o.status === 'out' ? 'pt-out' : o.status === 'done' ? 'pt-done' : ''
  return (
    <a className={`pp-row pt-row ${o.status === 'cancelled' ? 'pt-off' : ''}`} href={`${base}/outings/${o.id}`} target="_blank" rel="noreferrer" title="Open in Pocket Trails">
      <span className="pp-when">{o.date ? <>{fmtDate(o.date, false)}{time && <span className="muted"> {time}</span>}</> : <span className="muted">No date</span>}</span>
      <span className="pp-ico" aria-hidden="true"><Icon name={icon} size={14} /></span>
      <span className="pp-txt"><span className="pp-name">{o.title || o.route_name || 'Outing'}</span>{sub && <span className="pp-sub">{sub}</span>}</span>
      <span className="pp-meta">
        {o.distance_m > 0 && <span>{fmtKm(o.distance_m / 1000, unit)}</span>}
        {climb && <span>{climb}</span>}
        <span className={`pt-status ${cls}`}>{STATUS[o.status] || o.status}</span>
      </span>
    </a>
  )
}
