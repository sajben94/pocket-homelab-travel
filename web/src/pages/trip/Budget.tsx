import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { PersonDot } from '../../components/WithPeople'
import type { Expense, PlanLine, Trip } from '../../lib/types'
import { EXPENSE_ICONS } from '../../lib/types'
import { cap, fmtDate, todayISO } from '../../lib/format'
import { Card, Confirm, Empty, Field, Modal, Segmented, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select } from '../../components/Select'
import { AmountInput, Money, MoneyInput } from '../../components/Money'
import { useTrip } from './ctx'

/** A cost that belongs to the whole trip (stays, tickets, insurance) rather than one day — as the server spreads them. */
function wholeTrip(e: Expense, trip: Trip) {
  if (e.link_kind === 'lodging' || e.link_kind === 'transport' || e.category === 'lodging' || e.category === 'insurance') return true
  return e.category === 'transport' && (!e.date || (!!trip.start_date && e.date < trip.start_date) || (!!trip.end_date && e.date > trip.end_date))
}

const pct = (a: number, b: number) => b > 0 ? Math.round(a / b * 100) : 0

/**
 * Budgeting: a plan per category made before the trip, how the money is going
 * while it runs (pace, forecast, what is left per day), and afterwards how good
 * the plan was. Who paid what lives on the Spending tab.
 */
