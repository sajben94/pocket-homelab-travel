import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { TripPerson } from '../../lib/types'
import { Card, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select } from '../../components/Select'
import { PersonDot } from '../../components/WithPeople'
import { useTrip } from './ctx'

/**
 * Who is on the trip. Everyone here shares its bills; someone with an account
 * also gets in (as an editor or a viewer — the owner decides). Pick people you
 * travelled with before, or type a new name: they are kept for next time.
 */
export function TripPeopleCard() {
  const { ov, trip, isOwner, canEdit, patch } = useTrip()
  const { people, reloadPeople } = useApp()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const list = ov.people || []
  const base = `${V1}/trips/${trip.id}/people`
  const refresh = async () => { patch({ people: await api.get<TripPerson[]>(base) }) }
  const add = async (body: { person_id?: number; name?: string }) => {
    setBusy(true)
    try { await api.post(base, body); await refresh(); reloadPeople() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const update = async (tp: TripPerson, body: Record<string, unknown>) => {
    try { const np = await api.put<TripPerson>(`${base}/${tp.id}`, body); patch({ people: list.map(x => x.id === tp.id ? np : x) }) } catch (e) { toast((e as Error).message, 'err') }
  }
  const remove = async (tp: TripPerson) => {
    try { await api.del(`${base}/${tp.id}`); patch({ people: list.filter(x => x.id !== tp.id) }); reloadPeople() } catch (e) { toast((e as Error).message, 'err') }
  }
  // the owner's address book (someone else's trip: just type a name)
  const candidates = isOwner ? people.filter(p => !list.some(tp => tp.person_id === p.id)) : []
  return (
    <Card title={`People · ${list.length}`} actions={<Link to="/people" className="btn ghost sm">All people</Link>}>
      <p className="muted small">Everyone here shares the trip's bills. People with an account can open the trip — owners decide if they edit or only look.</p>
      <div className="trip-people">
        {list.map(tp => (
          <div key={tp.id} className={`tp-row ${tp.active ? '' : 'inactive'}`}>
            <PersonDot name={tp.name} color={tp.color} size={26} account={tp.user_id != null && tp.role !== "owner"} />
            <span className="grow">
              {tp.person_id ? <Link to={`/people/${tp.person_id}`}><b>{tp.name}</b></Link> : <b>{tp.name}</b>}
              <span className="muted small block">{tp.role === 'owner' ? 'owner' : tp.user_id != null ? (tp.role ? `can ${tp.role === 'viewer' ? 'look' : 'edit'}` : 'no access') : 'shares the bills'}{tp.weight !== 1 && ` · counts ${tp.weight}×`}{!tp.active && ' · not in new bills'}</span>
            </span>
            {tp.user_id != null && tp.role !== 'owner' && isOwner && (
              <Select value={tp.role || 'none'} onChange={v => update(tp, { role: v === 'none' ? '' : v })} options={[{ value: 'editor', label: 'Editor' }, { value: 'viewer', label: 'Viewer' }, { value: 'none', label: 'No access' }]} ariaLabel={`Access for ${tp.name}`} />
            )}
            {canEdit && tp.role !== 'owner' && <button type="button" className="icon-btn" title={`Take ${tp.name} off the trip`} aria-label={`Remove ${tp.name}`} onClick={() => remove(tp)}><Icon name="x" size={14} /></button>}
          </div>
        ))}
      </div>
      {canEdit && (
        <div className="mt-sm">
          <Select value="" onChange={v => v && add({ person_id: Number(v) })} disabled={busy}
            options={candidates.sort((a, b) => b.trips - a.trips || a.name.localeCompare(b.name)).map(p => ({ value: String(p.id), label: p.name, hint: p.trips ? `${p.trips} trip${p.trips === 1 ? '' : 's'}` : 'new' }))}
            onCreate={async name => { await add({ name }); return '' }} createLabel="Add" placeholder="+ Add someone — pick or type a name…" ariaLabel="Add someone" />
        </div>
      )}
    </Card>
  )
}

/**
 * Everyone on the trip at a glance, with a quick way to add someone: pick from
 * the people you travelled with (they autocomplete) or type a new name.
 */
export function PeopleStrip({ title = 'People' }: { title?: string }) {
  const { ov, trip, isOwner, canEdit, patch, go } = useTrip()
  const { people, reloadPeople } = useApp()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState(false)
  const list = ov.people || []
  const base = `${V1}/trips/${trip.id}/people`
  const add = async (body: { person_id?: number; name?: string }) => {
    setBusy(true)
    try { await api.post(base, body); patch({ people: await api.get<TripPerson[]>(base) }); reloadPeople(); toast('Added to the trip'); setOpen(false) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const candidates = isOwner ? people.filter(p => !list.some(tp => tp.person_id === p.id)).sort((a, b) => b.trips - a.trips || a.name.localeCompare(b.name)) : []
  return (
    <div className="people-strip-wrap"><div className="people-strip">
      <span className="ps-title">{title}</span>
      <span className="ps-people">
        {list.map(tp => (
          tp.person_id
            ? <Link key={tp.id} to={`/people/${tp.person_id}`} className={`ps-person ${tp.active ? '' : 'inactive'}`} title={tp.role === 'owner' ? `${tp.name} (owner)` : tp.name}><PersonDot name={tp.name} color={tp.color} size={24} account={tp.user_id != null && tp.role !== "owner"} /><span>{tp.name.split(' ')[0]}</span></Link>
            : <span key={tp.id} className="ps-person"><PersonDot name={tp.name} color={tp.color} size={24} account={tp.user_id != null && tp.role !== "owner"} /><span>{tp.name.split(' ')[0]}</span></span>
        ))}
      </span>
      {canEdit && <button type="button" className={`btn sm ps-add-btn ${open ? 'primary' : ''}`} onClick={() => setOpen(!open)} aria-expanded={open}><Icon name={open ? 'x' : 'plus'} size={14} />{open ? 'Close' : 'Add someone'}</button>}
      {canEdit && (
        <span className={`ps-add ${open ? 'open' : ''}`}>
          <Select value="" onChange={v => v && add({ person_id: Number(v) })} disabled={busy}
            options={candidates.map(p => ({ value: String(p.id), label: p.name, hint: p.trips ? `${p.trips} trip${p.trips === 1 ? '' : 's'} together` : 'new' }))}
            onCreate={async name => { await add({ name }); return '' }} createLabel="Add" placeholder="+ Add someone…" ariaLabel="Add someone to the trip" />
        </span>
      )}
      <button type="button" className="btn ghost sm" onClick={() => go('settings')} title="Weights, access and removing people">Manage</button>
    </div></div>
  )
}
