import { useEffect, useMemo, useState } from 'react'
import { App } from 'antd'
import { tripsApi } from '../services/trips'
import type { Trip, TripDetail } from '../services/types'
import { addDays } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'

export const AHEAD_DAYS = 7
/** Look-back periods offered on the detail pages, in days. */
export const PERIODS = [7, 30, 90]

export type TripOwner = { driver_id: string } | { bus_id: string }

/** Every trip of one driver or bus in the date range (the list endpoint pages at 100). */
async function loadTrips(schoolId: string, owner: TripOwner, from: string, to: string) {
  const all: Trip[] = []
  for (let page = 1; page <= 5; page++) {
    const res = await tripsApi.list(schoolId, { ...owner, from, to, page, page_size: 100 })
    all.push(...res.data)
    if (all.length >= res.meta.total || res.data.length === 0) break
  }
  return all
}

/**
 * One driver's or bus's trips for the last `period` days and the next week, today's trips
 * with their stops (for the schedule), and the numbers shown on their detail page.
 */
export function useEntityTrips(schoolId: string, owner: TripOwner, period: number, today: string) {
  const { message } = App.useApp()
  const ownerKey = JSON.stringify(owner)
  // Results are kept with the request they answer, so a stale one reads as "loading".
  const [tripsState, setTripsState] = useState<{ key: string; trips: Trip[] } | null>(null)
  const [detailsState, setDetailsState] = useState<{ ids: string; details: TripDetail[] } | null>(
    null,
  )

  const tripsKey = `${ownerKey}|${today}|${period}`
  const trips = tripsState?.key === tripsKey ? tripsState.trips : null
  useEffect(() => {
    let cancelled = false
    const o = JSON.parse(ownerKey) as TripOwner
    loadTrips(schoolId, o, addDays(today, -period), addDays(today, AHEAD_DAYS))
      .then((t) => !cancelled && setTripsState({ key: tripsKey, trips: t }))
      .catch((e: unknown) => {
        if (cancelled) return
        setTripsState({ key: tripsKey, trips: [] })
        message.error(errorMessage(e))
      })
    return () => {
      cancelled = true
    }
  }, [schoolId, ownerKey, today, period, tripsKey, message])

  const todayIds =
    trips === null
      ? null
      : trips
          .filter((t) => t.trip_date === today && t.status !== 'cancelled')
          .map((t) => t.id)
          .join(',')
  const todayDetails =
    todayIds === '' ? [] : detailsState?.ids === todayIds ? detailsState.details : null
  useEffect(() => {
    if (!todayIds) return
    let cancelled = false
    Promise.all(todayIds.split(',').map((id) => tripsApi.get(schoolId, id)))
      .then((d) => !cancelled && setDetailsState({ ids: todayIds, details: d }))
      .catch(() => !cancelled && setDetailsState({ ids: todayIds, details: [] }))
    return () => {
      cancelled = true
    }
  }, [schoolId, todayIds])

  const stats = useMemo(() => {
    const all = trips ?? []
    const isUpcoming = (t: Trip) =>
      t.trip_date >= today && t.status !== 'completed' && t.status !== 'cancelled'
    const upcoming = all
      .filter(isUpcoming)
      .sort(
        (a, b) => a.trip_date.localeCompare(b.trip_date) || b.trip_type.localeCompare(a.trip_type),
      )
    const past = all.filter((t) => t.trip_date <= today && !isUpcoming(t))
    const completed = past.filter((t) => t.status === 'completed')
    const cancelled = past.filter((t) => t.status === 'cancelled')
    const durations = completed
      .filter((t) => t.started_at && t.ended_at)
      .map((t) => (new Date(t.ended_at!).getTime() - new Date(t.started_at!).getTime()) / 60000)
    const decided = completed.length + cancelled.length
    const running = all.find((t) => t.status === 'started')
    return {
      upcoming,
      past,
      completed,
      cancelled,
      avgMinutes: durations.length
        ? Math.round(durations.reduce((a, b) => a + b, 0) / durations.length)
        : null,
      completionRate: decided ? Math.round((completed.length / decided) * 100) : null,
      selfAssigned: past.filter((t) => t.assigned_by === 'driver').length,
      running,
      /** The trip running now, else today's next one. */
      current: running ?? upcoming.find((t) => t.trip_date === today),
    }
  }, [trips, today])

  return { trips, todayDetails, loading: trips === null, ...stats }
}