export function BudgetTab() {
  const { ov, trip, canEdit, edit, reload, go } = useTrip()
  const { toast } = useToast()
  const [planning, setPlanning] = useState(false)
  const b = ov.budget
  const cur = b.currency
  const today = todayISO()
  const days = Math.max(1, trip.days || 1)
  const phase: 'before' | 'during' | 'after' = !trip.start_date || today < trip.start_date ? 'before' : trip.end_date && today > trip.end_date ? 'after' : 'during'
  const elapsed = phase === 'before' ? 0 : phase === 'after' ? days : Math.max(1, b.days_elapsed)
  const remaining = days - elapsed
  const cats = new Map((ov.split?.categories || []).map(c => [c.key, c]))
  const catLabel = (k: string) => `${cats.get(k)?.icon || EXPENSE_ICONS[k] || '•'} ${cats.get(k)?.name || cap(k)}`
  const plan = ov.budget_plan || []
  const planOf = (k: string) => { const l = plan.find(x => x.category === k); return l ? (l.per_day ? l.amount * days : l.amount) : 0 }
  const planTotal = plan.reduce((n, l) => n + (l.per_day ? l.amount * days : l.amount), 0)
  const budget = planTotal || trip.budget
  const bills = ov.expenses.filter(e => e.kind === 'actual' && !e.deleted && e.category !== 'reimbursement')
  // day-to-day spending so far (not stays or tickets), to project the rest of the trip
  const daily = (k?: string) => bills.filter(e => (!k || e.category === k) && !wholeTrip(e, trip) && e.date >= trip.start_date && e.date <= today).reduce((n, e) => n + e.amount_home, 0)
  const projection = (k?: string) => phase === 'during' && remaining > 0 ? Math.round(daily(k) / elapsed * remaining) : 0
  const actualOf = (k: string) => b.by_category.find(c => c.category === k)?.actual || 0
  const openOf = (k: string) => b.by_category.find(c => c.category === k)?.planned || 0
  const forecastOf = (k: string) => actualOf(k) + openOf(k) + projection(k)
  const forecast = b.actual + b.planned + projection()
  const keys = Array.from(new Set([...plan.map(l => l.category), ...b.by_category.map(c => c.category)])).filter(k => k !== 'reimbursement')
    .sort((x, y) => Math.max(planOf(y), forecastOf(y)) - Math.max(planOf(x), forecastOf(x)))
  const scale = Math.max(1, ...keys.map(k => Math.max(planOf(k), forecastOf(k))))
  const planned = ov.expenses.filter(e => e.kind === 'planned' && e.category !== 'reimbursement')
  const markPaid = async (e: Expense) => {
    if (e.source === 'item' && (e.link_kind === 'lodging' || e.link_kind === 'transport')) {
      try { await api.put(`${V1}/trips/${trip.id}/${e.link_kind}/${e.link_id}`, { paid: true }); toast(`Marked paid — it's a bill now, paid by you for everyone (change it under Spending)`); await reload() } catch (err) { toast((err as Error).message, 'err') }
      return
    }
    edit('expense', { ...e, kind: 'actual', date: e.date || todayISO() })
  }
  const over = budget > 0 && forecast > budget
  return (
    <>
      <button type="button" className="page-switch" onClick={() => go('spending')}><Icon name="users" size={16} /><span><b>Spending — who paid what</b><span className="muted small block">Bills, balances between people and who pays whom</span></span><span className="spacer" />→</button>
      <div className="stats">
        <div className="stat clickable" onClick={() => canEdit && setPlanning(true)}><div className="n">{budget > 0 ? <Money minor={budget} code={cur} compact /> : <span className="muted">—</span>}</div><div className="l">{budget > 0 ? <>budget · <Money minor={Math.round(budget / days)} code={cur} compact /> a day</> : canEdit ? 'no budget yet — plan it' : 'no budget'}</div></div>
        <div className="stat"><div className="n"><Money minor={b.actual} code={cur} compact /></div><div className="l">spent{budget > 0 && ` · ${pct(b.actual, budget)}%`}</div></div>
        <div className="stat"><div className="n"><Money minor={b.planned} code={cur} compact /></div><div className="l">still planned</div></div>
        <div className="stat"><div className={`n ${over ? 'bad' : ''}`}><Money minor={forecast} code={cur} compact /></div><div className="l">{phase === 'after' ? 'in the end' : 'expected in all'}{budget > 0 && <> · {over ? 'over' : 'under'} by <Money minor={Math.abs(budget - forecast)} code={cur} compact /></>}</div></div>
      </div>

      <div className="two-col">
        <HowItGoes phase={phase} budget={budget} forecast={forecast} elapsed={elapsed} remaining={remaining} days={days} daily={daily()} onPlan={() => setPlanning(true)} />
        <Card title={plan.length ? 'Plan vs actual' : 'By category'} actions={canEdit && <button type="button" className="btn ghost sm" onClick={() => setPlanning(true)}><Icon name="edit" size={13} />{plan.length ? 'Edit plan' : 'Plan by category'}</button>}>
          {keys.length === 0 ? <p className="muted">Nothing planned or spent yet.</p> : (
            <div className="plan-rows">
              {keys.map(k => {
                const p = planOf(k), a = actualOf(k), o = openOf(k), pr = projection(k), f = a + o + pr
                const diff = p > 0 ? f - p : 0
                return (
                  <div key={k} className="plan-row" title={`spent ${(a / 100).toFixed(2)} · planned ${(o / 100).toFixed(2)}${pr ? ` · at this pace ${(pr / 100).toFixed(2)} more` : ''}${p ? ` · plan ${(p / 100).toFixed(2)}` : ''}`}>
                    <span className="pr-name">{catLabel(k)}</span>
                    <span className="pr-track">
                      <span className="pr-spent" style={{ width: `${a / scale * 100}%`, background: cats.get(k)?.color }} />
                      <span className="pr-open" style={{ width: `${(o + pr) / scale * 100}%` }} />
                      {p > 0 && <i className="pr-plan" style={{ left: `${p / scale * 100}%` }} />}
                    </span>
                    <span className="pr-val small"><Money minor={f} code={cur} compact />{p > 0 && <span className={diff > 0 ? 'bad' : 'good'}> {diff > 0 ? '+' : '−'}<Money minor={Math.abs(diff)} code={cur} compact /></span>}</span>
                  </div>
                )
              })}
            </div>
          )}
          <div className="legend"><span className="key"><i className="sw" style={{ background: 'var(--accent)' }} />spent</span><span className="key"><i className="sw planned" />{phase === 'during' ? 'planned + at this pace' : 'still planned'}</span>{plan.length > 0 && <span className="key"><i className="sw plan-mark" />plan</span>}</div>
        </Card>
      </div>

      {phase === 'after' && plan.length > 0 && <PlanReview keys={keys} planOf={planOf} actualOf={actualOf} catLabel={catLabel} />}
      <DayByDay />
      <Numbers />

      <Card className="mt mb" title={`Still planned · ${planned.length}`} actions={canEdit && <button type="button" className="btn primary sm" onClick={() => edit('expense', null, { kind: 'planned' })}><Icon name="plus" size={14} />Planned cost</button>}>
        {planned.length === 0 ? <p className="muted small" style={{ margin: 0 }}>Unpaid stays, tickets and anything you expect to pay show up here. Once paid, “Paid” turns them into a bill under Spending.</p> : (
          <table className="table compact">
            <tbody>{planned.map(e => <tr key={e.id} className="clickable" onClick={() => edit('expense', e)}>
              <td className="muted small nowrap">{e.date ? fmtDate(e.date, false) : '—'}</td>
              <td><b>{e.name}</b> {e.source === 'item' && <span className="src-pill">{e.link_kind}</span>}<div className="small muted">{catLabel(e.category)}{e.notes && ` · ${e.notes}`}</div></td>
              <td className="num nowrap"><Money minor={e.amount} code={e.currency} />{e.currency !== cur && <div className="small muted"><Money minor={e.amount_home} code={cur} /></div>}</td>
              <td className="num">{canEdit && <button type="button" className="btn sm paid-btn" onClick={ev => { ev.stopPropagation(); markPaid(e) }} title="It's paid — make it a bill (who paid, for whom)"><Icon name="check" size={12} />Paid</button>}</td>
            </tr>)}</tbody>
          </table>
        )}
      </Card>
      {planning && <PlanModal onClose={() => setPlanning(false)} onSaved={() => { setPlanning(false); reload() }} />}
    </>
  )
}

