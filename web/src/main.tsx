import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { polyfillCountryFlagEmojis } from 'country-flag-emoji-polyfill'
import flagFont from './assets/TwemojiCountryFlags.woff2?url'
import './styles/base.css'
import './styles/travel.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'

// Windows has no flag emoji (🇮🇹 shows as "IT"): there, and only there, load a
// small flags-only font served from this box. --font lists it first.
polyfillCountryFlagEmojis('Twemoji Country Flags', flagFont)
import Home from './pages/Home'
import Trips from './pages/Trips'
import Stats from './pages/Stats'
import CountriesPage from './pages/Countries'
import CountryPage from './pages/Country'
import PeoplePage from './pages/People'
import PersonPage from './pages/Person'
import TripPage from './pages/Trip'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route element={<Gate><ErrorBoundary where="travel"><Layout /></ErrorBoundary></Gate>}>
        <Route path="/" element={<Home />} />
        <Route path="/trips" element={<Trips />} />
        <Route path="/insights" element={<Stats />} />
        <Route path="/countries" element={<CountriesPage />} />
        <Route path="/countries/:code" element={<CountryPage />} />
        <Route path="/people" element={<PeoplePage />} />
        <Route path="/people/:id" element={<PersonPage />} />
        <Route path="/trips/:id" element={<TripPage />} />
        <Route path="/trips/:id/:tab" element={<TripPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
