import { Link } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import type { PocketPlaces, PPPlace, PPVisit } from '../../lib/types'
import { fmtClock, fmtDate, plural } from '../../lib/format'
import { Card, useAsync } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Money } from '../../components/Money'
import { NeedsCore } from '../../components/NeedsCore'
import { useTrip } from './ctx'

// What a place is in Pocket Places, in a word and a picture.
const KINDS: Record<string, [string, string]> = {
  restaurant: ['Restaurant', '🍽'], cafe: ['Café', '☕'], bar: ['Bar', '🍸'], bakery: ['Bakery', '🥐'], street_food: ['Street food', '🌯'],
  dessert: ['Dessert', '🍨'], market: ['Market', '🧺'], shop: ['Shop', '🛍'], museum: ['Museum', '🖼'], sight: ['Sight', '🏛'],
  park: ['Park', '🌳'], viewpoint: ['Viewpoint', '🌄'], beach: ['Beach', '🏖'], nature: ['Nature', '🌿'], activity: ['Something to do', '🎟'],
  hotel: ['Stay', '🏨'], other: ['Place', '📍'],
}
const kind = (k: string) => KINDS[k] || KINDS.other
/** "4.5" for a rating, "" when there is none. */
const stars = (r: number) => r > 0 ? (Math.round(r * 10) / 10).toString() : ''
const price = (n: number) => n > 0 ? '€'.repeat(Math.min(4, n)) : ''
const SHOWN = 6

/**
 * The trip's card from Pocket Places: the places on your list at the
 * destination (to try, liked before), the lists you made for this trip and
 * where you went during it with what it cost. Loads on its own, so the rest
 * of the page never waits for it; without Pocket Core or with the link off it
 * stays in place, greyed, and says what it needs.
 */
