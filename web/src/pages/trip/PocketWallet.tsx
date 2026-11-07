import { Link } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import type { PocketWallet, PWPass } from '../../lib/types'
import { fmtClock, fmtDate } from '../../lib/format'
import { Card, useAsync } from '../../components/ui'
import { Icon } from '../../components/icons'
import { NeedsCore } from '../../components/NeedsCore'
import { useTrip } from './ctx'

/**
 * The trip's tickets and boarding passes from Pocket Wallet: the ones filed
 * with the trip there and the ones starting during it, by date and time,
 * each opening full screen in Wallet for the gate. Loads on its own, so the
 * rest of the page never waits for it; without Pocket Core or with the link
 * off it stays in place, greyed, and says what it needs.
 */
export function PocketWalletCard() {
  const { trip } = useTrip()
  const { data, error, loading, reload } = useAsync(() => api.get<PocketWallet>(`${V1}/trips/${trip.id}/pocket-wallet`),
    [trip.id, trip.start_date, trip.end_date])
  const pw = data
  const ok = pw?.status === 'ok'
  const base = pw?.wallet_url || ''
  const add = `${base}/passes?add=ticket`
  const open = ok && pw.passes.length > 0 ? <a className="btn ghost sm" href={`${base}/passes`} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open in Pocket Wallet</a> : null
  return (
    <Card title={<><Icon name="ticket" size={16} />Tickets and boarding passes</>} className={`span2 pp-card ${pw && !ok ? 'pp-dim' : ''}`} actions={open}
      sub="From your Pocket Wallet: the passes for this trip, by date and time. Open one to show it at the gate.">
      {!pw && loading ? <p className="muted pp-wait">Looking in Pocket Wallet…</p>
        : !pw ? <p className="muted pp-wait">Pocket Wallet could not be reached{error ? `: ${error}` : ''}. <button type="button" className="link-btn" onClick={() => reload()}>Try again</button></p>
        : pw.status === 'no_core' ? <NeedsCore what="Pocket Wallet" />
        : pw.status === 'off' ? <p className="muted pp-wait">Pocket Wallet is switched off for you. Connect Pocket Wallet in <Link to="/settings/integrations">Settings → Integrations</Link>.</p>
        : pw.status === 'unavailable' ? <p className="muted pp-wait">Pocket Wallet is not connected{pw.detail ? `: ${pw.detail}` : ''}. Connect Pocket Wallet in <Link to="/settings/integrations">Settings → Integrations</Link>.</p>
        : pw.status === 'error' ? <p className="muted pp-wait">Pocket Wallet did not answer{pw.detail ? `: ${pw.detail}` : ''}. <button type="button" className="link-btn" onClick={() => reload()} disabled={loading}>Try again</button></p>
        : pw.passes.length === 0 ? <p className="muted pp-wait">No tickets or boarding passes for this trip yet. <a href={add} target="_blank" rel="noreferrer">Add a pass in Pocket Wallet</a></p>
        : <div className="pp-rows">{pw.passes.map(p => <PassRow key={p.id} p={p} base={base} />)}</div>}
    </Card>
  )
}

function PassRow({ p, base }: { p: PWPass; base: string }) {
  const boarding = p.kind === 'boarding'
  const day = p.starts_at.slice(0, 10)
  const time = p.starts_at.length >= 16 ? fmtClock(p.starts_at.slice(11, 16)) : ''
  const route = p.venue && p.destination ? `${p.venue} → ${p.destination}` : ''
  const name = p.name || route || (boarding ? 'Boarding pass' : 'Ticket')
  const sub = [boarding ? 'Boarding pass' : 'Ticket', p.issuer, p.ref, p.name && route !== p.name ? route : '', !p.has_code && 'no barcode'].filter(Boolean).join(' · ')
  return (
    <a className={`pp-row pw-row ${p.archived ? 'pw-old' : ''}`} href={`${base}/cards/${p.id}?show=1`} target="_blank" rel="noreferrer"
      title={p.archived ? 'Open in Pocket Wallet (archived there)' : 'Open in Pocket Wallet, full screen for the gate'}>
      <span className="pp-when">{day ? <>{fmtDate(day, false)}{time && <span className="muted"> {time}</span>}</> : <span className="muted">No date</span>}</span>
      <span className="pp-ico" aria-hidden="true"><Icon name={boarding ? 'plane' : 'ticket'} size={14} /></span>
      <span className="pp-txt"><span className="pp-name">{name}</span><span className="pp-sub">{sub}</span></span>
      {(p.seat || p.gate) && <span className="pp-meta pw-meta">{p.seat && <span>Seat <b>{p.seat}</b></span>}{p.gate && <span>Gate <b>{p.gate}</b></span>}</span>}
    </a>
  )
}
