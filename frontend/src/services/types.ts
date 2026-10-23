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

/** A permanent Morning Pickup / Evening Drop driver+bus+route binding, set once by staff.
 * Never starts or tracks a trip itself - GET /driver/trips materializes it into a normal
 * trip each school day. */
export interface DriverRouteAssignmentInput {
  route_id: string
  trip_type: 'morning_pickup' | 'evening_drop'
  driver_id: string
  bus_id: string
}

export interface DriverRouteAssignment {
  id: string
  school_id: string
  trip_type: 'morning_pickup' | 'evening_drop'
  route: { id: string; code: string; name: string }
  bus: { id: string; vehicle_number: string }
  driver: { id: string; name: string; mobile: string }
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
  /** Eligible as a Special Trip/Outing's template route. */
  supports_outing: boolean
}

/** What a route's Morning Pickup/Evening Drop actually run as, derived live from its
 * stops' own pickup_time/drop_time (GET /routes/{id}/schedule). `supported` false means
 * the route doesn't run that type at all; `supported` true with null times means it does,
 * but none of its stops have a time set yet — never invented, the admin enters one. */
export interface RouteScheduleWindow {
  supported: boolean
  start_time: string | null
  end_time: string | null
}
export interface RouteSchedule {
  morning_pickup: RouteScheduleWindow
  evening_drop: RouteScheduleWindow
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
  /** Morning Pickup travel order, 1..N. */
  sequence: number
  /** Evening Drop travel order, independent of sequence; null if not part of the evening leg. */
  drop_sequence: number | null
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

export type TripType = 'morning_pickup' | 'evening_drop' | 'special_trip'
export type TripStatus = 'scheduled' | 'confirmed' | 'started' | 'completed' | 'cancelled'

export const tripTypeLabels: Record<TripType, string> = {
  morning_pickup: 'Morning Pickup',
  evening_drop: 'Evening Drop',
  special_trip: 'Special Trip / Outing',
}

export interface WaypointInput {
  name: string
  landmark: string
  latitude: number
  longitude: number
  geofence_radius_m: number
}

export interface Waypoint extends WaypointInput {
  id: string
  sequence: number
  arrived_at: string | null
}

export interface TripInput {
  trip_date: string
  trip_type: TripType
  /** Required for morning_pickup/evening_drop; optional template for special_trip. */
  route_id?: string
  bus_id: string
  driver_id: string
  notes?: string
  /** special_trip only, required when route_id is omitted: the outing's name. */
  title?: string
  /** special_trip only; ignored (always notifies) for the other two types. Default true. */
  notify_parents?: boolean
  /** special_trip without route_id only: the ad-hoc destinations, in order. */
  waypoints?: WaypointInput[]
  /**
   * HH:MM, school-local. Give both or neither — required in practice so this
   * trip gets overlap protection against the same bus/driver/route's other
   * trips that day; only a copy/bulk-create of a legacy trip with no schedule
   * of its own may legitimately send neither.
   */
  scheduled_start_time?: string
  scheduled_end_time?: string
}

export interface Trip {
  id: string
  school_id: string
  trip_date: string
  trip_type: TripType
  status: TripStatus
  /** Null for a routeless Special Trip/Outing; title is shown instead. */
  route: { id: string; code: string; name: string; stop_count: number } | null
  /** Null exactly when this trip has been unassigned (bus and driver are cleared together). */
  bus: { id: string; vehicle_number: string; capacity: number } | null
  driver: { id: string; name: string; mobile: string } | null
  /** Null for a trip created before this field existed, or whose route has no stop times to derive it from. */
  scheduled_start_at: string | null
  scheduled_end_at: string | null
  student_count: number
  /** Special Trip/Outing name; "" for route-based trips. */
  title: string
  notify_parents: boolean
  notes: string
  cancel_reason: string
  confirmed_at: string | null
  started_at: string | null
  ended_at: string | null
  cancelled_at: string | null
  created_at: string
  updated_at: string
  /** driver: the driver took this route in the Driver app; staff: school staff assigned it. */
  assigned_by: 'driver' | 'staff'
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
  /** Inclusive YYYY-MM-DD range, used when date is not set. */
  from?: string
  to?: string
  trip_type?: TripType
  route_id?: string
  driver_id?: string
  bus_id?: string
  /** asc = earliest date first (upcoming trips); default newest first. */
  sort?: 'asc'
}

/** POST /trips/bulk: one route + bus + driver on many dates. Morning Pickup and
 * Evening Drop never run at the same time of day, so each included type needs its
 * own window; empty (both omitted for a type) = no schedule, same as a single trip's
 * scheduled_start_time/end_time — never invented, never forced. */
export interface BulkTripsInput {
  dates: string[]
  trip_types: ('morning_pickup' | 'evening_drop')[]
  route_id: string
  bus_id: string
  driver_id: string
  notes?: string
  morning_start_time?: string
  morning_end_time?: string
  evening_start_time?: string
  evening_end_time?: string
}

/** POST /trips/unassign (and .../unassign/preview): the date-range/All Dates
 * alternative to an explicit trip_ids list — Trip Planner's cross-date Bulk
 * Unassign. At least one of all_dates/from/to is required; the rest narrow
 * it further and are all optional. */
export interface BulkUnassignFilter {
  all_dates?: boolean
  from?: string
  to?: string
  route_id?: string
  bus_id?: string
  driver_id?: string
  trip_type?: TripType
}

export interface BulkUnassignResult {
  results: { trip_id: string; status: 'unassigned' | 'skipped'; reason?: string }[]
}

/** POST /trips/unassign/preview: scope + auto-detected date span for a filter. */
export interface BulkUnassignPreviewResult {
  count: number
  eligible: number
  ineligible: number
  from: string | null
  to: string | null
  over_limit: boolean
  limit: number
}

/** GET /trips/calendar: trip counts of one date + trip type. */
export interface TripCalendarDay {
  trip_date: string
  trip_type: TripType
  /** Not cancelled. */
  assigned: number
  confirmed: number
  started: number
  completed: number
  cancelled: number
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

export type AnnouncementCategory =
  | 'school_announcement'
  | 'route_announcement'
  | 'bus_breakdown'
  | 'traffic_delay'
  | 'pickup_change'
  | 'emergency_message'
  | 'holiday'
  | 'other'

export const announcementCategoryLabels: Record<AnnouncementCategory, string> = {
  school_announcement: 'School announcement',
  route_announcement: 'Route announcement',
  bus_breakdown: 'Bus breakdown',
  traffic_delay: 'Traffic delay',
  pickup_change: 'Pickup change',
  emergency_message: 'Emergency message',
  holiday: 'Holiday',
  other: 'Other',
}

export interface DeliveryStats {
  recipients: number
  read: number
  sent: number
  pending: number
  failed: number
}

export interface Announcement {
  id: string
  school_id: string
  category: AnnouncementCategory
  target: 'school' | 'route'
  route_id: string | null
  route_code: string | null
  title: string
  message: string
  attachment_url: string
  scheduled_at: string | null
  status: 'scheduled' | 'sending' | 'sent' | 'cancelled'
  sent_at: string | null
  created_by_name: string | null
  created_at: string
  stats: DeliveryStats | null
}

export interface AnnouncementInput {
  category: AnnouncementCategory
  target: 'school' | 'route'
  route_id?: string | null
  title: string
  message: string
  attachment_url?: string
  scheduled_at?: string | null
}

export type DelayReason =
  'traffic' | 'bus_breakdown' | 'road_block' | 'weather' | 'driver_issue' | 'other'

export const delayReasonLabels: Record<DelayReason, string> = {
  traffic: 'Traffic',
  bus_breakdown: 'Bus breakdown',
  road_block: 'Road block',
  weather: 'Weather',
  driver_issue: 'Driver issue',
  other: 'Other',
}

export interface Alert {
  kind: 'delay' | 'emergency'
  id: string
  trip_id: string
  trip_type: TripType
  /** "" for a routeless Special Trip/Outing: see title. */
  route_code: string
  /** The outing's name; "" for route-based trips. */
  title: string
  bus_number: string
  driver_name: string
  created_at: string
  reported_by: string | null
  minutes?: number
  reason?: DelayReason
  note?: string
  message?: string
  latitude?: number
  longitude?: number
  status?: 'open' | 'acknowledged' | 'resolved'
  acknowledged_at?: string
  resolved_at?: string
}

export interface DashboardData {
  today: string
  totals: {
    buses_active: number
    buses_total: number
    buses_in_maintenance: number
    drivers_active: number
    students_on_transport: number
    students_unassigned: number
    routes_active: number
  }
  /** trip type → status → count, for today */
  trips: Partial<Record<TripType, Partial<Record<TripStatus, number>>>>
  live: { on_road: number; offline: number }
  delayed_trips: number
  open_emergencies: number
  routes: {
    route_id: string
    code: string
    name: string
    students: number
    morning_status: TripStatus | null
    evening_status: TripStatus | null
    morning_bus: string | null
    evening_bus: string | null
    delays: number
    missed_stops: number
  }[]
  /** Today's Special Trips/Outings, separate from the route-based list above. */
  outings: {
    trip_id: string
    title: string
    status: TripStatus
    bus_number: string
    driver_name: string
    trip_date: string
    /** "" when the outing has no template route. */
    route_code: string
  }[]
}

export type ReportFilterName = 'from' | 'to' | 'route_id' | 'bus_id' | 'driver_id' | 'trip_id'

export interface ReportDef {
  key: string
  title: string
  description: string
  filters: ReportFilterName[]
  columns: { key: string; label: string }[]
}

export interface AuditRow {
  id: string
  school_id: string | null
  school_name: string | null
  actor_id: string | null
  actor_name: string | null
  actor_role: string
  action: string
  entity_type: string
  entity_id: string | null
  before: unknown
  after: unknown
  ip: string
  request_id: string
  created_at: string
}

/** GET /trips/{id}/planned-path: google follows the roads, straight joins the stops. */
export interface PlannedPath {
  source: 'google' | 'straight'
  points: { latitude: number; longitude: number }[]
}

/** Travelled route saved when a trip is completed (GET /trips/{id}/route-snapshot). */
export interface RouteSnapshot {
  trip_id: string
  status: 'ready' | 'no_gps' | 'failed'
  has_image: boolean
  /** Google encoded polyline of the travelled path. */
  polyline: string
  distance_m: number
  point_count: number
  started_at: string | null
  ended_at: string | null
  /** Why there is no image (empty when ready). */
  error: string
  updated_at: string
}

export interface TrackPoint {
  latitude: number
  longitude: number
  speed_mps: number | null
  recorded_at: string
}

/** A staff inbox item: a driver took or released a route (GET /staff/notifications). */
export interface StaffNotice {
  id: string
  type: 'route_claimed' | 'route_released' | string
  title: string
  body: string
  data: {
    trip_id?: string
    route_id?: string
    route_code?: string
    driver_name?: string
    bus_number?: string
    trip_date?: string
    trip_type?: TripType
    at?: string
  }
  read_at: string | null
  created_at: string
}
