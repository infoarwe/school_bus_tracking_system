// Mirrors backend/api/openapi.yaml.

export type Role = 'super_admin' | 'school_admin' | 'transport_manager' | 'driver' | 'parent'

export const roleLabels: Record<Role, string> = {
  super_admin: 'Super Admin',
  school_admin: 'School Admin',
  transport_manager: 'Transport Manager',
  driver: 'Driver',
  parent: 'Parent',
}

export interface User {
  id: string
  school_id: string | null
  role: Role
  name: string
  email: string | null
  mobile: string | null
  status: 'active' | 'suspended'
  totp_enabled: boolean
  last_login_at: string | null
  created_at: string
  updated_at: string
}

export type WeekDay = 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun'

export interface SchoolInput {
  name: string
  code: string
  address: string
  city: string
  state: string
  pincode: string
  contact_name: string
  contact_phone: string
  contact_email: string
  working_days: WeekDay[]
  timezone: string
  transport_config: Record<string, unknown>
}

export interface School extends SchoolInput {
  id: string
  status: 'active' | 'inactive'
  created_at: string
  updated_at: string
}

export interface Me {
  user: User
  school: School | null
  two_factor_setup_required: boolean
}

export interface Session {
  id: string
  device_name: string
  user_agent: string
  ip: string
  created_at: string
  last_used_at: string
  expires_at: string
  current: boolean
}

export interface Tokens {
  access_token: string
  refresh_token: string
  token_type: 'Bearer'
  expires_in: number
  user: User
}

export type LoginResult =
  { mfa_required: true; mfa_token: string } | ({ mfa_required: false } & Tokens)

export interface PageMeta {
  page: number
  page_size: number
  total: number
}

export interface Paged<T> {
  data: T[]
  meta: PageMeta
}

export interface ListParams {
  page?: number
  page_size?: number
  q?: string
  status?: string
}

export type DriverStatus = 'active' | 'inactive' | 'suspended'

export interface DriverInput {
  name: string
  mobile: string
  license_number: string
  license_expiry: string | null
  address: string
  emergency_contact_name: string
  emergency_contact_phone: string
  id_proof_type: string
  id_proof_number: string
  notes: string
}

export interface Driver extends DriverInput {
  id: string
  school_id: string
  user_id: string
  status: DriverStatus
  last_login_at: string | null
  created_at: string
  updated_at: string
}

export type BusStatus = 'active' | 'inactive' | 'maintenance'

export interface BusInput {
  vehicle_number: string
  capacity: number
  make_model: string
  gps_device_id: string
  notes: string
}

export interface Bus extends BusInput {
  id: string
  school_id: string
  status: BusStatus
  created_at: string
  updated_at: string
}

export interface RouteInput {
  name: string
  code: string
  start_point: string
  description: string
  supports_pickup: boolean
  supports_drop: boolean
}

export interface BusRoute extends RouteInput {
  id: string
  school_id: string
  status: 'active' | 'inactive'
  stop_count: number
  created_at: string
  updated_at: string
  /** Only when fetching a single route. */
  stops?: Stop[]
}

export interface StopInput {
  name: string
  landmark: string
  latitude: number
  longitude: number
  pickup_time: string | null
  drop_time: string | null
  geofence_radius_m: number
}

export interface Stop extends StopInput {
  id: string
  route_id: string
  sequence: number
  created_at: string
  updated_at: string
}

export interface StopRef {
  id: string
  name: string
  sequence: number
  latitude: number
  longitude: number
  pickup_time: string | null
  drop_time: string | null
}

export interface StudentAssignment {
  id: string
  route_id: string
  route_code: string
  route_name: string
  pickup_stop: StopRef | null
  drop_stop: StopRef | null
  assigned_by_name: string | null
  assigned_at: string
  ended_at: string | null
}

export type TransportStatus = 'uses_transport' | 'not_using'
export type Relationship = 'father' | 'mother' | 'guardian'

export interface StudentInput {
  admission_no: string
  name: string
  class: string
  section: string
  notes?: string
  transport_status: TransportStatus
}

