import axios, { AxiosError } from 'axios'

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

export interface PageMeta {
  page: number
  page_size: number
  total: number
}

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL ?? '',
  timeout: 15000,
})

// Sprint 1 adds the Authorization header and token refresh here.

api.interceptors.response.use(
  (res) => res,
  (err: AxiosError<{ error?: ApiErrorBody }>) => {
    const body = err.response?.data?.error
    if (err.response && body) {
      return Promise.reject(new ApiError(err.response.status, body))
    }
    return Promise.reject(
      new ApiError(err.response?.status ?? 0, {
        code: 'network_error',
        message: 'Cannot reach the server. Check your connection.',
      }),
    )
  },
)

/** GET returning the `data` field of the standard response envelope. */
export async function getData<T>(url: string): Promise<T> {
  const res = await api.get<{ data: T }>(url)
  return res.data.data
}
