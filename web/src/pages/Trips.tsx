import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { SearchHit, Trip } from '../lib/types'
import { LODGING_ICONS, PLACE_ICONS, STATUS_LABEL } from '../lib/types'
import { describeDates, hitLink, matches, useSearch } from '../lib/search'
import { countryName, flag } from '../lib/countries'
import { daysBetween, fmtRange, plural, todayISO } from '../lib/format'
import { Empty, Loading, PageHeader, Segmented, useAsync } from '../components/ui'
import { Money } from '../components/Money'
import { TripModal } from './trip/TripSettings'
import { Icon } from '../components/icons'
import { useApp } from '../lib/app-context'
import { FoundTrips } from './FoundTrips'

type Filter = 'all' | 'upcoming' | 'past' | 'archived'

export default function Trips() {
  const [params, setParams] = useSearchParams()
  const [filter, setFilter] = useState<Filter>('all')
  const [q, setQ] = useState('')
  const [creating, setCreating] = useState(params.get('new') === '1')
  const navigate = useNavigate()
  const { reloadTrips } = useApp()
  const { data, loading, error, reload } = useAsync(() => api.get<Trip[]>(`${V1}/trips?archived=${filter === 'archived' ? '1' : '0'}`), [filter])
  useEffect(() => { if (params.get('new') === '1') { setCreating(true); params.delete('new'); setParams(params, { replace: true }) } }, [params, setParams])
  const today = todayISO()
  const { result, busy } = useSearch(q)
  const found = result ? new Map(result.trips.map(t => [t.id, t])) : null
  const dates = describeDates(result)
  const list = (data || []).filter(t => {
    // the server search ignores accents and understands "summer 2025"; until it answers, filter locally
    if (q && (found ? !found.has(t.id) : !matches(q, `${t.name} ${t.destination}`))) return false
    if (filter === 'upcoming') return !t.start_date || t.end_date >= today
    if (filter === 'past') return t.end_date && t.end_date < today
    return true
  })
  return (
    <>
      <PageHeader title="Trips" sub={data ? plural(data.length, 'trip') : ''} actions={<button type="button" className="btn primary" onClick={() => setCreating(true)}><Icon name="plus" size={15} />New trip</button>} />
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search trips, places, stays, “summer 2025”…" aria-label="Search trips" /></div>
        <Segmented value={filter} onChange={setFilter} options={[{ value: 'all', label: 'All' }, { value: 'upcoming', label: 'Upcoming' }, { value: 'past', label: 'Past' }, { value: 'archived', label: 'Archived' }]} size="sm" label="Show" />
      </div>
      {q && dates && <div className="hint-strip">Trips in <b>{dates}</b>{result?.query.words?.length ? <> matching “{result.query.words.join(' ')}”</> : null}{busy && <span className="muted"> · searching…</span>}</div>}
      {!q && (filter === 'all' || filter === 'past') && <FoundTrips onAdded={reload} />}
      {loading && !data ? <Loading /> : error ? <div className="callout danger">{error}</div> : list.length === 0 ? (
        <Empty icon="luggage" title={q ? 'No trips match' : filter === 'archived' ? 'Nothing archived' : 'No trips yet'}>{!q && filter !== 'archived' && 'Create your first trip, or import your collections from AdventureLog under Settings → Import.'}</Empty>
      ) : (
        <div className="trip-grid">
          {list.map(t => <TripCard key={t.id} t={t} hits={found?.get(t.id)?.hits} />)}
        </div>
      )}
      {creating && <TripModal onClose={() => setCreating(false)} onSaved={t => { setCreating(false); reload(); reloadTrips(); navigate(`/trips/${t.id}`) }} />}
    </>
  )
}

export function TripCard({ t, hits }: { t: Trip; hits?: SearchHit[] }) {
  const navigate = useNavigate()
  const today = todayISO()
  const running = t.start_date && t.start_date <= today && t.end_date >= today
  const until = t.start_date > today ? daysBetween(today, t.start_date) : 0
  const style = { '--c': t.color || '#0284c7', backgroundImage: t.has_cover ? `url(${V1}/trips/${t.id}/cover)` : undefined } as React.CSSProperties
  const c = t.counts
  return (
    <Link to={`/trips/${t.id}`} className="trip-card">
      <div className="cover" style={style}>
        <span className="badge">{running ? `Day ${daysBetween(t.start_date, today) + 1} of ${t.days}` : until ? `in ${until} days` : STATUS_LABEL[t.status]}</span>
        {t.days > 0 && <span className="days">{plural(t.days, 'day')}</span>}
      </div>
      <div className="body">
        <h3>{t.name}{t.countries && t.countries.some(c => c.days > 0) && <span className="card-flags" title={t.countries.filter(c => c.days > 0).map(c => `${countryName(c.code)} ${c.days}d`).join(', ')}>{t.countries.filter(c => c.days > 0).map(c => flag(c.code)).join('')}</span>}</h3>
        <div className="muted small">{t.destination || '—'} · {fmtRange(t.start_date, t.end_date)}</div>
        {c && <div className="facts">
          <span title="Places (visited)"><Icon name="pin" size={13} />{c.visited > 0 ? `${c.visited}/${c.places}` : c.places}</span>
          {c.lodging > 0 && <span title="Stays"><Icon name="bed" size={13} />{c.lodging}</span>}
          {c.transport > 0 && <span title="Transport legs"><Icon name="plane" size={13} />{c.transport}</span>}
          {c.checklist_items > 0 && <span title="Checklists"><Icon name="checklist" size={13} />{c.checklist_done}/{c.checklist_items}</span>}
          {(c.total > 0 || c.planned > 0) && <span className="spacer right" title={c.total > 0 ? "Spent plus booked stays and transport" : "Planned"}><Money minor={c.total > 0 ? c.total : c.planned} code={t.currency} compact />{c.total === 0 && <span className="muted"> planned</span>}</span>}
        </div>}
        {hits && hits.length > 0 && <div className="search-hits">{hits.slice(0, 4).map(h => <span key={h.kind + h.id} role="link" tabIndex={0} className="search-hit" title={h.kind === 'lodging' ? 'Stay' : 'Place'} onClick={e => { e.preventDefault(); e.stopPropagation(); navigate(hitLink(h)) }}>{h.kind === 'lodging' ? LODGING_ICONS[h.category] || '🛏' : PLACE_ICONS[h.category] || '📍'} {h.name}</span>)}{hits.length > 4 && <span className="muted small">+{hits.length - 4} more</span>}</div>}
        {t.budget > 0 && c && <div className={`budget-bar ${c.actual > t.budget ? 'over' : ''}`} style={{ height: 6 }}><div className="actual" style={{ width: `${Math.min(100, c.actual / t.budget * 100)}%` }} /><div className="planned" style={{ width: `${Math.max(0, Math.min(100 - c.actual / t.budget * 100, (c.planned - c.actual) / t.budget * 100))}%` }} /></div>}
      </div>
    </Link>
  )
}
