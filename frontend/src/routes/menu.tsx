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
  UserSwitchOutlined,
} from '@ant-design/icons'
import type { Role } from '../services/types'

export interface MenuEntry {
  path: string
  label: string
  icon: ReactNode
  /** Sprint in docs/SPRINT_PLAN.md that builds this page. */
  sprint: number
  /** Who sees it (CLAUDE.md permission matrix). The backend enforces the same rules. */
  roles: Role[]
}

const ALL: Role[] = ['super_admin', 'school_admin', 'transport_manager']
const ADMINS: Role[] = ['super_admin', 'school_admin']

export const menu: MenuEntry[] = [
  { path: '/', label: 'Dashboard', icon: <DashboardOutlined />, sprint: 8, roles: ALL },
  { path: '/live', label: 'Live Tracking', icon: <EnvironmentOutlined />, sprint: 5, roles: ALL },
  { path: '/trips', label: 'Daily Trips', icon: <CalendarOutlined />, sprint: 4, roles: ALL },
  { path: '/routes', label: 'Routes & Stops', icon: <NodeIndexOutlined />, sprint: 2, roles: ALL },
  { path: '/buses', label: 'Buses', icon: <CarOutlined />, sprint: 2, roles: ALL },
  { path: '/drivers', label: 'Drivers', icon: <IdcardOutlined />, sprint: 2, roles: ALL },
  { path: '/students', label: 'Students', icon: <TeamOutlined />, sprint: 3, roles: ALL },
  { path: '/parents', label: 'Parents', icon: <UserOutlined />, sprint: 3, roles: ALL },
  { path: '/announcements', label: 'Announcements', icon: <BellOutlined />, sprint: 7, roles: ALL },
  { path: '/alerts', label: 'Delays & Alerts', icon: <AlertOutlined />, sprint: 7, roles: ALL },
  { path: '/reports', label: 'Reports', icon: <BarChartOutlined />, sprint: 8, roles: ALL },
  { path: '/audit-logs', label: 'Audit Logs', icon: <FileSearchOutlined />, sprint: 8, roles: ALL },
  { path: '/users', label: 'School Users', icon: <UserSwitchOutlined />, sprint: 1, roles: ADMINS },
  { path: '/schools', label: 'Schools', icon: <BankOutlined />, sprint: 1, roles: ['super_admin'] },
  { path: '/settings', label: 'Settings', icon: <SettingOutlined />, sprint: 8, roles: ADMINS },
]
