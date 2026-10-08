import type { ReactNode } from 'react'
import {
  AlertOutlined,
  BarChartOutlined,
  BellOutlined,
  BankOutlined,
  CalendarOutlined,
  CarOutlined,
  DashboardOutlined,
  EnvironmentOutlined,
  FileSearchOutlined,
  IdcardOutlined,
  NodeIndexOutlined,
  SettingOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons'

export interface MenuEntry {
  path: string
  label: string
  icon: ReactNode
  /** Sprint in docs/SPRINT_PLAN.md that builds this page. */
  sprint: number
}

// Role-based filtering of this list arrives with auth in Sprint 1 (S1-12).
export const menu: MenuEntry[] = [
  { path: '/', label: 'Dashboard', icon: <DashboardOutlined />, sprint: 8 },
  { path: '/live', label: 'Live Tracking', icon: <EnvironmentOutlined />, sprint: 5 },
  { path: '/trips', label: 'Daily Trips', icon: <CalendarOutlined />, sprint: 4 },
  { path: '/routes', label: 'Routes & Stops', icon: <NodeIndexOutlined />, sprint: 2 },
  { path: '/buses', label: 'Buses', icon: <CarOutlined />, sprint: 2 },
  { path: '/drivers', label: 'Drivers', icon: <IdcardOutlined />, sprint: 2 },
  { path: '/students', label: 'Students', icon: <TeamOutlined />, sprint: 3 },
  { path: '/parents', label: 'Parents', icon: <UserOutlined />, sprint: 3 },
  { path: '/announcements', label: 'Announcements', icon: <BellOutlined />, sprint: 7 },
  { path: '/alerts', label: 'Delays & Alerts', icon: <AlertOutlined />, sprint: 7 },
  { path: '/reports', label: 'Reports', icon: <BarChartOutlined />, sprint: 8 },
  { path: '/audit-logs', label: 'Audit Logs', icon: <FileSearchOutlined />, sprint: 8 },
  { path: '/schools', label: 'Schools', icon: <BankOutlined />, sprint: 1 },
  { path: '/settings', label: 'Settings', icon: <SettingOutlined />, sprint: 8 },
]