export function PocketPlacesCard() {
  const { trip } = useTrip()
  const { data, error, loading, reload } = useAsync(() => api.get<PocketPlaces>(`${V1}/trips/${trip.id}/pocket-places`),
    [trip.id, trip.destination, trip.lat, trip.lng, trip.start_date, trip.end_date])
  const pp = data
  const ok = pp?.status === 'ok'
  const base = pp?.places_url || ''
  const where = trip.destination ? trip.destination.split(',')[0] : ''
  const empty = ok && !pp.lists.length && !pp.want.length && !pp.been.length && !pp.visits.length
  const open = ok && pp.trip_url ? <a className="btn ghost sm" href={pp.trip_url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open in Pocket Places</a> : null
  return (
    <Card title={<><Icon name="pin" size={16} />Pocket Places</>} className={`span2 pp-card ${pp && !ok ? 'pp-dim' : ''}`} actions={open}
      sub={where ? `Your places in ${where}: to try, liked before, and where you went during the trip.` : 'Your places at the destination: to try, liked before, and where you went during the trip.'}>
      {!pp && loading ? <p className="muted pp-wait">Looking in Pocket Places…</p>
        : !pp ? <p className="muted pp-wait">Pocket Places could not be reached{error ? `: ${error}` : ''}. <button type="button" className="link-btn" onClick={() => reload()}>Try again</button></p>
        : pp.status === 'no_core' ? <NeedsCore what="Pocket Places" />
        : pp.status === 'off' ? <p className="muted pp-wait">Pocket Places is switched off for you. Connect Pocket Places in <Link to="/settings/integrations">Settings → Integrations</Link>.</p>
        : pp.status === 'unavailable' ? <p className="muted pp-wait">Pocket Places is not connected{pp.detail ? `: ${pp.detail}` : ''}. Connect Pocket Places in <Link to="/settings/integrations">Settings → Integrations</Link>.</p>
        : pp.status === 'error' ? <p className="muted pp-wait">Pocket Places did not answer{pp.detail ? `: ${pp.detail}` : ''}. <button type="button" className="link-btn" onClick={() => reload()} disabled={loading}>Try again</button></p>
        : empty ? <p className="muted pp-wait">Nothing in Pocket Places for {where || 'this trip'} yet. Places you save there, and lists you make for this trip, show up here.</p>
        : <PlacesBody pp={pp} base={base} />}
    </Card>
  )
}

function PlacesBody({ pp, base }: { pp: PocketPlaces; base: string }) {
  const planned = pp.visits.filter(v => v.planned)
  const went = pp.visits.filter(v => !v.planned)
  return (
    <div className="pp-body">
      {pp.lists.length > 0 && <div className="pp-lists">
        <span className="muted small">{pp.lists.length === 1 ? 'List for this trip' : 'Lists for this trip'}</span>
        <div className="chips">{pp.lists.map(l => <a key={l.id} className="chip" href={`${base}/lists/${l.id}`} target="_blank" rel="noreferrer">{l.emoji && <span aria-hidden="true">{l.emoji}</span>}{l.name}<span className="muted">{l.been}/{l.count} been</span></a>)}</div>
      </div>}
      <div className="pp-cols">
        <PlaceColumn title="To try there" empty="Nothing on your list to try there yet." places={pp.want} base={base} more={pp.trip_url} />
        <PlaceColumn title="Liked before" empty="No places you have been to there yet." places={pp.been} base={base} more={pp.trip_url} />
      </div>
      {(went.length > 0 || planned.length > 0) && <div className="pp-visits">
        <div className="pp-h"><span>During the trip</span>{pp.spent.length > 0 && <span className="muted small">{pp.spent.map((s, i) => <span key={s.currency}>{i > 0 && ' · '}<Money minor={s.total} code={s.currency} /> on {plural(s.visits, 'visit')}</span>)}</span>}</div>
        <div className="pp-rows">{[...went, ...planned].map(v => <VisitRow key={v.id} v={v} base={base} />)}</div>
      </div>}
    </div>
  )
}

function PlaceColumn({ title, empty, places, base, more }: { title: string; empty: string; places: PPPlace[]; base: string; more?: string }) {
  const shown = places.slice(0, SHOWN)
  return (
    <div className="pp-col">
      <div className="pp-h"><span>{title}</span>{places.length > 0 && <span className="muted small">{places.length}</span>}</div>
      {places.length === 0 ? <p className="muted small pp-none">{empty}</p> : <div className="pp-rows">
        {shown.map(p => {
          const [label, ico] = kind(p.kind)
          const r = stars(p.rating), pr = price(p.price)
          return (
            <a key={p.id} className="pp-row" href={`${base}/places/${p.id}`} target="_blank" rel="noreferrer">
              <span className="pp-ico" aria-hidden="true">{ico}</span>
              <span className="pp-txt"><span className="pp-name">{p.name}{p.favourite && <Icon name="heart" size={12} className="pp-fav" />}</span><span className="pp-sub">{[p.cuisine || label, p.city].filter(Boolean).join(' · ')}</span></span>
              {(r || pr) && <span className="pp-meta">{r && <span><Icon name="star" size={11} />{r}</span>}{pr && <span>{pr}</span>}</span>}
            </a>
          )
        })}
        {places.length > SHOWN && more && <a className="pp-more" href={more} target="_blank" rel="noreferrer">{places.length - SHOWN} more in Pocket Places</a>}
      </div>}
    </div>
  )
}

function VisitRow({ v, base }: { v: PPVisit; base: string }) {
  const [label, ico] = kind(v.place_kind)
  return (
    <a className="pp-row" href={`${base}/places/${v.place_id}`} target="_blank" rel="noreferrer">
      <span className="pp-when">{fmtDate(v.date, false)}{v.time && <span className="muted"> {fmtClock(v.time)}</span>}</span>
      <span className="pp-ico" aria-hidden="true">{ico}</span>
      <span className="pp-txt"><span className="pp-name">{v.place_name || label}</span><span className="pp-sub">{[v.planned ? 'planned' : label, v.with_whom && `with ${v.with_whom}`].filter(Boolean).join(' · ')}</span></span>
      <span className="pp-meta">{v.rating > 0 && <span><Icon name="star" size={11} />{stars(v.rating)}</span>}{v.spent > 0 && <Money minor={v.spent} code={v.currency} />}</span>
    </a>
  )
}
