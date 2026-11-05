import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Person } from '../lib/types'
import { fold } from '../lib/search'
import { Icon } from './icons'

/** A round badge with someone's initials in their colour. */
export function PersonDot({ name, color, size = 22, account }: { name: string; color?: string; size?: number; account?: boolean }) {
  const ini = name.split(/\s+/).filter(Boolean).slice(0, 2).map(w => w[0]).join('').toUpperCase() || '?'
  return <span className={`person-dot ${account ? 'account' : ''}`} style={{ width: size, height: size, fontSize: size * .42, background: color || personColor(name) }} aria-hidden>{ini}</span>
}

const PALETTE = ['#0ea5e9', '#f97316', '#22c55e', '#a855f7', '#ef4444', '#eab308', '#14b8a6', '#ec4899', '#6366f1', '#84cc16']
/** A stable colour for someone without one. */
export function personColor(name: string): string {
  let h = 0
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) | 0
  return PALETTE[Math.abs(h) % PALETTE.length]
}

/**
 * "Travelling with": pick people and the whole app — trips, home, insights,
 * countries, search — shows only the trips you took with all of them.
 */
export function WithPicker() {
  const { people, withPeople, setWithPeople } = useApp()
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const h = (e: MouseEvent) => { if (root.current && !root.current.contains(e.target as Node)) setOpen(false) }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [open])
  // people removed from the book drop out of the filter
  useEffect(() => { if (people.length && withPeople.some(id => !people.some(p => p.id === id))) setWithPeople(withPeople.filter(id => people.some(p => p.id === id))) }, [people]) // eslint-disable-line react-hooks/exhaustive-deps
  const others = people.filter(p => !p.is_self)
  if (others.length === 0) return null
  const picked = withPeople.map(id => people.find(p => p.id === id)).filter(Boolean) as Person[]
  const list = others.filter(p => !q || fold(p.name).includes(fold(q))).sort((a, b) => b.trips - a.trips || a.name.localeCompare(b.name))
  const toggle = (id: number) => setWithPeople(withPeople.includes(id) ? withPeople.filter(x => x !== id) : [...withPeople, id])
  return (
    <div className="with-picker" ref={root}>
      <button type="button" className={`btn ghost ${picked.length ? 'with-on' : 'icon-only'}`} onClick={() => setOpen(o => !o)} aria-expanded={open} aria-haspopup="dialog" title="Travelling with — show only trips with these people">
        <Icon name="users" size={18} />
        {picked.length > 0 && <span className="with-names">{picked.map(p => p.name.split(' ')[0]).join(' + ')}</span>}
      </button>
      {open && (
        <div className="with-pop" role="dialog" aria-label="Travelling with">
          <div className="with-head"><b>Travelling with</b><span className="muted small">only trips with everyone picked</span></div>
          {others.length > 6 && <input className="with-search" value={q} onChange={e => setQ(e.target.value)} placeholder="Find someone…" autoFocus aria-label="Find someone" />}
          <div className="with-list">
            {list.map(p => (
              <label key={p.id} className="with-row">
                <input type="checkbox" checked={withPeople.includes(p.id)} onChange={() => toggle(p.id)} />
                <PersonDot name={p.name} color={p.color} />
                <span className="grow">{p.name}</span>
                <span className="muted small">{p.trips}</span>
              </label>
            ))}
            {list.length === 0 && <div className="muted small" style={{ padding: 8 }}>Nobody by that name.</div>}
          </div>
          <div className="with-foot">
            <Link to="/people" onClick={() => setOpen(false)} className="small">All people</Link>
            <span className="spacer" />
            {picked.length > 0 && <button type="button" className="btn ghost sm" onClick={() => setWithPeople([])}>Everyone</button>}
          </div>
        </div>
      )}
    </div>
  )
}

/** Shown above every page while the app is narrowed to some people. */
export function WithBanner() {
  const { people, withPeople, setWithPeople } = useApp()
  const picked = withPeople.map(id => people.find(p => p.id === id)).filter(Boolean) as Person[]
  if (!picked.length) return null
  return (
    <div className="with-banner">
      <Icon name="users" size={15} />
      <span>Only trips with {picked.map((p, i) => <span key={p.id}>{i > 0 && (i === picked.length - 1 ? ' and ' : ', ')}<Link to={`/people/${p.id}`}><b>{p.name}</b></Link></span>)}</span>
      <span className="spacer" />
      {picked.length > 1 && picked.map(p => <button key={p.id} type="button" className="chip sm" onClick={() => setWithPeople(withPeople.filter(x => x !== p.id))} title={`Drop ${p.name}`}>{p.name.split(' ')[0]} ×</button>)}
      <button type="button" className="btn ghost sm" onClick={() => setWithPeople([])}>Show everyone</button>
    </div>
  )
}
