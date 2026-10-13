import { getData, send } from './api'
import type { DriverRouteAssignment, DriverRouteAssignmentInput } from './types'

const base = (schoolId: string) => `/api/v1/schools/${schoolId}/driver-assignments`

export const driverAssignmentsApi = {
  /** Current (not yet deactivated or replaced) assignments; optionally filtered to one driver. */
  list: (schoolId: string, driverId?: string) =>
    getData<DriverRouteAssignment[]>(
      base(schoolId),
      driverId ? { driver_id: driverId } : undefined,
    ),
  create: (schoolId: string, input: DriverRouteAssignmentInput) =>
    send<DriverRouteAssignment>('post', base(schoolId), input),
  /** Ends the current row and creates a new one (history preserved); returns the new row. */
  update: (schoolId: string, id: string, input: DriverRouteAssignmentInput) =>
    send<DriverRouteAssignment>('put', `${base(schoolId)}/${id}`, input),
  deactivate: (schoolId: string, id: string) =>
    send<void>('post', `${base(schoolId)}/${id}/deactivate`),
}
