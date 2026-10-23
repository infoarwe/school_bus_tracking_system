import { api, getData, getPage, send } from './api'
import type {
  BulkTripsInput,
  BulkUnassignFilter,
  BulkUnassignPreviewResult,
  BulkUnassignResult,
  CopyTripsResult,
  PlannedPath,
  RouteSnapshot,
  Trip,
  TripCalendarDay,
  TripDetail,
  TripInput,
  TripListParams,
  TripProgressResponse,
  TripType,
} from './types'

const base = (schoolId: string) => `/api/v1/schools/${schoolId}/trips`

export const tripsApi = {
  list: (schoolId: string, params: TripListParams) => getPage<Trip>(base(schoolId), params),
  get: (schoolId: string, id: string) => getData<TripDetail>(`${base(schoolId)}/${id}`),
  progress: (schoolId: string, id: string) =>
    getData<TripProgressResponse>(`${base(schoolId)}/${id}/progress`),
  plannedPath: (schoolId: string, id: string) =>
    getData<PlannedPath>(`${base(schoolId)}/${id}/planned-path`),
  routeSnapshot: (schoolId: string, id: string) =>
    getData<RouteSnapshot>(`${base(schoolId)}/${id}/route-snapshot`),
  makeRouteSnapshot: (schoolId: string, id: string) =>
    send<RouteSnapshot>('post', `${base(schoolId)}/${id}/route-snapshot`),
  /** The PNG, fetched with the access token (an <img src> cannot send it). */
  routeSnapshotImage: (schoolId: string, id: string) =>
    api
      .get<Blob>(`${base(schoolId)}/${id}/route-snapshot/image`, { responseType: 'blob' })
      .then((res) => res.data),
  create: (schoolId: string, input: TripInput) => send<Trip>('post', base(schoolId), input),
  update: (schoolId: string, id: string, input: TripInput) =>
    send<Trip>('put', `${base(schoolId)}/${id}`, input),
  cancel: (schoolId: string, id: string, reason: string) =>
    send<Trip>('post', `${base(schoolId)}/${id}/cancel`, { reason }),
  override: (
    schoolId: string,
    id: string,
    status: 'started' | 'completed' | 'cancelled',
    reason: string,
  ) => send<Trip>('post', `${base(schoolId)}/${id}/override`, { status, reason }),
  copy: (
    schoolId: string,
    body: { from_date: string; to_dates: string[]; trip_types?: TripType[] },
  ) => send<CopyTripsResult>('post', `${base(schoolId)}/copy`, body),
  /** Same result shape as copy: created count + skipped dates with reasons. */
  bulk: (schoolId: string, body: BulkTripsInput) =>
    send<CopyTripsResult>('post', `${base(schoolId)}/bulk`, body),
  calendar: (schoolId: string, from: string, to: string) =>
    getData<TripCalendarDay[]>(`${base(schoolId)}/calendar`, { from, to }),
  /** Clears bus and driver together; route/schedule/stops/students untouched.
   * Safe to call again on an already-unassigned trip. */
  unassign: (schoolId: string, id: string) => send<Trip>('post', `${base(schoolId)}/${id}/unassign`),
  /** Per-trip result: each ID either "unassigned" or "skipped" with a reason. */
  bulkUnassign: (schoolId: string, tripIds: string[]) =>
    send<BulkUnassignResult>('post', `${base(schoolId)}/unassign`, { trip_ids: tripIds }),
  /** Date-range/All Dates alternative to an explicit trip_ids list (Trip
   * Planner's cross-date flow) — resolved server-side, capped at 500. */
  bulkUnassignFilter: (schoolId: string, filter: BulkUnassignFilter) =>
    send<BulkUnassignResult>('post', `${base(schoolId)}/unassign`, filter),
  /** Exact matching count (eligible/ineligible split) + auto-detected date
   * span for the same filter, before confirming. */
  bulkUnassignPreview: (schoolId: string, filter: BulkUnassignFilter) =>
    send<BulkUnassignPreviewResult>('post', `${base(schoolId)}/unassign/preview`, filter),
  /** Replaces just the driver (or just the bus) — bus/route/schedule (or
   * driver/route/schedule) and everything else stay exactly as they are. */
  reassignDriver: (schoolId: string, id: string, driverId: string) =>
    send<Trip>('patch', `${base(schoolId)}/${id}/driver`, { driver_id: driverId }),
  reassignBus: (schoolId: string, id: string, busId: string) =>
    send<Trip>('patch', `${base(schoolId)}/${id}/bus`, { bus_id: busId }),
}
