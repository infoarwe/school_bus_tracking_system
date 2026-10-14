import { api, getData, getPage, send } from './api'
import type {
  CopyTripsResult,
  PlannedPath,
  RouteSnapshot,
  Trip,
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
}
