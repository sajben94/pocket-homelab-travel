import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, setPeopleFilter, V1 } from './api'
import { setDateFormats, type DateFormat, type TimeFormat } from './format'
import type { Currency, Meta, Person, Settings, Trip, User } from './types'

export type ThemeMode = 'system' | 'light' | 'dark'

interface AuthStatus { needs_setup: boolean; auth_mode: string; authenticated: boolean; user: User | null; version: string; title: string; oidc?: boolean; oidc_name?: string }
interface AppState {
  status: AuthStatus | null
  user: User | null
  settings: Settings | null
  users: User[]
  currencies: Currency[]
  meta: Meta | null
  trips: Trip[]
  themeMode: ThemeMode
  theme: 'light' | 'dark'
  setThemeMode: (m: ThemeMode) => void
  toggleTheme: () => void
  refresh: () => Promise<void>
  reloadMeta: () => Promise<void>
  reloadTrips: () => Promise<void>
  setUser: (u: User | null) => void
  setSettings: (s: Settings) => void
  /** your address book of travel companions */
  people: Person[]
  reloadPeople: () => Promise<void>
  /** "travelling with": the whole app shows only trips with all of these people */
  withPeople: number[]
  setWithPeople: (ids: number[]) => void
}
const Ctx = createContext<AppState>(null!)
export function useApp() { return useContext(Ctx) }

const EMPTY_META: Meta = { place_categories: [], expense_categories: [], discover_categories: [], packing_templates: [], lodging_types: [], transport_types: [] }

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [settings, setSettingsRaw] = useState<Settings | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [currencies, setCurrencies] = useState<Currency[]>([])
  const [meta, setMeta] = useState<Meta | null>(null)
  const [trips, setTrips] = useState<Trip[]>([])
  const [people, setPeople] = useState<Person[]>([])
  const [withPeople, setWithRaw] = useState<number[]>(() => {
    try { const ids = JSON.parse(localStorage.getItem('pt.with') || '[]'); if (Array.isArray(ids)) { setPeopleFilter(ids); return ids } } catch { /* none */ }
    return []
  })
  // The theme follows the system unless set to light or dark.
  const [themeMode, setThemeModeRaw] = useState<ThemeMode>(() => (localStorage.getItem('pt-theme') as ThemeMode) || 'system')
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => setSystemDark(mq.matches)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [])
  const theme: 'light' | 'dark' = themeMode === 'system' ? (systemDark ? 'dark' : 'light') : themeMode
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.querySelectorAll('meta[name="theme-color"]').forEach(m => m.setAttribute('content', theme === 'dark' ? '#111113' : '#ffffff'))
  }, [theme])
  const setThemeMode = useCallback((m: ThemeMode) => { setThemeModeRaw(m); if (m === 'system') localStorage.removeItem('pt-theme'); else localStorage.setItem('pt-theme', m) }, [])
  useEffect(() => { if (settings?.title) document.title = settings.title }, [settings?.title])
  const setSettings = useCallback((s: Settings) => { setSettingsRaw(s); setDateFormats(s.date_format as DateFormat, s.time_format as TimeFormat) }, [])
  // The trips in the sidebar and the palette (archived ones stay out).
  const reloadTrips = useCallback(async () => { setTrips(await api.get<Trip[]>(`${V1}/trips?archived=0`).catch(() => [] as Trip[])) }, [])
  const reloadPeople = useCallback(async () => { setPeople(await api.get<Person[]>(`${V1}/people`).catch(() => [] as Person[])) }, [])
  const setWithPeople = useCallback((ids: number[]) => {
    setPeopleFilter(ids)
    setWithRaw(ids)
    try { if (ids.length) localStorage.setItem('pt.with', JSON.stringify(ids)); else localStorage.removeItem('pt.with') } catch { /* private mode */ }
    reloadTrips()
  }, [reloadTrips])
  const reloadMeta = useCallback(async () => {
    const [u, c, m] = await Promise.all([api.get<User[]>(`${V1}/users`), api.get<Currency[]>(`${V1}/currencies`), api.get<Meta>(`${V1}/meta`).catch(() => EMPTY_META)])
    setUsers(u); setCurrencies(c); setMeta(m)
    await Promise.all([reloadTrips(), reloadPeople()])
  }, [reloadTrips, reloadPeople])
  const refresh = useCallback(async () => {
    const st = await api.get<AuthStatus>(`${V1}/auth/status`)
    setStatus(st)
    setUser(st.user)
    if (st.authenticated) {
      setSettings(await api.get<Settings>(`${V1}/settings`))
      await reloadMeta()
    }
  }, [reloadMeta, setSettings])
  useEffect(() => { refresh().catch(() => setStatus({ needs_setup: false, auth_mode: 'local', authenticated: false, user: null, version: '', title: 'Pocket Travel' })) }, [refresh])
  useEffect(() => {
    const h = () => { setUser(null); setStatus(s => s ? { ...s, authenticated: false, user: null } : s) }
    window.addEventListener('pt:unauthorized', h)
    return () => window.removeEventListener('pt:unauthorized', h)
  }, [])
  return <Ctx.Provider value={{ status, user, settings, users, currencies, meta, trips, themeMode, theme, setThemeMode, toggleTheme: () => setThemeMode(theme === 'dark' ? 'light' : 'dark'), refresh, reloadMeta, reloadTrips, setUser, setSettings, people, reloadPeople, withPeople, setWithPeople }}>{children}</Ctx.Provider>
}
