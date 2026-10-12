import { api, send } from './api'
import type { StaffNotice } from './types'

/** The signed-in staff member's own inbox (route claims and releases by drivers). */
export const staffNotificationsApi = {
  list: async (pageSize = 20) => {
    const res = await api.get<{ data: StaffNotice[]; meta: { total: number; unread: number } }>(
      '/api/v1/staff/notifications',
      { params: { page_size: pageSize } },
    )
    return res.data
  },
  markRead: (ids: string[]) => send<void>('post', '/api/v1/staff/notifications/read', { ids }),
}
