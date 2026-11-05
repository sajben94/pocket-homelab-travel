import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { PersonPage as Page } from '../lib/types'
import { STATUS_LABEL } from '../lib/types'
import { countryName, flag } from '../lib/countries'
import { fmtRange, plural } from '../lib/format'
import { Card, Confirm, Empty, Field, Loading, Modal, Segmented, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { PersonDot } from '../components/WithPeople'
import { PersonModal } from './People'

/** Someone you travel with: every trip together, the days and countries, who else came along. */
export default function PersonPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { people, withPeople, setWithPeople, reloadPeople } = useApp()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => api.get<Page>(`${V1}/people/${id}`), [id])
  const [editing, setEditing] = useState(false)
  const [merging, setMerging] = useState(false)
  const [del, setDel] = useState(false)
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error} — <Link to="/people">all people</Link></span></div>
  if (!data) return null
  const { person: p, trips, totals, countries, company } = data
  const on = withPeople.includes(p.id)
  const remove = async () => { try { await api.del(`${V1}/people/${p.id}`); await reloadPeople(); toast(`${p.name} removed`); navigate('/people') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <>
      <div className="country-hero">
        <PersonDot name={p.name} color={p.color} size={64} account={p.user_id != null && !p.is_self} />
        <div className="grow">
          <div className="muted small"><Link to="/people">People</Link></div>
          <h1>{p.name}{p.is_self && <span className="muted"> (you)</span>}</h1>
          {p.notes && <div className="muted" style={{ whiteSpace: 'pre-wrap' }}>{p.notes}</div>}
        </div>
        <div className="row wrap">
          {!p.is_self && <button type="button" className={`btn ${on ? 'primary' : ''}`} onClick={() => setWithPeople(on ? withPeople.filter(x => x !== p.id) : [...withPeople, p.id])} title="Show only the trips with them across the whole app"><Icon name="users" size={15} />{on ? 'Showing only trips with them' : 'Show only trips with them'}</button>}
          <AddToTrip personId={p.id} name={p.name} onTrip={trips.map(t => t.id)} onAdded={reload} />
          {!p.is_self && <AccountButton id={p.id} name={p.name} userId={p.user_id} email={p.email} onDone={reload} />}
          <button type="button" className="btn" onClick={() => setEditing(true)}><Icon name="edit" size={15} />Edit</button>
          {!p.is_self && <button type="button" className="btn ghost" onClick={() => setMerging(true)} title="Fold a duplicate into someone else">Merge…</button>}
          {!p.is_self && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}>Remove</button>}
        </div>
      </div>
      <div className="stats">
        <div className="stat"><div className="n">{totals.trips}</div><div className="l">{p.is_self ? 'trips' : 'trips together'}{totals.upcoming > 0 && ` · +${totals.upcoming} planned`}</div></div>
        <div className="stat"><div className="n">{totals.days}</div><div className="l">days {p.is_self ? 'away' : 'together'}</div></div>
        <div className="stat"><div className="n">{totals.countries}</div><div className="l">countries</div></div>
      </div>
      <div className="country-grid">
        <Card title="Countries">
          {countries.length === 0 ? <p className="muted">None known yet.</p> : <div className="row wrap" style={{ gap: 6 }}>{countries.map(c => <Link key={c.code} to={`/countries/${c.code}`} className="country-chip">{flag(c.code)} {countryName(c.code)} <span className="muted">· {c.days} d</span></Link>)}</div>}
        </Card>
        <Card title={p.is_self ? 'Who you travel with' : 'Who else came along'}>
          {company.length === 0 ? <p className="muted">{p.is_self ? 'Nobody yet.' : 'Just the two of you.'}</p> : <div className="row wrap" style={{ gap: 6 }}>{company.slice(0, 20).map(c => { const other = people.find(x => x.name === c.name); return other ? <Link key={c.name} to={`/people/${other.id}`} className="country-chip"><PersonDot name={c.name} color={other.color} size={16} /> {c.name} <span className="muted">· {c.n}</span></Link> : <span key={c.name} className="country-chip">{c.name} <span className="muted">· {c.n}</span></span> })}</div>}
        </Card>
      </div>
      <h2 className="section-h">{p.is_self ? 'Your trips' : `Trips with ${p.name.split(' ')[0]}`}</h2>
      {trips.length === 0 ? <Card><Empty icon="luggage" title="No trips together yet">Add {p.name} to a trip from the trip's People.</Empty></Card> : (
        <div className="country-trips mb">
          {trips.map(t => (
            <Link key={t.id} to={`/trips/${t.id}`} className="ct-row" style={{ '--c': t.color || 'var(--accent)' } as React.CSSProperties}>
              <span className="ct-cover" style={t.has_cover ? { backgroundImage: `url(${V1}/trips/${t.id}/cover?v=${t.updated_at})` } : undefined} />
              <span className="grow">
                <b>{t.name}</b>{t.upcoming && <span className="badge">upcoming</span>}
                <span className="muted small block">{fmtRange(t.start_date, t.end_date)} · {STATUS_LABEL[t.status]}{t.others.length > 0 && ` · with ${t.others.join(', ')}`}</span>
              </span>
              <span className="ct-days"><b>{t.days}</b><span className="muted small"> days</span><span className="small block">{t.countries.filter(c => c.days > 0).map(c => flag(c.code)).join(' ')}</span></span>
            </Link>
          ))}
        </div>
      )}
      {editing && <PersonModal person={p} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); reload() }} />}
      {merging && <MergeModal id={p.id} name={p.name} onClose={() => setMerging(false)} onDone={into => { setMerging(false); reloadPeople(); navigate(`/people/${into}`) }} />}
      {del && <Confirm title={`Remove ${p.name}?`} message={`${p.name} leaves your people. On past trips they stay as a name, so the bills still add up.`} onCancel={() => setDel(false)} onConfirm={remove} />}
    </>
  )
}

