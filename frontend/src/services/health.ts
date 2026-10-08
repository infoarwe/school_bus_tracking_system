import { api, type ApiErrorBody } from './api'

export interface Health {
  status: 'ok' | 'degraded'
  checks: Record<string, 'up' | 'down'>
}

// /health returns 503 with a body when a dependency is down, so read the body either way.
export async function fetchHealth(): Promise<Health> {
  const res = await api.get<{ data?: Health; error?: ApiErrorBody }>('/health', {
    validateStatus: (s) => s === 200 || s === 503,
  })
  if (!res.data.data) throw new Error('Unexpected health response')
  return res.data.data
}
