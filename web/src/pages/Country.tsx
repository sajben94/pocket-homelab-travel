import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { CountryNote, CountryPage as Page } from '../lib/types'
import { LODGING_ICONS, PLACE_ICONS, STATUS_LABEL } from '../lib/types'
import { countryName, flag } from '../lib/countries'
import { fmtDate, fmtRange, plural } from '../lib/format'
import { Card, Empty, Field, Loading, Modal, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { MapView, type Marker } from '../components/Map'
import { FACT_LABELS, FactsGrid, factValues } from '../components/CountryFacts'

/** One country: what it is, what you noted, and every trip you took there. */
export default function CountryPage() {
  const { code = '' } = useParams()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useAsync(() => api.get<Page>(`${V1}/countries/${code.toUpperCase()}`), [code])
  const [editing, setEditing] = useState(false)
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error} — <Link to="/countries">all countries</Link></span></div>
  if (!data) return null
  const { country: c, note, trips = [], pins = [], totals, wiki } = data
  const markers: Marker[] = pins.map(p => ({ id: `${p.kind}${p.id}`, lat: p.lat, lng: p.lng, label: p.name, icon: p.kind === 'lodging' ? LODGING_ICONS[p.category] || '🛏' : PLACE_ICONS[p.category] || '📍', kind: p.kind === 'lodging' ? 'lodging' : p.status === 'visited' ? 'visited' : p.status === 'idea' ? 'idea' : 'booked', popup: `<b>${esc(p.name)}</b><br>${esc(p.trip_name)}`, onClick: () => navigate(`/trips/${p.trip_id}/${p.kind === 'lodging' ? 'stay' : 'places'}?open=${p.kind}:${p.id}`) }))
  return (
    <>
      <div className="country-hero">
        <span className="ch-flag" aria-hidden>{flag(c.code)}</span>
        <div className="grow">
          <div className="muted small"><Link to="/countries">Countries</Link> · {[c.region, c.subregion].filter(Boolean).join(' · ')}</div>
          <h1>{c.name}</h1>
          {c.official !== c.name && <div className="muted">{c.official}</div>}
        </div>
        <button type="button" className="btn" onClick={() => setEditing(true)}><Icon name="edit" size={15} />Edit facts & notes</button>
      </div>

      {totals && totals.trips > 0 ? (
        <div className="stats">
          <div className="stat"><div className="n">{totals.days}</div><div className="l">days there{totals.planned_days > 0 && <> · +{totals.planned_days} upcoming</>}</div></div>
          <div className="stat"><div className="n">{totals.trips}</div><div className="l">{totals.trips === 1 ? 'trip' : 'trips'}</div></div>
          {totals.first && <div className="stat"><div className="n sm">{fmtDate(totals.first)}</div><div className="l">first time</div></div>}
          {totals.last && totals.last !== totals.first && <div className="stat"><div className="n sm">{fmtDate(totals.last)}</div><div className="l">last time</div></div>}
        </div>
      ) : <div className="hint-strip"><Icon name="compass" size={14} />You haven't been here yet — the facts below are ready for when you plan it.</div>}

      <div className="country-grid">
        <Card title="Facts">
          <FactsGrid c={c} note={note} />
          {c.borders.length > 0 && <div className="mt-sm"><div className="muted small mb-xs">Neighbours</div><div className="row wrap" style={{ gap: 6 }}>{c.borders.map(b => <Link key={b} to={`/countries/${b}`} className="country-chip">{flag(b)} {countryName(b)}</Link>)}</div></div>}
          {c.landlocked && <p className="muted small mt-sm">Landlocked.</p>}
        </Card>
        <Card title="About" actions={wiki?.url && <a className="btn ghost sm" href={wiki.url} target="_blank" rel="noreferrer"><Icon name="external" size={13} />Wikipedia</a>}>
          {wiki?.extract ? <p className="about-text">{wiki.extract}</p> : <p className="muted">No summary available.</p>}
        </Card>
      </div>

      <Card title="Your notes" className="mb" actions={<button type="button" className="btn ghost sm" onClick={() => setEditing(true)}><Icon name="edit" size={13} />Edit</button>}>
        {note.notes ? <p className="about-text" style={{ whiteSpace: 'pre-wrap' }}>{note.notes}</p> : <p className="muted">Visa, plugs, tipping, what to eat, phrases — anything you want to remember about {c.name}. It shows on every trip that goes here.</p>}
      </Card>

      {trips.length > 0 && <>
        <h2 className="section-h">Trips to {c.name}</h2>
        <div className="country-trips mb">
          {trips.map(t => (
            <Link key={t.id} to={`/trips/${t.id}`} className="ct-row" style={{ '--c': t.color || 'var(--accent)' } as React.CSSProperties}>
              <span className="ct-cover" style={t.has_cover ? { backgroundImage: `url(${V1}/trips/${t.id}/cover?v=${t.updated_at})` } : undefined} />
              <span className="grow">
                <b>{t.name}</b>{t.upcoming && <span className="badge">upcoming</span>}
                <span className="muted small block">{fmtRange(t.start_date, t.end_date)} · {STATUS_LABEL[t.status]}</span>
              </span>
              <span className="ct-days"><b>{t.days}</b><span className="muted small"> {t.days === 1 ? 'day' : 'days'} here{t.trip_days > t.days ? ` of ${t.trip_days}` : ''}</span>{t.others.length > 0 && <span className="small block">also {t.others.map(flag).join(' ')}</span>}</span>
            </Link>
          ))}
        </div>
      </>}
      {markers.length > 0 && <Card title={`Where you stayed and went · ${plural(markers.length, 'pin')}`} className="mb" flush><MapView className="tall" markers={markers} /></Card>}
      {trips.length === 0 && <Card className="mb"><Empty icon="luggage" title={`No trips to ${c.name} yet`}><Link to="/trips?new=1">Plan one</Link> — once a stay or place is here, the trip shows up on this page.</Empty></Card>}
      {editing && <NoteModal name={c.name} note={note} defaults={Object.fromEntries(factValues(c, null).map(r => [r.key, typeof r.value === 'string' ? r.value : '']))} code={c.code} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); reload() }} />}
    </>
  )
}

