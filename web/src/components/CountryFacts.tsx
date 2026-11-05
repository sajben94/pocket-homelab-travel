import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { CountryFacts, CountryNote } from '../lib/types'
import { bigNumber, countryClock, countryName, flag } from '../lib/countries'

/** The built-in facts that can be corrected, with how they are labelled. */
export const FACT_LABELS: Record<string, string> = { capital: 'Capital', population: 'Population', area: 'Area', languages: 'Languages', currency: 'Currency', calling_code: 'Calling code', drives: 'Drives on the', timezone: 'Time', tld: 'Internet' }

/** "1 EUR = 1.96 BAM", from the person's home currency to the country's. */
function useRate(to?: string): string {
  const { settings } = useApp()
  const home = settings?.home_currency || 'EUR'
  const [txt, setTxt] = useState('')
  useEffect(() => {
    setTxt('')
    if (!to || to === home) return
    let live = true
    api.get<{ rate: number }>(`${V1}/fx?from=${home}&to=${to}`).then(r => { if (live && r.rate > 0) setTxt(`1 ${home} = ${r.rate >= 100 ? Math.round(r.rate).toLocaleString() : r.rate.toFixed(r.rate >= 10 ? 1 : 2)} ${to}`) }).catch(() => {})
    return () => { live = false }
  }, [to, home])
  return txt
}

/** Built-in facts with the person's corrections laid over them. */
export function factValues(c: CountryFacts, note?: CountryNote | null): { key: string; label: string; value: React.ReactNode; edited: boolean }[] {
  const o = note?.facts || {}
  const clock = countryClock(c.timezones)
  const cur = c.currencies[0]
  const rows: [string, React.ReactNode | null][] = [
    ['capital', c.capital.join(', ') || null],
    ['population', c.population ? <>{bigNumber(c.population)}<span className="muted small"> ({c.population_year})</span></> : null],
    ['area', c.area_km2 ? `${Math.round(c.area_km2).toLocaleString()} km²` : null],
    ['languages', c.languages.join(', ') || null],
    ['currency', cur ? <><b>{cur.code}</b> {cur.name}{cur.symbol && ` (${cur.symbol})`}{c.currencies.length > 1 && <span className="muted"> +{c.currencies.slice(1).map(x => x.code).join(', ')}</span>}</> : null],
    ['calling_code', c.calling_code || null],
    ['drives', c.drives ? `${c.drives} side` : null],
    ['timezone', clock ? <>{clock.time && <b>{clock.time} </b>}<span className="muted">{clock.offset}</span></> : null],
    ['tld', c.tld || null],
  ]
  return rows.filter(([k, v]) => v != null || o[k]).map(([k, v]) => ({ key: k, label: FACT_LABELS[k], value: o[k] || v, edited: !!o[k] }))
}

/** The facts as a definition grid; brief shows the ones that matter when packing. */
export function FactsGrid({ c, note, brief }: { c: CountryFacts; note?: CountryNote | null; brief?: boolean }) {
  const rate = useRate(c.currencies[0]?.code)
  const rows = factValues(c, note).filter(r => !brief || ['capital', 'currency', 'languages', 'drives', 'timezone', 'calling_code'].includes(r.key))
  return (
    <dl className="facts-grid">
      {rows.map(r => (
        <div key={r.key} className={r.edited ? 'edited' : ''} title={r.edited ? 'Your correction' : undefined}>
          <dt>{r.label}</dt>
          <dd>{r.value}{r.key === 'currency' && rate && !r.edited && <div className="muted small">{rate}</div>}</dd>
        </div>
      ))}
      {(note?.extra || []).map((f, i) => <div key={'x' + i} className="own"><dt>{f.label}</dt><dd>{f.value}</dd></div>)}
    </dl>
  )
}

/** A compact "where you're going" card for one country of a trip. */
export function CountryBrief({ code, days }: { code: string; days?: number }) {
  const [data, setData] = useState<{ country: CountryFacts; note: CountryNote } | null>(null)
  useEffect(() => { let live = true; api.get<{ country: CountryFacts; note: CountryNote }>(`${V1}/countries/${code}?brief=1`).then(d => { if (live) setData(d) }).catch(() => {}); return () => { live = false } }, [code])
  return (
    <div className="country-brief">
      <Link to={`/countries/${code}`} className="cb-head"><span className="cb-flag">{flag(code)}</span><span><b>{data?.country.name || countryName(code)}</b>{days ? <span className="muted small"> · {days} day{days === 1 ? '' : 's'}</span> : null}</span><span className="spacer" /><span className="small">More →</span></Link>
      {data && <FactsGrid c={data.country} note={data.note} brief />}
      {data?.note.notes && <p className="cb-notes small">{data.note.notes.length > 240 ? data.note.notes.slice(0, 240) + '…' : data.note.notes}</p>}
    </div>
  )
}
