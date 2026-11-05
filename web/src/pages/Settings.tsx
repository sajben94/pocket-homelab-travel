import { PocketModelSelect } from '../components/PocketModelSelect'
import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { CospendImport } from '../components/CospendImport'
import type { APIToken, ConnMode, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { CurrencySelect } from '../components/Money'
import { TILES } from '../components/Map'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { NeedsCore } from '../components/NeedsCore'

type TabId = 'general' | 'integrations' | 'import' | 'profile' | 'users' | 'api' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'Currency, distances, maps, date and time formats, and the app title.' },
  { id: 'integrations', label: 'Integrations', icon: 'layers', sub: 'Pocket Money, Pocket Movement or Dawarich, Pocket Places, Immich and OpenAI — your own accounts, through Pocket Core or directly.' },
  { id: 'import', label: 'Import', icon: 'download', sub: 'Bring projects over from Nextcloud Cospend, collections from AdventureLog, or restore a trip from a JSON backup.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'users', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'api', label: 'API tokens', icon: 'key', sub: 'Personal tokens for widgets, scripts and calendar subscriptions.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database and export single trips.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// names the other Pocket apps use for the same sections
const ALIAS: Record<string, TabId> = { apps: 'integrations', people: 'users', tokens: 'api' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = (TABS.find(t => t.id === (ALIAS[want] || want))?.id) || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  // old links (/settings?tab=integrations) land on /settings/integrations
  useEffect(() => { if (legacy || (raw && raw !== tab)) navigate(`/settings/${tab}`, { replace: true }) }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  // Settings sits outside the shell, so it shows the shortcuts sheet itself
  const [shortcuts, setShortcuts] = useState(false)
  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pt:shortcuts', open)
    return () => window.removeEventListener('pt:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to your trips" title="Back to your trips (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'general' && <General />}
            {tab === 'integrations' && <Integrations />}
            {tab === 'import' && <Import />}
            {tab === 'profile' && <Profile />}
            {tab === 'users' && <Users />}
            {tab === 'api' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Money & distances" sub="Yours alone — everyone on the box picks their own.">
        <SettingRow title="Home currency" help="The default for new trips and budgets."><CurrencySelect value={f.home_currency} onChange={c => setF({ ...f, home_currency: c })} /></SettingRow>
        <SettingRow title="Distances"><Segmented value={f.distance_unit} onChange={v => setF({ ...f, distance_unit: v as 'km' | 'mi' })} options={[{ value: 'km', label: 'km' }, { value: 'mi', label: 'miles' }]} size="sm" label="Distances" /></SettingRow>
      </Section>
      <Section title="Date & time">
        <SettingRow title="Date format"><Select value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
      </Section>
      <Section title="Stays" sub="What a new stay starts with; each stay can still have its own times.">
        <SettingRow title="Default check-in" help="Also used for stays saved without a time."><input type="time" value={f.checkin_time || '15:00'} onChange={e => setF({ ...f, checkin_time: e.target.value })} style={{ width: 130 }} aria-label="Default check-in time" /></SettingRow>
        <SettingRow title="Default check-out"><input type="time" value={f.checkout_time || '11:00'} onChange={e => setF({ ...f, checkout_time: e.target.value })} style={{ width: 130 }} aria-label="Default check-out time" /></SettingRow>
      </Section>
      <Section title="Maps & places">
        <SettingRow title="Map tiles" help="All free and keyless. Satellite is Esri imagery with a labels layer on top."><Select value={f.map_tiles} onChange={v => setF({ ...f, map_tiles: v })} options={Object.entries(TILES).map(([v, t]) => ({ value: v, label: t.label }))} /></SettingRow>
        <SettingRow title="Find photos for new places" help="Wikipedia and Wikimedia Commons, with credits."><Switch checked={f.auto_photos} onChange={v => setF({ ...f, auto_photos: v })} label="Find photos for new places" /></SettingRow>
      </Section>
      <Section title="This box" sub={isAdmin ? 'These apply to everyone on this box.' : 'Set by an admin for everyone.'}>
        <SettingRow title="App title" help="Shown in the sidebar and the browser tab."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} disabled={!isAdmin} style={{ width: 220 }} /></SettingRow>
        <SettingRow title="Look up exchange rates" help={f.fx_provider === 'none' ? 'Disabled on the server.' : 'Frankfurter / open.er-api.com.'}>
          <Switch checked={f.fx_auto} onChange={v => setF({ ...f, fx_auto: v })} disabled={!isAdmin || f.fx_provider === 'none'} label="Look up exchange rates" />
        </SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
      <Section title="How it fits together">
        <dl className="set-dl">
          <div><dt>Trips</dt><dd className="muted">Dates, a home currency and a budget. Everything else hangs off a trip.</dd></div>
          <div><dt>Places</dt><dd className="muted">Ideas until you drag them onto a day. Tick them off as you go.</dd></div>
          <div><dt>Budget</dt><dd className="muted">Stays and transport add their price as planned cost; Pocket Money brings the real spending in.</dd></div>
          <div><dt>Recorded</dt><dd className="muted">Pocket Movement (or Dawarich) draws where you actually went; Immich shows what you photographed.</dd></div>
          <div><dt>Outside</dt><dd className="muted">Places search and discover use OpenStreetMap; the weather is Open-Meteo. No keys needed.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const [test, setTest] = useState<Record<string, string>>({})
  if (!f) return <Loading />
  const run = async (which: 'money' | 'movement' | 'dawarich' | 'places' | 'immich' | 'openai', url: string, key: string) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  // Pocket AI only comes through Pocket Core: without one a saved "pocket" shows as what is in effect.
  const aiShown = !f.core_ready && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  // Dawarich and Immich: Pocket Core (address + your own key from Core), Direct (URL + API key) or Off.
  const conn = (which: 'dawarich' | 'immich', app: string, urlKey: 'dawarich_url' | 'immich_url', keyKey: 'dawarich_api_key' | 'immich_api_key', help: string) => (
      <Connection app={app} service source={f[`${which}_source`]} mode={f[`${which}_mode`]} via={f[`${which}_via`]} detail={f[`${which}_detail`]} core={f.core_ready} hasToken={!!f[keyKey]}
        onMode={m => save({ [`${which}_mode`]: m })} onDisconnect={() => save(which === 'immich' ? { immich_url: '', immich_api_key: '', immich_public_url: '' } : { dawarich_url: '', dawarich_api_key: '' })}
        test={<><button type="button" className="btn sm" onClick={() => run(which, f[urlKey], f[keyKey])}>Test connection</button>{status(which)}</>}
        direct={<div className="grid" style={{ gap: 12 }}>
          <div className="two-col tight">
            <Field label="URL" help={help}><input value={f[urlKey]} onChange={e => setF({ ...f, [urlKey]: e.target.value })} placeholder="http://host.docker.internal:8080" /></Field>
            <Field label="API key" help="Stored on the server; shown masked. Leave as is to keep the current one."><input value={f[keyKey]} onChange={e => setF({ ...f, [keyKey]: e.target.value })} autoComplete="off" /></Field>
          </div>
          {which === 'immich' && <Field label="Public URL (optional)" help="What the browser opens for “Open in Immich” when the API URL above is internal (e.g. host.docker.internal)."><input value={f.immich_public_url} onChange={e => setF({ ...f, immich_public_url: e.target.value })} placeholder="https://photos.example.com" /></Field>}
        </div>} />
  )
  const block = (which: 'dawarich' | 'immich', title: string, icon: string, app: string, urlKey: 'dawarich_url' | 'immich_url', keyKey: 'dawarich_api_key' | 'immich_api_key', what: string, help: string) => (
    <Section title={title} icon={icon} actions={connBadge(shownMode(f[`${which}_mode`], f.core_ready), f[`${which}_via`])} sub={what}>
      {conn(which, app, urlKey, keyKey, help)}
    </Section>
  )
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><span>These are your own accounts. Each person connects their own Pocket Money, Pocket Movement or Dawarich, Pocket Places, Immich and OpenAI (through Pocket Core, with your own key there, or directly); nobody else on this box uses yours. On a trip shared with you, the route and photos come from your accounts and the spending from the trip owner's Pocket Money.</span></div>
      <Section title="Pocket Money" icon="wallet" actions={connBadge(shownMode(f.money_mode, f.core_ready), f.money_via)} sub={<>What the trip really cost. Tag the expenses in Pocket Money (e.g. "lisbon 2027") and set the same Money tag on the trip; they appear as spent, in their own currency, converted at the day's rate. Synced every 30 minutes while a trip is running or upcoming, and on demand.</>}>
        <Connection app="Pocket Money" mode={f.money_mode} via={f.money_via} detail={f.money_detail} core={f.core_ready} hasToken={!!f.firefly_token}
          onMode={m => save({ money_mode: m })} onDisconnect={() => save({ firefly_url: '', firefly_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('money', f.firefly_url, f.firefly_token)}>Test connection</button>{status('money')}</>}
          direct={<div className="two-col tight">
            <Field label="Money or Firefly III URL" help="Pocket Money's Firefly-compatible API is its address plus /firefly (inside Docker e.g. http://host.docker.internal:8178/firefly); a real Firefly III works too."><input value={f.firefly_url} onChange={e => setF({ ...f, firefly_url: e.target.value })} placeholder="https://money.example.com/firefly" /></Field>
            <Field label="Token" help="Pocket Money → Settings → API tokens, or a Firefly personal access token. Stored on the server; shown masked."><input value={f.firefly_token} onChange={e => setF({ ...f, firefly_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
      </Section>
      <LocationSource f={f} save={save} test={<><button type="button" className="btn sm" onClick={() => run('movement', '', '')}>Test Pocket Movement</button>{status('movement')}</>}
        dawarich={conn('dawarich', 'Dawarich', 'dawarich_url', 'dawarich_api_key', 'Dawarich → Account → API key. Behind HTTPS use the public name; plain http works too.')} />
      <Section title="Pocket Places" icon="pin" actions={connBadge(f.places_mode, f.places_via)} sub="On each trip's page: the places on your list at the destination (to try, and the ones you liked before), the lists you made for the trip and where you went during it, with what it cost. Read from your own Pocket Places; nothing is changed there.">
        <Connection app="Pocket Places" coreOnly mode={f.places_mode} via={f.places_via} detail={f.places_detail} core={f.core_ready} hasToken={false}
          onMode={m => save({ places_mode: m === 'off' ? 'off' : 'core' })} onDisconnect={() => {}}
          test={<><button type="button" className="btn sm" onClick={() => run('places', '', '')}>Test Pocket Places</button>{status('places')}</>} direct={null} />
      </Section>
      {block('immich', 'Immich', 'camera', 'Immich', 'immich_url', 'immich_api_key', 'Every photo and video taken between the trip dates, grouped by day; the geotagged ones land on the map. Thumbnails are proxied so the browser never sees the key.', 'Immich → Account settings → API keys.')}
      <Section title="AI provider" icon="sparkles" actions={<span className={`badge ${f.ai_configured ? 'ok' : ''}`}>{f.ai_configured ? 'connected' : 'not set'}</span>}
        sub={<>Optional. Adds five buttons: <b>Suggest places</b> (popular, well-rated places for the destination, located with OpenStreetMap), <b>Plan my days</b> (assigns ideas to days and times), <b>Describe</b> a place with practical tips, a <b>packing list</b> tailored to the trip and weather, and a <b>recap</b> written from your journal, visited places and spending. Trip details are sent to the provider when you use them; nothing runs automatically.</>}>
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Text generation" help="Automatic prefers your enabled Pocket AI connection. Without one, it uses your saved OpenAI settings."><Select ariaLabel="Text generation provider" value={aiShown} onChange={v => setF({ ...f, ai_provider: v })} options={[{ value: 'auto', label: f.core_ready ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !f.core_ready, hint: f.core_ready ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} /></Field>
          {!f.core_ready && <NeedsCore what="Pocket AI" />}
          <p className="muted small" role="status">Saved selection: {f.ai_status?.detail}.{f.core_ready && ' Pocket AI uses your connected Claude account.'}</p>
          {f.core_ready && aiShown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(aiShown === 'openai' || (!f.core_ready && aiShown === 'auto')) && <>
          <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
          <div className="two-col tight">
            <Field label="Model" help="Default gpt-4o-mini (cheap, good enough). gpt-4o or gpt-4.1 give better suggestions."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
            <Field label="Base URL (optional)" help="For OpenAI-compatible servers: Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
          </div>
          <div className="row"><button type="button" className="btn sm" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test connection</button>{status('openai')}</div>
          </>}
        </div>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

/** The mode in effect as Settings shows it: a saved "core" without Pocket Core is Direct (the only way in then). */
const shownMode = (mode: ConnMode, core: boolean): ConnMode => mode === 'core' && !core ? 'direct' : mode

/**
 * One connection — a Pocket app, or a third-party service (`service`, e.g.
 * Dawarich, Immich) whose address and key come from Pocket Core — as Pocket
 * Core, Direct (URL + token / API key) or Off, with what it actually is right
 * now. The mode saves at once; Disconnect removes the saved URL and token.
 * With Pocket Core on the box the switch is Pocket Core | Off and Direct is
 * an advanced option behind a link (always shown while it is the mode in
 * use); without Core (`core` = connected and registered), Pocket Core shows
 * greyed out with what it needs and Direct is the way in.
 */
function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct, service, source, coreOnly }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode; service?: boolean; source?: string; coreOnly?: boolean
}) {
  // an app that only comes through Pocket Core (coreOnly) never offers Direct; without Core its Pocket Core choice shows greyed out
  const mode = coreOnly ? (saved === 'off' ? 'off' : 'core') : shownMode(saved, core)
  const ok = via !== ''
  const [advanced, setAdvanced] = useState(false)
  const showDirect = !coreOnly && (!core || mode === 'direct' || advanced)
  const secret = service ? 'API key' : 'token'
  const coreOk = service
    ? `Connected through Pocket Core (${source === 'box' ? "the box's key" : 'your own key'}). Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.`
    : `Connected through Pocket Core, as you. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.`
  const line = mode === 'off' ? `Off: Travel does not read ${app}.`
    : mode === 'core' ? (ok ? coreOk : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own URL and ${secret}; use Test connection to check it.` : `Not connected: ${(saved !== 'core' && detail) || `add the URL and ${service ? 'an API key' : 'a token'}`}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', disabled: !core, title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core' },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: service ? 'Your own URL and API key' : 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && !coreOnly && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      {!core && <NeedsCore what={coreOnly ? app : `${app} through Pocket Core`} />}
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title={`Remove the saved URL and ${secret}`}>Disconnect</button>}</div>
      </>}
      {mode === 'core' && core && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

/**
 * Where the recorded route, timeline, distances and countries come from:
 * Pocket Movement (only through Pocket Core; the default there) or Dawarich
 * (its connection shows in the same card while it is in use). Saves at once; Movement unavailable falls back
 * to Dawarich and says why.
 */
function LocationSource({ f, save, test, dawarich }: { f: SettingsT; save: (p: Partial<SettingsT>) => void; test: React.ReactNode; dawarich: React.ReactNode }) {
  const core = f.core_ready
  // without Core only Dawarich is in effect, whatever was saved
  const value = core ? (f.location_source || 'movement') : 'dawarich'
  const using = f.location_effective
  const line = using === 'movement'
    ? 'Pocket Movement, through Pocket Core as you: your own location history, with named stops, how you travelled and the countries you were in. Nothing to fill in.'
    : value === 'movement'
      ? `Pocket Movement is not available: ${f.location_detail || 'Pocket Core did not answer'}. Using Dawarich meanwhile.`
      : `Dawarich, connected as below.${core && f.movement_available ? ' Pocket Movement is available too.' : ''}`
  const ok = using === 'movement' || (using === 'dawarich' && value === 'dawarich' && f.dawarich_via !== '')
  return (
    <Section title="Where you went" icon="route" actions={<span className={`badge ${ok ? 'ok' : 'warn'}`}>{using === 'movement' ? 'Pocket Movement' : 'Dawarich'}</span>}
      sub="The recorded route on the map, the day-by-day timeline with your stops, distances and countries. Yours alone; on a trip shared with you it shows your own history.">
      <div className="conn-block">
        <div className="conn-mode">
          <Segmented size="sm" label="Location source" value={value} onChange={v => save({ location_source: v })} options={[
            { value: 'movement', label: 'Pocket Movement', disabled: !core, title: core ? 'Your own Pocket Movement, through Pocket Core' : 'Needs Pocket Core' },
            { value: 'dawarich', label: 'Dawarich', title: 'Your Dawarich' },
          ]} />
        </div>
        {!core && <NeedsCore what="Pocket Movement" />}
        <div className={`conn-status ${ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
        {core && value === 'movement' && <div className="row wrap">{test}</div>}
      </div>
      {/* One source, one card: Dawarich's connection shows here while it is the one in use (picked, or filling in for Movement). */}
      {using !== 'movement' && <div className="loc-dawarich">
        <div className="loc-dawarich-h">Dawarich connection</div>
        {dawarich}
      </div>}
    </Section>
  )
}

function Import() {
  const { toast } = useToast()
  const { reloadTrips } = useApp()
  const navigate = useNavigate()
  const [al, setAL] = useState({ url: '', username: '', password: '' })
  const [cols, setCols] = useState<{ id: string; name: string; start_date: string; end_date: string; location_count: number; is_archived: boolean }[] | null>(null)
  const [sel, setSel] = useState<string[]>([])
  const [images, setImages] = useState(true)
  const [busy, setBusy] = useState('')
  const [result, setResult] = useState<{ results: { collection: string; trip_id?: number; name?: string; counts?: Record<string, number>; error?: string }[] } | null>(null)
  const list = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy('list')
    try { const c = await api.post<typeof cols>(`${V1}/import/adventurelog/collections`, al); setCols(c); setSel((c || []).filter(x => !x.is_archived).map(x => x.id)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const run = async () => {
    setBusy('import')
    try { setResult(await api.post(`${V1}/import/adventurelog`, { ...al, collections: sel, images })); reloadTrips() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const importJSON = async (file: File) => {
    setBusy('json')
    try { const t = await api.post<{ id: number; name: string }>(`${V1}/import/trip`, JSON.parse(await file.text())); toast(`Imported ${t.name}`); reloadTrips(); navigate(`/trips/${t.id}`) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  return (
    <div className="stack loose">
      <Section title="From Nextcloud Cospend" icon="download" sub="A Cospend project's people, bills, categories and payment methods, through its share link — into a new trip or one of yours. Import again any time: it updates instead of doubling.">
        <CospendImport />
      </Section>
      <Section title="From AdventureLog" icon="download" sub="Collections become trips; locations, lodging, transportation, notes and checklists come along, with their images. Re-running skips what is already here. Legs that AdventureLog imported from Dawarich are left out — the route is live here.">
        <form onSubmit={list} className="grid" style={{ gap: 12 }}>
          <Field label="AdventureLog URL"><input value={al.url} onChange={e => setAL({ ...al, url: e.target.value })} placeholder="https://adventurelog.example.com" required /></Field>
          <div className="two-col tight">
            <Field label="Username"><input value={al.username} onChange={e => setAL({ ...al, username: e.target.value })} required autoComplete="off" /></Field>
            <Field label="Password"><input type="password" value={al.password} onChange={e => setAL({ ...al, password: e.target.value })} required autoComplete="new-password" /></Field>
          </div>
          <div><button className="btn" disabled={busy === 'list'}>{busy === 'list' ? 'Connecting…' : 'List collections'}</button></div>
        </form>
        {cols && (
          <div className="mt">
            {cols.length === 0 ? <p className="muted">No collections found.</p> : <>
              <table className="table compact"><tbody>{cols.map(c => <tr key={c.id}><td style={{ width: 28 }}><input type="checkbox" checked={sel.includes(c.id)} onChange={e => setSel(s => e.target.checked ? [...s, c.id] : s.filter(x => x !== c.id))} style={{ width: 'auto' }} aria-label={`Import ${c.name}`} /></td><td><b>{c.name}</b>{c.is_archived && <span className="badge" style={{ marginLeft: 6 }}>archived</span>}</td><td className="muted small">{c.start_date} → {c.end_date}</td><td className="muted small num">{c.location_count} places</td></tr>)}</tbody></table>
              <SettingRow title="Download images" help="Slower, but the places keep their pictures."><Switch checked={images} onChange={setImages} label="Download images" /></SettingRow>
              <div className="mt-sm"><button type="button" className="btn primary" onClick={run} disabled={busy === 'import' || sel.length === 0}>{busy === 'import' ? 'Importing… this can take a minute' : `Import ${sel.length} collection${sel.length === 1 ? '' : 's'}`}</button></div>
            </>}
          </div>
        )}
        {result && <div className="callout success mt"><Icon name="check-circle" size={15} /><div>{result.results.map((r, i) => <div key={i}>{r.error ? <span className="bad">✕ {r.collection}: {r.error}</span> : <span>✓ <Link to={`/trips/${r.trip_id}`}>{r.name}</Link> — {Object.entries(r.counts || {}).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new'}</span>}</div>)}</div></div>}
      </Section>
      <Section title="From a JSON backup" icon="upload" sub="A trip exported from Pocket Travel (trip settings → JSON backup) imports as a new trip owned by you.">
        <label className="btn"><Icon name="upload" size={15} />{busy === 'json' ? 'Importing…' : 'Choose file…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label>
      </Section>
    </div>
  )
}

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>} sub="Everyone signs in with their own account. A trip is visible only to its owner and the companions added under the trip's settings. Admins manage users and the box-wide settings but see no one else's trips.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account and the trips they own are deleted." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change your trips, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) and authorise a calendar subscription (<code>/cal/&lt;trip&gt;.ics?token=…</code>). See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage widget)" aria-label="Token name" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
    </div>
  )
}

function Data() {
  const { user } = useApp()
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="Admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>) with every trip. Uploaded documents and images live next to it under <code>files/</code>. Restore by stopping the app and replacing <code>pocket-travel.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Per-trip exports" icon="luggage" sub="Each trip has its own exports under its Settings tab: a calendar (.ics) you can subscribe to, GPX for maps and watches, and a JSON backup that imports again under Import." />
    </div>
  )
}

function About() {
  const { user, status } = useApp()
  const isAdmin = user?.role === 'admin'
  const { data } = useAsync(() => isAdmin ? api.get<Record<string, unknown>>(`${V1}/settings/system`) : Promise.resolve(null), [isAdmin])
  const counts = (data?.counts || {}) as Record<string, number>
  const tiles: [string, number | undefined, string][] = [['Trips', counts.trips, 'luggage'], ['Places', counts.places, 'pin'], ['Expenses', counts.expenses, 'wallet'], ['Journal', counts.journal, 'book']]
  const rows: [string, React.ReactNode][] = data ? [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))} · files {fmtBytes(Number(data.files_bytes))}</span>],
    ['More rows', `${counts.documents ?? 0} documents · ${counts.users ?? 0} users`],
  ] : []
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Travel{status?.version && <span className="muted small"> · {/^v?\d/.test(status.version) ? 'v' + status.version.replace(/^v/, '') : status.version}</span>}</h3>
          <p className="muted small">A self-hosted trip planner: itinerary, places, stays, transport, budget, packing lists and a journal — with the real spending from Pocket Money (or Firefly III), the recorded route from Dawarich and the photos from Immich on the same page. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      {data && <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{n ?? 0}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>}
      {data && <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>}
      <p className="muted small">Open source (MIT). Sibling projects: Pocket Habits, Pocket Inventory, Pocket Bookshelf, Pocket Split, Pocket Clips, Pocket Subtitles and Pocket Invoicing. Maps © OpenStreetMap contributors; weather by Open-Meteo; optional AI features via your own OpenAI-compatible key.</p>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => window.dispatchEvent(new CustomEvent('pt:shortcuts'))}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-travel" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
    </div>
  )
}
