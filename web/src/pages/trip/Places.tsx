import { useEffect, useState } from 'react'
import { api, uploadForm, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { GeoResult, Place, PlacePhoto, PlaceStatus, Trip } from '../../lib/types'
import { PLACE_ICONS, PLACE_STATUS_LABEL } from '../../lib/types'
import { appleMaps, cap, dateRange, fmtClock, fmtDate, gmaps, stars } from '../../lib/format'
import { Confirm, Empty, Field, Modal, Segmented, useAsync, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select } from '../../components/Select'
import { AmountInput, Money } from '../../components/Money'
import { GeoSearch } from '../../components/GeoSearch'
import { MapView } from '../../components/Map'
import { useTrip } from './ctx'
import { SuggestModal, describePlace } from './ai'

export function Places() {
  const { ov, trip, canEdit, edit, reload } = useTrip()
  const { meta } = useApp()
  const [q, setQ] = useState('')
  const [cat, setCat] = useState('')
  const [status, setStatus] = useState('')
  const [view, setView] = useState<'cards' | 'table'>('cards')
  const [suggest, setSuggest] = useState(false)
  const [finding, setFinding] = useState(false)
  const { toast } = useToast()
  const findMissing = async () => {
    setFinding(true)
    try {
      const r = await api.post<{ queued: number }>(`${V1}/trips/${trip.id}/places/photos/find-missing`)
      toast(`Looking up photos for ${r.queued} places — they appear as they are found`)
      // Poll a few times while the lookups run in the background.
      for (let i = 0; i < 12; i++) { await new Promise(res => setTimeout(res, 5000)); await reload() }
    } catch (e) { toast((e as Error).message, 'err') } finally { setFinding(false) }
  }
  const list = ov.places.filter(p => (!q || `${p.name} ${p.address} ${p.tags} ${p.notes}`.toLowerCase().includes(q.toLowerCase())) && (!cat || p.category === cat) && (!status || p.status === status))
  const cats = Array.from(new Set(ov.places.map(p => p.category)))
  return (
    <>
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter places…" aria-label="Filter places" /></div>
        <Select value={cat} onChange={setCat} options={cats.map(c => ({ value: c, label: `${PLACE_ICONS[c] || ''} ${cap(c)}` }))} placeholder="All categories" clearLabel="All categories" />
        <Select value={status} onChange={setStatus} options={Object.entries(PLACE_STATUS_LABEL).map(([v, l]) => ({ value: v, label: l }))} placeholder="Any status" clearLabel="Any status" />
        <Segmented value={view} onChange={setView} options={[{ value: 'cards', label: <Icon name="grid" size={14} />, title: 'Cards' }, { value: 'table', label: <Icon name="list" size={14} />, title: 'Table' }]} size="sm" label="View" />
        {canEdit && ov.places.some(p => !p.photos?.length) && <button type="button" className="btn" title="Look up pictures on Wikipedia and Wikimedia Commons for places without any" onClick={findMissing} disabled={finding}><Icon name="image" size={15} />{finding ? 'Looking…' : `Find photos (${ov.places.filter(p => !p.photos?.length).length})`}</button>}
        {canEdit && ov.integrations.ai.configured && <button type="button" className="btn" onClick={() => setSuggest(true)}><Icon name="sparkles" size={15} />Suggest</button>}
        {canEdit && <button type="button" className="btn primary" onClick={() => edit('place', null, {})}><Icon name="plus" size={15} />Place</button>}
      </div>
      {suggest && <SuggestModal onClose={() => setSuggest(false)} />}
      {list.length === 0 ? <Empty icon="pin" title={ov.places.length ? 'Nothing matches' : 'No places yet'}>{!ov.places.length && (ov.integrations.ai.configured ? 'Add the sights, restaurants and beaches you want to see, or press Suggest for popular places at the destination.' : 'Add the sights, restaurants and beaches you want to see. Use the map tab to discover what is nearby.')}</Empty> : view === 'cards' ? (
        <div className="pool">
          {list.map(p => (
            <div key={p.id} className="place-card" onClick={() => edit('place', p)}>
              <div className="img" style={p.photos?.length ? { backgroundImage: `url(${V1}/trips/${trip.id}/places/photos/${p.photos[0].id})` } : undefined}>{!p.photos?.length && (PLACE_ICONS[p.category] || '📍')}{p.photos?.length > 1 && <span className="img-count"><Icon name="camera" size={11} />{p.photos.length}</span>}</div>
              <div className="pc-body">
                <h4><span className={`status-dot ${p.status}`} /> {p.name}</h4>
                <div className="sub">{cap(p.category)}{p.day_date ? ` · ${fmtDate(p.day_date, false)}${p.start_time ? ' ' + fmtClock(p.start_time) : ''}` : ' · unscheduled'}</div>
                {p.address && <div className="sub">{p.address}</div>}
                <div className="row small" style={{ gap: 8 }}>{p.rating ? <span className="stars">{stars(p.rating)}</span> : null}{p.price > 0 && <Money minor={p.price} code={p.currency || trip.currency} />}{p.status === 'booked' && <span className="badge warn">booked</span>}</div>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <table className="table compact">
          <thead><tr><th /><th>Name</th><th>Category</th><th>When</th><th>Status</th><th className="num">Price</th></tr></thead>
          <tbody>{list.map(p => <tr key={p.id} className="clickable" onClick={() => edit('place', p)}><td>{PLACE_ICONS[p.category] || '📍'}</td><td><b>{p.name}</b><div className="small muted">{p.address}</div></td><td className="muted">{cap(p.category)}</td><td className="muted">{p.day_date ? `${fmtDate(p.day_date, false)} ${fmtClock(p.start_time)}` : '—'}</td><td><span className={`status-dot ${p.status}`} /> {PLACE_STATUS_LABEL[p.status]}</td><td className="num">{p.price > 0 && <Money minor={p.price} code={p.currency || trip.currency} />}</td></tr>)}</tbody>
        </table>
      )}
    </>
  )
}

const RATINGS = [1, 2, 3, 4, 5]

export function PlaceModal({ trip, item, defaults = {}, onClose, onSaved }: { trip: Trip; item: Place | null; defaults?: Record<string, unknown>; onClose: () => void; onSaved: () => void }) {
  const { meta } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<Partial<Place>>(item ? { ...item } : { name: '', category: 'sight', status: 'idea', currency: trip.currency, price: 0, day_date: '', start_time: '', duration_min: 0, address: '', link: '', phone: '', notes: '', description: '', tags: '', booking_ref: '', lat: null, lng: null, rating: null, ...defaults })
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const [tab, setTab] = useState<'main' | 'more'>('main')
  const [wikiBusy, setWikiBusy] = useState(false)
  const [aiBusy, setAiBusy] = useState(false)
  const [catTouched, setCatTouched] = useState(!!item)
  const [picked, setPicked] = useState<string>('')
  const { settings } = useApp()
  const { edit } = useTrip()
  const set = <K extends keyof Place>(k: K, v: Place[K]) => setF(x => ({ ...x, [k]: v }))
  const base = `${V1}/trips/${trip.id}/places`
  useEffect(() => { if (f.day_date && f.status === 'idea') set('status', 'planned') }, [f.day_date]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = { ...f, price: f.price || 0, duration_min: f.duration_min || 0 }
      if (item) await api.put(`${base}/${item.id}`, body); else await api.post(base, body)
      toast(item ? 'Place saved' : 'Place added'); onSaved()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => { try { await api.del(`${base}/${item!.id}`); toast('Place removed'); onSaved() } catch (e) { toast((e as Error).message, 'err') } }
  const pick = (r: GeoResult) => {
    setPicked(r.display)
    setF(x => ({ ...x, name: x.name || r.name, address: r.display, lat: r.lat, lng: r.lng, link: x.link || r.website || '', phone: x.phone || r.phone || '', category: !catTouched && r.suggest ? r.suggest : x.category }))
  }
  const dropPin = async (lat: number, lng: number) => {
    setF(x => ({ ...x, lat, lng }))
    try { const r = await api.get<GeoResult>(`${V1}/geo/reverse?lat=${lat}&lng=${lng}`); setPicked(r.display); setF(x => ({ ...x, address: r.display, name: x.name || r.name })) } catch { /* keep the coordinates */ }
  }
  const wiki = async () => {
    setWikiBusy(true)
    try { const w = await api.get<{ extract: string; url: string }>(`${V1}/geo/wiki?q=${encodeURIComponent(f.name || '')}`); set('description', w.extract); if (!f.link) set('link', w.url) } catch (e) { toast((e as Error).message, 'err') } finally { setWikiBusy(false) }
  }
  const describe = async () => {
    setAiBusy(true)
    try { const d = await describePlace(trip.id, { place_id: item?.id, name: f.name || '', address: f.address, category: f.category }); setF(x => ({ ...x, description: d.description, notes: x.notes ? x.notes + '\n\n' + d.tips : d.tips, duration_min: x.duration_min || d.duration_min })) } catch (e) { toast((e as Error).message, 'err') } finally { setAiBusy(false) }
  }
  const days = dateRange(trip.start_date, trip.end_date)
  const marker = f.lat != null && f.lng != null ? [{ id: 'p', lat: f.lat, lng: f.lng!, label: f.name || '', icon: PLACE_ICONS[f.category || 'sight'] }] : []
  return (
    <Modal title={item ? 'Edit place' : 'Add a place'} onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <div className="tabs sub"><button type="button" className={tab === 'main' ? 'active' : ''} onClick={() => setTab('main')}>Details</button><button type="button" className={tab === 'more' ? 'active' : ''} onClick={() => setTab('more')}>Location, notes & photo</button></div>
        {tab === 'main' && <>
          {!item && <SavedPlacePick tripId={trip.id} onPick={sp => { setCatTouched(true); setPicked(sp.address); setF(x => ({ ...x, ...reuse(sp) })) }} />}
          <div className="two-col tight">
            <Field label="Name"><input value={f.name || ''} onChange={e => set('name', e.target.value)} required autoFocus={!item} maxLength={120} /></Field>
            <Field label="Category"><Select value={f.category || 'sight'} onChange={v => { setCatTouched(true); set('category', v) }} options={(meta?.place_categories || []).map(c => ({ value: c, label: `${PLACE_ICONS[c] || ''} ${cap(c)}` }))} /></Field>
          </div>
          <Field label="Where is it" help="Search OpenStreetMap, paste a Google/Apple Maps link, or type coordinates — or click the map to drop the pin. The category is guessed from what the place is.">
            <GeoSearch onPick={pick} placeholder="Search a place, paste a maps link, or 42.42, 18.77…" initial={item?.address || ''} />
            <MapView className="sm" markers={marker} center={f.lat != null ? [f.lat, f.lng!] : trip.lat != null ? [trip.lat, trip.lng!] : undefined} zoom={f.lat != null ? 15 : 10} onClick={dropPin} fitKey={`${f.lat}:${f.lng}`} />
            <div className="small muted row" style={{ marginTop: 4, gap: 4, flexWrap: 'wrap' }}>{f.lat != null ? <><Icon name="pin" size={13} />{picked || f.address || `${f.lat.toFixed(5)}, ${f.lng!.toFixed(5)}`} <a href="#" onClick={e => { e.preventDefault(); setF(x => ({ ...x, lat: null, lng: null })); setPicked('') }}>clear</a></> : 'No location yet — the place will not show on the map.'}</div>
          </Field>
          <div className="two-col tight">
            <Field label="Day"><Select value={f.day_date || ''} onChange={v => set('day_date', v)} options={days.map((d, i) => ({ value: d, label: `Day ${i + 1} · ${fmtDate(d, false)}` }))} placeholder="Unscheduled (idea)" clearLabel="Unscheduled" /></Field>
            <div className="two-col tight">
              <Field label="Time"><input type="time" value={f.start_time || ''} onChange={e => set('start_time', e.target.value)} /></Field>
              <Field label="Duration (min)"><input type="number" min={0} step={15} value={f.duration_min || ''} onChange={e => set('duration_min', Number(e.target.value))} placeholder="90" /></Field>
            </div>
          </div>
          <Field group label="Status"><Segmented value={f.status || 'idea'} onChange={v => set('status', v as PlaceStatus)} options={Object.entries(PLACE_STATUS_LABEL).map(([v, l]) => ({ value: v, label: l }))} size="sm" /></Field>
          <div className="two-col tight">
            <Field label="Price / entry fee"><AmountInput amount={f.price ?? null} currency={f.currency || trip.currency} onAmount={m => set('price', m || 0)} onCurrency={c => set('currency', c)} /></Field>
            <Field group label="Rating"><div className="rating-pick">{RATINGS.map(r => <button type="button" key={r} className={(f.rating || 0) >= r ? 'on' : ''} onClick={() => set('rating', f.rating === r ? null : r)}>★</button>)}</div></Field>
          </div>
          <div className="two-col tight">
            <Field label="Link"><input type="url" value={f.link || ''} onChange={e => set('link', e.target.value)} placeholder="https://" /></Field>
            <Field label="Booking reference"><input value={f.booking_ref || ''} onChange={e => set('booking_ref', e.target.value)} /></Field>
          </div>
        </>}
        {tab === 'more' && <>
          <Field label="Address" help="Shown in lists and the calendar; the pin on the Details tab is what the map uses."><input value={f.address || ''} onChange={e => set('address', e.target.value)} /></Field>
          <div className="two-col tight">
            <Field label="Phone"><input value={f.phone || ''} onChange={e => set('phone', e.target.value)} /></Field>
            <Field label="Tags" help="Comma separated."><input value={f.tags || ''} onChange={e => set('tags', e.target.value)} placeholder="sunset, kids, rainy day" /></Field>
          </div>
          <Field label={<span className="row">Description <button type="button" className="btn ghost sm" onClick={wiki} disabled={wikiBusy || !f.name}>{wikiBusy ? 'Looking up…' : 'Fill from Wikipedia'}</button>{settings?.ai_configured && <button type="button" className="btn ghost sm" onClick={describe} disabled={aiBusy || !f.name}><Icon name="sparkles" size={14} />{aiBusy ? 'Writing…' : 'Describe + tips'}</button>}</span>}><textarea value={f.description || ''} onChange={e => set('description', e.target.value)} rows={4} /></Field>
          <Field label="Notes" help="Tips, opening hours, what to order."><textarea value={f.notes || ''} onChange={e => set('notes', e.target.value)} rows={3} /></Field>
          {item && <PhotoGallery tripId={trip.id} placeId={item.id} initial={item.photos || []} onChange={onSaved} />}
          {!item && <p className="muted small">Photos are looked up automatically once the place is saved (Wikipedia and Wikimedia Commons); you can add your own afterwards.</p>}
        </>}
        <div className="form-actions">
          {item && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}>Delete</button>}
          {item && <button type="button" className="btn ghost sm" title="Add this place again on another day, without typing it again" onClick={() => edit('place', null, reuse(f))}><Icon name="plus" size={13} />Visit again</button>}
          {item && f.lat != null && <><a className="btn ghost sm" href={gmaps(f.lat, f.lng!)} target="_blank" rel="noreferrer"><Icon name="external" size={13} />Google Maps</a><a className="btn ghost sm" href={appleMaps(f.lat, f.lng!)} target="_blank" rel="noreferrer"><Icon name="external" size={13} />Apple Maps</a></>}
          <span className="spacer" />
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className="btn primary" disabled={busy}>{item ? 'Save' : 'Add place'}</button>
        </div>
      </form>
      {del && <Confirm title={`Remove ${item?.name}?`} message="The place and its photo are deleted from this trip." onCancel={() => setDel(false)} onConfirm={remove} />}
    </Modal>
  )
}

/** What carries over when a place is added again: where and what it is, not when or whether it was visited. */
function reuse(p: Partial<Place>): Partial<Place> {
  return { name: p.name, category: p.category, description: p.description, address: p.address, lat: p.lat ?? null, lng: p.lng ?? null, link: p.link, phone: p.phone, rating: p.rating ?? null, tags: p.tags, notes: p.notes, price: p.price, currency: p.currency, duration_min: p.duration_min }
}

type SavedPlace = Place & { trip_name: string; visits: number; last_date: string }

/** "From your places": every place of every trip, so a beach or café you go back to is picked, not typed again. */
function SavedPlacePick({ tripId, onPick }: { tripId: number; onPick: (p: SavedPlace) => void }) {
  const { data } = useAsync(() => api.get<SavedPlace[]>(`${V1}/places/saved?trip=${tripId}`), [tripId])
  if (!data || data.length === 0) return null
  const options = data.map(p => ({ value: String(p.id), label: `${PLACE_ICONS[p.category] || '📍'} ${p.name}`, hint: [p.trip_id === tripId ? 'this trip' : p.trip_name, p.visits > 1 ? `${p.visits}×` : '', p.address].filter(Boolean).join(' · '), group: p.trip_id === tripId ? 'This trip' : 'Other trips' }))
  return (
    <Field label="Been there, or going back?" help="Pick a place from this or another trip to copy it — then just choose the day.">
      <Select value="" onChange={v => { const p = data.find(x => String(x.id) === v); if (p) onPick(p) }} options={options} placeholder="From your places…" ariaLabel="From your places" />
    </Field>
  )
}

/** The pictures of a place: look up more, upload, pick the cover, remove. */
function PhotoGallery({ tripId, placeId, initial, onChange }: { tripId: number; placeId: number; initial: PlacePhoto[]; onChange: () => void }) {
  const { toast } = useToast()
  const [photos, setPhotos] = useState<PlacePhoto[]>(initial)
  const [busy, setBusy] = useState('')
  const base = `${V1}/trips/${tripId}/places`
  const url = (p: PlacePhoto) => `${base}/photos/${p.id}`
  const find = async () => {
    setBusy('find')
    try { const r = await api.post<{ added: number; photos: PlacePhoto[] }>(`${base}/${placeId}/photos/find`); setPhotos(r.photos); toast(r.added ? `Added ${r.added} photo${r.added === 1 ? '' : 's'}` : 'Nothing new found on Wikipedia or Commons'); if (r.added) onChange() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const upload = async (files: FileList | null) => {
    if (!files?.length) return
    setBusy('upload')
    try { let list = photos; for (const f of Array.from(files)) list = await uploadForm<PlacePhoto[]>(`${base}/${placeId}/photos`, f); setPhotos(list); onChange() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const remove = async (p: PlacePhoto) => { try { await api.del(`${base}/photos/${p.id}`); setPhotos(x => x.filter(y => y.id !== p.id)); onChange() } catch (e) { toast((e as Error).message, 'err') } }
  const cover = async (p: PlacePhoto) => { try { setPhotos(await api.post<PlacePhoto[]>(`${base}/photos/${p.id}/cover`)); onChange() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Field group label={<span className="row">Photos <span className="muted small">{photos.length ? `· ${photos.length}` : ''}</span><span className="spacer" /><button type="button" className="btn ghost sm" onClick={find} disabled={!!busy}><Icon name="image" size={14} />{busy === 'find' ? 'Looking…' : 'Find photos'}</button><label className="btn ghost sm"><Icon name="upload" size={14} />{busy === 'upload' ? 'Uploading…' : 'Upload…'}<input type="file" accept="image/*" multiple hidden onChange={e => upload(e.target.files)} /></label></span>}>
      {photos.length === 0 ? <p className="muted small">No photos yet. “Find photos” looks on Wikipedia and Wikimedia Commons; or upload your own.</p> : (
        <div className="gallery">
          {photos.map((p, i) => (
            <figure key={p.id} className={`g-item ${i === 0 ? 'cover' : ''}`}>
              <a href={url(p)} target="_blank" rel="noreferrer"><img src={url(p)} alt="" loading="lazy" /></a>
              <figcaption>
                {i === 0 ? <span className="badge cat">cover</span> : <button type="button" className="icon-btn" title="Use as cover" aria-label="Use as cover" onClick={() => cover(p)}><Icon name="star" size={13} /></button>}
                <span className="credit" title={`${p.credit}${p.license ? ' · ' + p.license : ''}`}>{p.source_url ? <a href={p.source_url} target="_blank" rel="noreferrer">{p.credit || p.source}</a> : p.credit || (p.source === 'upload' ? 'uploaded' : p.source)}{p.license && <span className="muted"> · {p.license}</span>}</span>
                <button type="button" className="icon-btn" title="Remove" aria-label="Remove photo" onClick={() => remove(p)}><Icon name="x" size={13} /></button>
              </figcaption>
            </figure>
          ))}
        </div>
      )}
    </Field>
  )
}