/** Correct the facts for yourself, add your own (plugs, emergency number…), and write notes. */
function NoteModal({ code, name, note, defaults, onClose, onSaved }: { code: string; name: string; note: CountryNote; defaults: Record<string, string>; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [facts, setFacts] = useState<Record<string, string>>({ ...note.facts })
  const [extra, setExtra] = useState(note.extra.length ? note.extra : [])
  const [notes, setNotes] = useState(note.notes)
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api.put(`${V1}/countries/${code}/note`, { facts, extra, notes }); toast('Saved'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const SUGGEST = ['Emergency number', 'Plugs & voltage', 'Visa', 'Tipping', 'Tap water', 'Tolls / vignette']
  return (
    <Modal title={`${flag(code)} ${name}`} onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Notes" help="Shown on this page and on every trip that goes here."><textarea value={notes} onChange={e => setNotes(e.target.value)} rows={5} placeholder="Visa on arrival 30 days. Cash for buses. Try burek at…" /></Field>
        <Field group label="Your own facts">
          <div className="grid" style={{ gap: 6 }}>
            {extra.map((f, i) => (
              <div key={i} className="row" style={{ gap: 6 }}>
                <input value={f.label} onChange={e => setExtra(x => x.map((y, j) => j === i ? { ...y, label: e.target.value } : y))} placeholder="Label" style={{ maxWidth: 180 }} aria-label="Fact label" />
                <input value={f.value} onChange={e => setExtra(x => x.map((y, j) => j === i ? { ...y, value: e.target.value } : y))} placeholder="Value" className="grow" aria-label="Fact value" />
                <button type="button" className="icon-btn" aria-label="Remove fact" onClick={() => setExtra(x => x.filter((_, j) => j !== i))}><Icon name="x" size={14} /></button>
              </div>
            ))}
            <div className="row wrap" style={{ gap: 6 }}>
              <button type="button" className="btn sm" onClick={() => setExtra(x => [...x, { label: '', value: '' }])}><Icon name="plus" size={13} />Fact</button>
              {SUGGEST.filter(s => !extra.some(f => f.label === s)).map(s => <button key={s} type="button" className="chip sm" onClick={() => setExtra(x => [...x, { label: s, value: '' }])}>+ {s}</button>)}
            </div>
          </div>
        </Field>
        <Field group label="Correct the built-in facts" help="Leave empty to keep the built-in value.">
          <div className="two-col tight">
            {Object.entries(FACT_LABELS).map(([k, label]) => (
              <Field key={k} label={label}><input value={facts[k] || ''} onChange={e => setFacts(f => ({ ...f, [k]: e.target.value }))} placeholder={defaults[k] || ''} /></Field>
            ))}
          </div>
        </Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Modal>
  )
}

function esc(s: string): string { return s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] || c)) }
