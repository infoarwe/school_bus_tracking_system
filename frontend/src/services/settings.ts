import { getData, send } from './api'

export interface MapsSettings {
  /** Loads Google Maps in the admin web. Empty = OpenStreetMap is used. */
  browser_key: string
  /** The server key itself is never returned. */
  server_key_set: boolean
  server_key_hint: string
}

export const settingsApi = {
  getMaps: (schoolId: string) => getData<MapsSettings>(`/api/v1/schools/${schoolId}/settings/maps`),
  /** server_key: undefined keeps the stored key, '' removes it. */
  updateMaps: (schoolId: string, body: { browser_key: string; server_key?: string }) =>
    send<MapsSettings>('put', `/api/v1/schools/${schoolId}/settings/maps`, body),
}

export interface TrackingSettings {
  /** Days of GPS history to keep; 0 keeps none (live tracking still works). */
  location_retention_days: number
  /** A bus with no GPS for this long is shown as offline. */
  stale_after_seconds: number
  /** "Approaching" fires within this distance of the next stop. */
  approach_distance_m: number
  /** The school's location: destination of Morning Pickup ("School Reached"). */
  school_latitude: number | null
  school_longitude: number | null
  /** Arrival radius used for new stops. */
  default_geofence_m: number
}

export const trackingSettingsApi = {
  get: (schoolId: string) =>
    getData<TrackingSettings>(`/api/v1/schools/${schoolId}/settings/tracking`),
  update: (schoolId: string, body: TrackingSettings) =>
    send<TrackingSettings>('put', `/api/v1/schools/${schoolId}/settings/tracking`, body),
}

export interface PushSettings {
  configured: boolean
  project_id: string
  client_email: string
  updated_at: string | null
}

/** The school's own Firebase key (each school has its own branded apps). The key is never returned. */
export const pushSettingsApi = {
  get: (schoolId: string) => getData<PushSettings>(`/api/v1/schools/${schoolId}/settings/push`),
  upload: (schoolId: string, serviceAccountJson: string) =>
    send<PushSettings>('put', `/api/v1/schools/${schoolId}/settings/push`, {
      service_account_json: serviceAccountJson,
    }),
  remove: (schoolId: string) =>
    send<PushSettings>('delete', `/api/v1/schools/${schoolId}/settings/push`),
  test: (schoolId: string) =>
    send<{ ok: boolean; message: string }>(
      'post',
      `/api/v1/schools/${schoolId}/settings/push/test`,
    ),
}
