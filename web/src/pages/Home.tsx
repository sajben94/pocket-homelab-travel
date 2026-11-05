import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { HomeData, OnThisDay as Day } from '../lib/types'
import { countryName, flag } from '../lib/countries'
import { PLACE_ICONS, WX_ICON } from '../lib/types'
import { daysBetween, fmtClock, fmtDate, fmtRange, itineraryOrder, plural, todayISO, weekday } from '../lib/format'
import { Card, Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { Money } from '../components/Money'
import { MapView, type Marker } from '../components/Map'
import { TripCard } from './Trips'
import { Icon } from '../components/icons'

export default function Home() {
  const { user } = useApp()
  const navigate = useNavigate()
  const { data, error, loading } = useAsync(() => api.get<HomeData>(`${V1}/home`), [])
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return null
  const { current, next, upcoming, recent, today, journal, pins, hero_weather: wx } = data
    const hour = new Date().getHours()
  const greet = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'
  const first = (user?.name || '').split(' ')[0] || 'traveller'
  const hero = current || next
  const until = next ? daysBetween(todayISO(), next.start_date) : 0
  const heroStyle = hero?.has_cover ? { backgroundImage: `url(${V1}/trips/${hero.id}/cover?v=${hero.updated_at})` } : { background: `linear-gradient(135deg, ${hero?.color || '#0284c7'} 0%, #0f172a 100%)` }
  const markers: Marker[] = pins.map(p => ({ id: p.stay_id ? `s${p.stay_id}` : `t${p.id}`, lat: p.lat, lng: p.lng, label: p.stay ? `${p.name} · ${p.stay}` : p.name, icon: p.stay ? '🛏' : p.status === 'completed' ? '✓' : p.status === 'active' ? '✈' : '🧳', kind: p.status === 'completed' ? 'visited' : p.status === 'planning' ? 'idea' : 'booked',
    popup: p.stay ? `<b>${esc(p.name)}</b><br>🛏 ${esc(p.stay)}<br>${fmtRange(p.stay_from || p.start_date, p.stay_to || p.end_date)}` : `<b>${esc(p.name)}</b><br>${fmtRange(p.start_date, p.end_date)}`, onClick: () => navigate(`/trips/${p.id}`) }))
  const c = next?.counts
  return (
    <>
      <PageHeader title={`${greet}, ${first}`} sub={current ? `You are on the road.` : next ? `Next up in ${until} days.` : 'Where to next?'} actions={<Link className="btn primary" to="/trips?new=1"><Icon name="plus" size={15} />New trip</Link>} />

      {hero ? (
        <div className={`hero home-hero ${hero.has_cover ? 'has-cover' : ''}`} style={heroStyle} onClick={() => navigate(`/trips/${hero.id}`)}>
          <div className="hero-body">
            <div>
              <div className="row" style={{ gap: 8, marginBottom: 6 }}><span className="status-pill">{current && today ? `Day ${today.day_number} of ${hero.days}` : `In ${until} days`}</span>{wx && <span className="status-pill">{WX_ICON(wx.code)} {Math.round(wx.tmax)}° / {Math.round(wx.tmin)}° {current ? 'today' : 'on arrival'}</span>}</div>
              <h1>{hero.name}</h1>
              <div className="meta-line">{hero.destination && <span><Icon name="pin" size={14} />{hero.destination}</span>}<span><Icon name="calendar" size={14} />{fmtRange(hero.start_date, hero.end_date)}</span><span>{plural(hero.days, 'day')}</span></div>
            </div>
            <div className="row" onClick={e => e.stopPropagation()}>
              <Link className="btn glass" to={`/trips/${hero.id}/itinerary`}><Icon name="calendar" size={15} />Itinerary</Link>
              <Link className="btn glass" to={`/trips/${hero.id}/budget`}><Icon name="wallet" size={15} />Budget</Link>
              <Link className="btn glass" to={`/trips/${hero.id}/lists`}><Icon name="checklist" size={15} />Checklists</Link>
            </div>
          </div>
        </div>
      ) : null}

      {current && today && (
        <div className="overview-grid mb">
          <Card title="Today" actions={<Link className="btn ghost sm" to={`/trips/${current.id}/itinerary`}>Full day</Link>}>
            {today.places.length === 0 ? <p className="muted">Nothing scheduled — a free day, or drag some ideas onto it.</p> : (
              <div className="day-items">{[...today.places].sort(itineraryOrder).map(p => <Link key={p.id} className="item" to={`/trips/${current.id}/itinerary`} style={{ color: 'inherit' }}><span className="time">{fmtClock(p.start_time)}</span><span className={`ico ${p.status === 'visited' ? 'visited' : ''}`}>{PLACE_ICONS[p.category] || '📍'}</span><span className="txt"><span className="title">{p.name}</span><span className="sub">{p.address || p.notes}</span></span></Link>)}</div>
            )}
            {today.stay && <p className="muted small mt-sm">Sleeping at <b>{today.stay.name}</b>{today.stay.check_out && <> · check-out {fmtDate(today.stay.check_out, false)}</>}</p>}
          </Card>
          <Card title="Spending so far" actions={<Link className="btn ghost sm" to={`/trips/${current.id}/budget`}>Budget</Link>}>
            <div style={{ fontSize: 28, fontWeight: 700 }}><Money minor={today.budget.total} code={today.budget.currency} /></div>
            {today.budget.budget > 0 && <>
              <div className={`budget-bar mt-sm ${today.budget.actual > today.budget.budget ? 'over' : ''}`}><div className="actual" style={{ width: `${Math.min(100, today.budget.actual / today.budget.budget * 100)}%` }} /></div>
              <div className="small muted mt-sm"><Money minor={today.budget.remaining} code={today.budget.currency} /> left · ~<Money minor={today.budget.per_day} code={today.budget.currency} /> a day</div>
            </>}
          </Card>
        </div>
      )}

      {!current && next && c && (
        <div className="overview-grid mb">
          <Card title="Getting ready">
            <div className="kv">
              <dt>Places</dt><dd>{c.places} collected{c.places > 0 && <>, {c.places - (next.counts?.visited || 0)} to schedule</>}</dd>
              <dt>Stays</dt><dd>{c.lodging > 0 ? plural(c.lodging, 'stay') + ' booked' : <span className="bad">nothing booked yet</span>}</dd>
              <dt>Transport</dt><dd>{c.transport > 0 ? plural(c.transport, 'leg') : <span className="muted">none yet</span>}</dd>
              <dt>Checklists</dt><dd>{c.checklist_items > 0 ? `${c.checklist_done} of ${c.checklist_items} done` : <span className="muted">empty</span>}</dd>
              <dt>Planned cost</dt><dd>{c.planned > 0 ? <Money minor={c.planned} code={next.currency} /> : <span className="muted">—</span>}{next.budget > 0 && <> of <Money minor={next.budget} code={next.currency} /></>}</dd>
            </div>
            {c.checklist_items > 0 && <div className="bar lg mt-sm"><span className="fill" style={{ width: `${c.checklist_done / c.checklist_items * 100}%` }} /></div>}
          </Card>
          <Card title="Countdown"><div className="countdown">{until}<small>days</small></div><p className="muted mt-sm">{weekday(next.start_date, true)}, {fmtDate(next.start_date)}{wx && <> · {WX_ICON(wx.code)} {wx.label.toLowerCase()}, {Math.round(wx.tmax)}°</>}</p></Card>
        </div>
      )}

      {data.on_this_day?.length > 0 && <OnThisDay list={data.on_this_day} />}

      {pins.length > 0 && (
        <Card title="Where you've been — and where you're going" className="mb" flush>
          <MapView className="home-map" markers={markers} zoom={4} />
          <div className="legend pad" style={{ paddingTop: 8, paddingBottom: 12 }}><span style={{ '--d': '#16a34a' } as React.CSSProperties}>done</span><span style={{ '--d': '#d97706' } as React.CSSProperties}>booked / running</span><span style={{ '--d': '#94a3b8' } as React.CSSProperties}>planning</span><span className="muted">· click a pin to open the trip</span></div>
        </Card>
      )}

      <div className="home-grid">
        <div>
          {upcoming.length > 0 && <><h2 className="section-h">Upcoming</h2><div className="trip-grid mb">{upcoming.map(t => <TripCard key={t.id} t={t} />)}</div></>}
          {recent.length > 0 && <><h2 className="section-h">Past trips</h2><div className="trip-grid mb">{recent.map(t => <TripCard key={t.id} t={t} />)}</div></>}
          {upcoming.length === 0 && recent.length === 0 && <Card><Empty icon="luggage" title="No trips yet" /></Card>}
        </div>
        <Card title="From the journal">
          {journal.length === 0 ? <p className="muted">Entries you write during a trip show up here. A few lines a day beats a blank page later.</p> : journal.map(j => (
            <div key={j.id} className="journal-entry" onClick={() => navigate(`/trips/${j.trip_id}/journal`)} style={{ cursor: 'pointer' }}>
              <div className="j-head">{j.mood && <span style={{ fontSize: 16 }}>{j.mood}</span>}<b>{j.title || fmtDate(j.date)}</b><span className="muted small">{j.trip_name} · {fmtDate(j.date, false)}</span></div>
              <div className="j-body small" style={{ whiteSpace: 'normal', overflow: 'hidden', display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical' }}>{j.body}</div>
            </div>
          ))}
        </Card>
      </div>
    </>
  )
}

const ORD = (n: number) => n + (n % 10 === 1 && n % 100 !== 11 ? 'st' : n % 10 === 2 && n % 100 !== 12 ? 'nd' : n % 10 === 3 && n % 100 !== 13 ? 'rd' : 'th')

/** Earlier years on today's date: the 3rd day of the London trip in 2025, and where you were. */
function OnThisDay({ list }: { list: Day[] }) {
  return (
    <>
      <h2 className="section-h">On this day</h2>
      <div className="otd-grid mb">
        {list.map(d => (
          <Link key={`${d.trip_id}-${d.year}`} to={`/trips/${d.trip_id}/itinerary`} className="otd" style={{ '--c': d.color || 'var(--accent)' } as React.CSSProperties}>
            <span className="otd-cover" style={d.has_cover ? { backgroundImage: `url(${V1}/trips/${d.trip_id}/cover?v=${d.updated_at})` } : undefined}><b>{d.years_ago}</b><small>{d.years_ago === 1 ? 'year ago' : 'years ago'}</small></span>
            <span className="grow" style={{ minWidth: 0 }}>
              <span className="block"><b>{ORD(d.day_number)} day</b> of {d.trip_name} <span className="muted">· {d.year}</span></span>
              <span className="muted small block">{d.countries.map(c => `${flag(c)} ${countryName(c)}`).join(' · ')}{d.stay && <>{d.countries.length > 0 && ' · '}🛏 {d.stay}</>}</span>
              {d.places.length > 0 && <span className="small block otd-places">{d.places.slice(0, 4).join(' · ')}{d.places.length > 4 && ` +${d.places.length - 4}`}</span>}
            </span>
          </Link>
        ))}
      </div>
    </>
  )
}

function esc(s: string): string { return s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] || c)) }
