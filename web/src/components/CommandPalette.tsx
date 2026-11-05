// ⌘K / "/" — jump to a trip, one of its tabs or a page, or run a command from the keyboard.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { Icon } from './icons'
import { TripThumb, tripWhen } from './Sidebar'
import { describeDates, fold, hitLink, useSearch } from '../lib/search'
import { LODGING_ICONS, PLACE_ICONS } from '../lib/types'
import { fmtDate, fmtRange } from '../lib/format'
import { allCountries, flag } from '../lib/countries'
import { PersonDot } from './WithPeople'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

export const TRIP_TABS: [string, string, string][] = [['overview', 'Overview', 'luggage'], ['itinerary', 'Itinerary', 'calendar'], ['places', 'Places', 'pin'], ['map', 'Map', 'map'], ['stay', 'Stay & transport', 'bed'], ['budget', 'Budget', 'wallet'], ['spending', 'Spending', 'users'], ['lists', 'Checklists', 'checklist'], ['journal', 'Journal', 'book'], ['photos', 'Photos', 'camera'], ['documents', 'Documents', 'file'], ['settings', 'Settings', 'settings']]

export function CommandPalette({ onClose, commands }: { onClose: () => void; commands: PaletteCommand[] }) {
  const { trips, people } = useApp()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [hi, setHi] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  const { result, busy } = useSearch(q)
  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    const n = q.trim()
    const words = fold(n).split(/\s+/).filter(Boolean)
    const hit = (text: string) => { const t = fold(text); return words.every(w => t.includes(w)) }
    const found = result ? new Map(result.trips.map(t => [t.id, t])) : null
    for (const t of trips) {
      const text = `${t.name} ${t.destination} ${t.start_date.slice(0, 4)}`
      const srv = found?.get(t.id)
      if (n && !srv && !hit(text)) {
        // "lisbon budget" → the Budget tab of the Lisbon trip
        for (const [tab, label, icon] of TRIP_TABS) if (tab !== 'overview' && hit(`${text} ${label}`) && words.some(w => fold(label).includes(w)))
          out.push({ key: `t${t.id}-${tab}`, group: 'Trips', icon: <Icon name={icon} size={16} />, label: `${t.name} — ${label}`, run: () => navigate(`/trips/${t.id}/${tab}`) })
        continue
      }
      const via = srv && !srv.direct && srv.hits.length ? `has ${srv.hits.slice(0, 3).map(h => h.name).join(', ')}${srv.hits.length > 3 ? '…' : ''}` : ''
      out.push({ key: `t${t.id}`, group: 'Trips', icon: <TripThumb t={t} size={20} />, label: t.name, sub: [t.destination, tripWhen(t), via].filter(Boolean).join(' · '), run: () => navigate(`/trips/${t.id}`) })
    }
    if (result) {
      for (const h of result.places) out.push({ key: `p${h.id}`, group: 'Places', icon: <span>{PLACE_ICONS[h.category] || '📍'}</span>, label: h.name, sub: <>in <b>{h.trip_name}</b>{h.date ? ` · ${fmtDate(h.date, false)}` : h.trip_start ? ` · ${fmtRange(h.trip_start, h.trip_end)}` : ''}{h.address ? ` · ${h.address}` : ''}</>, run: () => navigate(hitLink(h)) })
      for (const h of result.stays) out.push({ key: `l${h.id}`, group: 'Stays', icon: <span>{LODGING_ICONS[h.category] || '🛏'}</span>, label: h.name, sub: <>in <b>{h.trip_name}</b>{h.date ? ` · ${fmtRange(h.date, h.date_end)}` : ''}{h.address ? ` · ${h.address}` : ''}</>, run: () => navigate(hitLink(h)) })
    }
    // people: "ana" → her page (and the trips together are found above)
    if (words.length) for (const p of people) {
      if (!hit(p.name)) continue
      out.push({ key: `pp${p.id}`, group: 'People', icon: <PersonDot name={p.name} color={p.color} size={18} />, label: p.name, sub: `${p.trips} trip${p.trips === 1 ? '' : 's'} together`, run: () => navigate(`/people/${p.id}`) })
    }
    // countries: "bosnia" → its page (with what you noted and every trip there)
    if (words.length) for (const c of allCountries()) {
      if (!hit(`${c.name} ${c.code}`)) continue
      out.push({ key: `c${c.code}`, group: 'Countries', icon: <span>{flag(c.code)}</span>, label: c.name, sub: 'Country facts, your notes and trips', run: () => navigate(`/countries/${c.code}`) })
      if (out.filter(r => r.group === 'Countries').length >= 4) break
    }
    for (const c of commands) {
      if (n && !hit(`${c.label} ${c.keywords || ''}`)) continue
      out.push({ key: c.id, group: 'Commands', icon: <Icon name={c.icon} size={16} />, label: c.label, hint: c.hint, run: c.run })
    }
    return out
  }, [q, trips, commands, navigate, result, people])
  const dates = describeDates(result)
  useEffect(() => setHi(0), [q])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Search and commands">
        <div className="pal-input">
          <Icon name="search" size={18} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search trips, places, stays — or try “summer 2025”…" aria-label="Search"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {dates && <div className="pal-group">Dates: {dates}</div>}
          {rows.length === 0 && <div className="pal-empty">{busy ? 'Searching…' : 'Nothing matches.'}</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow"><span className="pal-label">{r.label}</span>{r.sub && <span className="pal-sub">{r.sub}</span>}</span>
                  {r.hint && <kbd>{r.hint}</kbd>}
                  {i === hi && <Icon name="chevron-right" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
      </div>
    </div>, document.body)
}
