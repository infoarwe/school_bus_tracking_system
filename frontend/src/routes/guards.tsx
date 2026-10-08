import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { Flex, Result, Spin } from 'antd'
import { useAuth } from '../context/AuthContext'
import type { Role } from '../services/types'

/** Requires login. Sends a Super Admin without 2FA to the setup page first. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { state } = useAuth()
  const location = useLocation()

  if (state.status === 'loading') {
    return (
      <Flex align="center" justify="center" style={{ minHeight: '100vh' }}>
        <Spin size="large" />
      </Flex>
    )
  }
  if (state.status === 'anonymous') {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  if (state.me.two_factor_setup_required && location.pathname !== '/setup-2fa') {
    return <Navigate to="/setup-2fa" replace />
  }
  return <>{children}</>
}

/** Hides a page from roles that may not use it. The API enforces the same rule. */
export function RoleGate({ roles, children }: { roles: Role[]; children: ReactNode }) {
  const { hasRole } = useAuth()
  if (!hasRole(...roles)) {
    return <Result status="403" title="No access" subTitle="Your role cannot open this page." />
  }
  return <>{children}</>
}
