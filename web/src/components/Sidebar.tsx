// Left column: new trip, sections, the trips, settings — the Pocket Calendar sidebar.
import { useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Trip } from '../lib/types'
import { daysBetween, todayISO } from '../lib/format'
import { Icon } from './icons'
import type { AddKind } from './Layout'
import { type Anchor, type MenuItem, Menu, rectOf } from './ui'

/** Running, upcoming (or undated) and past trips, each in the order you would look for them. */
export function splitTrips(trips: Trip[]) {
  const today = todayISO()
  const upcoming = trips.filter(t => !t.end_date || t.end_date >= today).sort((a, b) => (a.start_date || '9999').localeCompare(b.start_date || '9999'))
  const past = trips.filter(t => t.end_date && t.end_date < today).sort((a, b) => b.end_date.localeCompare(a.end_date))
  return { upcoming, past }
}

/** "Day 3 of 10", "in 12 days", or the month it was. */
export function tripWhen(t: Trip, short = false): string {
  const today = todayISO()
  if (!t.start_date) return short ? '' : 'no dates'
  if (t.start_date <= today && (!t.end_date || t.end_date >= today)) return short ? `day ${daysBetween(t.start_date, today) + 1}/${t.days}` : `Day ${daysBetween(t.start_date, today) + 1} of ${t.days}`
  if (t.start_date > today) { const n = daysBetween(today, t.start_date); return short ? `${n} d` : n === 1 ? 'tomorrow' : `in ${n} days` }
  return new Date(t.start_date + 'T12:00:00').toLocaleDateString(undefined, { month: 'short', year: '2-digit' })
}

/** The trip's cover photo, or its colour with a suitcase. */
export function TripThumb({ t, size = 22 }: { t: Trip; size?: number }) {
  return (
    <span className="trip-ico" style={{ width: size, height: size, '--c': t.color || 'var(--accent)' } as React.CSSProperties}>
      {t.has_cover ? <img src={`${V1}/trips/${t.id}/cover?v=${t.updated_at}`} alt="" loading="lazy" /> : <Icon name="luggage" size={Math.round(size * 0.62)} strokeWidth={2} />}
    </span>
  )
}

export function Sidebar({ onAdd, onNavigate, target }: { onAdd: (k: AddKind, t?: Trip) => void; onNavigate: () => void; target?: Trip }) {
  const { trips, status, settings } = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const { upcoming, past } = splitTrips(trips)
  const title = settings?.title || status?.title || 'Pocket Travel'
  const today = todayISO()
  const running = trips.filter(t => t.start_date && t.start_date <= today && t.end_date >= today).length
  const nav = [
    { to: '/', label: 'Home', icon: 'home', end: true },
    { to: '/trips', label: 'Trips', icon: 'luggage', end: true, count: trips.length },
    { to: '/insights', label: 'Insights', icon: 'chart', end: true },
    { to: '/countries', label: 'Countries', icon: 'globe', end: false },
    { to: '/people', label: 'People', icon: 'users', end: false },
  ]
  return (
    <aside className="sidebar" aria-label="Travel">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Travel' ? title : <>Pocket <b>Travel</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={addItems(onAdd, target)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {running > 0 && n.to === '/trips' ? <span className="badge accent" title="On the road">{running} now</span> : !!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <TripGroup title="Upcoming" list={upcoming} onAdd={onAdd} onNavigate={onNavigate} add={() => onAdd('trip')} />
      {past.length > 0 && <TripGroup title="Past trips" list={past} onAdd={onAdd} onNavigate={onNavigate} closed />}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pt:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

/** The New menu: a trip, or something on the trip in front of you. */
export function addItems(onAdd: (k: AddKind, t?: Trip) => void, target?: Trip): MenuItem[] {
  return [
    { label: 'New trip', icon: 'luggage', hint: 'N', onSelect: () => onAdd('trip') },
    ...(target ? ['sep' as const, { heading: target.name },
      { label: 'Place', icon: 'pin', hint: 'P', onSelect: () => onAdd('place', target) },
      { label: 'Cost', icon: 'wallet', hint: 'C', onSelect: () => onAdd('expense', target) },
      { label: 'Stay', icon: 'bed', onSelect: () => onAdd('lodging', target) },
      { label: 'Transport', icon: 'plane', onSelect: () => onAdd('transport', target) },
      { label: 'Journal entry', icon: 'book', hint: 'J', onSelect: () => onAdd('journal', target) },
    ] : []),
  ]
}

function TripGroup({ title, list, onAdd, onNavigate, add, closed }: { title: string; list: Trip[]; onAdd: (k: AddKind, t?: Trip) => void; onNavigate: () => void; add?: () => void; closed?: boolean }) {
  const [open, setOpen] = useState(!closed)
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={() => setOpen(o => !o)}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />{title}</button>
        {add && <button type="button" className="btn ghost icon-only xs" aria-label="New trip" title="New trip (n)" onClick={add}><Icon name="plus" size={14} /></button>}
      </h4>
      {open && (list.length === 0
        ? <p className="sb-empty">Nothing planned. <button type="button" className="link-btn" onClick={add}>Plan a trip</button></p>
        : <ul className="cal-list">{list.map(t => <TripRow key={t.id} t={t} onAdd={onAdd} onNavigate={onNavigate} />)}</ul>)}
    </section>
  )
}

function TripRow({ t, onAdd, onNavigate }: { t: Trip; onAdd: (k: AddKind, t?: Trip) => void; onNavigate: () => void }) {
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const go = (tab = '') => { onNavigate(); navigate(`/trips/${t.id}${tab ? `/${tab}` : ''}`) }
  const canEdit = t.role !== 'viewer'
  const today = todayISO()
  const now = t.start_date && t.start_date <= today && t.end_date >= today
  const items: MenuItem[] = [
    { label: 'Open', icon: 'luggage', onSelect: () => go() },
    ...(canEdit ? [{ label: 'Add a place', icon: 'pin', onSelect: () => onAdd('place', t) }, { label: 'Add a cost', icon: 'wallet', onSelect: () => onAdd('expense', t) }] : []),
    'sep',
    { label: 'Itinerary', icon: 'calendar', onSelect: () => go('itinerary') },
    { label: 'Map', icon: 'map', onSelect: () => go('map') },
    { label: 'Budget', icon: 'wallet', onSelect: () => go('budget') },
    { label: 'Checklists', icon: 'checklist', onSelect: () => go('lists') },
    { label: 'Journal', icon: 'book', onSelect: () => go('journal') },
    { label: 'Photos', icon: 'camera', onSelect: () => go('photos') },
    'sep',
    { label: 'Trip settings', icon: 'settings', onSelect: () => go('settings') },
  ]
  const when = tripWhen(t, true)
  return (
    <li className="cal-row trip-row">
      <NavLink to={`/trips/${t.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={[t.name, t.destination].filter(Boolean).join(' · ')}>
        <TripThumb t={t} size={22} />
        <span className="cal-name ellipsis">{t.name}</span>
        {when && <span className={`trip-meta ${now ? 'now' : ''}`}>{when}</span>}
      </NavLink>
      <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${t.name}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
      {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={210} />}
    </li>
  )
}
