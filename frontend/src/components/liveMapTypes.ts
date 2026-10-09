import type { BusLocation } from '../services/live'
import type { Stop } from '../services/types'

export type BusState = 'moving' | 'stopped' | 'offline'

export const busStateColors: Record<BusState, string> = {
  moving: '#52c41a',
  stopped: '#faad14',
  offline: '#8c8c8c',
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
  /** Stops of the selected bus's route, drawn in order. */
  routeStops?: Stop[]
  height?: number
}
