import { api, getData, getPage, send } from './api'
import type {
  ImportResult,
  ListParams,
  Parent,
  ParentInput,
  RouteStudent,
  Student,
  StudentAssignment,
  StudentInput,
  StudentListParams,
} from './types'

const school = (schoolId: string) => `/api/v1/schools/${schoolId}`

export const studentsApi = {
  list: (schoolId: string, params: StudentListParams) =>
    getPage<Student>(`${school(schoolId)}/students`, params),
  get: (schoolId: string, id: string) => getData<Student>(`${school(schoolId)}/students/${id}`),
  classes: (schoolId: string) => getData<string[]>(`${school(schoolId)}/students/classes`),
  create: (schoolId: string, input: StudentInput) =>
    send<Student>('post', `${school(schoolId)}/students`, input),
  update: (schoolId: string, id: string, input: StudentInput) =>
    send<Student>('put', `${school(schoolId)}/students/${id}`, input),
  setStatus: (schoolId: string, id: string, status: Student['status']) =>
    send<Student>('patch', `${school(schoolId)}/students/${id}/status`, { status }),
  assign: (
    schoolId: string,
    id: string,
    body: { route_id: string; pickup_stop_id: string | null; drop_stop_id: string | null },
  ) => send<Student>('put', `${school(schoolId)}/students/${id}/assignment`, body),
  unassign: (schoolId: string, id: string) =>
    send<void>('delete', `${school(schoolId)}/students/${id}/assignment`),
  history: (schoolId: string, id: string) =>
    getData<StudentAssignment[]>(`${school(schoolId)}/students/${id}/assignments`),
  routeStudents: (schoolId: string, routeId: string) =>
    getData<RouteStudent[]>(`${school(schoolId)}/routes/${routeId}/students`),

  /** CSV import; dryRun validates without saving. */
  import: async (schoolId: string, file: File, dryRun: boolean) => {
    const form = new FormData()
    form.append('file', file)
    const res = await api.post<{ data: ImportResult }>(
      `${school(schoolId)}/students/import`,
      form,
      {
        params: { dry_run: dryRun },
        timeout: 120000,
      },
    )
    return res.data.data
  },
}

export const parentsApi = {
  list: (schoolId: string, params: ListParams) =>
    getPage<Parent>(`${school(schoolId)}/parents`, params),
  create: (schoolId: string, input: ParentInput) =>
    send<Parent>('post', `${school(schoolId)}/parents`, input),
  update: (schoolId: string, id: string, input: ParentInput) =>
    send<Parent>('put', `${school(schoolId)}/parents/${id}`, input),
  setStatus: (schoolId: string, id: string, status: Parent['status']) =>
    send<Parent>('patch', `${school(schoolId)}/parents/${id}/status`, { status }),
}
