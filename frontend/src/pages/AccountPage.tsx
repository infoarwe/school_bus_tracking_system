import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Alert,
  App,
  Avatar,
  Button,
  Card,
  Col,
  Divider,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Table,
  Tag,
  Typography,
  theme,
} from 'antd'
import {
  CrownOutlined,
  DesktopOutlined,
  EditOutlined,
  InfoCircleOutlined,
  LogoutOutlined,
  MobileOutlined,
  SafetyCertificateOutlined,
  TabletOutlined,
  UserOutlined,
} from '@ant-design/icons'
import PageHeader from '../components/PageHeader'
import { timeAgo } from '../components/dashboard/time'
import { useAuth } from '../context/AuthContext'
import { authApi } from '../services/auth'
import { roleLabels, type Session } from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

/** A session used within the access-token lifetime counts as active. */
const ACTIVE_MS = 15 * 60 * 1000

const initials = (name: string) =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('')

/** "Chrome • Windows" from a user agent; good enough to recognise your own devices. */
function describeAgent(ua: string): { label: string; kind: 'desktop' | 'mobile' | 'tablet' } {
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\/|Opera/.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : /okhttp|Dalvik/i.test(ua)
              ? 'Android app'
              : ''
  const os = /Windows/.test(ua)
    ? 'Windows'
    : /Android/.test(ua)
      ? 'Android'
      : /iPhone|iPad|iOS/.test(ua)
        ? 'iOS'
        : /Mac OS X|Macintosh/.test(ua)
          ? 'macOS'
          : /Linux/.test(ua)
            ? 'Linux'
            : ''
  const kind = /iPad|Tablet/.test(ua)
    ? 'tablet'
    : /Mobi|Android|iPhone/.test(ua)
      ? 'mobile'
      : 'desktop'
  return { label: [browser, os].filter(Boolean).join(' • '), kind }
}

const kindIcon = {
  desktop: <DesktopOutlined />,
  mobile: <MobileOutlined />,
  tablet: <TabletOutlined />,
}

function InfoRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Row gutter={16} style={{ padding: '6px 0' }}>
      <Col xs={24} sm={8}>
        <Typography.Text type="secondary">{label}</Typography.Text>
      </Col>
      <Col xs={24} sm={16}>
        {children}
      </Col>
    </Row>
  )
}

