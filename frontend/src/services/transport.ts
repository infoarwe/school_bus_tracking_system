import { getData, getPage, send } from './api'
import type {
  Bus,
  BusInput,
  BusRoute,
  BusStatus,
  Driver,
  DriverInput,
  DriverStatus,
  ListParams,
  RouteInput,
  Stop,
  StopInput,
} from './types'

const school = (schoolId: string) => `/api/v1/schools/${schoolId}`

export const driversApi = {
  list: (schoolId: string, params: ListParams) =>
    getPage<Driver>(`${school(schoolId)}/drivers`, params),
  create: (schoolId: string, input: DriverInput) =>
    send<Driver>('post', `${school(schoolId)}/drivers`, input),
  update: (schoolId: string, id: string, input: DriverInput) =>
    send<Driver>('put', `${school(schoolId)}/drivers/${id}`, input),
  setStatus: (schoolId: string, id: string, status: DriverStatus) =>
    send<Driver>('patch', `${school(schoolId)}/drivers/${id}/status`, { status }),
}

export const busesApi = {
  list: (schoolId: string, params: ListParams) => getPage<Bus>(`${school(schoolId)}/buses`, params),
  create: (schoolId: string, input: BusInput) =>
    send<Bus>('post', `${school(schoolId)}/buses`, input),
  update: (schoolId: string, id: string, input: BusInput) =>
    send<Bus>('put', `${school(schoolId)}/buses/${id}`, input),
  setStatus: (schoolId: string, id: string, status: BusStatus) =>
    send<Bus>('patch', `${school(schoolId)}/buses/${id}/status`, { status }),
}

const route = (schoolId: string, routeId: string) => `${school(schoolId)}/routes/${routeId}`

export const routesApi = {
  list: (schoolId: string, params: ListParams) =>
    getPage<BusRoute>(`${school(schoolId)}/routes`, params),
  get: (schoolId: string, id: string) => getData<BusRoute>(route(schoolId, id)),
  create: (schoolId: string, input: RouteInput) =>
    send<BusRoute>('post', `${school(schoolId)}/routes`, input),
  update: (schoolId: string, id: string, input: RouteInput) =>
    send<BusRoute>('put', route(schoolId, id), input),
  setStatus: (schoolId: string, id: string, status: BusRoute['status']) =>
    send<BusRoute>('patch', `${route(schoolId, id)}/status`, { status }),

  createStop: (schoolId: string, routeId: string, input: StopInput & { position?: number }) =>
    send<Stop>('post', `${route(schoolId, routeId)}/stops`, input),
  updateStop: (schoolId: string, routeId: string, stopId: string, input: StopInput) =>
    send<Stop>('put', `${route(schoolId, routeId)}/stops/${stopId}`, input),
  deleteStop: (schoolId: string, routeId: string, stopId: string) =>
    send<void>('delete', `${route(schoolId, routeId)}/stops/${stopId}`),
  reorderStops: (schoolId: string, routeId: string, stopIds: string[]) =>
    send<Stop[]>('put', `${route(schoolId, routeId)}/stops/order`, { stop_ids: stopIds }),
}
