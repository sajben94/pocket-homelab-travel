import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { Field, useToast } from './ui'
import { Icon } from './icons'
import { Select } from './Select'

interface Result { trip_id: number; name: string; created: boolean; members: number; bills: number; new_bills: number }

/**
 * Bring a Nextcloud Cospend project over through its share link: into a new
 * trip, or into one you pick. Running it again updates what came before.
 */
export function CospendImport({ tripId, onDone }: { tripId?: number; onDone?: (r: Result) => void }) {
  const { trips, reloadTrips, reloadPeople } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [f, setF] = useState({ link: '', password: '', trip: tripId ? String(tripId) : '' })
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<Result | null>(null)
  const run = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const r = await api.post<Result>(`${V1}/import/cospend`, { link: f.link, password: f.password || undefined, trip_id: f.trip ? Number(f.trip) : undefined })
      setResult(r); reloadTrips(); reloadPeople()
      toast(`${r.bills} bills from “${r.name}”${r.new_bills < r.bills ? ` (${r.new_bills} new)` : ''}`)
      onDone?.(r)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={run} className="grid" style={{ gap: 12 }}>
      <Field label="Cospend share link" help="In Cospend: the project's Sharing → a public link (copy it, or its MoneyBuster link cospend://…).">
        <input value={f.link} onChange={e => setF({ ...f, link: e.target.value })} placeholder="https://cloud.example.com/index.php/apps/cospend/s/… or cospend://…" required />
      </Field>
      <div className="two-col tight">
        <Field label="Password" help="If the link has one and it is not in the link."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} autoComplete="off" /></Field>
        {!tripId && <Field label="Into"><Select value={f.trip} onChange={v => setF({ ...f, trip: v })} options={trips.map(t => ({ value: String(t.id), label: t.name, hint: t.start_date.slice(0, 4) }))} placeholder="A new trip" clearLabel="A new trip" /></Field>}
      </div>
      <div className="row"><button className="btn primary" disabled={busy}><Icon name="download" size={15} />{busy ? 'Importing…' : 'Import'}</button>
        {result && !tripId && <button type="button" className="btn" onClick={() => navigate(`/trips/${result.trip_id}/spending`)}>Open “{result.name}”</button>}
        {result && <span className="muted small">{result.members} people, {result.bills} bills{result.created ? ' — new trip' : ''}</span>}
      </div>
    </form>
  )
}
