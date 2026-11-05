// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useMatch, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Trip } from '../lib/types'
import { todayISO } from '../lib/format'
import { Icon } from './icons'
import { Sidebar, addItems, splitTrips } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { Avatar, type Anchor, Menu, type MenuItem, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { TripModal } from '../pages/trip/TripSettings'
import { AskButton } from './AskButton'
import { WithBanner, WithPicker } from './WithPeople'

const SIDEBAR_KEY = 'pt-sidebar'

/** What the New menu can add: a trip, or something on a trip (opened on the trip's page). */
export type AddKind = 'trip' | 'place' | 'expense' | 'lodging' | 'transport' | 'journal'

export default function Layout() {
  const { trips, reloadTrips, withPeople } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  const tripPath = loc.pathname.split('/').slice(0, 3).join('/')
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [tripPath])
  // A trip saved, archived or deleted on a page shows in the sidebar once you move on.
  useEffect(() => { reloadTrips() }, [tripPath]) // eslint-disable-line react-hooks/exhaustive-deps

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [newTrip, setNewTrip] = useState(false)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  // the trip on screen, else the one you are on, else the next one you can edit
  const tm = useMatch('/trips/:id/*')
  const current = trips.find(t => String(t.id) === tm?.params.id)
  const today = todayISO()
  const editable = splitTrips(trips).upcoming.filter(t => t.role !== 'viewer')
  const target: Trip | undefined = current ? (current.role !== 'viewer' ? current : undefined) : editable.find(t => t.start_date && t.start_date <= today) || editable[0]

  /** Add a trip here, or open the trip and its form (`?add=` on the trip page). */
  const add = (kind: AddKind, t?: Trip) => {
    closeDrawer()
    if (kind === 'trip') { setNewTrip(true); return }
    const trip = t || target
    if (!trip) { setNewTrip(true); return }
    const tab = loc.pathname.startsWith(`/trips/${trip.id}`) ? loc.pathname.split('/')[3] || '' : ''
    navigate(`/trips/${trip.id}${tab ? '/' + tab : ''}?add=${kind}`)
  }
  const addRef = useRef(add)
  addRef.current = add

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pt:shortcuts', open)
    return () => window.removeEventListener('pt:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest?.('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': e.preventDefault(); addRef.current('trip'); break
        case 'p': e.preventDefault(); addRef.current('place'); break
        case 'c': e.preventDefault(); addRef.current('expense'); break
        case 'j': e.preventDefault(); addRef.current('journal'); break
        case 'h': go('/'); break
        case 't': go('/trips'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'trip', label: 'New trip', icon: 'luggage', hint: 'N', keywords: 'create plan holiday vacation', run: () => add('trip') },
    ...(target ? [
      { id: 'place', label: `Add a place — ${target.name}`, icon: 'pin', hint: 'P', keywords: 'sight restaurant idea', run: () => add('place') },
      { id: 'cost', label: `Add a cost — ${target.name}`, icon: 'wallet', hint: 'C', keywords: 'expense money spent budget', run: () => add('expense') },
      { id: 'stay', label: `Add a stay — ${target.name}`, icon: 'bed', keywords: 'hotel lodging apartment', run: () => add('lodging') },
      { id: 'transport', label: `Add transport — ${target.name}`, icon: 'plane', keywords: 'flight train leg', run: () => add('transport') },
      { id: 'journal', label: `Write in the journal — ${target.name}`, icon: 'book', hint: 'J', keywords: 'entry diary', run: () => add('journal') },
    ] : []),
    { id: 'home', label: 'Home', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'trips', label: 'Trips', icon: 'luggage', hint: 'T', run: () => navigate('/trips') },
    { id: 'import', label: 'Import from AdventureLog or a backup', icon: 'download', keywords: 'adventurelog json', run: () => navigate('/settings/import') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
      {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
      <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
        <Sidebar onAdd={add} onNavigate={closeDrawer} target={target} />
      </div>
      <div className="main">
        <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} trip={current} tripId={tm?.params.id} />
        <main className="content"><div className="content-inner"><WithBanner /><Outlet key={withPeople.join(',')} /></div></main>
      </div>
      {mobile && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
      {fabMenu && <Menu anchor={fabMenu} align="right" items={addItems(add, target)} onClose={() => setFabMenu(null)} width={230} />}
      {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      {newTrip && <TripModal onClose={() => setNewTrip(false)} onSaved={t => { setNewTrip(false); reloadTrips(); navigate(`/trips/${t.id}`) }} />}
    </div>
  )
}

const SECTION: Record<string, string> = { '': 'Home', trips: 'Trips', insights: 'Insights', countries: 'Countries', people: 'People' }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, trip, tripId }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; trip?: Trip; tripId?: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const section = SECTION[loc.pathname.split('/')[1] || ''] || ''
  // an archived or shared trip is not in the sidebar list: ask for its name
  const [name, setName] = useState('')
  useEffect(() => {
    setName('')
    if (!tripId || trip) return
    let live = true
    api.get<Trip>(`${V1}/trips/${tripId}`).then(t => { if (live) setName(t.name) }).catch(() => {})
    return () => { live = false }
  }, [tripId, trip])
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pt:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  const tripName = trip?.name || name
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {tripId ? <><button type="button" className="crumb" onClick={() => navigate('/trips')}>Trips</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{tripName}</span></> : section}
      </h1>
      <span className="spacer" />
      <WithPicker />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
