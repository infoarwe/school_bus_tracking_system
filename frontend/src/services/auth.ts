import { getData, send } from './api'
import type { LoginResult, Me, Session } from './types'

const device = () => 'Admin web'

export const authApi = {
  login: (email: string, password: string) =>
    send<LoginResult>('post', '/api/v1/auth/login', { email, password, device_name: device() }),
  login2fa: (mfaToken: string, code: string) =>
    send<LoginResult>('post', '/api/v1/auth/login/2fa', {
      mfa_token: mfaToken,
      code,
      device_name: device(),
    }),
  me: () => getData<Me>('/api/v1/auth/me'),
  logout: () => send<void>('post', '/api/v1/auth/logout'),
  sessions: () => getData<Session[]>('/api/v1/auth/sessions'),
  revokeSession: (id: string) => send<void>('delete', `/api/v1/auth/sessions/${id}`),
  setup2fa: () =>
    send<{ secret: string; otpauth_url: string; qr_code_png: string }>(
      'post',
      '/api/v1/auth/2fa/setup',
    ),
  enable2fa: (code: string) => send<void>('post', '/api/v1/auth/2fa/enable', { code }),
}
