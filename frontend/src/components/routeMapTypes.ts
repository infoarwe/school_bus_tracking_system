import type { Stop } from '../services/types'

export interface PickedPoint {
  latitude: number
  longitude: number
  radius: number
}

export interface RouteMapProps {
  stops: Stop[]
  highlightId?: string | null
  /** When set, clicking the map reports the location (stop form). */
  onPick?: (lat: number, lng: number) => void
  picked?: PickedPoint | null
  height?: number
  /** GPS trail to draw (trip replay). The map fits to it instead of the stops. */
  track?: { lat: number; lng: number }[]
  /** Bus position on the trail (trip replay). */
  bus?: { lat: number; lng: number } | null
}

/** Centre of India, used before there is anything to show. */
export const DEFAULT_CENTER = { lat: 20.59, lng: 78.96 }
