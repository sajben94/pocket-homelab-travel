import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Person } from '../lib/types'
import { fmtDate, plural } from '../lib/format'
import { matches } from '../lib/search'
import { Card, Empty, Field, Modal, PageHeader, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { PersonDot, personColor } from '../components/WithPeople'

/** Everyone you travel with — saved once, added to any trip, filtered by. */
export default function PeoplePage() {
  const { people, withPeople, setWithPeople } = useApp()
  const [q, setQ] = useState('')
  const [adding, setAdding] = useState(false)
  const navigate = useNavigate()
  const list = people.filter(p => !q || matches(q, `${p.name} ${p.email}`))
  return (
    <>
      <PageHeader title="People" sub="Everyone you travel with. Add them to a trip, split its bills, or show only the trips with them." actions={<button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="plus" size={15} />Person</button>} />
      {people.length > 8 && <div className="toolbar"><div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find someone…" aria-label="Find someone" /></div></div>}
      {people.some(p => p.user_id != null && !p.is_self) && <div className="legend" style={{ marginTop: 0, marginBottom: 12 }}><span className="key"><span className="person-dot account" style={{ width: 14, height: 14, background: 'var(--muted)' }} />can sign in here</span><span className="key"><span className="person-dot" style={{ width: 14, height: 14, background: 'var(--muted)' }} />only shares the bills</span></div>}
      {people.length <= 1 ? <Card><Empty icon="users" title="Nobody yet">Add the people you travel with — with or without an account here. Then add them to trips, split bills with them, and look back at everything you did together.</Empty></Card> : (
        <div className="people-grid">
          {list.map(p => (
            <Link key={p.id} to={`/people/${p.id}`} className={`person-card ${p.user_id != null && !p.is_self ? 'account' : ''}`} title={p.user_id != null && !p.is_self ? `${p.name} can sign in here` : undefined}>
              <PersonDot name={p.name} color={p.color} size={40} account={p.user_id != null && !p.is_self} />
              <span className="grow">
                <b>{p.name}{p.is_self && <span className="muted"> (you)</span>}</b>
                <span className="muted small block">{p.trips ? `${plural(p.trips, 'trip')}${p.last_trip ? ` · last ${fmtDate(p.last_trip)}` : ''}` : 'no trips yet'}</span>
              </span>
              {!p.is_self && <button type="button" className={`btn sm ${withPeople.includes(p.id) ? 'primary' : 'ghost'}`} title="Show only trips with them" onClick={e => { e.preventDefault(); setWithPeople(withPeople.includes(p.id) ? withPeople.filter(x => x !== p.id) : [...withPeople, p.id]) }}><Icon name="users" size={13} />{withPeople.includes(p.id) ? 'With' : 'Filter'}</button>}
            </Link>
          ))}
        </div>
      )}
      {adding && <PersonModal onClose={() => setAdding(false)} onSaved={p => { setAdding(false); navigate(`/people/${p.id}`) }} />}
    </>
  )
}

export const PERSON_COLORS = ['', '#0ea5e9', '#f97316', '#22c55e', '#a855f7', '#ef4444', '#eab308', '#14b8a6', '#ec4899', '#6366f1', '#84cc16', '#64748b']

/** Add or edit someone: name, colour, an account on this box (gives access to shared trips), notes. */
export function PersonModal({ person, onClose, onSaved }: { person?: Person; onClose: () => void; onSaved: (p: Person) => void }) {
  const { users, user, reloadPeople } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: person?.name || '', color: person?.color || '', user_id: person?.user_id ?? null as number | null, email: person?.email || '', notes: person?.notes || '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = person?.is_self ? { name: f.name, color: f.color, notes: f.notes } : f
      const p = person ? await api.put<Person>(`${V1}/people/${person.id}`, body) : await api.post<Person>(`${V1}/people`, body)
      await reloadPeople(); toast(person ? 'Saved' : `${p.name} added`); onSaved(p)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const accounts = users.filter(u => u.id !== user?.id)
  return (
    <Modal title={person ? `Edit ${person.name}` : 'Add a person'} onClose={onClose}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus maxLength={80} /></Field>
        <Field group label="Colour"><div className="row wrap" style={{ gap: 6 }}>{PERSON_COLORS.map(c => <button type="button" key={c || 'auto'} className={`color-dot ${f.color === c ? 'active' : ''}`} style={{ background: c || personColor(f.name || '?'), outline: c ? undefined : '1px dashed var(--border-strong)' }} title={c ? c : 'Automatic'} onClick={() => setF({ ...f, color: c })} />)}</div></Field>
        {!person?.is_self && accounts.length > 0 && (
          <Field label="Account on this box" help="Linked people get access to the trips they are on (as editors; change it per trip). Without an account they only share the bills.">
            <Select value={f.user_id ? String(f.user_id) : ''} onChange={v => setF({ ...f, user_id: v ? Number(v) : null })} options={accounts.map(u => ({ value: String(u.id), label: u.name || u.email, hint: u.email }))} placeholder="No account" clearLabel="No account" />
          </Field>
        )}
        {!person?.is_self && <Field label="Email" help="Optional, for your own reference."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>}
        <Field label="Notes"><textarea value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} rows={3} placeholder="Allergies, passport expiry, favourite seat…" /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{person ? 'Save' : 'Add'}</button></div>
      </form>
    </Modal>
  )
}