/** Before: what the plan allows per day and how much is booked. During: pace and what is left per day. After: how it ended. */
function HowItGoes({ phase, budget, forecast, elapsed, remaining, days, daily, onPlan }: { phase: 'before' | 'during' | 'after'; budget: number; forecast: number; elapsed: number; remaining: number; days: number; daily: number; onPlan: () => void }) {
  const { ov, trip, canEdit } = useTrip()
  const b = ov.budget
  const cur = b.currency
  const committed = b.actual + b.planned
  const left = budget - committed
  const title = phase === 'before' ? 'Planning ahead' : phase === 'during' ? `Day ${elapsed} of ${days}` : 'How it went'
  const scale = Math.max(1, budget, forecast)
  return (
    <Card title={title}>
      {budget > 0 && (
        <div className="pvp-bar" title="spent · still planned · budget">
          <span className="paid" style={{ width: `${b.actual / scale * 100}%` }} />
          <span className="open" style={{ width: `${(forecast - b.actual) / scale * 100}%` }} />
          <i className="pvp-budget" style={{ left: `${budget / scale * 100}%` }} />
        </div>
      )}
      <dl className="pvp-rows mt-sm">
        {phase === 'before' && <>
          {budget > 0 ? <>
            <dt>Budget a day</dt><dd><Money minor={Math.round(budget / days)} code={cur} /></dd>
            <dt>Booked or paid already</dt><dd><Money minor={committed} code={cur} /> <span className="muted">· {pct(committed, budget)}%</span></dd>
            <dt>Left for the days there</dt><dd className={left < 0 ? 'bad' : ''}><Money minor={left} code={cur} /> <span className="muted">· <Money minor={Math.round(left / days)} code={cur} compact /> a day</span></dd>
          </> : <><dt>Booked or paid already</dt><dd><Money minor={committed} code={cur} /></dd><dt>That is a day</dt><dd><Money minor={Math.round(committed / days)} code={cur} /></dd></>}
        </>}
        {phase === 'during' && <>
          <dt>Spent so far</dt><dd><Money minor={b.actual} code={cur} /></dd>
          <dt>Day to day, on average</dt><dd><Money minor={Math.round(daily / Math.max(1, elapsed))} code={cur} /> <span className="muted">a day (food, fun…)</span></dd>
          <dt>At this pace, in all</dt><dd className={budget > 0 && forecast > budget ? 'bad' : ''}><Money minor={forecast} code={cur} /></dd>
          {budget > 0 && remaining > 0 && <><dt>You can still spend</dt><dd className={left < 0 ? 'bad' : 'good'}><Money minor={Math.round(Math.max(0, left) / remaining)} code={cur} /> <span className="muted">a day for {remaining} more {remaining === 1 ? 'day' : 'days'}</span></dd></>}
        </>}
        {phase === 'after' && <>
          <dt>Spent</dt><dd><Money minor={b.actual} code={cur} /></dd>
          <dt>A day</dt><dd><Money minor={Math.round(b.actual / days)} code={cur} /></dd>
          {budget > 0 && <><dt>Against the budget</dt><dd className={b.actual > budget ? 'bad' : 'good'}>{b.actual > budget ? 'over' : 'under'} by <Money minor={Math.abs(budget - b.actual)} code={cur} /> <span className="muted">· {pct(b.actual, budget)}%</span></dd></>}
          {b.planned > 0 && <><dt>Never paid</dt><dd className="muted"><Money minor={b.planned} code={cur} /> still marked planned</dd></>}
        </>}
      </dl>
      {budget === 0 && canEdit && <button type="button" className="btn sm mt-sm" onClick={onPlan}><Icon name="plus" size={13} />Plan a budget{phase !== 'before' ? ' (it is never too late)' : ''}</button>}
      {phase === 'during' && <p className="muted small" style={{ margin: '8px 0 0' }}>The pace counts day-to-day spending only — stays and tickets of {trip.name} are already in.</p>}
    </Card>
  )
}