function MergeModal({ id, name, onClose, onDone }: { id: number; name: string; onClose: () => void; onDone: (into: number) => void }) {
  const { people } = useApp()
  const { toast } = useToast()
  const [into, setInto] = useState('')
  const [busy, setBusy] = useState(false)
  const target = people.find(p => String(p.id) === into)
  const go = async () => {
    setBusy(true)
    try { await api.post(`${V1}/people/${id}/merge`, { into: Number(into) }); toast(`${name} merged into ${target?.name}`); onDone(Number(into)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={`Merge ${name} into…`} onClose={onClose}>
      <div className="grid" style={{ gap: 14 }}>
        <p className="muted">Pick who {name} really is. Every trip and bill of {name} moves to them, and {name} disappears from your people.</p>
        <Select value={into} onChange={setInto} options={people.filter(p => p.id !== id).map(p => ({ value: String(p.id), label: p.is_self ? `${p.name} (you)` : p.name, hint: plural(p.trips, 'trip') }))} placeholder="Choose someone…" />
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={!into || busy} onClick={go}>Merge</button></div>
      </div>
    </Modal>
  )
}

/** Put this person on one of your trips (upcoming ones first). */
function AddToTrip({ personId, name, onTrip, onAdded }: { personId: number; name: string; onTrip: number[]; onAdded: () => void }) {
  const { trips, user, reloadPeople } = useApp()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const mine = trips.filter(t => t.owner_id === user?.id && !onTrip.includes(t.id)).sort((a, b) => b.start_date.localeCompare(a.start_date))
  if (mine.length === 0) return null
  const add = async (id: string) => {
    if (!id) return
    setBusy(true)
    try { await api.post(`${V1}/trips/${id}/people`, { person_id: personId }); toast(`${name} added to ${mine.find(t => String(t.id) === id)?.name}`); reloadPeople(); onAdded() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return <div style={{ minWidth: 220 }}><Select value="" onChange={add} disabled={busy} options={mine.map(t => ({ value: String(t.id), label: t.name, hint: t.start_date ? t.start_date.slice(0, 7) : '' }))} placeholder="+ Add to a trip…" ariaLabel={`Add ${name} to a trip`} /></div>
}

/**
 * Make someone a user of this box (link an existing account, or create one —
 * admins only), or take the account away from them again. A user gets into
 * the trips they are on; the account itself is not deleted.
 */
function AccountButton({ id, name, userId, email, onDone }: { id: number; name: string; userId: number | null; email: string; onDone: () => void }) {
  const { users, user, status, reloadMeta, reloadPeople, people } = useApp()
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [unlink, setUnlink] = useState(false)
  const account = users.find(u => u.id === userId)
  const link = async (uid: number | null) => {
    await api.put(`${V1}/people/${id}`, { user_id: uid })
    await reloadPeople(); onDone()
  }
  if (userId != null) return (
    <>
      <button type="button" className="btn account-btn" onClick={() => setUnlink(true)} title={account ? `Signs in as ${account.email}` : undefined}><Icon name="user" size={15} />User{account ? ` · ${account.email}` : ''}<span className="muted small">· remove</span></button>
      {unlink && <Confirm title={`Remove ${name} as a user?`} confirmLabel="Remove as user" message={`${name} stays in your people and on their trips, but no longer gets into them with ${account?.email || 'their account'}. The account itself is kept.`} onCancel={() => setUnlink(false)} onConfirm={async () => { try { await link(null); toast(`${name} is no longer a user`) } catch (e) { toast((e as Error).message, 'err') } finally { setUnlink(false) } }} />}
    </>
  )
  // accounts nobody in your people stands for yet
  const free = users.filter(u => u.id !== user?.id && !people.some(p => p.user_id === u.id))
  const admin = user?.role === 'admin'
  if (!admin && free.length === 0) return null
  return (
    <>
      <button type="button" className="btn" onClick={() => setOpen(true)}><Icon name="user" size={15} />Make a user</button>
      {open && <MakeUserModal name={name} email={email} free={free} admin={admin} local={status?.auth_mode === 'local'} onClose={() => setOpen(false)}
        onLink={async uid => { try { await link(uid); toast(`${name} is a user now`); setOpen(false) } catch (e) { toast((e as Error).message, 'err') } }}
        onCreate={async f => {
          try { const u = await api.post<{ id: number }>(`${V1}/users`, { ...f, role: 'member' }); await reloadMeta(); await link(u.id); toast(`${name} can sign in now`); setOpen(false) } catch (e) { toast((e as Error).message, 'err') }
        }} />}
    </>
  )
}

function MakeUserModal({ name, email, free, admin, local, onClose, onLink, onCreate }: { name: string; email: string; free: { id: number; name: string; email: string }[]; admin: boolean; local: boolean; onClose: () => void; onLink: (id: number) => Promise<void>; onCreate: (f: { name: string; email: string; password: string }) => Promise<void> }) {
  const [how, setHow] = useState<'new' | 'existing'>(admin ? 'new' : 'existing')
  const [pick, setPick] = useState('')
  const [f, setF] = useState({ name, email, password: '' })
  const [busy, setBusy] = useState(false)
  const go = async (e: React.FormEvent) => { e.preventDefault(); setBusy(true); try { if (how === 'existing') await onLink(Number(pick)); else await onCreate(f) } finally { setBusy(false) } }
  return (
    <Modal title={`Make ${name} a user`} onClose={onClose}>
      <form onSubmit={go} className="grid" style={{ gap: 14 }}>
        <p className="muted small" style={{ margin: 0 }}>A user signs in here and gets into the trips {name} is on (as an editor — change it per trip). Their bills and trips stay as they are.</p>
        {admin && free.length > 0 && <Segmented value={how} onChange={setHow} options={[{ value: 'new', label: 'New account' }, { value: 'existing', label: 'Existing account' }]} size="sm" />}
        {how === 'existing' ? (
          <Field label="Account"><Select value={pick} onChange={setPick} options={free.map(u => ({ value: String(u.id), label: u.name || u.email, hint: u.email }))} placeholder="Choose an account…" /></Field>
        ) : <>
          <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required autoFocus /></Field>
          {local && <Field label="Password" help="At least 8 characters — tell it to them."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={8} required autoComplete="new-password" /></Field>}
        </>}
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || (how === 'existing' && !pick)}>{how === 'existing' ? 'Link' : 'Create user'}</button></div>
      </form>
    </Modal>
  )
}
