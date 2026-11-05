import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Stats } from '../lib/types'
import { flag } from '../lib/countries'
import { matches } from '../lib/search'
import { Loading, PageHeader, Segmented, useAsync } from '../components/ui'
import { Icon } from '../components/icons'

interface Lite { code: string; name: string; region: string; subregion: string; capital: string }

/** Every country of the world, the ones you've been to marked; search by name or capital. */
export default function CountriesPage() {
  const { data: all } = useAsync(() => api.get<Lite[]>(`${V1}/countries`), [])
  const { data: stats } = useAsync(() => api.get<Stats>(`${V1}/stats`), [])
  const [q, setQ] = useState('')
  const [show, setShow] = useState<'all' | 'visited' | 'not'>('visited')
  const days = useMemo(() => new Map((stats?.countries || []).map(c => [c.code, c])), [stats])
  const groups = useMemo(() => {
    const list = (all || []).filter(c => (!q || matches(q, `${c.name} ${c.capital} ${c.code} ${c.subregion}`)) && (show === 'all' || (show === 'visited') === ((days.get(c.code)?.days || 0) > 0)))
    const by = new Map<string, Lite[]>()
    for (const c of list) { const r = c.region || 'Other'; if (!by.has(r)) by.set(r, []); by.get(r)!.push(c) }
    return [...by.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [all, q, show, days])
  if (!all) return <Loading />
  const visited = [...days.values()].filter(c => c.days > 0).length
  return (
    <>
      <PageHeader title="Countries" sub={`${visited} of ${all.length} visited`} actions={<Link className="btn" to="/insights"><Icon name="globe" size={15} />Insights</Link>} />
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Country or capital…" aria-label="Search countries" autoFocus /></div>
        <Segmented value={show} onChange={setShow} options={[{ value: 'all', label: 'All' }, { value: 'visited', label: 'Visited' }, { value: 'not', label: 'Not yet' }]} size="sm" label="Show" />
      </div>
      {groups.map(([region, list]) => (
        <section key={region} className="mb">
          <h2 className="section-h">{region} <span className="muted small">{list.length}</span></h2>
          <div className="country-index">
            {list.map(c => {
              const s = days.get(c.code)
              return (
                <Link key={c.code} to={`/countries/${c.code}`} className={`ci-item ${s && s.days > 0 ? 'visited' : ''}`}>
                  <span className="ci-flag">{flag(c.code)}</span>
                  <span className="grow"><b>{c.name}</b><span className="muted small block">{c.capital || c.subregion}</span></span>
                  {s && (s.days > 0 || s.planned_days > 0) && <span className="small ci-days">{s.days > 0 ? `${s.days} d` : 'planned'}</span>}
                </Link>
              )
            })}
          </div>
        </section>
      ))}
    </>
  )
}
