import { useEffect, useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { LiveSocket, type BusLocation } from '../services/live'
import type { TripProgress, TripStatus } from '../services/types'

export interface LiveState {
  /** Latest position per trip in progress. */
  buses: Record<string, BusLocation>
  /** Stop statuses and ETAs per trip in progress. */
  progress: Record<string, TripProgress>
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
            setState((s) => ({ ...s, buses: { ...s.buses, [m.trip_id]: m.location } }))
            break
          case 'progress':
            setState((s) => ({ ...s, progress: { ...s.progress, [m.trip_id]: m.progress } }))
            break
          case 'trip_status':
            setState((s) => {
              const buses = { ...s.buses }
              const progress = { ...s.progress }
              if (m.status === 'completed' || m.status === 'cancelled') {
                delete buses[m.trip_id]
                delete progress[m.trip_id]
              }
              return {
                ...s,
                buses,
                progress,
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
