import { api, getData, send } from './api'

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

/** The school's white-label app branding (S8-09). null = the app's default. */
export interface Branding {
  school_id: string
  school_name: string
  /** As set; '' = not set (the apps show the school name). */
  app_name: string
  display_name: string
  primary_color: string | null
  secondary_color: string | null
  /** Path on the API origin; resolve with apiUrl(). */
  logo_url: string | null
  updated_at: string | null
}

export const brandingApi = {
  get: (schoolId: string) => getData<Branding>(`/api/v1/schools/${schoolId}/settings/branding`),
  /** Empty strings reset to the app default. */
  update: (
    schoolId: string,
    body: { app_name: string; primary_color: string; secondary_color: string },
  ) => send<Branding>('put', `/api/v1/schools/${schoolId}/settings/branding`, body),
  uploadLogo: async (schoolId: string, file: File) => {
    const form = new FormData()
    form.append('logo', file)
    const res = await api.put<{ data: Branding }>(
      `/api/v1/schools/${schoolId}/settings/branding/logo`,
      form,
      { timeout: 60000 },
    )
    return res.data.data
  },
  removeLogo: (schoolId: string) =>
    send<Branding>('delete', `/api/v1/schools/${schoolId}/settings/branding/logo`),
}
