import { useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Layout, Menu, Typography } from 'antd'
import { menu } from '../routes/menu'

const { Sider, Header, Content } = Layout

export default function AdminLayout() {
  const [collapsed, setCollapsed] = useState(false)
  const navigate = useNavigate()
  const { pathname } = useLocation()

  const selected = menu
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
          items={menu.map((m) => ({ key: m.path, icon: m.icon, label: m.label }))}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header className="app-header">
          <Typography.Text strong>Admin</Typography.Text>
        </Header>
        <Content className="app-content">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
