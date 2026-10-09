import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { App } from 'antd'
import { useAuth } from './AuthContext'
import { useCurrentSchool } from './SchoolContext'
import { alertsApi } from '../services/alerts'
import { LiveSocket } from '../services/live'
import type { Alert } from '../services/types'

interface AlertsValue {
  /** Unresolved emergencies and today's delays, newest first. */
  alerts: Alert[]
  openEmergencies: Alert[]
  reload: () => void
}

const AlertsContext = createContext<AlertsValue>({
  alerts: [],
  openEmergencies: [],
  reload: () => {},
})

/**
 * Staff alerts for the current school, kept live over the WebSocket. A new
 * emergency pops up wherever the user is in the admin web.
 */
export function AlertsProvider({ children }: { children: ReactNode }) {
  const { notification } = App.useApp()
  const { hasRole } = useAuth()
  const { schoolId } = useCurrentSchool()
  const isSuper = hasRole('super_admin')
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [version, setVersion] = useState(0)

  useEffect(() => {
    if (!schoolId) return
    let cancelled = false
    alertsApi
      .list(schoolId)
      .then((a) => !cancelled && setAlerts(a))
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [schoolId, version])

  useEffect(() => {
    if (!schoolId) return
    const socket = new LiveSocket(
      isSuper ? schoolId : null,
      (m) => {
        if (m.type !== 'alert') return
        const a = m.alert
        setAlerts((list) => [a, ...list.filter((x) => x.id !== a.id)])
        if (a.kind === 'emergency' && a.status === 'open') {
          notification.error({
            message: `Emergency on ${a.route_code} (${a.bus_number})`,
            description: `${a.message} · ${a.driver_name}`,
            duration: 0,
          })
        }
      },
      () => {},
    )
    socket.subscribeSchool()
    return () => socket.close()
  }, [schoolId, isSuper, notification])

  const reload = useCallback(() => setVersion((v) => v + 1), [])
  const value = useMemo<AlertsValue>(
    () => ({
      alerts,
      openEmergencies: alerts.filter((a) => a.kind === 'emergency' && a.status !== 'resolved'),
      reload,
    }),
    [alerts, reload],
  )
  return <AlertsContext.Provider value={value}>{children}</AlertsContext.Provider>
}

export function useAlerts(): AlertsValue {
  return useContext(AlertsContext)
}
