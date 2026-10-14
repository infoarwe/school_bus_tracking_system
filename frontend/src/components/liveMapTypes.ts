import type { ReactNode } from 'react'
import type { BusLocation } from '../services/live'
import type { Stop, StopProgress } from '../services/types'
import type { LatLng } from '../utils/geo'

export type BusState = 'moving' | 'stopped' | 'offline'

export const busStateColors: Record<BusState, string> = {
  moving: '#16a34a',
  stopped: '#f59e0b',
  offline: '#8c8c8c',
}

/** Map line colours: the planned route, the travelled path, and where it left the route. */
export const pathColors = {
  planned: '#1d4ed8',
  travelled: '#16a34a',
  off: '#dc2626',
}

/** Moving above ~4 km/h; offline when the server marked the bus stale. */
export function busState(b: BusLocation): BusState {
  if (b.stale) return 'offline'
  return (b.speed_mps ?? 0) > 1 ? 'moving' : 'stopped'
}

export interface LiveMapProps {
  buses: BusLocation[]
  selectedId: string | null
  onSelect: (tripId: string) => void
  /** Stops of the selected bus's route, drawn in order (used until live progress arrives). */
  routeStops?: Stop[]
  /** Live stop statuses of the selected trip, in travel order (preferred over routeStops). */
  progressStops?: StopProgress[]
  /** Planned road path of the selected trip; empty hides it. */
  plannedPath?: LatLng[]
  /** Where the selected bus actually went, split into on-route / off-route runs. */
  travelled?: { off: boolean; path: LatLng[] }[]
  /** Contents of the info window over the selected bus. */
  renderInfo?: (bus: BusLocation) => ReactNode
  /** Width covered by a panel floating over the map's left side, kept clear when fitting a route. */
  insetLeft?: number
  height?: number
}
