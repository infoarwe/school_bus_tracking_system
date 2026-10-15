import { useState } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import {
  Avatar,
  Badge,
  Breadcrumb,
  Button,
  Drawer,
  Dropdown,
  Flex,
  Grid,
  Layout,
  Popover,
  Select,
  Typography,
} from 'antd'
import {
  BellOutlined,
  LogoutOutlined,
  MenuOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { AlertsProvider, useAlerts } from '../context/AlertsContext'
import { useCurrentSchool } from '../context/SchoolContext'
import { menu } from '../routes/menu'
import SidebarNav from './SidebarNav'
import { roleLabels } from '../services/types'

const { Sider, Header, Content } = Layout

const COLLAPSED_KEY = 'sbt.sidebarCollapsed'
function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

/** Labels for pages below a menu entry (e.g. /routes/:id). */
const SUB_PAGES: { pattern: RegExp; label: string }[] = [
  { pattern: /^\/routes\/[^/]+/, label: 'Route details' },
  { pattern: /^\/drivers\/[^/]+/, label: 'Driver details' },
  { pattern: /^\/buses\/[^/]+/, label: 'Bus details' },
  { pattern: /^\/students\/import/, label: 'Import CSV' },
  { pattern: /^\/account/, label: 'My account' },
]

export default function AdminLayout() {
  return (
    <AlertsProvider>
      <AdminShell />
    </AlertsProvider>
  )
}

/**
 * The bell: open emergencies (red) and the staff inbox (drivers taking or releasing
 * routes). The badge counts both; the panel lists the inbox.
 */
function AlertBell() {
  const navigate = useNavigate()
  const { openEmergencies, inbox, inboxUnread, markInboxRead } = useAlerts()
  const [open, setOpen] = useState(false)
  const count = openEmergencies.length + inboxUnread
  const label = [
    openEmergencies.length &&
      `${openEmergencies.length} open emergenc${openEmergencies.length === 1 ? 'y' : 'ies'}`,
    inboxUnread && `${inboxUnread} unread notification${inboxUnread === 1 ? '' : 's'}`,
  ]
    .filter(Boolean)
    .join(', ')

  const go = (path: string, ids: string[] = []) => {
    setOpen(false)
    void markInboxRead(ids).catch(() => {})
    navigate(path)
  }

  const panel = (
    <div className="bell-panel">
      {openEmergencies.length > 0 && (
        <button type="button" className="bell-panel__emergency" onClick={() => go('/alerts')}>
          {label.split(',')[0]}: open Delays &amp; Alerts
        </button>
      )}
      <Flex justify="space-between" align="center" className="bell-panel__head">
        <Typography.Text strong>Notifications</Typography.Text>
        {inboxUnread > 0 && (
          <Button
            type="link"
            size="small"
            onClick={() =>
              void markInboxRead(inbox.filter((n) => !n.read_at).map((n) => n.id)).catch(() => {})
            }
          >
            Mark all read
          </Button>
        )}
      </Flex>
      {inbox.length === 0 ? (
        <Typography.Paragraph type="secondary" className="bell-panel__empty">
          Nothing yet. You are told here when a driver takes or gives back a route.
        </Typography.Paragraph>
      ) : (
        <div className="bell-panel__list">
          {inbox.slice(0, 8).map((n) => (
            <button
              type="button"
              key={n.id}
              className={`bell-item${n.read_at ? '' : ' bell-item--unread'}`}
              onClick={() =>
                go(n.data.trip_date ? `/trips?date=${n.data.trip_date}` : '/trips', [n.id])
              }
            >
              <span className="bell-item__title">{n.title}</span>
              <span className="bell-item__body">{n.body}</span>
              <span className="bell-item__time">{new Date(n.created_at).toLocaleString()}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )

  return (
    <Popover
      content={panel}
      trigger="click"
      open={open}
      onOpenChange={setOpen}
      placement="bottomRight"
      arrow={false}
    >
      <Badge count={count} size="small">
        <Button
          type="text"
          className="header-icon-button"
          icon={<BellOutlined />}
          danger={openEmergencies.length > 0}
          aria-label={label ? `Notifications: ${label}` : 'Notifications'}
          title={label || 'Notifications'}
        />
      </Badge>
    </Popover>
  )
}

function AdminShell() {
  const [collapsed, setCollapsedState] = useState(readCollapsed)
  const setCollapsed = (v: boolean) => {
    setCollapsedState(v)
    try {
      localStorage.setItem(COLLAPSED_KEY, v ? '1' : '0')
    } catch {
      // storage unavailable: the choice lasts for this page only
    }
  }
  const [drawerOpen, setDrawerOpen] = useState(false)
  const screens = Grid.useBreakpoint()
  const mobile = !screens.lg
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const { me, hasRole, logout } = useAuth()
  const { school, schoolId, schools, setSchoolId } = useCurrentSchool()

  const visible = menu.filter((m) => hasRole(...m.roles))
  const current = visible.find((m) =>
    m.path === '/' ? pathname === '/' : pathname === m.path || pathname.startsWith(m.path + '/'),
  )
  const sub = SUB_PAGES.find((p) => p.pattern.test(pathname))

  return (
    <Layout className="app-shell">
      <a href="#main" className="skip-link">
        Skip to content
      </a>
      {mobile ? (
        <Drawer
          placement="left"
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          size={264}
          closable={false}
          className="sidenav-drawer"
          styles={{ body: { padding: 0 } }}
        >
          <SidebarNav
            entries={visible}
            collapsed={false}
            onClose={() => setDrawerOpen(false)}
            onNavigate={() => setDrawerOpen(false)}
          />
        </Drawer>
      ) : (
        <Sider
          width={236}
          collapsedWidth={72}
          collapsible
          collapsed={collapsed}
          trigger={null}
          className="app-sider"
          // Stays in view while the page scrolls; antd animates the width change.
          style={{ position: 'sticky', top: 0, height: '100vh' }}
        >
          <SidebarNav
            entries={visible}
            collapsed={collapsed}
            onToggle={() => setCollapsed(!collapsed)}
          />
        </Sider>
      )}
      <Layout>
        <Header className="app-header">
          <Button
            type="text"
            icon={
              mobile ? <MenuOutlined /> : collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />
            }
            className="header-icon-button"
            aria-label={mobile ? 'Open menu' : collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            aria-expanded={mobile ? drawerOpen : !collapsed}
            onClick={() => (mobile ? setDrawerOpen(true) : setCollapsed(!collapsed))}
          />
          <Flex align="center" gap="middle" style={{ flex: 1, minWidth: 0 }}>
            {hasRole('super_admin') ? (
              <Select
                showSearch={{ optionFilterProp: 'label' }}
                allowClear
                aria-label="School to manage"
                placeholder="Select a school to manage"
                style={{ width: 280, maxWidth: '100%' }}
                value={schoolId}
                onChange={(id) => setSchoolId(id ?? null)}
                options={schools.map((s) => ({ value: s.id, label: `${s.name} (${s.code})` }))}
              />
            ) : (
              <Typography.Text strong ellipsis>
                {school?.name}
              </Typography.Text>
            )}
          </Flex>
          {schoolId && <AlertBell />}
          <Dropdown
            menu={{
              items: [
                { key: 'account', icon: <UserOutlined />, label: 'My account' },
                { type: 'divider' },
                { key: 'logout', icon: <LogoutOutlined />, label: 'Log out', danger: true },
              ],
              onClick: ({ key }) => {
                if (key === 'account') navigate('/account')
                if (key === 'logout') void logout()
              },
            }}
          >
            <Button type="text" className="user-button" aria-label="Account menu">
              <Avatar size="small" icon={<UserOutlined />} />
              {screens.md && (
                <Flex vertical align="flex-start" style={{ lineHeight: 1.2 }}>
                  <Typography.Text strong>{me?.user.name}</Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {me && roleLabels[me.user.role]}
                  </Typography.Text>
                </Flex>
              )}
            </Button>
          </Dropdown>
        </Header>
        <Content id="main" tabIndex={-1} className="app-content">
          {pathname !== '/' && (current || sub) && (
            <Breadcrumb
              className="app-breadcrumb"
              items={[
                { title: <Link to="/">Home</Link> },
                ...(current && current.path !== '/'
                  ? [
                      {
                        title: sub ? <Link to={current.path}>{current.label}</Link> : current.label,
                      },
                    ]
                  : []),
                ...(sub ? [{ title: sub.label }] : []),
              ]}
            />
          )}
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
