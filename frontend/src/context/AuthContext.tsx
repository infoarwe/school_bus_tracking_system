import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { App } from 'antd'
import { setSessionLostHandler } from '../services/api'
import { authApi } from '../services/auth'
import { tokenStore } from '../services/tokenStore'
import type { Me, Role } from '../services/types'

type AuthState =
  { status: 'loading' } | { status: 'anonymous' } | { status: 'authenticated'; me: Me }

interface AuthValue {
  state: AuthState
  me: Me | null
  hasRole: (...roles: Role[]) => boolean
  /** Returns an mfa token when an authenticator code is still needed. */
  login: (email: string, password: string) => Promise<{ mfaToken?: string }>
  complete2fa: (mfaToken: string, code: string) => Promise<void>
  logout: () => Promise<void>
  reloadMe: () => Promise<void>
}

const AuthContext = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const { message } = App.useApp()
  const [state, setState] = useState<AuthState>(() =>
    tokenStore.access ? { status: 'loading' } : { status: 'anonymous' },
  )

  const reloadMe = useCallback(async () => {
    const me = await authApi.me()
    setState({ status: 'authenticated', me })
  }, [])

  // Restore the session on page load.
  useEffect(() => {
    if (!tokenStore.access) return
    authApi
      .me()
      .then((me) => setState({ status: 'authenticated', me }))
      .catch(() => {
        tokenStore.clear()
        setState({ status: 'anonymous' })
      })
  }, [])

  useEffect(() => {
    setSessionLostHandler((err) => {
      setState({ status: 'anonymous' })
      message.warning(err.message)
    })
  }, [message])

  const login = useCallback(
    async (email: string, password: string) => {
      const res = await authApi.login(email, password)
      if (res.mfa_required) return { mfaToken: res.mfa_token }
      tokenStore.set(res)
      await reloadMe()
      return {}
    },
    [reloadMe],
  )

  const complete2fa = useCallback(
    async (mfaToken: string, code: string) => {
      const res = await authApi.login2fa(mfaToken, code)
      if (res.mfa_required) throw new Error('Unexpected second 2FA step')
      tokenStore.set(res)
      await reloadMe()
    },
    [reloadMe],
  )

  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } catch {
      // already logged out on the server; clear locally anyway
    }
    tokenStore.clear()
    setState({ status: 'anonymous' })
  }, [])

  const value = useMemo<AuthValue>(() => {
    const me = state.status === 'authenticated' ? state.me : null
    return {
      state,
      me,
      hasRole: (...roles) => !!me && roles.includes(me.user.role),
      login,
      complete2fa,
      logout,
      reloadMe,
    }
  }, [state, login, complete2fa, logout, reloadMe])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider')
  return ctx
}
