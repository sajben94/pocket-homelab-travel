import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { api, ApiError, V1 } from '../../lib/api'
import type { TripShare } from '../../lib/types'
import { Card, Confirm, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select } from '../../components/Select'
import { useTrip } from './ctx'

type ShareLink = TripShare & { link: string; base_url: string }

const LEVELS = [
  { value: '1', label: 'Look only' },
  { value: '2', label: 'Add and edit bills' },
  { value: '3', label: 'Bills and people' },
  { value: '4', label: 'Everything' },
]

/**
 * The trip as a Nextcloud Cospend project: MoneyBuster (or any Cospend
 * client) opens it with this link or QR code and works on the same bills
 * and people as the app.
 */
export function CospendLinkCard() {
  const { trip, reload } = useTrip()
  const { toast } = useToast()
  const [link, setLink] = useState<ShareLink | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [qr, setQr] = useState('')
  const [off, setOff] = useState(false)
  const [renew, setRenew] = useState(false)
  const base = `${V1}/trips/${trip.id}/split/share`
  useEffect(() => {
    api.get<ShareLink>(base).then(setLink).catch(e => { if (!(e instanceof ApiError && e.status === 404)) toast((e as Error).message, 'err') }).finally(() => setLoaded(true))
  }, [base]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!link?.link) { setQr(''); return }
    QRCode.toDataURL(link.link, { margin: 1, width: 220, errorCorrectionLevel: 'M' }).then(setQr).catch(() => setQr(''))
  }, [link?.link])
  const save = async (body: Record<string, unknown>) => {
    try { setLink(await api.put<ShareLink>(base, body)); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const copy = async (text: string, what: string) => { try { await navigator.clipboard.writeText(text); toast(`${what} copied`) } catch { toast('Copy failed — select and copy it by hand', 'err') } }
  const turnOff = async () => { try { await api.del(base); setLink(null); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setOff(false) } }
  return (
    <Card title="MoneyBuster & Cospend">
      {!loaded ? null : !link ? (
        <>
          <p className="muted">Open this trip's bills in <b>MoneyBuster</b> on your phone — or any Nextcloud Cospend app. It is the same data: a bill or a person added there shows up here, and the other way round.</p>
          <button type="button" className="btn primary" onClick={() => save({})}><Icon name="link" size={15} />Create a link</button>
        </>
      ) : (
        <div className="cospend-link">
          {qr && <img src={qr} alt="QR code of the Cospend link" className="qr" width={180} height={180} />}
          <div className="grid" style={{ gap: 8, minWidth: 0 }}>
            <p className="small muted" style={{ margin: 0 }}>In MoneyBuster: <b>+</b> → <b>Add a shared project</b> → scan this code, or open the link on the phone.</p>
            <div className="mono small link-box" title={link.link}>{link.link}</div>
            <div className="row wrap" style={{ gap: 6 }}>
              <button type="button" className="btn sm" onClick={() => copy(link.link, 'Link')}><Icon name="copy" size={13} />Copy link</button>
              <a className="btn sm" href={link.link}><Icon name="external" size={13} />Open</a>
            </div>
            <details className="small">
              <summary className="muted">Typing it in by hand</summary>
              <dl className="kv" style={{ marginTop: 6 }}>
                <dt>Server</dt><dd className="mono">{link.base_url}</dd>
                <dt>Project ID</dt><dd className="mono">{link.token}</dd>
                <dt>Password</dt><dd className="mono">{link.password}</dd>
              </dl>
            </details>
            <div className="row wrap" style={{ gap: 6, alignItems: 'center' }}>
              <span className="small muted">People with the link can</span>
              <Select value={String(link.access_level)} onChange={v => save({ access_level: Number(v) })} options={LEVELS} ariaLabel="What the link allows" />
            </div>
            <div className="row wrap" style={{ gap: 6 }}>
              <button type="button" className="btn ghost sm" onClick={() => setRenew(true)}>New link</button>
              <button type="button" className="btn ghost sm danger-text" onClick={() => setOff(true)}>Turn off</button>
            </div>
          </div>
        </div>
      )}
      {renew && <Confirm title="Make a new link?" message="The old link stops working; phones using it need the new one." onCancel={() => setRenew(false)} onConfirm={() => { setRenew(false); save({ new: true, access_level: link?.access_level }) }} />}
      {off && <Confirm title="Turn the link off?" message="MoneyBuster and other Cospend apps lose access to this trip. The bills stay." onCancel={() => setOff(false)} onConfirm={turnOff} />}
    </Card>
  )
}
