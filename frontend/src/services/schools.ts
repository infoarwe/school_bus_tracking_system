import { getData, getPage, send } from './api'
import type { ListParams, School, SchoolInput } from './types'

/** Parts of a school's data the Super Admin can clear (POST /schools/{id}/clear-data). */
export type ClearScope = 'trips' | 'notifications' | 'students' | 'routes' | 'buses' | 'drivers'

export interface ClearDataResult {
  dry_run: boolean
  scopes: ClearScope[]
  /** Rows deleted (or that would be) per kind, e.g. trips, gps_points, logins. */
  deleted: Record<string, number>
}

export const schoolsApi = {
  list: (params: ListParams) => getPage<School>('/api/v1/schools', params),
  get: (id: string) => getData<School>(`/api/v1/schools/${id}`),
  create: (input: SchoolInput) => send<School>('post', '/api/v1/schools', input),
  update: (id: string, input: SchoolInput) => send<School>('put', `/api/v1/schools/${id}`, input),
  /** Deactivating needs the school's code typed as confirmation (critical action). */
  setStatus: (id: string, status: School['status'], confirmCode?: string) =>
    send<School>('patch', `/api/v1/schools/${id}/status`, { status, confirm_code: confirmCode }),
  /** dry_run counts only; a real clear needs the school's code as confirmation. */
  clearData: (id: string, scopes: ClearScope[], opts: { dryRun?: boolean; confirmCode?: string }) =>
    send<ClearDataResult>('post', `/api/v1/schools/${id}/clear-data`, {
      scopes,
      dry_run: opts.dryRun ?? false,
      confirm_code: opts.confirmCode,
    }),
}
