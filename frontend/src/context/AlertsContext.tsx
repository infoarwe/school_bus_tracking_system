import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { App } from 'antd'
import { useAuth } from './AuthContext'
import { useCurrentSchool } from './SchoolContext'
import { alertsApi } from '../services/alerts'
import { LiveSocket } from '../services/live'
import { staffNotificationsApi } from '../services/staffNotifications'
import type { Alert, StaffNotice } from '../services/types'

interface AlertsValue {
  /** Unresolved emergencies and today's delays, newest first. */
  alerts: Alert[]
  openEmergencies: Alert[]
  reload: () => void
  /** The staff inbox: drivers taking or releasing routes. */
  inbox: StaffNotice[]
  inboxUnread: number
  markInboxRead: (ids: string[]) => Promise<void>
}

const AlertsContext = createContext<AlertsValue>({
  alerts: [],
  openEmergencies: [],
  reload: () => {},
  inbox: [],
  inboxUnread: 0,
  markInboxRead: async () => {},
})

// The WebSocket pushes new items; this poll only catches up after a missed event.
const INBOX_POLL_MS = 120000

/**
 * Staff alerts and inbox for the current school, kept live over one WebSocket.
 * A new emergency pops up wherever the user is in the admin web; so does a driver
 * taking or releasing a route.
 */
export function AlertsProvider({ children }: { children: ReactNode }) {
  const { notification } = App.useApp()
  const { hasRole } = useAuth()
  const { schoolId } = useCurrentSchool()
  const isSuper = hasRole('super_admin')
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [version, setVersion] = useState(0)
  const [inbox, setInbox] = useState<{ items: StaffNotice[]; unread: number }>({
    items: [],
    unread: 0,
  })
  const [inboxVersion, setInboxVersion] = useState(0)

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
    let cancelled = false
    staffNotificationsApi
      .list()
      .then((r) => !cancelled && setInbox({ items: r.data, unread: r.meta.unread }))
      .catch(() => {}) // the bell simply shows nothing new
    return () => {
      cancelled = true
    }
  }, [inboxVersion])

  useEffect(() => {
    const t = setInterval(() => setInboxVersion((v) => v + 1), INBOX_POLL_MS)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    if (!schoolId) return
    const socket = new LiveSocket(
      isSuper ? schoolId : null,
      (m) => {
        if (m.type === 'staff_notification') {
          setInboxVersion((v) => v + 1)
          notification.info({
            message: m.notification.title,
            description: m.notification.body,
            placement: 'bottomRight',
          })
          return
        }
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
  const markInboxRead = useCallback(async (ids: string[]) => {
    if (ids.length === 0) return
    await staffNotificationsApi.markRead(ids)
    setInboxVersion((v) => v + 1)
  }, [])
  const value = useMemo<AlertsValue>(
    () => ({
      alerts,
      openEmergencies: alerts.filter((a) => a.kind === 'emergency' && a.status !== 'resolved'),
      reload,
      inbox: inbox.items,
      inboxUnread: inbox.unread,
      markInboxRead,
    }),
    [alerts, reload, inbox, markInboxRead],
  )
  return <AlertsContext.Provider value={value}>{children}</AlertsContext.Provider>
}

export function useAlerts(): AlertsValue {
  return useContext(AlertsContext)
}