function SectionTitle({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  const { token } = theme.useToken()
  return (
    <Flex align="center" gap={12}>
      <Flex
        align="center"
        justify="center"
        style={{
          width: 36,
          height: 36,
          borderRadius: token.borderRadiusLG,
          background: token.colorPrimaryBg,
          color: token.colorPrimary,
          fontSize: 18,
        }}
      >
        {icon}
      </Flex>
      <Typography.Title level={5} style={{ margin: 0 }}>
        {children}
      </Typography.Title>
    </Flex>
  )
}

export default function AccountPage() {
  const { me, reloadMe } = useAuth()
  const { message } = App.useApp()
  const { token } = theme.useToken()
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [reloadKey, setReloadKey] = useState(0)
  // When the list was loaded: "Active now" and "Idle" are relative to it.
  const [now, setNow] = useState(() => Date.now())
  const [editing, setEditing] = useState(false)
  const [changingPassword, setChangingPassword] = useState(false)

  useEffect(() => {
    let cancelled = false
    authApi
      .sessions()
      .then((s) => {
        if (cancelled) return
        setSessions(s)
        setNow(Date.now())
      })
      .catch((e: unknown) => !cancelled && message.error(errorMessage(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [reloadKey, message])

  const reloadSessions = useCallback(() => {
    setLoading(true)
    setReloadKey((k) => k + 1)
  }, [])

  const revoke = useCallback(
    async (id: string) => {
      try {
        await authApi.revokeSession(id)
        message.success('Device logged out.')
        reloadSessions()
      } catch (e) {
        message.error(errorMessage(e))
      }
    },
    [message, reloadSessions],
  )

  if (!me) return null
  const { user, school } = me

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="My account"
        subtitle="View and manage your profile, security settings and devices."
      />

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={15}>
          <Card style={{ height: '100%' }}>
            <Flex gap={32} wrap align="flex-start">
              <Flex vertical align="center" gap="small" style={{ minWidth: 140, margin: '0 auto' }}>
                <Avatar
                  size={112}
                  style={{
                    background: token.colorPrimaryBg,
                    color: token.colorPrimary,
                    fontSize: 40,
                    fontWeight: 600,
                  }}
                >
                  {initials(user.name) || <UserOutlined />}
                </Avatar>
              </Flex>
              <div style={{ flex: '1 1 320px', minWidth: 0 }}>
                <Flex justify="space-between" align="center" wrap gap="small">
                  <SectionTitle icon={<UserOutlined />}>Profile information</SectionTitle>
                  <Button icon={<EditOutlined />} onClick={() => setEditing(true)}>
                    Edit profile
                  </Button>
                </Flex>
                <div style={{ marginTop: 16 }}>
                  <InfoRow label="Full name">
                    <Typography.Text strong>{user.name}</Typography.Text>
                  </InfoRow>
                  <InfoRow label="Role">
                    <Tag
                      color="purple"
                      icon={user.role === 'super_admin' ? <CrownOutlined /> : undefined}
                    >
                      {roleLabels[user.role]}
                    </Tag>
                  </InfoRow>
                  <InfoRow label="Email">{user.email ?? '—'}</InfoRow>
                  <InfoRow label="Mobile">{user.mobile ?? '—'}</InfoRow>
                  <InfoRow label="School">{school?.name ?? 'All schools'}</InfoRow>
                </div>
              </div>
            </Flex>
          </Card>
        </Col>

        <Col xs={24} xl={9}>
          <Card style={{ height: '100%' }}>
            <SectionTitle icon={<SafetyCertificateOutlined />}>Security</SectionTitle>
            <Divider style={{ margin: '16px 0' }} />
            <Flex justify="space-between" align="center" gap="small" wrap>
              <Typography.Text>Password</Typography.Text>
              <Typography.Text type="secondary">••••••••</Typography.Text>
              <Button onClick={() => setChangingPassword(true)}>Change</Button>
            </Flex>
            <Divider style={{ margin: '16px 0' }} />
            <Flex justify="space-between" align="center" gap="small" wrap>
              <Typography.Text>
                Two-factor authentication{' '}
                <Typography.Text type="secondary">
                  <InfoCircleOutlined title="A code from an authenticator app is asked at every login." />
                </Typography.Text>
              </Typography.Text>
              {user.totp_enabled ? (
                <Tag color="green">On</Tag>
              ) : (
                <Flex gap="small" align="center">
                  <Tag>Off</Tag>
                  <Button onClick={() => navigate('/setup-2fa')}>Set up</Button>
                </Flex>
              )}
            </Flex>
          </Card>
        </Col>
      </Row>

      <Card>
        <Flex justify="space-between" align="center" wrap gap="middle" style={{ marginBottom: 16 }}>
          <SectionTitle icon={<DesktopOutlined />}>Logged-in devices</SectionTitle>
          <Alert
            type="info"
            showIcon
            style={{ flex: '0 1 520px' }}
            message="These devices are logged in to your account. If you see one you don't recognise, log it out and change your password."
          />
        </Flex>
        <Table<Session>
          rowKey="id"
          loading={loading}
          dataSource={sessions}
          pagination={false}
          scroll={{ x: 860 }}
          columns={[
            {
              title: 'Device',
              render: (_, s) => {
                const agent = describeAgent(s.user_agent)
                return (
                  <Flex gap={12} align="center">
                    <span style={{ fontSize: 22, color: token.colorTextSecondary }}>
                      {kindIcon[agent.kind]}
                    </span>
                    <div>
                      <Flex gap={8} align="center">
                        <Typography.Text strong>
                          {s.device_name || 'Unknown device'}
                        </Typography.Text>
                        {s.current && <Tag color="blue">This device</Tag>}
                      </Flex>
                      {agent.label && (
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          {agent.label}
                        </Typography.Text>
                      )}
                    </div>
                  </Flex>
                )
              },
            },
            { title: 'IP address', dataIndex: 'ip' },
            {
              title: 'Last active',
              dataIndex: 'last_used_at',
              render: (v: string, s) => (
                <div>
                  <div>{new Date(v).toLocaleString()}</div>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {s.current || now - new Date(v).getTime() < ACTIVE_MS
                      ? 'Active now'
                      : timeAgo(v, now)}
                  </Typography.Text>
                </div>
              ),
            },
            {
              title: 'Signed in',
              dataIndex: 'created_at',
              render: (v: string) => new Date(v).toLocaleString(),
            },
            {
              title: 'Status',
              render: (_, s) =>
                s.current || now - new Date(s.last_used_at).getTime() < ACTIVE_MS ? (
                  <Tag color="green">Active</Tag>
                ) : (
                  <Tag>Idle</Tag>
                ),
            },
            {
              title: 'Actions',
              width: 130,
              render: (_, s) =>
                s.current ? (
                  <Typography.Text type="secondary">—</Typography.Text>
                ) : (
                  <Popconfirm title="Log out this device?" onConfirm={() => revoke(s.id)}>
                    <Button danger icon={<LogoutOutlined />}>
                      Log out
                    </Button>
                  </Popconfirm>
                ),
            },
          ]}
        />
      </Card>

      <EditProfileModal
        open={editing}
        name={user.name}
        onClose={() => setEditing(false)}
        onSaved={async () => {
          setEditing(false)
          message.success('Profile updated.')
          await reloadMe()
        }}
      />
      <ChangePasswordModal
        open={changingPassword}
        onClose={() => setChangingPassword(false)}
        onSaved={() => {
          setChangingPassword(false)
          message.success('Password changed. Your other devices have been logged out.')
          reloadSessions()
        }}
      />
    </Flex>
  )
}

function EditProfileModal({
  open,
  name,
  onClose,
  onSaved,
}: {
  open: boolean
  name: string
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<{ name: string }>()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async ({ name }: { name: string }) => {
    setSaving(true)
    setError(null)
    try {
      await authApi.updateMe(name.trim())
      onSaved()
    } catch (e) {
      setError(applyApiErrors(form, e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title="Edit profile"
      open={open}
      onCancel={() => {
        setError(null)
        onClose()
      }}
      onOk={() => form.submit()}
      okText="Save"
      confirmLoading={saving}
      destroyOnHidden
    >
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      <Form
        form={form}
        layout="vertical"
        onFinish={submit}
        preserve={false}
        initialValues={{ name }}
      >
        <Form.Item
          name="name"
          label="Full name"
          rules={[{ required: true, whitespace: true, message: 'Enter your name.' }, { max: 200 }]}
        >
          <Input autoFocus />
        </Form.Item>
        <Typography.Text type="secondary">
          Email and mobile are your login; ask whoever manages your account to change them.
        </Typography.Text>
      </Form>
    </Modal>
  )
}

interface PasswordForm {
  current_password: string
  new_password: string
  confirm: string
}

function ChangePasswordModal({
  open,
  onClose,
  onSaved,
}: {
  open: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<PasswordForm>()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (v: PasswordForm) => {
    setSaving(true)
    setError(null)
    try {
      await authApi.changePassword(v.current_password, v.new_password)
      form.resetFields()
      onSaved()
    } catch (e) {
      setError(applyApiErrors(form, e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title="Change password"
      open={open}
      onCancel={() => {
        setError(null)
        onClose()
      }}
      onOk={() => form.submit()}
      okText="Change password"
      confirmLoading={saving}
      destroyOnHidden
    >
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      <Form form={form} layout="vertical" onFinish={submit} preserve={false}>
        <Form.Item
          name="current_password"
          label="Current password"
          rules={[{ required: true, message: 'Enter your current password.' }]}
        >
          <Input.Password autoComplete="current-password" autoFocus />
        </Form.Item>
        <Form.Item
          name="new_password"
          label="New password"
          extra="At least 8 characters."
          rules={[
            { required: true, message: 'Enter a new password.' },
            { min: 8, message: 'At least 8 characters.' },
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <Form.Item
          name="confirm"
          label="Confirm new password"
          dependencies={['new_password']}
          rules={[
            { required: true, message: 'Type the new password again.' },
            ({ getFieldValue }) => ({
              validator: (_, value) =>
                !value || value === getFieldValue('new_password')
                  ? Promise.resolve()
                  : Promise.reject(new Error('The passwords do not match.')),
            }),
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <Typography.Text type="secondary">
          Your other devices will be logged out. This one stays logged in.
        </Typography.Text>
      </Form>
    </Modal>
  )
}
