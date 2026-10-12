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
  /** Sidebar section. */
  group: MenuGroup
}

export type MenuGroup = 'Overview' | 'Operations' | 'Transport setup' | 'People' | 'Administration'

const ALL: Role[] = ['super_admin', 'school_admin', 'transport_manager']
const ADMINS: Role[] = ['super_admin', 'school_admin']

export const menu: MenuEntry[] = [
  {
    path: '/',
    label: 'Dashboard',
    icon: <DashboardOutlined />,
    sprint: 8,
    roles: ALL,
    group: 'Overview',
  },
  {
    path: '/live',
    label: 'Live Tracking',
    icon: <EnvironmentOutlined />,
    sprint: 5,
    roles: ALL,
    group: 'Overview',
  },
  {
    path: '/trips',
    label: 'Daily Trips',
    icon: <CalendarOutlined />,
    sprint: 4,
    roles: ALL,
    group: 'Operations',
  },
  {
    path: '/routes',
    label: 'Routes & Stops',
    icon: <NodeIndexOutlined />,
    sprint: 2,
    roles: ALL,
    group: 'Transport setup',
  },
  {
    path: '/buses',
    label: 'Buses',
    icon: <CarOutlined />,
    sprint: 2,
    roles: ALL,
    group: 'Transport setup',
  },
  {
    path: '/drivers',
    label: 'Drivers',
    icon: <IdcardOutlined />,
    sprint: 2,
    roles: ALL,
    group: 'Transport setup',
  },
  {
    path: '/students',
    label: 'Students',
    icon: <TeamOutlined />,
    sprint: 3,
    roles: ALL,
    group: 'People',
  },
  {
    path: '/parents',
    label: 'Parents',
    icon: <UserOutlined />,
    sprint: 3,
    roles: ALL,
    group: 'People',
  },
  {
    path: '/announcements',
    label: 'Announcements',
    icon: <BellOutlined />,
    sprint: 7,
    roles: ALL,
    group: 'Operations',
  },
  {
    path: '/alerts',
    label: 'Delays & Alerts',
    icon: <AlertOutlined />,
    sprint: 7,
    roles: ALL,
    group: 'Operations',
  },
  {
    path: '/reports',
    label: 'Reports',
    icon: <BarChartOutlined />,
    sprint: 8,
    roles: ALL,
    group: 'Administration',
  },
  {
    path: '/audit-logs',
    label: 'Audit Logs',
    icon: <FileSearchOutlined />,
    sprint: 8,
    roles: ALL,
    group: 'Administration',
  },
  {
    path: '/users',
    label: 'School Users',
    icon: <UserSwitchOutlined />,
    sprint: 1,
    roles: ADMINS,
    group: 'Administration',
  },
  {
    path: '/schools',
    label: 'Schools',
    icon: <BankOutlined />,
    sprint: 1,
    roles: ['super_admin'],
    group: 'Administration',
  },
  {
    path: '/settings',
    label: 'Settings',
    icon: <SettingOutlined />,
    sprint: 4,
    roles: ADMINS,
    group: 'Administration',
  },
]