/** After the trip: each category's plan against what it really cost. */
function PlanReview({ keys, planOf, actualOf, catLabel }: { keys: string[]; planOf: (k: string) => number; actualOf: (k: string) => number; catLabel: (k: string) => string }) {
  const { ov } = useTrip()
  const cur = ov.budget.currency
  const rows = keys.filter(k => planOf(k) > 0 || actualOf(k) > 0)
  const planned = rows.filter(k => planOf(k) > 0)
  const close = planned.filter(k => Math.abs(actualOf(k) - planOf(k)) <= planOf(k) * 0.1).length
  return (
    <Card title="How good was the plan" className="mt" actions={planned.length > 0 && <span className="muted small">{close} of {planned.length} categories within 10%</span>}>
      <table className="table compact">
        <thead><tr><th>Category</th><th className="num">Plan</th><th className="num">Actual</th><th className="num">Difference</th><th>Verdict</th></tr></thead>
        <tbody>{rows.map(k => {
          const p = planOf(k), a = actualOf(k), d = a - p
          const verdict = p === 0 ? 'not planned' : Math.abs(d) <= p * 0.1 ? 'spot on' : d > 0 ? `${pct(d, p)}% over` : `${pct(-d, p)}% under`
          return <tr key={k}><td>{catLabel(k)}</td><td className="num"><Money minor={p} code={cur} /></td><td className="num"><Money minor={a} code={cur} /></td><td className={`num ${p && d > p * 0.1 ? 'bad' : p && d < -p * 0.1 ? 'good' : ''}`}>{d > 0 ? '+' : d < 0 ? '−' : ''}<Money minor={Math.abs(d)} code={cur} /></td><td className="small">{verdict}</td></tr>
        })}</tbody>
      </table>
    </Card>
  )
}

/** Plain numbers: busiest and quietest day, a day per person, where the money went, the biggest bills. */
function Numbers() {
  const { ov, trip, edit } = useTrip()
  const b = ov.budget
  const cur = b.currency
  const days = (ov.day_costs || []).filter(d => d.date !== 'before' && d.date !== 'after')
  const sum = (m: Record<string, number>) => Object.values(m).reduce((n, v) => n + v, 0)
  const withSpend = days.filter(d => sum(d.actual) > 0)
  if (b.actual === 0) return null
  const busiest = withSpend.reduce((x, d) => !x || sum(d.actual) > sum(x.actual) ? d : x, withSpend[0])
  const quietest = withSpend.reduce((x, d) => !x || sum(d.actual) < sum(x.actual) ? d : x, withSpend[0])
  const people = Math.max(1, (ov.people || []).filter(p => p.active).length)
  const top = [...b.by_category].filter(c => c.actual > 0).sort((x, y) => y.actual - x.actual)[0]
  const cats = new Map((ov.split?.categories || []).map(c => [c.key, c]))
  return (
    <Card title="Numbers" className="mt">
      <div className="num-grid">
        <div><span className="muted small block">A day</span><b><Money minor={Math.round(b.actual / Math.max(1, trip.days))} code={cur} /></b></div>
        {people > 1 && <div><span className="muted small block">A day per person</span><b><Money minor={Math.round(b.actual / Math.max(1, trip.days) / people)} code={cur} /></b></div>}
        {busiest && <div><span className="muted small block">Busiest day</span><b><Money minor={sum(busiest.actual)} code={cur} /></b> <span className="muted small">{fmtDate(busiest.date, false)}</span></div>}
        {quietest && quietest !== busiest && <div><span className="muted small block">Quietest day</span><b><Money minor={sum(quietest.actual)} code={cur} /></b> <span className="muted small">{fmtDate(quietest.date, false)}</span></div>}
        {top && <div><span className="muted small block">Most went to</span><b>{cats.get(top.category)?.icon} {cats.get(top.category)?.name || cap(top.category)}</b> <span className="muted small">{pct(top.actual, b.actual)}%</span></div>}
      </div>
      {b.top.length > 0 && <>
        <div className="muted small mt-sm">Biggest bills</div>
        <div className="day-items">{b.top.map(e => <div key={e.id} className="item clickable" onClick={() => edit('expense', e)}><span className="ico">{cats.get(e.category)?.icon || '•'}</span><span className="txt"><span className="title">{e.name}</span><span className="sub">{e.date ? fmtDate(e.date, false) : ''}</span></span><b><Money minor={e.amount_home} code={cur} /></b></div>)}</div>
      </>}
    </Card>
  )
}

