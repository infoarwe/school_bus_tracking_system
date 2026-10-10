import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { tokenStore } from './tokenStore'
import type { Paged, Tokens } from './types'

// Matches the backend error format in backend/internal/httpx/response.go.
export interface ApiErrorBody {
  code: string
  message: string
  fields?: Record<string, string>
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fields?: Record<string, string>

  constructor(status: number, body: ApiErrorBody) {
    super(body.message)
    this.status = status
    this.code = body.code
    this.fields = body.fields
  }
}

const baseURL = import.meta.env.VITE_API_URL ?? ''

export const api = axios.create({ baseURL, timeout: 15000 })

/** Absolute URL for a path the API returns (e.g. a logo_url), for <img src>. */
export const apiUrl = (path: string) => baseURL + path

// Called when the session cannot be recovered; AuthContext sends the user to login.
let onSessionLost: (reason: ApiError) => void = () => {}
export function setSessionLostHandler(fn: (reason: ApiError) => void) {
  onSessionLost = fn
}

api.interceptors.request.use((config) => {
  if (tokenStore.access && !config.headers.Authorization) {
    config.headers.Authorization = `Bearer ${tokenStore.access}`
  }
  return config
})

// One refresh at a time: parallel 401s all wait for the same refresh call.
let refreshing: Promise<boolean> | null = null

async function refreshTokens(): Promise<boolean> {
  const refresh = tokenStore.refresh
  if (!refresh) return false
  try {
    const res = await axios.post<{ data: Tokens }>(`${baseURL}/api/v1/auth/refresh`, {
      refresh_token: refresh,
    })
    tokenStore.set(res.data.data)
    return true
  } catch {
    return false
  }
}

const REFRESHABLE = new Set(['token_invalid', 'unauthorized'])
const SESSION_LOST = new Set([
  'token_invalid',
  'unauthorized',
  'session_revoked',
  'refresh_token_invalid',
  'account_suspended',
  'school_inactive',
])

type RetryConfig = InternalAxiosRequestConfig & { _retried?: boolean }

api.interceptors.response.use(
  (res) => res,
  async (err: AxiosError<{ error?: ApiErrorBody }>) => {
    const body = err.response?.data?.error
    if (!err.response || !body) {
      return Promise.reject(
        new ApiError(err.response?.status ?? 0, {
          code: 'network_error',
          message: 'Cannot reach the server. Check your connection.',
        }),
      )
    }

    const config = err.config as RetryConfig | undefined
    // Login, refresh and OTP calls report their own errors; never refresh or redirect for them.
    const isAuthCall = /\/auth\/(login|refresh|otp)/.test(config?.url ?? '')
    if (REFRESHABLE.has(body.code) && config && !config._retried && !isAuthCall) {
      refreshing ??= refreshTokens().finally(() => (refreshing = null))
      if (await refreshing) {
        config._retried = true
        config.headers.Authorization = `Bearer ${tokenStore.access}`
        return api.request(config)
      }
    }

    const apiErr = new ApiError(err.response.status, body)
    if (SESSION_LOST.has(body.code) && !isAuthCall) {
      tokenStore.clear()
      onSessionLost(apiErr)
    }
    return Promise.reject(apiErr)
  },
)

/** GET returning the `data` field of the standard response envelope. */
export async function getData<T>(url: string, params?: object): Promise<T> {
  const res = await api.get<{ data: T }>(url, { params })
  return res.data.data
}

export async function getPage<T>(url: string, params?: object): Promise<Paged<T>> {
  const res = await api.get<Paged<T>>(url, { params })
  return res.data
}

export async function send<T>(
  method: 'post' | 'put' | 'patch' | 'delete',
  url: string,
  body?: unknown,
): Promise<T> {
  const res = await api.request<{ data: T }>({ method, url, data: body })
  return res.data?.data
}
