import { createContext, useContext, type Dispatch, type SetStateAction } from 'react'
import type { DayWeather, Overview, Photo, Route, TimelineDay, Trip } from '../../lib/types'

export type EditKind = 'place' | 'lodging' | 'transport' | 'expense' | 'journal' | 'trip'
export interface TripState {
  ov: Overview
  trip: Trip
  canEdit: boolean
  isOwner: boolean
  reload: () => Promise<void>
  patch: (p: Partial<Overview>) => void
  weather: DayWeather[]
  route: Route | null
  timeline: TimelineDay[]
  /** the trip's photos without the ones the viewer hid */
  photos: Photo[]
  /** every photo from the trip's days, hidden ones marked */
  allPhotos: Photo[]
  setAllPhotos: Dispatch<SetStateAction<Photo[]>>
  /** the first photo load has answered */
  photosReady: boolean
  extErrors: Record<string, string>
  refreshExternal: () => Promise<void>
  edit: (kind: EditKind, item?: unknown, defaults?: Record<string, unknown>) => void
  go: (tab: string) => void
}
export const TripCtx = createContext<TripState>(null!)
export const useTrip = () => useContext(TripCtx)