/** The budget plan: an amount per category, for the whole trip or per day. */
function PlanModal({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const { ov, trip } = useTrip()
  const { meta } = useApp()
  const { toast } = useToast()
  const days = Math.max(1, trip.days || 1)
  const keys = ov.split?.categories.length ? ov.split.categories.filter(c => c.key !== 'reimbursement').map(c => ({ key: c.key, label: `${c.icon} ${c.name}` })) : (meta?.expense_categories || []).map(c => ({ key: c, label: `${EXPENSE_ICONS[c] || ''} ${cap(c)}` }))
  const perDayByDefault = new Set(['food', 'groceries', 'activities', 'fuel', 'shopping'])
  const [lines, setLines] = useState<Record<string, PlanLine>>(() => Object.fromEntries(keys.map(k => [k.key, ov.budget_plan.find(l => l.category === k.key) || { category: k.key, amount: 0, per_day: perDayByDefault.has(k.key) }])))
  const [busy, setBusy] = useState(false)
  const totalOf = (l: PlanLine) => l.per_day ? l.amount * days : l.amount
  const total = Object.values(lines).reduce((n, l) => n + totalOf(l), 0)
  const cur = trip.currency
  const set = (k: string, v: Partial<PlanLine>) => setLines(x => ({ ...x, [k]: { ...x[k], ...v } }))
  // start from what is already booked or spent
  const fill = () => setLines(x => Object.fromEntries(Object.entries(x).map(([k, l]) => {
    const c = ov.budget.by_category.find(y => y.category === k)
    const known = (c?.actual || 0) + (c?.planned || 0)
    return [k, known > 0 && l.amount === 0 ? { ...l, amount: known, per_day: false } : l]
  })))
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api.put(`${V1}/trips/${trip.id}/budget/plan`, { lines: Object.values(lines).filter(l => l.amount > 0) }); toast('Budget planned'); onSaved() } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Plan the budget" onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 12 }}>
        <p className="muted small" style={{ margin: 0 }}>How much may each kind of cost be? Stays and tickets are easiest as a total, food and fun per day ({days} {days === 1 ? 'day' : 'days'}). The trip budget becomes the sum.</p>
        <div className="plan-edit">
          {keys.map(k => {
            const l = lines[k.key]
            return (
              <div key={k.key} className="pe-row">
                <span className="grow">{k.label}</span>
                <MoneyInput value={l.amount || null} code={cur} onChange={m => set(k.key, { amount: m || 0 })} ariaLabel={`${k.label} plan`} />
                <Segmented size="sm" value={l.per_day ? 'day' : 'total'} onChange={v => set(k.key, { per_day: v === 'day' })} options={[{ value: 'total', label: 'total' }, { value: 'day', label: 'a day' }]} label="Total or per day" />
                <span className="pe-total small muted">{l.amount > 0 && l.per_day ? <>= <Money minor={totalOf(l)} code={cur} /></> : ''}</span>
              </div>
            )
          })}
        </div>
        <div className="row"><button type="button" className="btn ghost sm" onClick={fill} title="Fill empty categories with what is already booked or spent">Fill from what's booked</button><span className="spacer" /><span>Budget <b><Money minor={total} code={cur} /></b> <span className="muted small">· <Money minor={Math.round(total / days)} code={cur} /> a day</span></span></div>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Save plan</button></div>
      </form>
    </Modal>
  )
}

