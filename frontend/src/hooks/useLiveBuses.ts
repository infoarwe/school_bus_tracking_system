import { useEffect, useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { LiveSocket, type BusLocation } from '../services/live'
import type { TripProgress, TripStatus } from '../services/types'
import { bearing, haversine } from '../utils/geo'

/** A point of a bus's path received live (lat/lng plus fix time in ms). */
export interface TrailPoint {
  lat: number
  lng: number
  t: number
}

/** Live trail kept per trip; older points come from the stored track. */
const MAX_TRAIL = 2000

export interface LiveState {
  /** Latest position per trip in progress. */
  buses: Record<string, BusLocation>
  /** Stop statuses and ETAs per trip in progress. */
  progress: Record<string, TripProgress>
  /** Positions received since the page opened, per trip, oldest first. */
  trails: Record<string, TrailPoint[]>
  connected: boolean
  /** Bumped on every trip status change, so callers can refetch the trip list. */
  statusVersion: number
  lastStatus: { tripId: string; status: TripStatus } | null
}

/** Live positions of every bus of the school, over the WebSocket. */
export function useLiveBuses(schoolId: string): LiveState {
  const { hasRole } = useAuth()
  const isSuper = hasRole('super_admin')
  const [state, setState] = useState<LiveState>({
    buses: {},
    progress: {},
    trails: {},
    connected: false,
    statusVersion: 0,
    lastStatus: null,
  })

  useEffect(() => {
    const socket = new LiveSocket(
      isSuper ? schoolId : null,
      (m) => {
        switch (m.type) {
          case 'subscribed':
            if (m.channel === 'school') {
              const buses: Record<string, BusLocation> = {}
              for (const b of m.snapshot ?? []) buses[b.trip_id] = b
              setState((s) => ({ ...s, buses }))
            }
            break
          case 'location':
          case 'bus_stale':
            setState((s) => {
              const loc = withHeading(m.location, s.buses[m.trip_id])
              const trail = s.trails[m.trip_id] ?? []
              const t = new Date(loc.recorded_at).getTime()
              const fresh =
                m.type === 'location' && !(trail.length && trail[trail.length - 1].t >= t)
              return {
                ...s,
                buses: { ...s.buses, [m.trip_id]: loc },
                trails: fresh
                  ? {
                      ...s.trails,
                      [m.trip_id]: [
                        ...trail.slice(-(MAX_TRAIL - 1)),
                        { lat: loc.latitude, lng: loc.longitude, t },
                      ],
                    }
                  : s.trails,
              }
            })
            break
          case 'progress':
            setState((s) => ({ ...s, progress: { ...s.progress, [m.trip_id]: m.progress } }))
            break
          case 'trip_status':
            setState((s) => {
              const buses = { ...s.buses }
              const progress = { ...s.progress }
              const trails = { ...s.trails }
              if (m.status === 'completed' || m.status === 'cancelled') {
                delete buses[m.trip_id]
                delete progress[m.trip_id]
                delete trails[m.trip_id]
              }
              return {
                ...s,
                buses,
                progress,
                trails,
                statusVersion: s.statusVersion + 1,
                lastStatus: { tripId: m.trip_id, status: m.status },
              }
            })
            break
        }
      },
      (connected) => setState((s) => ({ ...s, connected })),
    )
    socket.subscribeSchool()
    return () => socket.close()
  }, [schoolId, isSuper])

  return state
}

/**
 * Fills in the direction of travel when the phone sent none: the bearing from
 * the previous position if the bus moved at least 5 m, else the previous heading.
 */
function withHeading(loc: BusLocation, prev: BusLocation | undefined): BusLocation {
  if (loc.heading != null || !prev) return loc
  const a = { lat: prev.latitude, lng: prev.longitude }
  const b = { lat: loc.latitude, lng: loc.longitude }
  return { ...loc, heading: haversine(a, b) >= 5 ? bearing(a, b) : prev.heading }
}
