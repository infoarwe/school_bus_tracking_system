import { getData, getPage, send } from './api'
import type { Alert, Announcement, AnnouncementInput, DelayReason, ListParams } from './types'

const school = (schoolId: string) => `/api/v1/schools/${schoolId}`

export const alertsApi = {
  list: (schoolId: string) => getData<Alert[]>(`${school(schoolId)}/alerts`),
  reportDelay: (
    schoolId: string,
    tripId: string,
    body: { minutes: number; reason: DelayReason; note?: string },
  ) => send<Alert>('post', `${school(schoolId)}/trips/${tripId}/delays`, body),
  acknowledge: (schoolId: string, id: string) =>
    send<Alert>('post', `${school(schoolId)}/emergencies/${id}/acknowledge`),
  resolve: (schoolId: string, id: string) =>
    send<Alert>('post', `${school(schoolId)}/emergencies/${id}/resolve`),
}

export const announcementsApi = {
  list: (schoolId: string, params: ListParams) =>
    getPage<Announcement>(`${school(schoolId)}/announcements`, params),
  create: (schoolId: string, input: AnnouncementInput) =>
    send<Announcement>('post', `${school(schoolId)}/announcements`, input),
  cancel: (schoolId: string, id: string) =>
    send<Announcement>('post', `${school(schoolId)}/announcements/${id}/cancel`),
}
