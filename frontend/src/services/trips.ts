import { getData, getPage, send } from './api'
import type {
  CopyTripsResult,
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
