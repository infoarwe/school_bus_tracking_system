import { getData, getPage, send } from './api'
import type { ListParams, School, SchoolInput } from './types'

export const schoolsApi = {
  list: (params: ListParams) => getPage<School>('/api/v1/schools', params),
  get: (id: string) => getData<School>(`/api/v1/schools/${id}`),
  create: (input: SchoolInput) => send<School>('post', '/api/v1/schools', input),
  update: (id: string, input: SchoolInput) => send<School>('put', `/api/v1/schools/${id}`, input),
  /** Deactivating needs the school's code typed as confirmation (critical action). */
  setStatus: (id: string, status: School['status'], confirmCode?: string) =>
    send<School>('patch', `/api/v1/schools/${id}/status`, { status, confirm_code: confirmCode }),
}
