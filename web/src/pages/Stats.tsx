import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Stats, StatsCountry, StatsTrip } from '../lib/types'
import { countryName, flag } from '../lib/countries'
import { fmtRange, plural, todayISO } from '../lib/format'
import { Card, Empty, Loading, PageHeader, Segmented, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

/** Where you went, for how long and when. */
export default function StatsPage() {
  const { data, error, loading, reload } = useAsync(() => api.get<Stats>(`${V1}/stats`), [])
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const [focus, setFocusNow] = useState<string | null>(null) // a country highlighted in the timeline
  // Leaving a tile keeps its highlight a moment: moving to the next tile crosses
  // the gap between them without the timeline flashing back to all trips.
  const clear = useRef<ReturnType<typeof setTimeout> | null>(null)
  const setFocus = useCallback((code: string | null) => {
    if (clear.current) { clearTimeout(clear.current); clear.current = null }
    if (code) setFocusNow(code)
    else clear.current = setTimeout(() => setFocusNow(null), 500)
  }, [])
  useEffect(() => () => { if (clear.current) clearTimeout(clear.current) }, [])
  const detect = async () => {
    setBusy(true)
    try { const r = await api.post<{ detected: number; missing: number }>(`${V1}/countries/detect`); toast(r.missing ? `Found ${r.detected}, ${r.missing} still to look up` : `Countries found for ${plural(r.detected, 'spot')}`); await reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return null
  const { totals } = data
  return (
    <>
      <PageHeader title="Insights" sub="Where you went, for how long, and when" actions={<Link className="btn" to="/countries"><Icon name="globe" size={15} />All countries</Link>} />
      {totals.pending_lookups > 0 && (
        <div className="hint-strip"><Icon name="compass" size={14} />Looking up the country of {plural(totals.pending_lookups, 'stay or place', 'stays and places')} from their coordinates — the numbers fill in as they are found.<span className="spacer" /><button type="button" className="btn sm" onClick={detect} disabled={busy}>{busy ? 'Looking up…' : 'Look up now'}</button></div>
      )}
      <div className="stats">
        <Link className="stat" to="/countries"><div className="n">{totals.countries}</div><div className="l">countries visited</div></Link>
        <div className="stat"><div className="n">{totals.days}</div><div className="l">days away</div></div>
        <Link className="stat" to="/trips"><div className="n">{totals.trips}</div><div className="l">trips taken</div></Link>
        <div className="stat"><div className="n">{totals.places_visited}</div><div className="l">places visited</div></div>
      </div>
      {data.trips.length === 0 ? <Card><Empty icon="globe" title="Nothing to look back on yet">Trips with dates show up here: the countries you were in, for how many days, and when.</Empty></Card> : <>
        <CountryStrip countries={data.countries} onFocus={setFocus} />
        <Timeline trips={data.trips} focus={focus} />
        <YearChart data={data} />
      </>}
    </>
  )
}

const ZOOM_KEY = 'pt.insights.tile'
const ORDER_KEY = 'pt.insights.order'
const GAP = 12

/**
 * Countries as tiles, most visited first, sliding left to right. Each tile is a
 * glass filled with its share of the days spent in the countries in view: scroll
 * past the big ones and the rest fill up. Zoom (− / +, or ctrl/pinch + wheel)
 * picks how many fit.
 */
function CountryStrip({ countries, onFocus }: { countries: StatsCountry[]; onFocus: (code: string | null) => void }) {
  const navigate = useNavigate()
  const box = useRef<HTMLDivElement>(null)
  const [tile, setTile] = useState(() => { try { return Number(localStorage.getItem(ZOOM_KEY)) || 150 } catch { return 150 } })
  const [order, setOrderNow] = useState<'visits' | 'days'>(() => { try { return localStorage.getItem(ORDER_KEY) === 'days' ? 'days' : 'visits' } catch { return 'visits' } })
  const setOrder = (o: 'visits' | 'days') => { setOrderNow(o); try { localStorage.setItem(ORDER_KEY, o) } catch { /* private mode */ } ; box.current?.scrollTo({ left: 0 }) }
  const [win, setWin] = useState<[number, number]>([0, 0])
  const list = useMemo(() => [...countries].sort((a, b) => order === 'days'
    ? (b.days - a.days) || (b.trips.length - a.trips.length) || (b.planned_days - a.planned_days)
    : (b.trips.length - a.trips.length) || (b.days - a.days) || (b.planned_days - a.planned_days)), [countries, order])
  const measure = useCallback(() => {
    const el = box.current
    if (!el) return
    const step = tile + GAP
    // a tile counts as in view when at least half of it shows
    const first = Math.max(0, Math.round(el.scrollLeft / step))
    const last = Math.min(list.length - 1, Math.floor((el.scrollLeft + el.clientWidth - tile / 2) / step))
    setWin(w => w[0] === first && w[1] === last ? w : [first, last])
  }, [tile, list.length])
  useLayoutEffect(() => { measure() }, [measure])
  useEffect(() => {
    const el = box.current
    if (!el) return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [measure])
  const zoom = useCallback((dir: number, anchor?: number) => {
    setTile(t => {
      const n = Math.max(96, Math.min(260, Math.round(t * (dir > 0 ? 1.15 : 1 / 1.15))))
      try { localStorage.setItem(ZOOM_KEY, String(n)) } catch { /* private mode */ }
      // keep the tile under the pointer (or the first in view) where it was
      const el = box.current
      if (el) { const a = anchor ?? 0; const idx = (el.scrollLeft + a) / (t + GAP); requestAnimationFrame(() => { el.scrollLeft = idx * (n + GAP) - a }) }
      return n
    })
  }, [])
  useEffect(() => {
    const el = box.current
    if (!el) return
    const wheel = (e: WheelEvent) => {
      if (e.ctrlKey || e.metaKey) { e.preventDefault(); zoom(e.deltaY < 0 ? 1 : -1, e.clientX - el.getBoundingClientRect().left) }
      else if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) { e.preventDefault(); el.scrollLeft += e.deltaY } // a mouse wheel slides the strip
    }
    el.addEventListener('wheel', wheel, { passive: false })
    return () => el.removeEventListener('wheel', wheel)
  }, [zoom])
  const slide = (dir: number) => { const el = box.current; if (el) el.scrollBy({ left: dir * Math.max(tile + GAP, el.clientWidth - tile), behavior: 'smooth' }) }
  if (list.length === 0) return <Card title="Countries" className="mb"><p className="muted">No countries known yet. They come from the coordinates of stays, places and trip destinations.</p></Card>
  const visible = list.slice(win[0], win[1] + 1)
  const inView = (i: number) => i >= win[0] && i <= win[1]
  // what the mode is about: visits (trips) or days
  const value = (c: StatsCountry) => order === 'days' ? c.days : c.trips.length
  // the percentage is each country's share of all of them; the water only compares what is in view
  const total = list.reduce((n, c) => n + value(c), 0)
  const maxInView = Math.max(1, ...visible.map(value))
  const sum = visible.reduce((n, c) => n + c.days, 0)
  return (
    <Card title={`Countries · ${list.filter(c => c.days > 0).length}`} className="mb" actions={
      <div className="row" style={{ gap: 4 }}>
        <Segmented value={order} onChange={setOrder} options={[{ value: 'visits', label: 'Visits' }, { value: 'days', label: 'Days' }]} size="sm" label="Order by" />
        <span className="muted small hide-sm" style={{ marginLeft: 6 }}>{visible.length} in view · {plural(sum, 'day')}</span>
        <button type="button" className="icon-btn" onClick={() => zoom(1)} disabled={tile >= 260} title="Fewer, bigger (ctrl + wheel)" aria-label="Zoom in"><Icon name="plus" size={15} /></button>
        <button type="button" className="icon-btn" onClick={() => zoom(-1)} disabled={tile <= 96} title="More, smaller (ctrl + wheel)" aria-label="Zoom out"><Icon name="minus" size={15} /></button>
      </div>}>
      <p className="muted small" style={{ marginTop: -4 }}>{order === 'days' ? 'Most days first.' : 'Most visited first.'} The percentage is the share of all your {order === 'days' ? 'days away' : 'visits'}; the water compares the countries in view — slide on and the rest fill up.</p>
      <div className="cstrip-wrap">
        <button type="button" className="cstrip-nav left" onClick={() => slide(-1)} disabled={win[0] === 0} aria-label="Previous countries"><Icon name="chevron-left" size={18} /></button>
        <div className="cstrip" ref={box} onScroll={measure} style={{ '--tile': `${tile}px`, '--gap': `${GAP}px` } as React.CSSProperties} role="list">
          {list.map((c, i) => {
            const v = value(c)
            const share = total > 0 ? v / total * 100 : 0
            const level = inView(i) ? v / maxInView * 92 : 0
            const avg = c.trips.length ? c.days / c.trips.length : 0
            return (
              <button type="button" key={c.code} role="listitem" className={`ctile ${inView(i) ? '' : 'out'} ${c.days === 0 ? 'only-planned' : ''}`} onClick={() => navigate(`/countries/${c.code}`)} onMouseEnter={() => onFocus(c.code)} onMouseLeave={() => onFocus(null)} onFocus={() => onFocus(c.code)} onBlur={() => onFocus(null)}
                title={`${countryName(c.code)} — ${plural(c.trips.length, 'visit')}, ${plural(c.days, 'day')}${c.planned_days ? ` (+${c.planned_days} upcoming)` : ''}, ${avg.toFixed(1)} days per visit · ${share.toFixed(1)}% of all ${order === 'days' ? 'days' : 'visits'}`}>
                <span className="liquid" style={{ height: `${level}%` }}><svg className="wave" viewBox="0 0 120 10" preserveAspectRatio="none" aria-hidden><path d="M0 5 Q 15 0 30 5 T 60 5 T 90 5 T 120 5 V10 H0 Z" /></svg></span>
                {share > 0 && <span className="ct-share">{share < 10 ? share.toFixed(1) : Math.round(share)}%</span>}
                <span className="ct-top"><span className="ct-flag">{flag(c.code)}</span><span className="ct-name">{countryName(c.code)}</span></span>
                <span className="ct-main"><b>{v}</b><small>{order === 'days' ? (v === 1 ? 'day' : 'days') : (v === 1 ? 'visit' : 'visits')}</small></span>
                <span className="ct-sub">{order === 'days' ? plural(c.trips.length, 'visit') : plural(c.days, 'day')}{c.planned_days > 0 && <> · +{c.planned_days} d</>}{avg > 0 && <> · ⌀ {avg < 10 ? avg.toFixed(1) : Math.round(avg)} d</>}</span>
              </button>
            )
          })}
        </div>
        <button type="button" className="cstrip-nav right" onClick={() => slide(1)} disabled={win[1] >= list.length - 1} aria-label="Next countries"><Icon name="chevron-right" size={18} /></button>
      </div>
    </Card>
  )
}

const MONTHS = ['J', 'F', 'M', 'A', 'M', 'J', 'J', 'A', 'S', 'O', 'N', 'D']
const dayOfYear = (d: Date) => Math.floor((Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()) - Date.UTC(d.getUTCFullYear(), 0, 1)) / 864e5)
const daysIn = (y: number) => (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0 ? 366 : 365

/**
 * One row per year, January to December; each trip a bar where it fell. Hover a
 * country tile to light up its trips; click a year for its trips in detail.
 */
function Timeline({ trips, focus }: { trips: StatsTrip[]; focus: string | null }) {
  const navigate = useNavigate()
  const [hover, setHover] = useState<{ t: StatsTrip; x: number; y: number } | null>(null)
  const [open, setOpen] = useState<number | null>(null)
  const years = useMemo(() => {
    const by = new Map<number, { items: { t: StatsTrip; from: number; to: number; days: number }[]; days: number }>()
    const today = todayISO()
    for (const t of trips) {
      const s = new Date(t.start_date + 'T00:00:00Z'), e = new Date(t.end_date + 'T00:00:00Z')
      for (let y = s.getUTCFullYear(); y <= e.getUTCFullYear(); y++) {
        const from = y === s.getUTCFullYear() ? dayOfYear(s) : 0
        const to = y === e.getUTCFullYear() ? dayOfYear(e) + 1 : daysIn(y)
        if (!by.has(y)) by.set(y, { items: [], days: 0 })
        const row = by.get(y)!
        row.items.push({ t, from: from / daysIn(y), to: to / daysIn(y), days: to - from })
        if (t.start_date <= today) row.days += to - from
      }
    }
    return [...by.entries()].sort((a, b) => b[0] - a[0])
  }, [trips])
  const today = todayISO()
  const ty = Number(today.slice(0, 4)), tpos = dayOfYear(new Date(today + 'T00:00:00Z')) / daysIn(ty)
  const lit = (t: StatsTrip) => !focus || t.countries.some(c => c.code === focus)
  const tip = (e: React.SyntheticEvent<HTMLElement>, t: StatsTrip) => { const r = (e.currentTarget.closest('.tl') as HTMLElement).getBoundingClientRect(), b = e.currentTarget.getBoundingClientRect(); setHover({ t, x: b.left - r.left + b.width / 2, y: b.top - r.top }) }
  return (
    <Card title="Timeline" className="mb" actions={focus && <span className="small muted">{flag(focus)} {countryName(focus)}</span>}>
      <div className={`tl ${focus ? 'focused' : ''}`} onMouseLeave={() => setHover(null)}>
        <div className="tl-row tl-axis"><span className="tl-year" /><span className="tl-track">{MONTHS.map((m, i) => <span key={i} style={{ left: `${i / 12 * 100}%` }}>{m}</span>)}</span><span className="tl-sum" /></div>
        {years.map(([y, row]) => (
          <div key={y}>
            <div className="tl-row">
              <button type="button" className={`tl-year ${open === y ? 'open' : ''}`} onClick={() => setOpen(open === y ? null : y)} aria-expanded={open === y} title="Show this year's trips">{y}</button>
              <span className="tl-track">
                {MONTHS.map((_, i) => <i key={i} className="tl-grid" style={{ left: `${i / 12 * 100}%` }} />)}
                {y === ty && <i className="tl-today" style={{ left: `${tpos * 100}%` }} title="Today" />}
                {row.items.map(({ t, from, to }) => (
                  <button type="button" key={t.id} className={`tl-bar ${t.upcoming ? 'upcoming' : ''} ${lit(t) ? 'lit' : 'dim'}`} style={{ left: `${from * 100}%`, width: `max(6px, ${(to - from) * 100}%)`, '--c': t.color || 'var(--accent)' } as React.CSSProperties}
                    onClick={() => navigate(`/trips/${t.id}`)} onMouseEnter={e => tip(e, t)} onFocus={e => tip(e, t)} onBlur={() => setHover(null)} aria-label={`${t.name}, ${fmtRange(t.start_date, t.end_date)}`}>
                    {(to - from) > 0.06 && <span>{t.countries.filter(c => c.days > 0).slice(0, 3).map(c => flag(c.code)).join('')}</span>}
                  </button>
                ))}
              </span>
              <span className="tl-sum">{row.days > 0 ? <><b>{row.days}</b> d · {row.items.filter(x => !x.t.upcoming).length}</> : <span className="muted">planned</span>}</span>
            </div>
            {open === y && (
              <div className="tl-detail">
                {[...row.items].sort((a, b) => a.from - b.from).map(({ t, days }) => (
                  <Link key={t.id} to={`/trips/${t.id}`} className="tl-detail-row" style={{ '--c': t.color || 'var(--accent)' } as React.CSSProperties}>
                    <i className="dot" /><b>{t.name}</b><span className="muted small">{fmtRange(t.start_date, t.end_date)}</span>
                    <span className="spacer" />
                    <span className="small">{t.countries.filter(c => c.days > 0).map(c => `${flag(c.code)} ${c.days}`).join('  ')}</span>
                    <span className="small muted" style={{ minWidth: 48, textAlign: 'right' }}>{days} d</span>
                  </Link>
                ))}
              </div>
            )}
          </div>
        ))}
        {hover && (
          <div className="tl-tip" style={{ left: hover.x, top: hover.y }}>
            <b>{hover.t.name}</b>
            <div className="muted small">{fmtRange(hover.t.start_date, hover.t.end_date)} · {plural(hover.t.days, 'day')}{hover.t.upcoming ? ' · upcoming' : ''}</div>
            {hover.t.countries.length > 0 && <div className="small">{hover.t.countries.filter(c => c.days > 0).map(c => `${flag(c.code)} ${countryName(c.code)} ${c.days}d`).join(' · ')}</div>}
          </div>
        )}
      </div>
      <div className="legend"><span className="key"><i className="sw" style={{ background: 'var(--accent)' }} />trip, in its colour</span><span className="key"><i className="sw planned" />upcoming</span><span className="key"><i className="sw" style={{ background: 'var(--danger, #dc2626)', width: 2 }} />today</span><span className="muted">· click a year for its trips</span></div>
    </Card>
  )
}

/** Days away per calendar year, with the countries that were new that year. */
function YearChart({ data }: { data: Stats }) {
  if (data.years.length === 0) return null
  const max = Math.max(1, ...data.years.map(y => y.days))
  return (
    <Card title="Days away per year" className="mb">
      <div className="vbars" role="list">
        {data.years.map(y => (
          <div key={y.year} className="vbar" role="listitem" title={`${y.year}: ${plural(y.days, 'day')} away on ${plural(y.trips, 'trip')}${y.countries.length ? ` · ${y.countries.map(countryName).join(', ')}` : ''}${y.new_countries.length ? ` · new: ${y.new_countries.map(countryName).join(', ')}` : ''}`}>
            <span className="vbar-value">{y.days}</span>
            <span className="vbar-track"><span className="vbar-fill" style={{ height: `${y.days / max * 100}%` }} /></span>
            <span className="vbar-label">{y.year}</span>
            <span className="vbar-new">{y.new_countries.map(flag).join('')}</span>
          </div>
        ))}
      </div>
      <p className="muted small mt-sm">Flags under a year are countries visited for the first time that year.</p>
    </Card>
  )
}
