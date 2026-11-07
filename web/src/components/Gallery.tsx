import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { photoKey, photoThumb } from '../lib/api'
import type { Photo } from '../lib/types'
import { fmtDate, fmtISOTime, weekday } from '../lib/format'
import { Icon } from './icons'

/**
 * Full-screen photo viewer, like a phone's gallery: ← → keys, tap the left or
 * right part of the photo, swipe, Esc closes; a strip of thumbnails below.
 */
export function Gallery({ photos, index, onIndex, onClose, actions, notice }: {
  photos: Photo[]; index: number; onIndex: (i: number) => void; onClose: () => void
  /** buttons for the photo shown (top right) */
  actions?: (p: Photo) => React.ReactNode
  /** a line shown over the photo (e.g. "Added Colosseum · Edit") */
  notice?: React.ReactNode
}) {
  const p = photos[index]
  const [loaded, setLoaded] = useState('')
  const [chrome, setChrome] = useState(true)
  const strip = useRef<HTMLDivElement>(null)
  const swipe = useRef<{ x: number; y: number; t: number } | null>(null)
  const go = (n: number) => { const i = index + n; if (i >= 0 && i < photos.length) onIndex(i) }
  const goRef = useRef(go); goRef.current = go
  const closeRef = useRef(onClose); closeRef.current = onClose

  useEffect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const h = (e: KeyboardEvent) => {
      if (document.querySelector('.modal-backdrop')) return // a dialog on top has the keys
      if (e.key === 'ArrowRight') { e.preventDefault(); goRef.current(1) }
      else if (e.key === 'ArrowLeft') { e.preventDefault(); goRef.current(-1) }
      else if (e.key === 'Escape') { e.preventDefault(); closeRef.current() }
    }
    window.addEventListener('keydown', h)
    return () => { window.removeEventListener('keydown', h); document.body.style.overflow = prev }
  }, [])
  // the neighbours load ahead; the strip keeps the current one in view
  useEffect(() => {
    for (const n of [index + 1, index - 1, index + 2]) if (photos[n]) new Image().src = photoThumb(photos[n], 'preview')
    strip.current?.querySelector<HTMLElement>(`[data-i="${index}"]`)?.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' })
  }, [index, photos])
  if (!p) return null
  const big = photoThumb(p, 'preview')

  const onDown = (e: React.PointerEvent) => { swipe.current = { x: e.clientX, y: e.clientY, t: Date.now() } }
  const onUp = (e: React.PointerEvent) => {
    const s = swipe.current; swipe.current = null
    if (!s) return
    const dx = e.clientX - s.x, dy = e.clientY - s.y
    if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.5) { go(dx < 0 ? 1 : -1); return }
    if (dy > 90 && Math.abs(dy) > Math.abs(dx) * 1.5) { onClose(); return } // swipe down closes
    if (Math.abs(dx) < 8 && Math.abs(dy) < 8) {
      // a tap: left third back, right third forward, middle shows / hides the bars
      const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
      const x = (e.clientX - r.left) / r.width
      if (x < 0.3) go(-1)
      else if (x > 0.7) go(1)
      else setChrome(c => !c)
    }
  }

  return createPortal(
    <div className={`photo-viewer ${chrome ? '' : 'bare'}`} role="dialog" aria-modal="true" aria-label="Photo viewer">
      <div className="gal-top">
        <button type="button" className="gal-btn" onClick={onClose} aria-label="Close (Esc)"><Icon name="x" size={20} /></button>
        <div className="gal-title">
          <b>{weekday(p.taken_at.slice(0, 10), true)}, {fmtDate(p.taken_at.slice(0, 10))} · {fmtISOTime(p.taken_at)}</b>
          <span>{[p.city, p.country, p.device].filter(Boolean).join(' · ')}{p.hidden ? ' · hidden from the trip' : ''}</span>
        </div>
        <span className="gal-count">{index + 1} / {photos.length}</span>
        <div className="gal-actions">{actions?.(p)}</div>
      </div>
      <div className="gal-stage" onPointerDown={onDown} onPointerUp={onUp} onPointerCancel={() => { swipe.current = null }}>
        {loaded !== big && <img key={photoKey(p) + 's'} className="gal-img low" src={photoThumb(p)} alt="" draggable={false} />}
        <img key={photoKey(p)} className={`gal-img ${loaded === big ? 'in' : ''}`} src={big} alt={p.file_name} draggable={false} onLoad={() => setLoaded(big)} />
        {p.type === 'VIDEO' && <span className="gal-video"><Icon name="play" size={14} />Video {p.duration?.slice(3, 8)} · open it in the photo app to play</span>}
        {index > 0 && <button type="button" className="gal-nav prev" onPointerDown={e => e.stopPropagation()} onPointerUp={e => e.stopPropagation()} onClick={() => go(-1)} aria-label="Previous (←)"><Icon name="chevron-left" size={30} /></button>}
        {index < photos.length - 1 && <button type="button" className="gal-nav next" onPointerDown={e => e.stopPropagation()} onPointerUp={e => e.stopPropagation()} onClick={() => go(1)} aria-label="Next (→)"><Icon name="chevron-right" size={30} /></button>}
        {notice && <div className="gal-notice" onPointerDown={e => e.stopPropagation()} onPointerUp={e => e.stopPropagation()}>{notice}</div>}
      </div>
      <div className="gal-strip" ref={strip}>
        {photos.map((x, i) => (
          <button type="button" key={photoKey(x)} data-i={i} className={i === index ? 'on' : ''} onClick={() => onIndex(i)} aria-label={`Photo ${i + 1}`}>
            <img src={photoThumb(x)} alt="" loading="lazy" draggable={false} />
          </button>
        ))}
      </div>
    </div>, document.body)
}
