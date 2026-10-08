import { getPage, send } from './api'
import type { ListParams, Role, User } from './types'

export type StaffRole = Extract<Role, 'school_admin' | 'transport_manager'>

export interface UserInput {
  name: string
  email: string
  mobile?: string | null
}

const base = (schoolId: string) => `/api/v1/schools/${schoolId}/users`

export const usersApi = {
  list: (schoolId: string, params: ListParams & { role?: StaffRole }) =>
    getPage<User>(base(schoolId), params),
  create: (schoolId: string, input: UserInput & { role: StaffRole; password: string }) =>
    send<User>('post', base(schoolId), input),
  update: (schoolId: string, id: string, input: UserInput) =>
    send<User>('put', `${base(schoolId)}/${id}`, input),
  setStatus: (schoolId: string, id: string, status: User['status']) =>
    send<User>('patch', `${base(schoolId)}/${id}/status`, { status }),
  resetPassword: (schoolId: string, id: string, password: string) =>
    send<void>('post', `${base(schoolId)}/${id}/password`, { password }),
}
