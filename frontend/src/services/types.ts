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
