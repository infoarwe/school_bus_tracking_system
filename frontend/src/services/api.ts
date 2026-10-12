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
  /** X-Request-ID of the failed call: lets support find it in the server log. */
  readonly requestId?: string

  constructor(status: number, body: ApiErrorBody, requestId?: string) {
    super(body.message)
    this.status = status
    this.code = body.code
    this.fields = body.fields
    this.requestId = requestId
  }
}

/**
 * The error for a response without our JSON error body (proxy errors, timeouts, no
 * network). Never shows raw server output to the user.
 */
function transportError(err: AxiosError): ApiError {
  const status = err.response?.status ?? 0
  const requestId = err.response?.headers?.['x-request-id'] as string | undefined
  if (!err.response) {
    const timedOut = err.code === 'ECONNABORTED' || err.code === 'ETIMEDOUT'
    return new ApiError(0, {
      code: timedOut ? 'timeout' : 'network_error',
      message: timedOut
        ? 'The server took too long to answer. Check your connection and try again.'
        : 'Cannot reach the server. Check your internet connection and try again.',
    })
  }
  if (status === 413) {
    return new ApiError(status, { code: 'too_large', message: 'The file is too large to upload.' })
  }
  if (status === 429) {
    return new ApiError(status, {
      code: 'rate_limited',
      message: 'Too many requests. Wait a few seconds and try again.',
    })
  }
  if (status >= 500) {
    return new ApiError(
      status,
      {
        code: 'server_unavailable',
        message: 'The server is temporarily unavailable. Please try again in a minute.',
      },
      requestId,
    )
  }
  return new ApiError(
    status,
    { code: 'unexpected_response', message: `Unexpected answer from the server (${status}).` },
    requestId,
  )
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
    if (!err.response || !body) return Promise.reject(transportError(err))

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

    const requestId = err.response.headers['x-request-id'] as string | undefined
    const status = err.response.status
    const apiErr = new ApiError(
      status,
      status >= 500
        ? {
            ...body,
            // The server never sends details for a 500; give staff something to report.
            message:
              'Something went wrong on the server. Please try again; if it keeps happening, ' +
              `report reference ${requestId ?? 'unknown'}.`,
          }
        : body,
      requestId,
    )
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
