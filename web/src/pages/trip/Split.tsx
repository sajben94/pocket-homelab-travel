import { useEffect, useMemo, useRef, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Expense, SplitInfo, TripCategory, TripPaymentMode } from '../../lib/types'
import { ago, fmtDate, groupBills, plural, todayISO, weekday, type GroupedBill } from '../../lib/format'
import { Card, Confirm, Empty, Field, Modal, useAsync, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select } from '../../components/Select'
import { AmountInput, Money } from '../../components/Money'
import { PersonDot } from '../../components/WithPeople'
import { useTrip } from './ctx'
import { CospendLinkCard } from './CospendLink'
import { PeopleStrip } from './TripPeople'
import { CospendImport } from '../../components/CospendImport'

/**
 * The trip's money between people, as in Nextcloud Cospend: every spent cost
 * is a bill someone paid for some of the people here; money handed over
 * (an advance, paying someone back) counts too. Balances say who is up or
 * down, the plan says who pays whom, and clicking someone shows what they
 * paid and what their share of everything was.
 */
export function Split() {
  const { ov, trip, canEdit, edit, reload, go } = useTrip()
  const { user } = useApp()
  const { toast } = useToast()
  const split = ov.split
  const [mode, setMode] = useState('')
  const [cat, setCat] = useState('')
  const [syncing, setSyncing] = useState(false)
  const [lists, setLists] = useState(false)
  const [focus, setFocus] = useState<number | null>(null)
  const [center, setCenter] = useState('')
  const [transfer, setTransfer] = useState<{ from?: number; to?: number; amount?: number } | null>(null)
  const { data: centered } = useAsync(() => center ? api.get<SplitInfo>(`${V1}/trips/${trip.id}/split?center=${center}`) : Promise.resolve(null), [center, ov.split])
  if (!split) return <Card><Empty icon="wallet" title="Nothing to split yet" /></Card>
  const people = split.people
  const name = (id: number | null) => people.find(p => p.id === id)?.name || '—'
  const person = (id: number | null) => people.find(p => p.id === id)
  const cats = new Map(split.categories.map(c => [c.key, c]))
  const modes = new Map(split.payment_modes.map(m => [m.id, m]))
  const all = groupBills(ov.expenses.filter(e => e.kind === 'actual'))
  const bills = all.filter(e => (!mode || e.payment_mode_id === Number(mode)) && (!cat || e.category === cat))
  // only what the bills use is offered as a filter
  const usedCats = split.categories.filter(c => all.some(b => b.category === c.key))
  const usedModes = split.payment_modes.filter(m => all.some(b => b.payment_mode_id === m.id))
  const spentAll = split.balances.reduce((n, b) => n + b.spent, 0)
  const integ = ov.integrations.firefly
  const sync = async () => {
    setSyncing(true)
    try { const r = await api.post<{ synced: number }>(`${V1}/trips/${trip.id}/money/sync`); toast(`Synced ${r.synced} transactions from Pocket Money`); await reload() } catch (e) { toast((e as Error).message, 'err') } finally { setSyncing(false) }
  }
  const me = people.find(p => p.user_id === user?.id)
  const myBalance = me ? split.balances.find(b => b.member_id === me.id) : undefined
  const maxAbs = Math.max(1, ...split.balances.map(b => Math.abs(b.balance)))
  const cur = split.currency
  const plan = center && centered ? centered.settlement : split.settlement
  const addBill = () => edit('expense', null, { kind: 'actual', date: new Date().toISOString().slice(0, 10) })
  return (
    <>
      <PeopleStrip title="Splitting between" />
      <div className="split-head">
        <div className="stats grow" style={{ marginBottom: 0 }}>
          <div className="stat"><div className="n"><Money minor={split.total} code={cur} /></div><div className="l">spent together · {plural(split.bills, 'bill')}</div></div>
          {myBalance && <div className="stat clickable" onClick={() => setFocus(myBalance.member_id)}><div className={`n ${myBalance.balance > 0 ? 'good' : myBalance.balance < 0 ? 'bad' : ''}`}><Money minor={Math.abs(myBalance.balance)} code={cur} /></div><div className="l">{myBalance.balance > 0 ? 'you get back' : myBalance.balance < 0 ? 'you owe' : 'you are square'}</div></div>}
          {myBalance && <div className="stat clickable" onClick={() => setFocus(myBalance.member_id)}><div className="n"><Money minor={myBalance.spent} code={cur} /></div><div className="l">your share</div></div>}
        </div>
        {canEdit && (
          <div className="split-actions">
            <button type="button" className="btn primary big-btn" onClick={addBill}><Icon name="plus" size={18} />Add a bill</button>
            {people.length > 1 && <button type="button" className="btn big-btn" onClick={() => setTransfer({})} title="An advance, or paying someone back — all or part"><Icon name="wallet" size={16} />Money given</button>}
          </div>
        )}
      </div>
      <button type="button" className="page-switch" onClick={() => go('budget')}><Icon name="wallet" size={16} /><span><b>Budget & statistics</b><span className="muted small block">Plan before the trip, see how it is going, day by day and by category</span></span><span className="spacer" />→</button>

      <div className="two-col">
        <Card title="Balances" actions={<span className="muted small">click someone for the details</span>}>
          {people.length < 2 ? <p className="muted">Add the people you travel with above to split bills with them.</p> : (
            <div className="balances" role="list">
              {split.balances.map(b => {
                const p = person(b.member_id)
                const w = Math.abs(b.balance) / maxAbs * 50
                return (
                  <button type="button" key={b.member_id} className={`bal-row ${focus === b.member_id ? 'sel' : ''}`} onClick={() => setFocus(focus === b.member_id ? null : b.member_id)}>
                    <span className="bal-name"><PersonDot name={b.name} color={p?.color} size={22} /><span><span className="block">{b.name}</span><span className="muted small block">paid <Money minor={b.paid} code={cur} compact /> · share <Money minor={b.spent} code={cur} compact />{spentAll > 0 && <b className="bal-pct" title="Their part of everything spent together"> {Math.round(b.spent / spentAll * 100)}%</b>}{(b.sent > 0 || b.received > 0) && <> · {b.sent > 0 && <>gave <Money minor={b.sent} code={cur} compact /></>}{b.sent > 0 && b.received > 0 && ', '}{b.received > 0 && <>got <Money minor={b.received} code={cur} compact /></>}</>}</span></span></span>
                    <span className="bal-track"><i className="bal-mid" />{b.balance !== 0 && <span className={`bal-bar ${b.balance > 0 ? 'pos' : 'neg'}`} style={b.balance > 0 ? { left: '50%', width: `${w}%` } : { right: '50%', width: `${w}%` }} />}</span>
                    <span className={`bal-val ${b.balance > 0 ? 'good' : b.balance < 0 ? 'bad' : 'muted'}`}>{b.balance > 0 ? '+' : b.balance < 0 ? '−' : ''}<Money minor={Math.abs(b.balance)} code={cur} /></span>
                  </button>
                )
              })}
            </div>
          )}
        </Card>
        <Card title="Who pays whom" actions={people.length > 2 && <Select value={center} onChange={setCenter} options={people.map(p => ({ value: String(p.id), label: `Through ${p.name}` }))} placeholder="Fewest payments" clearLabel="Fewest payments" ariaLabel="How to settle" />}>
          {plan.length === 0 ? <p className="muted">{split.bills ? 'Everyone is square.' : 'No bills yet.'}</p> : (
            <div className="day-items">
              {plan.map((t, i) => (
                <div key={i} className="item settle-row">
                  <PersonDot name={name(t.from)} color={person(t.from)?.color} size={24} />
                  <span className="txt"><span className="title">{name(t.from)} <span className="muted">→</span> {name(t.to)}</span></span>
                  <b><Money minor={t.amount} code={cur} /></b>
                  {canEdit && <button type="button" className="btn sm" onClick={() => setTransfer(t)} title="Record it — all of it or a part">Paid…</button>}
                </div>
              ))}
            </div>
          )}
          <p className="muted small mt-sm">{center ? `Everyone settles with ${name(Number(center))}.` : 'The fewest payments that square everyone up.'} “Paid…” records a payment — the whole amount or a part.</p>
        </Card>
      </div>

      {focus != null && person(focus) && <PersonMoney member={focus} onPick={setFocus} />}

      <Card title={`Bills · ${bills.length}`} className="mb" actions={
        <div className="row wrap" style={{ gap: 6 }}>
          {usedCats.length > 1 && <span className="bill-filter"><Select value={cat} onChange={setCat} options={usedCats.map(c => ({ value: c.key, label: `${c.icon} ${c.name}` }))} placeholder="Any category" clearLabel="Any category" ariaLabel="Filter by category" /></span>}
          {usedModes.length > 0 && <span className="bill-filter"><Select value={mode} onChange={setMode} options={usedModes.map(m => ({ value: String(m.id), label: `${m.icon} ${m.name}` }))} placeholder="Any payment" clearLabel="Any payment" ariaLabel="Filter by payment method" /></span>}
          {integ.configured && trip.firefly_tag && canEdit && <button type="button" className="btn sm" onClick={sync} disabled={syncing} title={integ.synced_at ? `Synced ${ago(integ.synced_at)}` : 'Not synced yet'}><Icon name="refresh" size={13} />{syncing ? 'Syncing…' : 'Pocket Money'}</button>}
        </div>}>
        {integ.configured && !trip.firefly_tag && canEdit && <div className="hint-strip"><Icon name="info" size={14} />Set a Money tag under <a href="#" onClick={e => { e.preventDefault(); edit('trip', trip) }}>Edit trip</a> and every expense in Pocket Money with that tag lands here as a bill.</div>}
        {bills.length === 0 ? <Empty icon="wallet" title={all.length ? 'Nothing matches' : 'No bills yet'}>{canEdit && !all.length && <button type="button" className="btn primary big-btn mt-sm" onClick={addBill}><Icon name="plus" size={18} />Add the first bill</button>}</Empty>
          : <BillTimeline bills={bills} cats={cats} modes={modes} name={name} person={person} />}
      </Card>

      <div className="two-col">
        {canEdit && <CospendLinkCard />}
        <Card title="Categories & payment methods" actions={canEdit && <button type="button" className="btn ghost sm" onClick={() => setLists(true)}><Icon name="edit" size={13} />Edit</button>}>
          <div className="row wrap" style={{ gap: 6 }}>{split.categories.map(c => <span key={c.id} className="country-chip">{c.icon} {c.name}{split.by_category[c.key] ? <span className="muted"> · <Money minor={split.by_category[c.key]} code={cur} compact /></span> : null}</span>)}</div>
          <div className="row wrap mt-sm" style={{ gap: 6 }}>{split.payment_modes.map(m => <span key={m.id} className="country-chip">{m.icon} {m.name}</span>)}</div>
        </Card>
      </div>
      {split.trash > 0 && <Trash />}
      {canEdit && <Card title="Import from Nextcloud Cospend" className="mb"><p className="muted small">Bring an existing Cospend project's bills and people into this trip. Run it again later and it updates instead of doubling.</p><CospendImport tripId={trip.id} onDone={() => reload()} /></Card>}
      {lists && <ListsModal categories={split.categories} modes={split.payment_modes} onClose={() => { setLists(false); reload() }} />}
      {transfer && <TransferModal initial={transfer} onClose={() => setTransfer(null)} onSaved={() => { setTransfer(null); reload() }} />}
    </>
  )
}

type Bill = GroupedBill<Expense>

/**
 * The bills under a strip of the trip's days: the strip scrolls sideways, a
 * day jumps the list to its bills, and scrolling the list (about six bills
 * high) moves the strip along. Bills before or after the trip get one chip each.
 */
function BillTimeline({ bills, cats, modes, name, person }: { bills: Bill[]; cats: Map<string, TripCategory>; modes: Map<number, TripPaymentMode>; name: (id: number | null) => string; person: (id: number | null) => { color: string } | undefined }) {
  const { ov, trip, edit } = useTrip()
  const split = ov.split!
  const people = split.people
  const cur = split.currency
  const listRef = useRef<HTMLDivElement>(null)
  const stripRef = useRef<HTMLDivElement>(null)
  const [active, setActive] = useState('')
  // the list: oldest day first, so it runs the same way as the strip
  const byDay = new Map<string, Bill[]>()
  for (const b of [...bills].sort((a, b) => (a.date || '').localeCompare(b.date || '') || (a.bill_time || '').localeCompare(b.bill_time || ''))) byDay.set(b.date || '', [...(byDay.get(b.date || '') || []), b])
  const listDays = [...byDay.keys()]
  const slot = (d: string) => !d || (trip.start_date && d < trip.start_date) ? 'before' : trip.end_date && d > trip.end_date ? 'after' : d
  // the strip: every day of the trip, plus before / after when there are bills then
  const tripDays: string[] = []
  if (trip.start_date && trip.end_date) for (let d = new Date(trip.start_date + 'T00:00:00Z'); d.toISOString().slice(0, 10) <= trip.end_date; d.setUTCDate(d.getUTCDate() + 1)) tripDays.push(d.toISOString().slice(0, 10))
  const chips = [...(listDays.some(d => slot(d) === 'before') ? ['before'] : []), ...tripDays, ...listDays.filter(d => slot(d) === d && !tripDays.includes(d)), ...(listDays.some(d => slot(d) === 'after') ? ['after'] : [])]
  const totalOf = (chip: string) => listDays.filter(d => slot(d) === chip).reduce((n, d) => n + byDay.get(d)!.filter(b => b.category !== 'reimbursement').reduce((m, b) => m + b.total_home, 0), 0)
  const totals = new Map(chips.map(c => [c, totalOf(c)]))
  const max = Math.max(1, ...totals.values())
  const jump = (chip: string) => {
    const day = listDays.find(d => slot(d) === chip) ?? listDays.find(d => slot(d) > chip) ?? listDays[listDays.length - 1]
    const el = listRef.current?.querySelector<HTMLElement>(`[data-day="${day}"]`)
    if (el && listRef.current) listRef.current.scrollTo({ top: el.offsetTop - listRef.current.offsetTop, behavior: 'smooth' })
    setActive(chip)
  }
  const onScroll = () => {
    const list = listRef.current
    if (!list) return
    let cur = listDays[0]
    for (const el of list.querySelectorAll<HTMLElement>('[data-day]')) if (el.offsetTop - list.offsetTop <= list.scrollTop + 8) cur = el.dataset.day || ''
    if (list.scrollTop + list.clientHeight >= list.scrollHeight - 2) cur = listDays[listDays.length - 1]
    const chip = slot(cur)
    if (chip !== active) setActive(chip)
  }
  // keep the active day in view on the strip
  useEffect(() => {
    const el = stripRef.current?.querySelector<HTMLElement>(`[data-chip="${active}"]`)
    const strip = stripRef.current
    if (el && strip) strip.scrollTo({ left: el.offsetLeft - strip.offsetLeft - strip.clientWidth / 2 + el.clientWidth / 2, behavior: 'smooth' })
  }, [active])
  // during the trip, start at today's bills
  useEffect(() => {
    const t = todayISO()
    if (trip.start_date && trip.start_date <= t && t <= trip.end_date && listDays.length) jump(slot(listDays.filter(d => d <= t).pop() || listDays[0]))
    else setActive(slot(listDays[0]))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return (
    <>
      <div className="bill-strip" ref={stripRef} role="tablist" aria-label="Days of the trip">
        {chips.map(c => {
          const v = totals.get(c) || 0
          const has = listDays.some(d => slot(d) === c)
          return (
            <button type="button" key={c} data-chip={c} role="tab" aria-selected={active === c} className={`bs-day ${active === c ? 'on' : ''} ${has ? '' : 'empty'}`} onClick={() => jump(c)} title={c === 'before' ? 'Before the trip' : c === 'after' ? 'After the trip' : fmtDate(c)}>
              <span className="bs-wd">{c === 'before' ? 'before' : c === 'after' ? 'after' : weekday(c).slice(0, 2)}</span>
              <span className="bs-n">{c === 'before' ? '←' : c === 'after' ? '→' : Number(c.slice(8, 10))}</span>
              <span className="bs-bar"><i style={{ height: `${v / max * 100}%` }} /></span>
            </button>
          )
        })}
      </div>
      <div className="bill-scroll" ref={listRef} onScroll={onScroll}>
        <div className="bill-days">
          {listDays.map(day => {
            const list = byDay.get(day)!
            return (
              <div key={day} className="bill-day" data-day={day}>
                <div className="bill-day-head"><b>{day ? fmtDate(day) : 'No date'}</b><span className="muted small">{plural(list.length, 'bill')}</span><span className="spacer" /><Money minor={list.filter(b => b.category !== 'reimbursement').reduce((n, b) => n + b.total_home, 0)} code={cur} /></div>
                {list.map(b => {
                  const c = cats.get(b.category)
                  const m = modes.get(b.payment_mode_id)
                  const reimb = b.category === 'reimbursement'
                  return (
                    <div key={b.id} className={`bill-row ${reimb ? 'reimb' : ''}`} role="button" tabIndex={0} onClick={() => edit('expense', b)} onKeyDown={e => e.key === 'Enter' && edit('expense', b)}>
                      <span className="bill-ico" title={c?.name}>{c?.icon || '📌'}</span>
                      <span className="grow" style={{ minWidth: 0 }}>
                        <span className="bill-what">{b.name}{b.parts.length > 0 && <span className="badge" title={b.parts.map(p => `${name(p.owers[0])} +${(p.amount / 100).toFixed(2)}`).join(', ')}>uneven</span>}{b.source !== 'manual' && b.source !== 'cospend' && <span className="badge">{b.source === 'item' ? b.link_kind : b.source === 'money' ? 'Pocket Money' : b.source}</span>}</span>
                        <span className="muted small bill-meta">
                          {reimb ? <>{name(b.payer_id)} gave {b.owers.map(name).join(', ')}</> : <><PersonDot name={name(b.payer_id)} color={person(b.payer_id)?.color} size={14} /> {name(b.payer_id)} paid · for {b.owers.length === people.length && people.length > 1 ? 'everyone' : b.owers.map(id => name(id).split(' ')[0]).join(', ')}{b.parts.length > 0 && ` + ${b.parts.map(p => name(p.owers[0]).split(' ')[0]).join(', ')}`}</>}
                          {b.bill_time && ` · ${b.bill_time}`}{m && ` · ${m.icon} ${m.name}`}{b.notes && ` · ${b.notes}`}
                        </span>
                      </span>
                      <span className="num nowrap"><b><Money minor={b.total} code={b.currency} /></b>{b.currency !== cur && <div className="muted small"><Money minor={b.total_home} code={cur} /></div>}</span>
                    </div>
                  )
                })}
              </div>
            )
          })}
        </div>
      </div>
    </>
  )
}

/**
 * One person's money on the trip: the bills they paid, and their share of
 * every bill (theirs and everyone else's), with the money they gave or got.
 * Chips switch between people.
 */
function PersonMoney({ member, onPick }: { member: number; onPick: (id: number | null) => void }) {
  const { ov, edit } = useTrip()
  const split = ov.split!
  const people = split.people
  const cur = split.currency
  const [view, setView] = useState<'share' | 'paid'>('share')
  const name = (id: number | null) => people.find(p => p.id === id)?.name || '—'
  const weight = (id: number) => { const w = people.find(p => p.id === id)?.weight || 1; return w === 0 ? 1 : w }
  const bills = groupBills(ov.expenses.filter(e => e.kind === 'actual'))
  // their part of a bill: a weighted share of the main bill plus their own extra part
  const shareOf = (b: (typeof bills)[number]) => {
    const total = b.owers.reduce((n, id) => n + weight(id), 0)
    const main = b.owers.includes(member) && total > 0 ? b.amount_home * weight(member) / total : 0
    return Math.round(main + b.parts.filter(p => p.owers[0] === member).reduce((n, p) => n + p.amount_home, 0))
  }
  const bal = split.balances.find(b => b.member_id === member)
  const paid = bills.filter(b => b.payer_id === member && b.category !== 'reimbursement')
  const mine = bills.map(b => ({ b, share: shareOf(b) })).filter(x => x.share > 0 && x.b.category !== 'reimbursement')
  const money = bills.filter(b => b.category === 'reimbursement' && (b.payer_id === member || b.owers.includes(member)))
  const p = people.find(x => x.id === member)
  return (
    <Card className="mb person-money" title={<span className="row" style={{ gap: 8 }}><PersonDot name={name(member)} color={p?.color} size={24} />{name(member)}</span>}
      actions={<span className="row" style={{ gap: 6 }}>
        <span className="pm-chips">{people.map(x => <button type="button" key={x.id} className={`chip sm ${x.id === member ? 'active' : ''}`} onClick={() => onPick(x.id)}>{x.name.split(' ')[0]}</button>)}</span>
        <button type="button" className="icon-btn" aria-label="Close" onClick={() => onPick(null)}><Icon name="x" size={15} /></button>
      </span>}>
      {bal && <div className="pm-sum">
        <span>paid <b><Money minor={bal.paid} code={cur} /></b></span>
        <span>share <b><Money minor={bal.spent} code={cur} /></b></span>
        {bal.sent > 0 && <span>gave <b><Money minor={bal.sent} code={cur} /></b></span>}
        {bal.received > 0 && <span>got <b><Money minor={bal.received} code={cur} /></b></span>}
        <span className={bal.balance > 0 ? 'good' : bal.balance < 0 ? 'bad' : 'muted'}>{bal.balance > 0 ? 'gets back' : bal.balance < 0 ? 'owes' : 'square'} <b><Money minor={Math.abs(bal.balance)} code={cur} /></b></span>
      </div>}
      <div className="row mt-sm" style={{ gap: 8 }}>
        <div className="tabs sub" style={{ margin: 0 }}>
          <button type="button" className={view === 'share' ? 'active' : ''} onClick={() => setView('share')}>What they spent · {mine.length}</button>
          <button type="button" className={view === 'paid' ? 'active' : ''} onClick={() => setView('paid')}>What they paid · {paid.length}</button>
        </div>
      </div>
      <table className="table compact mt-sm">
        <tbody>
          {view === 'share' ? mine.map(({ b, share }) => (
            <tr key={b.id} className="clickable" onClick={() => edit('expense', b)}>
              <td className="muted nowrap small">{fmtDate(b.date, false)}</td>
              <td><b>{b.name}</b><div className="muted small">{b.payer_id === member ? 'they paid' : `${name(b.payer_id)} paid`} <Money minor={b.total_home} code={cur} /></div></td>
              <td className="num nowrap"><b><Money minor={share} code={cur} /></b></td>
            </tr>
          )) : paid.map(b => (
            <tr key={b.id} className="clickable" onClick={() => edit('expense', b)}>
              <td className="muted nowrap small">{fmtDate(b.date, false)}</td>
              <td><b>{b.name}</b><div className="muted small">for {b.owers.length === people.length ? 'everyone' : b.owers.map(id => name(id).split(' ')[0]).join(', ')}{b.parts.length > 0 && ` + ${b.parts.map(x => name(x.owers[0]).split(' ')[0]).join(', ')}`}</div></td>
              <td className="num nowrap"><b><Money minor={b.total_home} code={cur} /></b></td>
            </tr>
          ))}
          {money.map(b => (
            <tr key={b.id} className="clickable reimb" onClick={() => edit('expense', b)}>
              <td className="muted nowrap small">{fmtDate(b.date, false)}</td>
              <td>💰 {b.payer_id === member ? <>gave {b.owers.map(name).join(', ')}</> : <>got from {name(b.payer_id)}</>}{b.name && !b.name.includes('→') && <span className="muted small"> · {b.name}</span>}</td>
              <td className="num nowrap"><Money minor={b.total_home} code={cur} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  )
}

/** Money handed from one person to another: an advance, or paying someone back (all or part). */
function TransferModal({ initial, onClose, onSaved }: { initial: { from?: number; to?: number; amount?: number }; onClose: () => void; onSaved: () => void }) {
  const { ov, trip } = useTrip()
  const { toast } = useToast()
  const people = ov.split?.people || []
  const owner = people.find(p => p.role === 'owner') || people[0]
  const [f, setF] = useState({ from: initial.from ?? people.find(p => p.id !== owner?.id)?.id ?? 0, to: initial.to ?? owner?.id ?? 0, amount: initial.amount ?? 0, currency: trip.currency, date: new Date().toISOString().slice(0, 10), note: '' })
  const [busy, setBusy] = useState(false)
  const name = (id: number) => people.find(p => p.id === id)?.name || ''
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!f.amount || f.from === f.to) { toast('Pick two people and an amount', 'err'); return }
    setBusy(true)
    try {
      await api.post(`${V1}/trips/${trip.id}/expenses`, { name: f.note || `${name(f.from)} → ${name(f.to)}`, category: 'reimbursement', kind: 'actual', amount: f.amount, currency: f.currency, date: f.date, payer_id: f.from, owers: [f.to] })
      toast('Recorded'); onSaved()
    } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const opts = people.map(p => ({ value: String(p.id), label: p.name }))
  return (
    <Modal title="Money given" onClose={onClose}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <p className="muted small" style={{ margin: 0 }}>Cash handed over or a bank transfer: someone gave you 200 at the start, or pays you back — all of it or a part. It moves the balances; it is not spending.</p>
        <div className="two-col tight">
          <Field label="From"><Select value={String(f.from)} onChange={v => setF({ ...f, from: Number(v) })} options={opts} /></Field>
          <Field label="To"><Select value={String(f.to)} onChange={v => setF({ ...f, to: Number(v) })} options={opts} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Amount"><AmountInput amount={f.amount || null} currency={f.currency} onAmount={m => setF({ ...f, amount: m || 0 })} onCurrency={c => setF({ ...f, currency: c })} autoFocus /></Field>
          <Field label="Date"><input type="date" value={f.date} onChange={e => setF({ ...f, date: e.target.value })} /></Field>
        </div>
        <Field label="Note"><input value={f.note} onChange={e => setF({ ...f, note: e.target.value })} placeholder="Advance at the airport" /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Record</button></div>
      </form>
    </Modal>
  )
}

/** Deleted bills: restore one, or empty the trash for good. */
function Trash() {
  const { trip, canEdit, reload, ov } = useTrip()
  const { toast } = useToast()
  const { data, reload: again } = useAsync(() => api.get<Expense[]>(`${V1}/trips/${trip.id}/split/trash`), [trip.id, ov.split?.trash])
  const [empty, setEmpty] = useState(false)
  const restore = async (e: Expense) => { try { await api.post(`${V1}/trips/${trip.id}/expenses/${e.id}/restore`); toast('Restored'); await reload(); again() } catch (err) { toast((err as Error).message, 'err') } }
  const purge = async () => { try { await api.del(`${V1}/trips/${trip.id}/split/trash`); toast('Trash emptied'); await reload() } catch (err) { toast((err as Error).message, 'err') } finally { setEmpty(false) } }
  return (
    <Card title={`Trash · ${data?.length ?? ov.split?.trash ?? 0}`} className="mb" actions={canEdit && <button type="button" className="btn ghost sm danger-text" onClick={() => setEmpty(true)}>Empty trash</button>}>
      <div className="day-items">{groupBills(data || []).map(e => (
        <div key={e.id} className="item">
          <span className="txt"><span className="title">{e.name}</span><span className="sub">{fmtDate(e.date, false)}</span></span>
          <Money minor={e.total} code={e.currency} />
          {canEdit && <button type="button" className="btn ghost sm" onClick={() => restore(e)}>Restore</button>}
        </div>
      ))}</div>
      {empty && <Confirm title="Empty the trash?" message="These bills are deleted for good, here and in MoneyBuster." onCancel={() => setEmpty(false)} onConfirm={purge} />}
    </Card>
  )
}

/** Add, rename and remove the trip's bill categories and payment methods. */
function ListsModal({ categories, modes, onClose }: { categories: TripCategory[]; modes: TripPaymentMode[]; onClose: () => void }) {
  const { trip } = useTrip()
  const { toast } = useToast()
  const [cats, setCats] = useState(categories)
  const [pms, setPms] = useState(modes)
  const [nc, setNc] = useState({ icon: '', name: '' })
  const [np, setNp] = useState({ icon: '', name: '' })
  const base = `${V1}/trips/${trip.id}/split`
  const run = async (fn: () => Promise<void>) => { try { await fn() } catch (e) { toast((e as Error).message, 'err') } }
  const sorted = useMemo(() => cats, [cats])
  return (
    <Modal title="Categories & payment methods" onClose={onClose} size="lg">
      <div className="two-col">
        <div className="grid" style={{ gap: 6 }}>
          <b>Categories</b>
          {sorted.map(c => (
            <div key={c.id} className="row" style={{ gap: 6 }}>
              <input value={c.icon} onChange={e => setCats(x => x.map(y => y.id === c.id ? { ...y, icon: e.target.value } : y))} onBlur={() => run(async () => { await api.put(`${base}/categories/${c.id}`, c) })} style={{ width: 52, textAlign: 'center' }} aria-label="Icon" />
              <input value={c.name} onChange={e => setCats(x => x.map(y => y.id === c.id ? { ...y, name: e.target.value } : y))} onBlur={() => run(async () => { await api.put(`${base}/categories/${c.id}`, c) })} className="grow" aria-label="Name" />
              {c.key !== 'reimbursement' && c.key !== 'other' && <button type="button" className="icon-btn" aria-label={`Remove ${c.name}`} onClick={() => run(async () => { await api.del(`${base}/categories/${c.id}`); setCats(x => x.filter(y => y.id !== c.id)) })}><Icon name="x" size={14} /></button>}
            </div>
          ))}
          <form className="row" style={{ gap: 6 }} onSubmit={e => { e.preventDefault(); if (nc.name.trim()) run(async () => { const c = await api.post<TripCategory>(`${base}/categories`, nc); setCats(x => [...x, c]); setNc({ icon: '', name: '' }) }) }}>
            <input value={nc.icon} onChange={e => setNc({ ...nc, icon: e.target.value })} placeholder="🏷" style={{ width: 52, textAlign: 'center' }} aria-label="New icon" />
            <input value={nc.name} onChange={e => setNc({ ...nc, name: e.target.value })} placeholder="New category" className="grow" aria-label="New category" />
            <button className="btn sm">Add</button>
          </form>
        </div>
        <div className="grid" style={{ gap: 6 }}>
          <b>Payment methods</b>
          {pms.map(m => (
            <div key={m.id} className="row" style={{ gap: 6 }}>
              <input value={m.icon} onChange={e => setPms(x => x.map(y => y.id === m.id ? { ...y, icon: e.target.value } : y))} onBlur={() => run(async () => { await api.put(`${base}/payment-modes/${m.id}`, m) })} style={{ width: 52, textAlign: 'center' }} aria-label="Icon" />
              <input value={m.name} onChange={e => setPms(x => x.map(y => y.id === m.id ? { ...y, name: e.target.value } : y))} onBlur={() => run(async () => { await api.put(`${base}/payment-modes/${m.id}`, m) })} className="grow" aria-label="Name" />
              <button type="button" className="icon-btn" aria-label={`Remove ${m.name}`} onClick={() => run(async () => { await api.del(`${base}/payment-modes/${m.id}`); setPms(x => x.filter(y => y.id !== m.id)) })}><Icon name="x" size={14} /></button>
            </div>
          ))}
          <form className="row" style={{ gap: 6 }} onSubmit={e => { e.preventDefault(); if (np.name.trim()) run(async () => { const m = await api.post<TripPaymentMode>(`${base}/payment-modes`, np); setPms(x => [...x, m]); setNp({ icon: '', name: '' }) }) }}>
            <input value={np.icon} onChange={e => setNp({ ...np, icon: e.target.value })} placeholder="💳" style={{ width: 52, textAlign: 'center' }} aria-label="New icon" />
            <input value={np.name} onChange={e => setNp({ ...np, name: e.target.value })} placeholder="New payment method" className="grow" aria-label="New payment method" />
            <button className="btn sm">Add</button>
          </form>
        </div>
      </div>
      <div className="form-actions mt"><span className="spacer" /><button type="button" className="btn primary" onClick={onClose}>Done</button></div>
    </Modal>
  )
}

