import { useState } from 'react'
import { api, uploadForm, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { GeoResult, Lodging, Transport, Trip } from '../../lib/types'
import { LODGING_ICONS, PLACE_ICONS, TRANSPORT_ICONS } from '../../lib/types'
import { appleMaps, cap, daysBetween, fmtDate, fmtDateTime, fmtKm, gmaps, todayISO } from '../../lib/format'
import { Card, Confirm, Empty, Field, Modal, useToast } from '../../components/ui'
import { Icon } from '../../components/icons'
import { Select, type SelectOption } from '../../components/Select'
import { AmountInput, Money } from '../../components/Money'
import { GeoSearch } from '../../components/GeoSearch'
import { LocationPicker, type PickedLocation } from '../../components/LocationPicker'
import { itineraryOrder, stayClock, stayTime, withClock } from '../../lib/format'
import { useTrip } from './ctx'

export function Stay() {
  const { ov, trip, canEdit, edit } = useTrip()
  const { settings } = useApp()
  const today = todayISO()
  const unit = settings?.distance_unit || 'km'
  return (
    <div className="two-col">
      <Card title="Where you sleep" actions={canEdit && <button type="button" className="btn primary sm" onClick={() => edit('lodging', null, {})}><Icon name="plus" size={14} />Stay</button>}>
        {ov.lodging.length === 0 ? <Empty icon="bed" title="No stays yet">Hotels, apartments, a friend's couch — with check-in dates they show up in the itinerary and the calendar export.</Empty> : (
          <div className="day-items">
            {ov.lodging.map(l => {
              const now = l.check_in.slice(0, 10) <= today && today < l.check_out.slice(0, 10)
              const nights = daysBetween(l.check_in.slice(0, 10), l.check_out.slice(0, 10))
              return (
                <div key={l.id} className="item" onClick={() => edit('lodging', l)}>
                  <span className="ico lodging">{LODGING_ICONS[l.type] || '🛏'}</span>
                  <span className="txt">
                    <span className="title">{l.name}{now && <span className="badge cat">current stay</span>}{l.paid && <span className="badge">paid</span>}</span>
                    <span className="sub wrap">{fmtDate(l.check_in, false)} {stayTime(l.check_in, 'in', settings)} → {fmtDate(l.check_out, false)} {stayTime(l.check_out, 'out', settings)}{nights > 0 && ` · ${nights} night${nights === 1 ? '' : 's'}`}{l.address && ` · ${l.address}`}</span>
                    <span className="sub wrap">{[l.reservation_number && `Reservation ${l.reservation_number}`, l.phone, l.price > 0 && <Money key="m" minor={l.price} code={l.currency || trip.currency} />].filter(Boolean).map((x, i) => <span key={i}>{i > 0 && ' · '}{x}</span>)}</span>
                  </span>
                  <span className="row" onClick={e => e.stopPropagation()}>{l.link && <a className="icon-btn" href={l.link} target="_blank" rel="noreferrer" title="Booking" aria-label="Booking"><Icon name="link" size={15} /></a>}{l.lat != null && <a className="icon-btn" href={gmaps(l.lat, l.lng!)} target="_blank" rel="noreferrer" title="Directions" aria-label="Directions"><Icon name="navigation" size={15} /></a>}</span>
                </div>
              )
            })}
          </div>
        )}
      </Card>
      <Card title="How you get there" actions={canEdit && <button type="button" className="btn primary sm" onClick={() => edit('transport', null, {})}><Icon name="plus" size={14} />Leg</button>}>
        {ov.transport.length === 0 ? <Empty icon="plane" title="No transport yet">Flights, trains, ferries and drives. Booking references and seats live here, and the calendar export carries them.</Empty> : (
          <div className="day-items">
            {ov.transport.map(t => (
              <div key={t.id} className="item" onClick={() => edit('transport', t)}>
                <span className="ico transport">{TRANSPORT_ICONS[t.type] || '➡️'}</span>
                <span className="txt">
                  <span className="title">{t.name || `${t.from_name} → ${t.to_name}`}{t.paid && <span className="badge">paid</span>}</span>
                  <span className="sub wrap">{t.depart_at && fmtDateTime(t.depart_at)}{t.arrive_at && ` → ${fmtDateTime(t.arrive_at, false)}`}{t.name && (t.from_name || t.to_name) && ` · ${t.from_name} → ${t.to_name}`}</span>
                  <span className="sub wrap">{[t.carrier, t.number, t.booking_ref && `ref ${t.booking_ref}`, t.seat && `seat ${t.seat}`, t.distance_km > 0 && fmtKm(t.distance_km, unit), t.price > 0 && <Money key="m" minor={t.price} code={t.currency || trip.currency} />].filter(Boolean).map((x, i) => <span key={i}>{i > 0 && ' · '}{x}</span>)}</span>
                </span>
                {t.link && <a className="icon-btn" href={t.link} target="_blank" rel="noreferrer" title="Booking" aria-label="Booking" onClick={e => e.stopPropagation()}><Icon name="link" size={15} /></a>}
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}

export function LodgingModal({ trip, item, defaults = {}, onClose, onSaved }: { trip: Trip; item: Lodging | null; defaults?: Record<string, unknown>; onClose: () => void; onSaved: () => void }) {
  const { meta } = useApp()
  const { toast } = useToast()
  const { settings } = useApp()
  const defIn = settings?.checkin_time || '15:00', defOut = settings?.checkout_time || '11:00'
  const [f, setF] = useState<Partial<Lodging>>(item ? { ...item, check_in: item.check_in.slice(0, 10), check_out: item.check_out.slice(0, 10) } : { name: '', type: 'hotel', address: '', check_in: trip.start_date, check_out: trip.end_date, reservation_number: '', link: '', phone: '', price: 0, currency: trip.currency, paid: false, notes: '', lat: null, lng: null, rating: null, ...defaults })
  // times live next to the dates: "2026-07-05T15:00"; a stay saved without one shows the default
  const [inTime, setInTime] = useState(stayClock(item?.check_in) || defIn)
  const [outTime, setOutTime] = useState(stayClock(item?.check_out) || defOut)
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const set = <K extends keyof Lodging>(k: K, v: Lodging[K]) => setF(x => ({ ...x, [k]: v }))
  const base = `${V1}/trips/${trip.id}/lodging`
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    const body = { ...f, check_in: withClock(f.check_in, inTime), check_out: withClock(f.check_out, outTime) }
    try { if (item) await api.put(`${base}/${item.id}`, body); else await api.post(base, body); toast(item ? 'Stay saved' : 'Stay added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => { try { await api.del(`${base}/${item!.id}`); toast('Stay removed'); onSaved() } catch (e) { toast((e as Error).message, 'err') } }
  const locate = (p: PickedLocation) => setF(x => {
    const r = p.result
    if (p.via === 'search' && r) return { ...x, lat: r.lat, lng: r.lng, name: x.name || r.name, address: r.display, link: x.link || r.website || '', phone: x.phone || r.phone || '' }
    // a dropped pin or typed coordinates: keep the address someone typed, fill it only when empty
    return { ...x, lat: p.lat, lng: p.lng, address: x.address || p.address || '' }
  })
  const upload = async (file: File) => { if (!item) return; try { await uploadForm(`${base}/${item.id}/image`, file); toast('Image saved'); onSaved() } catch (e) { toast((e as Error).message, 'err') } }
  const nights = daysBetween(f.check_in || '', f.check_out || '')
  return (
    <Modal title={item ? 'Edit stay' : 'Add a stay'} onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Name"><input value={f.name || ''} onChange={e => set('name', e.target.value)} required autoFocus={!item} /></Field>
          <Field label="Type"><Select value={f.type || 'hotel'} onChange={v => set('type', v)} options={(meta?.lodging_types || []).map(t => ({ value: t, label: `${LODGING_ICONS[t] || ''} ${cap(t)}` }))} /></Field>
        </div>
        <Field label="Location" help="Search a hotel, or for a rental without an address paste the GPS coordinates or the maps link from the booking — or click the map.">
          <LocationPicker lat={f.lat ?? null} lng={f.lng ?? null} onChange={locate} icon={LODGING_ICONS[f.type || 'hotel'] || '🛏'} center={trip.lat != null ? [trip.lat, trip.lng!] : undefined} searchPlaceholder="Search hotel or address, or paste a maps link…" />
        </Field>
        <Field label="Address" help="Optional — directions use the pin."><input value={f.address || ''} onChange={e => set('address', e.target.value)} /></Field>
        <div className="two-col tight">
          <Field label="Check-in"><div className="row" style={{ gap: 6 }}><input type="date" value={f.check_in || ''} onChange={e => set('check_in', e.target.value)} /><input type="time" value={inTime} onChange={e => setInTime(e.target.value)} style={{ maxWidth: 120 }} aria-label="Check-in time" /></div></Field>
          <Field label="Check-out" help={nights > 0 ? `${nights} night${nights === 1 ? '' : 's'}` : undefined}><div className="row" style={{ gap: 6 }}><input type="date" value={f.check_out || ''} onChange={e => set('check_out', e.target.value)} /><input type="time" value={outTime} onChange={e => setOutTime(e.target.value)} style={{ maxWidth: 120 }} aria-label="Check-out time" /></div></Field>
        </div>
        <div className="two-col tight">
          <Field label="Total price" help="Becomes a planned cost in the budget."><AmountInput amount={f.price ?? null} currency={f.currency || trip.currency} onAmount={m => set('price', m || 0)} onCurrency={c => set('currency', c)} /></Field>
          <Field label="Reservation number"><input value={f.reservation_number || ''} onChange={e => set('reservation_number', e.target.value)} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Booking link"><input type="url" value={f.link || ''} onChange={e => set('link', e.target.value)} placeholder="https://" /></Field>
          <Field label="Phone"><input value={f.phone || ''} onChange={e => set('phone', e.target.value)} /></Field>
        </div>
        <label className="row"><input type="checkbox" checked={!!f.paid} onChange={e => set('paid', e.target.checked)} style={{ width: 'auto' }} /> Paid already</label>
        <Field label="Notes" help="Door codes, parking, breakfast times."><textarea value={f.notes || ''} onChange={e => set('notes', e.target.value)} rows={3} /></Field>
        {item && <Field label="Photo"><div className="row">{item.has_image && <img src={`${V1}/trips/${trip.id}/lodging/${item.id}/image?v=${item.updated_at}`} alt="" style={{ height: 60, borderRadius: 8 }} />}<label className="btn sm"><Icon name="upload" size={14} />Upload image…<input type="file" accept="image/*" hidden onChange={e => e.target.files?.[0] && upload(e.target.files[0])} /></label></div></Field>}
        <div className="form-actions">
          {item && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}>Delete</button>}
          {item && f.lat != null && <><a className="btn ghost sm" href={gmaps(f.lat, f.lng!)} target="_blank" rel="noreferrer"><Icon name="external" size={13} />Google Maps</a><a className="btn ghost sm" href={appleMaps(f.lat, f.lng!)} target="_blank" rel="noreferrer"><Icon name="external" size={13} />Apple Maps</a></>}
          <span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{item ? 'Save' : 'Add stay'}</button>
        </div>
      </form>
      {del && <Confirm title={`Remove ${item?.name}?`} message="The stay and its planned cost are deleted." onCancel={() => setDel(false)} onConfirm={remove} />}
    </Modal>
  )
}

export function TransportModal({ trip, item, defaults = {}, onClose, onSaved }: { trip: Trip; item: Transport | null; defaults?: Record<string, unknown>; onClose: () => void; onSaved: () => void }) {
  const { meta } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<Partial<Transport>>(item ? { ...item } : { type: 'flight', name: '', carrier: '', number: '', from_name: '', to_name: '', depart_at: trip.start_date ? trip.start_date + 'T08:00' : '', arrive_at: '', booking_ref: '', seat: '', link: '', price: 0, currency: trip.currency, paid: false, distance_km: 0, notes: '', from_lat: null, from_lng: null, to_lat: null, to_lng: null, ...defaults })
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const [touchedDepart, setTouchedDepart] = useState(false)
  const set = <K extends keyof Transport>(k: K, v: Transport[K]) => setF(x => ({ ...x, [k]: v }))
  const base = `${V1}/trips/${trip.id}/transport`
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (item) await api.put(`${base}/${item.id}`, f); else await api.post(base, f); toast(item ? 'Leg saved' : 'Leg added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => { try { await api.del(`${base}/${item!.id}`); toast('Leg removed'); onSaved() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Modal title={item ? 'Edit transport' : 'Add transport'} onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Type"><Select value={f.type || 'flight'} onChange={v => set('type', v)} options={(meta?.transport_types || []).map(t => ({ value: t, label: `${TRANSPORT_ICONS[t] || ''} ${cap(t)}` }))} /></Field>
          <Field label="Name" help="Optional — defaults to from → to."><input value={f.name || ''} onChange={e => set('name', e.target.value)} placeholder="Flight to Lisbon" autoFocus={!item} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="From" help="A stay or place of this trip, or search anywhere."><Endpoint name={f.from_name || ''} lat={f.from_lat ?? null} lng={f.from_lng ?? null} onPick={(e, day) => setF(x => ({ ...x, from_name: e.name, from_lat: e.lat, from_lng: e.lng, depart_at: !item && day && !touchedDepart ? day + 'T' + ((x.depart_at || '').slice(11, 16) || '08:00') : x.depart_at }))} /></Field>
          <Field label="To"><Endpoint name={f.to_name || ''} lat={f.to_lat ?? null} lng={f.to_lng ?? null} onPick={e => setF(x => ({ ...x, to_name: e.name, to_lat: e.lat, to_lng: e.lng }))} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Departs"><input type="datetime-local" value={f.depart_at || ''} onChange={e => { setTouchedDepart(true); set('depart_at', e.target.value) }} /></Field>
          <Field label="Arrives"><input type="datetime-local" value={f.arrive_at || ''} onChange={e => set('arrive_at', e.target.value)} /></Field>
        </div>
        <div className="three-col tight">
          <Field label="Carrier"><input value={f.carrier || ''} onChange={e => set('carrier', e.target.value)} placeholder="Ryanair, ÖBB…" /></Field>
          <Field label="Flight / train no."><input value={f.number || ''} onChange={e => set('number', e.target.value)} /></Field>
          <Field label="Seat"><input value={f.seat || ''} onChange={e => set('seat', e.target.value)} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Price" help="Becomes a planned cost in the budget."><AmountInput amount={f.price ?? null} currency={f.currency || trip.currency} onAmount={m => set('price', m || 0)} onCurrency={c => set('currency', c)} /></Field>
          <Field label="Booking reference"><input value={f.booking_ref || ''} onChange={e => set('booking_ref', e.target.value)} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Link"><input type="url" value={f.link || ''} onChange={e => set('link', e.target.value)} placeholder="https://" /></Field>
          <Field label="Distance (km)" help="Filled from the coordinates when left empty."><input type="number" min={0} step="0.1" value={f.distance_km || ''} onChange={e => set('distance_km', Number(e.target.value))} /></Field>
        </div>
        <label className="row"><input type="checkbox" checked={!!f.paid} onChange={e => set('paid', e.target.checked)} style={{ width: 'auto' }} /> Paid already</label>
        <Field label="Notes"><textarea value={f.notes || ''} onChange={e => set('notes', e.target.value)} rows={2} /></Field>
        <div className="form-actions">
          {item && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}>Delete</button>}
          <span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{item ? 'Save' : 'Add leg'}</button>
        </div>
      </form>
      {del && <Confirm title="Remove this leg?" message="The leg and its planned cost are deleted." onCancel={() => setDel(false)} onConfirm={remove} />}
    </Modal>
  )
}

interface EndpointValue { name: string; lat: number | null; lng: number | null }

/**
 * One end of a leg: pick a stay or place already in the trip (the car from the
 * apartment to the trailhead) or search anywhere (an airport, a station).
 * Picking a scheduled place also reports its day so a new leg can depart then.
 */
function Endpoint({ name, lat, lng, onPick }: EndpointValue & { onPick: (e: EndpointValue, day?: string) => void }) {
  const { ov } = useTrip()
  const options: SelectOption[] = [
    ...ov.lodging.filter(l => l.lat != null).map(l => ({ value: `l${l.id}`, label: `${LODGING_ICONS[l.type] || '🛏'} ${l.name}`, hint: fmtDate(l.check_in, false), group: 'Stays' })),
    ...[...ov.places].filter(p => p.lat != null).sort(itineraryOrder).map(p => ({ value: `p${p.id}`, label: `${PLACE_ICONS[p.category] || '📍'} ${p.name}`, hint: p.day_date ? fmtDate(p.day_date, false) : 'idea', group: p.day_date ? 'Places' : 'Ideas' })),
  ]
  const same = (a: number | null, b: number | null) => a != null && b != null && Math.abs(a - b) < 1e-6
  const current = ov.lodging.find(l => l.name === name && same(l.lat, lat) && same(l.lng, lng)) ? `l${ov.lodging.find(l => l.name === name && same(l.lat, lat) && same(l.lng, lng))!.id}`
    : ov.places.find(p => p.name === name && same(p.lat, lat) && same(p.lng, lng)) ? `p${ov.places.find(p => p.name === name && same(p.lat, lat) && same(p.lng, lng))!.id}` : ''
  const pick = (v: string) => {
    if (!v) { onPick({ name: '', lat: null, lng: null }); return }
    const id = Number(v.slice(1))
    if (v[0] === 'l') { const l = ov.lodging.find(x => x.id === id); if (l) onPick({ name: l.name, lat: l.lat, lng: l.lng }, l.check_out.slice(0, 10) || undefined) }
    else { const p = ov.places.find(x => x.id === id); if (p) onPick({ name: p.name, lat: p.lat, lng: p.lng }, p.day_date || undefined) }
  }
  return (
    <div className="grid" style={{ gap: 6 }}>
      {options.length > 0 && <Select value={current} onChange={pick} options={options} placeholder="From this trip…" clearLabel="Clear" ariaLabel="A stay or place of this trip" />}
      <GeoSearch key={name} initial={current ? '' : name} onPick={r => onPick({ name: r.name, lat: r.lat, lng: r.lng })} placeholder={current ? 'or somewhere else…' : 'Airport, station, city…'} />
      {lat != null && !current && <div className="small muted">{name || 'Pinned'} · {lat.toFixed(4)}, {lng!.toFixed(4)}</div>}
    </div>
  )
}
