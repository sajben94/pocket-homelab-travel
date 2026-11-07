import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { DayWeather, Expense, JournalEntry, Lodging, Overview, Photo, Place, Route, TimelineDay, Transport, Trip } from '../lib/types'
import { STATUS_LABEL } from '../lib/types'
import { daysBetween, fmtRange, plural, todayISO } from '../lib/format'
import { Loading, Tabs, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { TRIP_TABS } from '../components/CommandPalette'
import { countryName, flag } from '../lib/countries'
import { useApp } from '../lib/app-context'
import { TripCtx, type EditKind } from './trip/ctx'
import { OverviewTab } from './trip/Overview'
import { Itinerary } from './trip/Itinerary'
import { Places, PlaceModal } from './trip/Places'
import { MapTab } from './trip/MapTab'
import { Stay, LodgingModal, TransportModal } from './trip/Stay'
import { BudgetTab, ExpenseModal } from './trip/Budget'
import { Split } from './trip/Split'
import { Checklists } from './trip/Lists'
import { Journal, JournalModal } from './trip/Journal'
import { Photos } from './trip/Photos'
import { Documents } from './trip/Documents'
import { TripSettings, TripModal } from './trip/TripSettings'

const TABS = TRIP_TABS.map(([id, label, icon]) => ({ id, label, icon }))
const ADD_KINDS: EditKind[] = ['place', 'lodging', 'transport', 'expense', 'journal']

export default function TripPage() {
  const { id, tab: rawTab = 'overview' } = useParams()
  const tab = rawTab === 'split' ? 'spending' : rawTab // the tab used to be called Split
  const navigate = useNavigate()
  const { toast } = useToast()
  const { reloadTrips } = useApp()
  const [params, setParams] = useSearchParams()
  const [ov, setOv] = useState<Overview | null>(null)
  const [err, setErr] = useState('')
  const [weather, setWeather] = useState<DayWeather[]>([])
  const [route, setRoute] = useState<Route | null>(null)
  const [timeline, setTimeline] = useState<TimelineDay[]>([])
  const [allPhotos, setAllPhotos] = useState<Photo[]>([])
  const [photosReady, setPhotosReady] = useState(false)
  const photos = useMemo(() => allPhotos.filter(p => !p.hidden), [allPhotos])
  const [extErrors, setExtErrors] = useState<Record<string, string>>({})
  const [editing, setEditing] = useState<{ kind: EditKind; item?: unknown; defaults?: Record<string, unknown> } | null>(null)
  const base = `${V1}/trips/${id}`

  const reload = useCallback(async () => {
    try { setOv(await api.get<Overview>(`${base}/overview`)); setErr('') } catch (e) { setErr((e as Error).message) }
  }, [base])
  useEffect(() => { reload() }, [reload])

  const loadExternal = useCallback(async (refresh = false) => {
    const q = refresh ? '?refresh=1' : ''
    const errs: Record<string, string> = {}
    await Promise.all([
      api.get<DayWeather[]>(`${base}/weather${q}`).then(setWeather).catch(e => { errs.weather = (e as Error).message }),
      api.get<Route>(`${base}/location/route${q}`).then(setRoute).catch(e => { errs.dawarich = (e as Error).message }),
      api.get<TimelineDay[]>(`${base}/location/timeline${q}`).then(setTimeline).catch(e => { errs.dawarich = (e as Error).message }),
      // Immich and PhotoPrism; one library failing while the other answers comes in X-Photos-Error
      fetch(`${base}/photos${q}`, { credentials: 'same-origin' }).then(async res => {
        if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || res.statusText)
        const partial = res.headers.get('X-Photos-Error')
        if (partial) errs.photos_partial = partial
        setAllPhotos(await res.json())
      }).catch(e => { errs.immich = (e as Error).message }).finally(() => setPhotosReady(true)),
    ])
    setExtErrors(errs)
  }, [base])
  useEffect(() => { if (ov) loadExternal() }, [ov?.trip.id, ov?.trip.start_date, ov?.trip.end_date]) // eslint-disable-line react-hooks/exhaustive-deps

  const refreshExternal = useCallback(async () => {
    try { await api.post(`${base}/refresh`); await reload(); await loadExternal(true); toast('Refreshed') } catch (e) { toast((e as Error).message, 'err') }
  }, [base, reload, loadExternal, toast])

  const patch = useCallback((p: Partial<Overview>) => setOv(o => o ? { ...o, ...p } : o), [])
  const go = useCallback((t: string) => navigate(`/trips/${id}/${t}`), [navigate, id])
  const edit = useCallback((kind: EditKind, item?: unknown, defaults?: Record<string, unknown>) => setEditing({ kind, item, defaults }), [])

  const state = useMemo(() => ov ? ({
    ov, trip: ov.trip, canEdit: ov.trip.role !== 'viewer', isOwner: ov.trip.role === 'owner',
    reload, patch, weather, route, timeline, photos, allPhotos, setAllPhotos, photosReady, extErrors, refreshExternal, edit, go,
  }) : null, [ov, reload, patch, weather, route, timeline, photos, allPhotos, photosReady, extErrors, refreshExternal, edit, go])

  // ?add=place|expense|lodging|transport|journal (the New menu, shortcuts, links) opens that form on this trip
  const addKind = params.get('add') as EditKind | null
  useEffect(() => {
    if (!addKind || !ov || String(ov.trip.id) !== id) return
    const next = new URLSearchParams(params); next.delete('add'); setParams(next, { replace: true })
    if (ov.trip.role === 'viewer' || !ADD_KINDS.includes(addKind)) return
    const t = ov.trip, today = todayISO()
    const on = t.start_date && t.start_date <= today && t.end_date >= today
    const defaults: Record<string, unknown> = addKind === 'expense' ? { kind: on || (t.end_date && t.end_date < today) ? 'actual' : 'planned' } : addKind === 'journal' ? { date: on ? today : t.end_date || today } : {}
    setEditing({ kind: addKind, item: null, defaults })
  }, [addKind, ov, id]) // eslint-disable-line react-hooks/exhaustive-deps

  // ?open=place:12 | lodging:3 | transport:4 (search results, links) opens that item's form
  const openParam = params.get('open')
  useEffect(() => {
    if (!openParam || !ov || String(ov.trip.id) !== id) return
    const next = new URLSearchParams(params); next.delete('open'); setParams(next, { replace: true })
    const [kind, raw] = openParam.split(':'), oid = Number(raw)
    const item = kind === 'place' ? ov.places.find(p => p.id === oid) : kind === 'lodging' ? ov.lodging.find(l => l.id === oid) : kind === 'transport' ? ov.transport.find(x => x.id === oid) : kind === 'expense' ? ov.expenses.find(x => x.id === oid) : undefined
    if (item) setEditing({ kind: kind as EditKind, item })
  }, [openParam, ov, id]) // eslint-disable-line react-hooks/exhaustive-deps

  if (err) return <div className="callout danger"><Icon name="alert" size={15} /><span>{err} — <Link to="/trips">back to trips</Link></span></div>
  if (!state || !ov) return <Loading />
  const t = ov.trip
  const today = todayISO()
  const running = t.start_date && t.start_date <= today && t.end_date >= today
  const until = t.start_date > today ? daysBetween(today, t.start_date) : 0
  const heroStyle = t.has_cover ? { backgroundImage: `url(${V1}/trips/${t.id}/cover?v=${t.updated_at})` } : { background: `linear-gradient(135deg, ${t.color || '#0284c7'} 0%, #0f172a 100%)` }
  const close = () => setEditing(null)
  const afterSave = async () => { close(); await reload(); reloadTrips() }
  return (
    <TripCtx.Provider value={state}>
      <div className={`hero ${t.has_cover ? 'has-cover' : ''}`} style={heroStyle}>
        <div className="hero-body">
          <div>
            <div className="row" style={{ gap: 8, marginBottom: 6 }}><span className="status-pill">{running ? `Day ${daysBetween(t.start_date, today) + 1} of ${t.days}` : until ? `In ${until} days` : STATUS_LABEL[t.status]}</span>{t.archived && <span className="status-pill"><Icon name="archive" size={12} />Archived</span>}</div>
            <h1>{t.name}</h1>
            <div className="meta-line"><span><Icon name="pin" size={14} />{t.destination || 'No destination yet'}</span><span><Icon name="calendar" size={14} />{fmtRange(t.start_date, t.end_date)}</span>{t.days > 0 && <span>{plural(t.days, 'day')}</span>}{ov.countries.some(c => c.days > 0) && <span className="hero-countries" title={ov.countries.map(c => `${countryName(c.code)}: ${plural(c.days, 'day')}`).join('\n')}>{ov.countries.filter(c => c.days > 0).map(c => <Link key={c.code} to={`/countries/${c.code}`}>{flag(c.code)}{ov.countries.length > 1 && <small> {c.days}d</small>}</Link>)}</span>}{ov.people.length > 1 && <span className="hero-people"><Icon name="users" size={14} />{ov.people.map((p, i) => <span key={p.id}>{i > 0 && ', '}{p.person_id ? <Link to={`/people/${p.person_id}`}>{p.name.split(' ')[0]}</Link> : p.name.split(' ')[0]}</span>)}</span>}</div>
          </div>
          <div className="row">
            {state.canEdit && <button type="button" className="btn glass" onClick={() => edit('place', null, {})}><Icon name="pin" size={15} />Place</button>}
            {state.canEdit && <button type="button" className="btn glass" onClick={() => edit('expense', null, { kind: running || t.end_date < today ? 'actual' : 'planned' })}><Icon name="wallet" size={15} />Cost</button>}
            <button type="button" className="btn glass" onClick={() => edit('trip', t)}><Icon name="edit" size={15} />Edit</button>
          </div>
        </div>
      </div>
      <div className="trip-tabs"><Tabs tabs={TABS} value={tab} onChange={go} /></div>
      <div className="mt">
        {tab === 'overview' && <OverviewTab />}
        {tab === 'itinerary' && <Itinerary />}
        {tab === 'places' && <Places />}
        {tab === 'map' && <MapTab />}
        {tab === 'stay' && <Stay />}
        {tab === 'budget' && <BudgetTab />}
        {tab === 'spending' && <Split />}
        {tab === 'lists' && <Checklists />}
        {tab === 'journal' && <Journal />}
        {tab === 'photos' && <Photos />}
        {tab === 'documents' && <Documents />}
        {tab === 'settings' && <TripSettings />}
      </div>
      {editing?.kind === 'place' && <PlaceModal trip={t} item={editing.item as Place | null} defaults={editing.defaults} onClose={close} onSaved={afterSave} />}
      {editing?.kind === 'lodging' && <LodgingModal trip={t} item={editing.item as Lodging | null} defaults={editing.defaults} onClose={close} onSaved={afterSave} />}
      {editing?.kind === 'transport' && <TransportModal trip={t} item={editing.item as Transport | null} defaults={editing.defaults} onClose={close} onSaved={afterSave} />}
      {editing?.kind === 'expense' && <ExpenseModal trip={t} item={editing.item as Expense | null} defaults={editing.defaults} onClose={close} onSaved={afterSave} />}
      {editing?.kind === 'journal' && <JournalModal trip={t} item={editing.item as JournalEntry | null} defaults={editing.defaults} onClose={close} onSaved={afterSave} />}
      {editing?.kind === 'trip' && <TripModal trip={editing.item as Trip} detected={ov.countries} onClose={close} onSaved={afterSave} />}
    </TripCtx.Provider>
  )
}
