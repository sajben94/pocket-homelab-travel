import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, uploadForm, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { APIToken, CountryDays, GeoResult, Trip, TripStatus } from '../../lib/types'
import { allCountries, countryName, flag } from '../../lib/countries'
import { STATUS_LABEL } from '../../lib/types'
import { TRIP_COLORS } from '../../lib/format'
import { Card, Confirm, Field, Modal, Segmented, useAsync, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select } from '../../components/Select'
import { AmountInput } from '../../components/Money'
import { GeoSearch } from '../../components/GeoSearch'
import { useTrip } from './ctx'
import { TripPeopleCard } from './TripPeople'

export function TripSettings() {
  const { trip, isOwner, canEdit, reload, edit, ov } = useTrip()
  const { status } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [del, setDel] = useState(false)
  const [dup, setDup] = useState(false)
  const { data: tokens } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const base = `${V1}/trips/${trip.id}`
  const remove = async () => { try { await api.del(base); toast('Trip deleted'); navigate('/trips') } catch (e) { toast((e as Error).message, 'err') } }
  const uploadCover = async (file: File) => { try { await uploadForm(`${base}/cover`, file); await reload(); toast('Cover updated') } catch (e) { toast((e as Error).message, 'err') } }
  const archive = async () => { try { await api.put(base, { archived: !trip.archived }); await reload() } catch (e) { toast((e as Error).message, 'err') } }
  const calUrl = `${location.origin}/cal/${trip.id}.ics?token=`
  return (
    <div className="overview-grid">
      <Card title="Trip" actions={canEdit && <button type="button" className="btn sm" onClick={() => edit('trip', trip)}><Icon name="edit" size={14} />Edit details</button>}>
        <dl className="kv">
          <dt>Status</dt><dd>{STATUS_LABEL[trip.status]}{trip.archived && ' · archived'}</dd>
          <dt>Currency</dt><dd>{trip.currency}</dd>
          <dt>Money tag</dt><dd>{trip.firefly_tag || <span className="muted">none</span>}</dd>
          <dt>Recorded route</dt><dd>{trip.dawarich ? 'route and timeline on (Pocket Movement or Dawarich, Settings → Integrations)' : 'off'}</dd>
          <dt>Immich</dt><dd>{trip.immich ? 'photos on' : 'off'}</dd>
          <dt>Timezone</dt><dd>{trip.timezone || <span className="muted">server default</span>}</dd>
          {trip.external_id && <><dt>Imported from</dt><dd className="mono small">{trip.external_id}</dd></>}
        </dl>
        {canEdit && <div className="row mt"><label className="btn sm"><Icon name="image" size={14} />Upload cover photo…<input type="file" accept="image/*" hidden onChange={e => e.target.files?.[0] && uploadCover(e.target.files[0])} /></label>{trip.has_cover && <button type="button" className="btn ghost sm" onClick={async () => { await api.del(`${base}/cover`); reload() }}>Remove cover</button>}</div>}
      </Card>
      <TripPeopleCard />
      <Card title="Export & calendar">
        <div className="row wrap mb-sm">
          <a className="btn sm" href={`${base}/export/calendar.ics`}><Icon name="calendar" size={14} />Calendar (.ics)</a>
          <a className="btn sm" href={`${base}/export/trip.gpx`}><Icon name="map" size={14} />GPX (places)</a>
          {ov.integrations.dawarich.configured && trip.dawarich && <a className="btn sm" href={`${base}/export/trip.gpx?route=1`}><Icon name="route" size={14} />GPX with recorded route</a>}
          <a className="btn sm" href={`${base}/export/trip.json`}><Icon name="download" size={14} />JSON backup</a>
        </div>
        <p className="muted small">Subscribe your phone's calendar to the trip so stays, legs and scheduled places appear with their booking references. The feed needs an API token (Settings → API tokens):</p>
        <pre className="small">{calUrl}{tokens && tokens.length ? 'pt_… (your token)' : 'pt_…'}</pre>
        {status?.auth_mode === 'none' && <p className="muted small">Login is off on this server; any token still works for the feed.</p>}
      </Card>
      <Card title="Danger zone">
        <div className="row wrap">
          <button type="button" className="btn sm" onClick={() => setDup(true)}><Icon name="copy" size={14} />Duplicate trip…</button>
          {isOwner && <button type="button" className="btn sm" onClick={archive}><Icon name="archive" size={14} />{trip.archived ? 'Unarchive' : 'Archive'}</button>}
          {isOwner && <button type="button" className="btn sm danger" onClick={() => setDel(true)}><Icon name="trash" size={14} />Delete trip</button>}
        </div>
        <p className="muted small mt-sm">Duplicating copies places, stays, transport, planned costs and checklists (unticked) into a new planning trip; dates shift to the new start. Deleting removes everything including documents.</p>
      </Card>
      {del && <Confirm title={`Delete ${trip.name}?`} message="Every place, stay, cost, checklist, journal entry and document in it is deleted. This cannot be undone." onCancel={() => setDel(false)} onConfirm={remove} />}
      {dup && <DuplicateModal trip={trip} onClose={() => setDup(false)} onDone={t => { setDup(false); navigate(`/trips/${t.id}`) }} />}
      {!isOwner && <p className="muted small">Only the owner can share, archive or delete the trip.</p>}
    </div>
  )
}

