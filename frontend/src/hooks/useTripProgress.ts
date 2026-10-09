import { useEffect, useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { LiveSocket } from '../services/live'
import { tripsApi } from '../services/trips'
import type { TripProgressResponse } from '../services/types'

/**
 * One trip's stop-by-stop progress: loaded once, then kept live over the WebSocket
 * while the trip is in progress. Stop events are refetched when a stop changes.
 */
export function useTripProgress(schoolId: string, tripId: string): TripProgressResponse | null {
  const { hasRole } = useAuth()
  const isSuper = hasRole('super_admin')
  const [data, setData] = useState<TripProgressResponse | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = () =>
      tripsApi
        .progress(schoolId, tripId)
        .then((d) => !cancelled && setData(d))
        .catch(() => {})
    void load()

    const socket = new LiveSocket(
      isSuper ? schoolId : null,
      (m) => {
        if (!('trip_id' in m) || m.trip_id !== tripId) return
        if (m.type === 'progress') setData((d) => (d ? { ...d, progress: m.progress } : d))
        if (m.type === 'stop_status' || m.type === 'trip_status') void load()
      },
      () => {},
    )
    socket.subscribeTrip(tripId)
    return () => {
      cancelled = true
      socket.close()
    }
  }, [schoolId, tripId, isSuper])

  return data
}