export function ExpenseModal({ trip, item: picked, defaults = {}, onClose, onSaved }: { trip: Trip; item: Expense | null; defaults?: Record<string, unknown>; onClose: () => void; onSaved: () => void }) {
  const { meta } = useApp()
  const { ov } = useTrip()
  const { toast } = useToast()
  const people = ov.people || []
  const split = ov.split
  // an extra part of an uneven split opens its whole bill
  const item = picked?.parent_id ? ov.expenses.find(x => x.id === picked.parent_id) || picked : picked
  const parts = item ? ov.expenses.filter(x => x.parent_id === item.id) : []
  const [extras, setExtras] = useState<Record<number, number>>(() => Object.fromEntries(parts.map(p => [p.owers[0], p.amount])))
  const [uneven, setUneven] = useState(parts.length > 0)
  const [f, setF] = useState<Partial<Expense>>(item ? { ...item, amount: item.amount + parts.reduce((n, p) => n + p.amount, 0) } : { name: '', category: 'other', amount: 0, currency: trip.currency, date: todayISO(), kind: 'planned', notes: '', payer_id: null, owers: [], payment_mode_id: 0, bill_time: '', repeat: 'n', repeat_until: '', repeat_freq: 1, ...defaults })
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const set = <K extends keyof Expense>(k: K, v: Expense[K]) => setF(x => ({ ...x, [k]: v }))
  const base = `${V1}/trips/${trip.id}/expenses`
  const synced = item?.source === 'money' || item?.source === 'firefly'
  const locked = synced || item?.source === 'item'
  const bill = f.kind === 'actual'
  // a new bill: paid by the owner, for everyone taking part
  const owner = people.find(p => p.role === 'owner') || people[0]
  const payer = f.payer_id ?? owner?.id ?? null
  const owers = f.owers && f.owers.length ? f.owers : people.filter(p => p.active).map(p => p.id)
  const toggleOwer = (id: number) => set('owers', owers.includes(id) ? owers.filter(x => x !== id) : [...owers, id])
  const totalWeight = owers.reduce((n, id) => n + (people.find(p => p.id === id)?.weight || 1), 0)
  // uneven: each extra is owed on top; the rest is shared equally (by weight) by everyone ticked
  const extraSum = uneven ? Object.values(extras).reduce((n, v) => n + (v || 0), 0) : 0
  const rest = (f.amount || 0) - extraSum
  const shareOf = (id: number) => (owers.includes(id) && totalWeight > 0 ? Math.round(rest * (people.find(p => p.id === id)?.weight || 1) / totalWeight) : 0) + (uneven ? extras[id] || 0 : 0)
  const canUneven = bill && !locked && people.length > 1
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      if (uneven && rest < 0) { toast('The extra parts add up to more than the bill', 'err'); setBusy(false); return }
      const billPart = bill ? { payer_id: payer, owers, payment_mode_id: f.payment_mode_id || 0, bill_time: f.bill_time || '', ...(locked ? {} : { extras: uneven ? Object.entries(extras).filter(([, v]) => v > 0).map(([id, v]) => ({ member_id: Number(id), amount: v })) : [] }) } : {}
      const body: Record<string, unknown> = locked ? { category: f.category, notes: f.notes, ...billPart } : { ...f, ...billPart }
      if (item) await api.put(`${base}/${item.id}`, body); else await api.post(base, body)
      toast('Saved'); onSaved()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => { try { await api.del(`${base}/${item!.id}`); toast(bill ? 'Moved to the trash' : 'Removed'); onSaved() } catch (e) { toast((e as Error).message, 'err') } }
  const catOptions = split?.categories.length
    ? split.categories.map(c => ({ value: c.key, label: `${c.icon} ${c.name}` }))
    : (meta?.expense_categories || []).map(c => ({ value: c, label: `${EXPENSE_ICONS[c] || ''} ${cap(c)}` }))
  return (
    <Modal title={item ? (bill ? 'Edit bill' : 'Edit cost') : bill ? 'Add a bill' : 'Add a cost'} onClose={onClose} size={bill && people.length > 1 ? 'lg' : undefined}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        {synced && <div className="callout">This comes from {item?.source === 'money' ? 'Pocket Money' : 'Firefly III'} (#{item?.external_id}). Change the amount or date there; here you can re-categorise it and say who it was for.</div>}
        {item?.source === 'item' && <div className="callout">This is the price of a {item.link_kind}. Edit the amount on the {item.link_kind} itself{bill ? '; who paid and for whom are set here.' : '.'}{!bill && (item.link_kind === 'lodging' || item.link_kind === 'transport') && <> <button type="button" className="btn sm" style={{ marginLeft: 6 }} onClick={async () => { try { await api.put(`${V1}/trips/${trip.id}/${item.link_kind}/${item.link_id}`, { paid: true }); toast('Paid — it is a bill now'); onSaved() } catch (e) { toast((e as Error).message, 'err') } }}><Icon name="check" size={13} />It's paid</button></>}</div>}
        <Field label="What"><input value={f.name || ''} onChange={e => set('name', e.target.value)} required autoFocus={!item} disabled={locked} /></Field>
        {!locked && <Field group label="Kind"><Segmented value={f.kind || 'planned'} onChange={v => set('kind', v as 'planned' | 'actual')} options={[{ value: 'planned', label: 'Planned (estimate)' }, { value: 'actual', label: 'Spent (a bill)' }]} size="sm" /></Field>}
        <div className="two-col tight">
          <Field label="Amount"><AmountInput amount={f.amount ?? null} currency={f.currency || trip.currency} onAmount={m => set('amount', m || 0)} onCurrency={c => set('currency', c)} autoFocus={!!item && !locked} /></Field>
          <div className="two-col tight">
            <Field label="Date"><input type="date" value={f.date || ''} onChange={e => set('date', e.target.value)} disabled={locked} /></Field>
            {bill && <Field label="Time"><input type="time" value={f.bill_time || ''} onChange={e => set('bill_time', e.target.value)} /></Field>}
          </div>
        </div>
        {bill && people.length > 0 && <>
          <div className="two-col tight">
            <Field label="Paid by"><Select value={payer ? String(payer) : ''} onChange={v => set('payer_id', Number(v))} options={people.map(p => ({ value: String(p.id), label: p.name }))} /></Field>
            <Field label="Paid with"><Select value={f.payment_mode_id ? String(f.payment_mode_id) : ''} onChange={v => set('payment_mode_id', Number(v) || 0)} options={(split?.payment_modes || []).map(m => ({ value: String(m.id), label: `${m.icon} ${m.name}` }))} placeholder="—" clearLabel="Not said" /></Field>
          </div>
          {people.length > 1 && (
            <Field group label={<span className="row">For whom <span className="spacer" />{canUneven && <Segmented value={uneven ? 'uneven' : 'even'} onChange={v => setUneven(v === 'uneven')} options={[{ value: 'even', label: 'Equally' }, { value: 'uneven', label: 'Uneven' }]} size="sm" label="How to split" />}<button type="button" className="btn ghost sm" onClick={() => set('owers', people.map(p => p.id))}>Everyone</button><button type="button" className="btn ghost sm" onClick={() => payer && set('owers', [payer])}>Just the payer</button></span>}>
              {uneven && <p className="small muted" style={{ margin: '0 0 6px' }}>Put what someone had on top (their wine, a dessert) as their extra. What is left — <b><Money minor={Math.max(0, rest)} code={f.currency || trip.currency} /></b> — is shared equally by everyone ticked. Give everyone an extra and untick all for exact amounts.{rest < 0 && <span className="bad"> The extras are more than the bill.</span>}</p>}
              <div className={`owers ${uneven ? 'uneven' : ''}`}>
                {people.map(p => {
                  const on = owers.includes(p.id)
                  const share = shareOf(p.id)
                  return (
                    <div key={p.id} className={`ower ${on || (uneven && extras[p.id]) ? 'on' : ''}`}>
                      <label className="row grow" style={{ gap: 8, minWidth: 0, cursor: 'pointer' }}>
                        <input type="checkbox" checked={on} onChange={() => toggleOwer(p.id)} title={uneven ? 'Shares what is left' : undefined} />
                        <PersonDot name={p.name} color={p.color} size={20} />
                        <span className="grow ellipsis">{p.name}{p.weight !== 1 && <span className="muted small"> ×{p.weight}</span>}</span>
                      </label>
                      {uneven && <span className="extra-in" title={`${p.name}'s extra, on top of an equal share`}>+<MoneyInput value={extras[p.id] || null} code={f.currency || trip.currency} onChange={m => setExtras(x => ({ ...x, [p.id]: m || 0 }))} ariaLabel={`${p.name}'s extra`} /></span>}
                      {share > 0 && <span className="small muted nowrap"><Money minor={share} code={f.currency || trip.currency} /></span>}
                    </div>
                  )
                })}
              </div>
            </Field>
          )}
        </>}
        <Field label="Category"><Select value={f.category || 'other'} onChange={v => set('category', v)} options={catOptions} /></Field>
        <Field label="Notes"><input value={f.notes || ''} onChange={e => set('notes', e.target.value)} /></Field>
        <div className="form-actions">
          {item && !locked && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}>Delete</button>}
          <span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{item ? 'Save' : 'Add'}</button>
        </div>
      </form>
      {del && <Confirm title={bill ? 'Move this bill to the trash?' : 'Delete this cost?'} message={bill ? 'It stops counting; you can restore it from the trash on the Split tab.' : 'It is removed from the budget.'} onCancel={() => setDel(false)} onConfirm={remove} />}
    </Modal>
  )
}

/**
 * What each day costs, stacked by category: paid in colour, still planned
 * hatched. Stays, tickets and insurance are shared out over every day of the
 * trip; dinner sits on the evening it was eaten. Pick a person to see only
 * their share. A long trip scrolls sideways.
 */
function DayByDay() {
  const { ov, trip } = useTrip()
  const all = ov.day_costs || []
  const people = ov.people || []
  const cats = new Map((ov.split?.categories || []).map(c => [c.key, c]))
  const [hover, setHover] = useState<string | null>(null)
  const [who, setWho] = useState('')
  if (!all.some(d => Object.keys(d.actual).length || Object.keys(d.planned).length)) return null
  const sum = (m: Record<string, number>) => Object.values(m).reduce((n, v) => n + v, 0)
  const days = all.map(d => who ? { ...d, actual: d.people?.[who] || {}, planned: {} } : d).filter(d => !who || (d.date !== 'before' && d.date !== 'after') || sum(d.actual) > 0)
  const max = Math.max(1, ...days.map(d => sum(d.actual) + sum(d.planned)))
  const used = Array.from(new Set(days.flatMap(d => [...Object.keys(d.actual), ...Object.keys(d.planned)]))).sort((a, b) => (cats.get(a)?.order ?? 99) - (cats.get(b)?.order ?? 99))
  const color = (k: string) => cats.get(k)?.color || '#94a3b8'
  const label = (d: string) => d === 'before' ? 'Before the trip' : d === 'after' ? 'After the trip' : fmtDate(d, false)
  const tripDays = days.filter(d => d.date !== 'before' && d.date !== 'after')
  const spent = tripDays.reduce((n, d) => n + sum(d.actual), 0)
  const avg = tripDays.length ? Math.round(spent / tripDays.length) : 0
  const h = days.find(d => d.date === hover)
  const person = people.find(p => String(p.id) === who)
  return (
    <Card title="Day by day" className="mt" actions={<span className="row" style={{ gap: 8 }}>
      {avg > 0 && <span className="muted small nowrap">⌀ <Money minor={avg} code={trip.currency} /> a day{person ? ` for ${person.name.split(' ')[0]}` : ''}</span>}
      {people.length > 1 && <Select value={who} onChange={setWho} options={people.map(p => ({ value: String(p.id), label: p.name }))} placeholder="Everyone" clearLabel="Everyone" ariaLabel="Whose days" />}
    </span>}>
      <div className="dbd-wrap">
        <span className="dbd-max small muted"><Money minor={max} code={trip.currency} compact /></span>
        <div className={`dbd ${days.length > 16 ? 'long' : ''}`} onMouseLeave={() => setHover(null)}>
          {days.map(d => {
            const a = sum(d.actual), p = sum(d.planned)
            return (
              <div key={d.date} className={`dbd-col ${hover === d.date ? 'hi' : ''} ${d.date === 'before' || d.date === 'after' ? 'edge' : ''}`} onMouseEnter={() => setHover(d.date)} onClick={() => setHover(d.date)} tabIndex={0} onFocus={() => setHover(d.date)} aria-label={`${label(d.date)}: ${(a / 100).toFixed(2)} paid, ${(p / 100).toFixed(2)} planned`}>
                <div className="dbd-bar">
                  {p > 0 && <div className="dbd-planned" style={{ height: `${p / max * 100}%` }} />}
                  {used.filter(k => d.actual[k]).reverse().map(k => <div key={k} className="dbd-seg" style={{ height: `${d.actual[k] / max * 100}%`, background: color(k) }} title={`${cats.get(k)?.name || k}: ${(d.actual[k] / 100).toFixed(2)}`} />)}
                </div>
                <div className="dbd-x">{d.date === 'before' ? '←' : d.date === 'after' ? '→' : Number(d.date.slice(8, 10))}</div>
              </div>
            )
          })}
        </div>
      </div>
      <div className="dbd-info">
        {h ? <><b>{label(h.date)}</b><b><Money minor={sum(h.actual) + sum(h.planned)} code={trip.currency} /></b>{used.filter(k => h.actual[k] || h.planned[k]).map(k => <span key={k}><i className="sw" style={{ background: color(k) }} /> {cats.get(k)?.name || k} <Money minor={(h.actual[k] || 0) + (h.planned[k] || 0)} code={trip.currency} />{h.planned[k] ? <span className="muted"> planned</span> : null}</span>)}{!Object.keys(h.actual).length && !Object.keys(h.planned).length && <span className="muted">nothing</span>}</>
          : <span className="legend" style={{ margin: 0 }}>{used.map(k => <span key={k} className="key"><i className="sw" style={{ background: color(k) }} />{cats.get(k)?.name || k}</span>)}{!who && <span className="key"><i className="sw planned" />planned</span>}</span>}
      </div>
      <p className="muted small" style={{ margin: '6px 0 0' }}>Stays, tickets and insurance are shared out over all {trip.days} days; meals, drinks and the rest sit on the day they happened.</p>
    </Card>
  )
}