function DuplicateModal({ trip, onClose, onDone }: { trip: Trip; onClose: () => void; onDone: (t: Trip) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: `${trip.name} (copy)`, start_date: '' })
  const [busy, setBusy] = useState(false)
  const go = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { onDone(await api.post<Trip>(`${V1}/trips/${trip.id}/duplicate`, f)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Duplicate trip" onClose={onClose}>
      <form onSubmit={go} className="grid" style={{ gap: 14 }}>
        <Field label="New name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus /></Field>
        <Field label="New start date" help="Everything with a date shifts by the same number of days. Leave empty to keep the dates."><input type="date" value={f.start_date} onChange={e => setF({ ...f, start_date: e.target.value })} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Duplicate</button></div>
      </form>
    </Modal>
  )
}

/** Create or edit a trip. */
export function TripModal({ trip, detected = [], onClose, onSaved }: { trip?: Trip; detected?: CountryDays[]; onClose: () => void; onSaved: (t: Trip) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<Partial<Trip>>(trip ? { ...trip } : { name: '', destination: '', description: '', start_date: '', end_date: '', status: 'planning', currency: settings?.home_currency || 'EUR', budget: 0, color: TRIP_COLORS[0], link: '', notes: '', firefly_tag: '', dawarich: true, immich: true, lat: null, lng: null, timezone: '' })
  const [busy, setBusy] = useState(false)
  const [tab, setTab] = useState<'main' | 'more'>('main')
  const { data: tags } = useAsync(() => settings?.firefly_configured ? api.get<{ id: string; name: string }[]>(`${V1}/money/tags`).catch(() => []) : Promise.resolve([] as { id: string; name: string }[]), [settings?.firefly_configured])
  const set = <K extends keyof Trip>(k: K, v: Trip[K]) => setF(x => ({ ...x, [k]: v }))
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = { ...f, budget: f.budget || 0 }
      const t = trip ? await api.put<Trip>(`${V1}/trips/${trip.id}`, body) : await api.post<Trip>(`${V1}/trips`, body)
      toast(trip ? 'Trip saved' : 'Trip created'); onSaved(t)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const pickDest = (r: GeoResult) => setF(x => ({ ...x, destination: x.destination || (r.country && r.name !== r.country ? `${r.name}, ${r.country}` : r.name), lat: r.lat, lng: r.lng }))
  const tagOpts = (tags || []).map(t => ({ value: t.name, label: t.name }))
  if (f.firefly_tag && !tagOpts.some(t => t.value === f.firefly_tag)) tagOpts.unshift({ value: f.firefly_tag, label: f.firefly_tag })
  return (
    <Modal title={trip ? 'Edit trip' : 'New trip'} onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <div className="tabs sub"><button type="button" className={tab === 'main' ? 'active' : ''} onClick={() => setTab('main')}>Basics</button><button type="button" className={tab === 'more' ? 'active' : ''} onClick={() => setTab('more')}>Budget, integrations & notes</button></div>
        {tab === 'main' && <>
          <Field label="Name"><input value={f.name || ''} onChange={e => set('name', e.target.value)} required autoFocus={!trip} placeholder="Barcelona 2026" maxLength={80} /></Field>
          <Field label="Destination" help="Search to pin the trip on the map — that's where the weather and 'Discover nearby' look."><GeoSearch onPick={pickDest} placeholder="Barcelona, Tokyo, Yosemite…" initial="" /><input className="mt-sm" value={f.destination || ''} onChange={e => set('destination', e.target.value)} placeholder="Shown on the trip card" /></Field>
          <CountriesField manual={f.countries_manual || []} detected={detected} onChange={v => set('countries_manual', v)} />
          <div className="two-col tight">
            <Field label="Start"><input type="date" value={f.start_date || ''} onChange={e => set('start_date', e.target.value)} /></Field>
            <Field label="End"><input type="date" value={f.end_date || ''} min={f.start_date || undefined} onChange={e => set('end_date', e.target.value)} /></Field>
          </div>
          <Field group label="Status"><Segmented value={f.status || 'planning'} onChange={v => set('status', v as TripStatus)} options={Object.entries(STATUS_LABEL).map(([v, l]) => ({ value: v, label: l }))} size="sm" /></Field>
          <Field group label="Colour"><div className="row">{TRIP_COLORS.map(c => <span key={c} className={`color-dot ${f.color === c ? 'active' : ''}`} style={{ background: c }} onClick={() => set('color', c)} />)}</div></Field>
          <Field label="Description"><textarea value={f.description || ''} onChange={e => set('description', e.target.value)} rows={2} placeholder="What this trip is about." /></Field>
        </>}
        {tab === 'more' && <>
          <div className="two-col tight">
            <Field label="Budget" help="Everything is converted into this currency."><AmountInput amount={f.budget ?? null} currency={f.currency || 'EUR'} onAmount={m => set('budget', m || 0)} onCurrency={c => set('currency', c)} /></Field>
            <Field label="Timezone" help="For day boundaries of the recorded route. Leave empty for the server's."><input value={f.timezone || ''} onChange={e => set('timezone', e.target.value)} placeholder="Europe/Podgorica" /></Field>
          </div>
          {trip && trip.role !== 'owner' ? <Field label="Money tag" help="Spending is pulled from the trip owner's Pocket Money; only the owner can change the tag."><input value={f.firefly_tag || ''} disabled /></Field> : <Field label="Money tag" help={settings?.firefly_configured ? 'Expenses in Pocket Money carrying this tag are pulled in as spent money, every 30 minutes while the trip runs.' : 'Connect Pocket Money under Settings → Integrations first; you can still type the tag now.'}>
            {tagOpts.length > 0 ? <Select value={f.firefly_tag || ''} onChange={v => set('firefly_tag', v)} options={tagOpts} placeholder="No tag" clearLabel="No tag" onCreate={v => v} createLabel="Use tag" /> : <input value={f.firefly_tag || ''} onChange={e => set('firefly_tag', e.target.value)} placeholder="lustica 2026" />}
          </Field>}
          <label className="row"><input type="checkbox" checked={f.dawarich !== false} onChange={e => set('dawarich', e.target.checked)} style={{ width: 'auto' }} /> Show the recorded route and timeline (Pocket Movement or Dawarich)</label>
          <label className="row"><input type="checkbox" checked={f.immich !== false} onChange={e => set('immich', e.target.checked)} style={{ width: 'auto' }} /> Show photos from Immich</label>
          <Field label="Link"><input type="url" value={f.link || ''} onChange={e => set('link', e.target.value)} placeholder="https://" /></Field>
          <Field label="Notes"><textarea value={f.notes || ''} onChange={e => set('notes', e.target.value)} rows={4} placeholder="Anything that doesn't fit elsewhere: emergency numbers, the Wi-Fi password, who's allergic to what." /></Field>
        </>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{trip ? 'Save' : 'Create trip'}</button></div>
      </form>
    </Modal>
  )
}

/**
 * The trip's countries. Detected from where you slept and what you visited
 * (shown with their days); editing the list makes it yours, and only those
 * countries count. Empty = detect again.
 */
function CountriesField({ manual, detected, onChange }: { manual: string[]; detected: CountryDays[]; onChange: (codes: string[]) => void }) {
  const options = useMemo(() => allCountries().map(c => ({ value: c.code, label: `${flag(c.code)} ${c.name}` })), [])
  const auto = detected.filter(c => c.days > 0).map(c => c.code)
  const list = manual.length ? manual : auto
  const days = (code: string) => detected.find(c => c.code === code)?.days || 0
  const edit = (codes: string[]) => onChange(codes)
  return (
    <Field group label="Countries" help={manual.length ? 'Your list: only these countries count for this trip.' : auto.length ? 'Detected from the stays and places of the trip. Add or remove one to set them yourself.' : 'Detected from the stays and places once they have a location — or add them here.'}>
      <div className="row wrap" style={{ gap: 6 }}>
        {list.map(code => (
          <span key={code} className={`country-chip ${manual.length ? '' : 'auto'}`} title={countryName(code)}>
            {flag(code)} {countryName(code)}{days(code) > 0 && <span className="muted"> · {days(code)}d</span>}
            <button type="button" className="icon-btn" aria-label={`Remove ${countryName(code)}`} onClick={() => edit(list.filter(c => c !== code))} style={{ width: 20, height: 20 }}><Icon name="x" size={12} /></button>
          </span>
        ))}
        <div style={{ minWidth: 200 }}><Select value="" onChange={v => v && !list.includes(v) && edit([...list, v])} options={options.filter(o => !list.includes(o.value))} placeholder="Add a country…" ariaLabel="Add a country" /></div>
        {manual.length > 0 && <button type="button" className="btn ghost sm" onClick={() => edit([])}>Use detected</button>}
      </div>
    </Field>
  )
}
