import { api, getData, getPage } from './api'
import type { AuditRow, DashboardData, ListParams, PageMeta, ReportDef, TrackPoint } from './types'

const school = (schoolId: string) => `/api/v1/schools/${schoolId}`

export type ReportQuery = Partial<
  Record<'from' | 'to' | 'route_id' | 'bus_id' | 'driver_id' | 'trip_id', string>
>

export const dashboardApi = {
  get: (schoolId: string) => getData<DashboardData>(`${school(schoolId)}/dashboard`),
}

export const reportsApi = {
  list: (schoolId: string) => getData<ReportDef[]>(`${school(schoolId)}/reports`),
  run: async (
    schoolId: string,
    key: string,
    query: ReportQuery,
    page: number,
    pageSize: number,
  ) => {
    const res = await api.get<{ data: { report: ReportDef; rows: string[][] }; meta: PageMeta }>(
      `${school(schoolId)}/reports/${key}`,
      { params: { ...query, page, page_size: pageSize } },
    )
    return { rows: res.data.data.rows, meta: res.data.meta }
  },
  /** Downloads the full CSV (sent with the auth header, so not a plain link). */
  downloadCsv: async (schoolId: string, key: string, query: ReportQuery) => {
    const res = await api.get<Blob>(`${school(schoolId)}/reports/${key}`, {
      params: { ...query, format: 'csv' },
      responseType: 'blob',
      timeout: 120000,
    })
    const name =
      /filename="([^"]+)"/.exec(String(res.headers['content-disposition'] ?? ''))?.[1] ??
      `${key}.csv`
    const url = URL.createObjectURL(res.data)
    const a = document.createElement('a')
    a.href = url
    a.download = name
    a.click()
    URL.revokeObjectURL(url)
  },
}

export interface AuditQuery extends ListParams {
  action?: string
  entity_type?: string
  from?: string
  to?: string
  school_id?: string
}

export const auditApi = {
  school: (schoolId: string, params: AuditQuery) =>
    getPage<AuditRow>(`${school(schoolId)}/audit-logs`, params),
  /** Super Admin: every school and platform-level entries. */
  platform: (params: AuditQuery) => getPage<AuditRow>('/api/v1/audit-logs', params),
}

export const trackApi = {
  get: (schoolId: string, tripId: string) =>
    getData<TrackPoint[]>(`${school(schoolId)}/trips/${tripId}/track`),
}