export interface Student extends StudentInput {
  id: string
  school_id: string
  status: 'active' | 'inactive'
  assignment: StudentAssignment | null
  /** Only on GET of a single student. */
  parents?: { parent_id: string; name: string; mobile: string; relationship: Relationship }[]
  created_at: string
  updated_at: string
}

export interface StudentListParams extends ListParams {
  class?: string
  section?: string
  route_id?: string
  assigned?: '' | 'yes' | 'no'
  transport_status?: string
}

export interface ChildLink {
  student_id: string
  name: string
  admission_no: string
  class: string
  section: string
  relationship: Relationship
}

export interface ParentInput {
  name: string
  mobile: string
  alternate_mobile: string
  email: string
  address: string
  children: { student_id: string; relationship: Relationship }[]
}

export interface Parent extends Omit<ParentInput, 'children'> {
  id: string
  school_id: string
  user_id: string
  status: 'active' | 'inactive'
  last_login_at: string | null
  children: ChildLink[]
  created_at: string
  updated_at: string
}

export interface RouteStudent {
  student_id: string
  name: string
  admission_no: string
  class: string
  section: string
  pickup_stop_id: string | null
  drop_stop_id: string | null
}

export interface ImportResult {
  dry_run: boolean
  imported: boolean
  total_rows: number
  students_created: number
  students_updated: number
  parents_created: number
  parent_links: number
  assignments_set: number
  errors: { row: number; field: string; message: string }[]
}

export type TripType = 'morning_pickup' | 'evening_drop'
export type TripStatus = 'scheduled' | 'confirmed' | 'started' | 'completed' | 'cancelled'

export const tripTypeLabels: Record<TripType, string> = {
  morning_pickup: 'Morning Pickup',
  evening_drop: 'Evening Drop',
}

export interface TripInput {
  trip_date: string
  trip_type: TripType
  route_id: string
  bus_id: string
  driver_id: string
  notes?: string
}

export interface Trip {
  id: string
  school_id: string
  trip_date: string
  trip_type: TripType
  status: TripStatus
  route: { id: string; code: string; name: string; stop_count: number }
  bus: { id: string; vehicle_number: string; capacity: number }
  driver: { id: string; name: string; mobile: string }
  student_count: number
  notes: string
  cancel_reason: string
  confirmed_at: string | null
  started_at: string | null
  ended_at: string | null
  cancelled_at: string | null
  created_at: string
  updated_at: string
}

export interface TripStatusChange {
  from_status: TripStatus | null
  to_status: TripStatus
  changed_by_name: string | null
  changed_by_role: string
  reason: string
  created_at: string
}

export interface TripDetail extends Trip {
  history: TripStatusChange[]
  stops: (Stop & { student_count: number })[]
}

export interface TripListParams extends ListParams {
  date?: string
  trip_type?: TripType
  route_id?: string
}

export interface CopyTripsResult {
  created: number
  skipped: { trip_date: string; trip_type: TripType; route_code: string; reason: string }[]
}

export type StopStatus = 'upcoming' | 'approaching' | 'reached' | 'crossed'

export interface StopProgress {
  /** Stop ID, or "school" for the school at the end of Morning Pickup. */
  stop_id: string
  sequence: number
  name: string
  latitude: number
  longitude: number
  radius_m: number
  scheduled_time: string | null
  status: StopStatus
  missed: boolean
  reached_at: string | null
  crossed_at: string | null
  eta_seconds: number | null
  distance_m: number | null
}

export interface TripProgress {
  trip_id: string
  trip_type: TripType
  stops: StopProgress[]
  speed_mps: number
  eta_source: 'google' | 'estimate'
  updated_at: string
}

export interface StopEventRow {
  stop_id: string | null
  stop_name: string | null
  event_type: 'approaching' | 'reached' | 'crossed' | 'school_reached'
  missed: boolean
  occurred_at: string
}

export interface TripProgressResponse {
  progress: TripProgress | null
  events: StopEventRow[]
}
