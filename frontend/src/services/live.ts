import { api } from './api'
import { tokenStore } from './tokenStore'
import type { TripProgress, TripStatus, TripType } from './types'

// Mirrors BusLocation in backend/internal/tracking/live.go.
export interface BusLocation {
  trip_id: string
  school_id: string
  trip_type: TripType
  route_id: string
  route_code: string
  bus_number: string
  driver_name: string
  latitude: number
  longitude: number
  accuracy_m: number
  speed_mps: number | null
  heading: number | null
  recorded_at: string
  received_at: string
  stale: boolean
}

export type LiveMessage =
  | {
      type: 'subscribed'
      channel: 'school' | 'trip'
      trip_id?: string
      status?: TripStatus
      snapshot?: BusLocation[]
    }
  | { type: 'location' | 'bus_stale'; trip_id: string; location: BusLocation }
  | { type: 'trip_status'; trip_id: string; status: TripStatus }
  | { type: 'progress'; trip_id: string; progress: TripProgress }
  | {
      type: 'stop_status'
      trip_id: string
      stop: { stop_id: string; type: string; missed: boolean; at: string }
    }
  | { type: 'error'; code: string; message: string; trip_id?: string }
  | { type: 'pong' | 'unsubscribed' }

const SESSION_ENDED = 4401

function socketURL(schoolId: string | null): string {
  const base = import.meta.env.VITE_API_URL || window.location.origin
  const url = new URL('/api/v1/ws', base)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.searchParams.set('access_token', tokenStore.access ?? '')
  if (schoolId) url.searchParams.set('school_id', schoolId)
  return url.toString()
}

/**
 * Live tracking socket with automatic reconnect (1 s, 2 s, 4 s … 30 s). Subscriptions
 * are re-sent after every reconnect. If the server ends the session or rejects the
 * token, the access token is refreshed (through the API client) before retrying.
 */
export class LiveSocket {
  private ws: WebSocket | null = null
  private subs = new Set<string>()
  private retry = 0
  private timer: ReturnType<typeof setTimeout> | undefined
  private closed = false

  constructor(
    /** Only for a Super Admin, who is not bound to one school. */
    private readonly superAdminSchoolId: string | null,
    private readonly onMessage: (m: LiveMessage) => void,
    private readonly onConnection: (connected: boolean) => void,
  ) {
    this.connect()
  }

  subscribeSchool() {
    this.subscribe(JSON.stringify({ type: 'subscribe', channel: 'school' }))
  }

  subscribeTrip(tripId: string) {
    this.subscribe(JSON.stringify({ type: 'subscribe', channel: 'trip', trip_id: tripId }))
  }

  close() {
    this.closed = true
    clearTimeout(this.timer)
    this.ws?.close()
  }

  private subscribe(msg: string) {
    this.subs.add(msg)
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(msg)
  }

  private connect() {
    let opened = false
    const ws = new WebSocket(socketURL(this.superAdminSchoolId))
    this.ws = ws
    ws.onopen = () => {
      opened = true
      this.retry = 0
      this.onConnection(true)
      this.subs.forEach((m) => ws.send(m))
    }
    ws.onmessage = (e) => {
      try {
        this.onMessage(JSON.parse(e.data as string) as LiveMessage)
      } catch {
        // ignore malformed frames
      }
    }
    ws.onclose = (e) => {
      this.onConnection(false)
      if (this.closed) return
      // A failed handshake (usually an expired token) or an ended session: refresh first.
      const needsAuth = e.code === SESSION_ENDED || !opened
      const delay = Math.min(30000, 1000 * 2 ** this.retry++)
      this.timer = setTimeout(async () => {
        if (needsAuth) {
          try {
            await api.get('/api/v1/auth/me') // the API client refreshes the token if it can
          } catch {
            return // session lost: the app has already sent the user to login
          }
        }
        if (!this.closed) this.connect()
      }, delay)
    }
  }
}
