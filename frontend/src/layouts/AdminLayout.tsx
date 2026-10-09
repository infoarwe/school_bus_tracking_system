import { useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Avatar, Badge, Button, Dropdown, Flex, Layout, Menu, Select, Typography } from 'antd'
import { BellOutlined, LogoutOutlined, UserOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { AlertsProvider, useAlerts } from '../context/AlertsContext'
import { useCurrentSchool } from '../context/SchoolContext'
import { menu } from '../routes/menu'
import { roleLabels } from '../services/types'

const { Sider, Header, Content } = Layout

export default function AdminLayout() {
  return (
    <AlertsProvider>
      <AdminShell />
    </AlertsProvider>
  )
}

/** Red bell with the number of unresolved emergencies; opens Delays & Alerts. */
function AlertBell() {
  const navigate = useNavigate()
  const { openEmergencies, alerts } = useAlerts()
  return (
    <Badge count={openEmergencies.length} size="small">
      <Button
        type="text"
        icon={<BellOutlined />}
        danger={openEmergencies.length > 0}
        aria-label="Alerts"
        title={`${alerts.length} alert(s) today`}
        onClick={() => navigate('/alerts')}
      />
    </Badge>
  )
}

function AdminShell() {
  const [collapsed, setCollapsed] = useState(false)
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const { me, hasRole, logout } = useAuth()
  const { school, schoolId, schools, setSchoolId } = useCurrentSchool()

  const visible = menu.filter((m) => hasRole(...m.roles))
  const selected = visible
    .filter((m) => (m.path === '/' ? pathname === '/' : pathname.startsWith(m.path)))
    .map((m) => m.path)

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} breakpoint="lg">
        <div className="brand">{collapsed ? 'SBT' : 'School Bus Tracking'}</div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={selected}
          items={visible.map((m) => ({ key: m.path, icon: m.icon, label: m.label }))}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header className="app-header">
          <Flex align="center" gap="middle" style={{ flex: 1, minWidth: 0 }}>
            {hasRole('super_admin') ? (
              <Select
                showSearch={{ optionFilterProp: 'label' }}
                allowClear
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
            <Flex align="center" gap="small" style={{ cursor: 'pointer' }}>
              <Avatar icon={<UserOutlined />} />
              <Flex vertical style={{ lineHeight: 1.2 }}>
                <Typography.Text strong>{me?.user.name}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {me && roleLabels[me.user.role]}
                </Typography.Text>
              </Flex>
            </Flex>
          </Dropdown>
        </Header>
        <Content className="app-content">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
