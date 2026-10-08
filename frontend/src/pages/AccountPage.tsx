import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { App, Button, Card, Descriptions, Flex, Popconfirm, Table, Tag, Typography } from 'antd'
import { useAuth } from '../context/AuthContext'
import { authApi } from '../services/auth'
import { roleLabels, type Session } from '../services/types'
import { errorMessage } from '../utils/formErrors'

export default function AccountPage() {
  const { me } = useAuth()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    authApi
      .sessions()
      .then((s) => !cancelled && setSessions(s))
      .catch((e: unknown) => !cancelled && message.error(errorMessage(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [reloadKey, message])

  const revoke = useCallback(
    async (id: string) => {
      try {
        await authApi.revokeSession(id)
        message.success('Device logged out.')
        setLoading(true)
        setReloadKey((k) => k + 1)
      } catch (e) {
        message.error(errorMessage(e))
      }
    },
    [message],
  )

  if (!me) return null
  const { user, school } = me

  return (
    <Flex vertical gap="middle">
      <Typography.Title level={3} style={{ margin: 0 }}>
        My account
      </Typography.Title>
      <Card title="Profile">
        <Descriptions column={{ xs: 1, md: 2 }} size="small">
          <Descriptions.Item label="Name">{user.name}</Descriptions.Item>
          <Descriptions.Item label="Role">{roleLabels[user.role]}</Descriptions.Item>
          <Descriptions.Item label="Email">{user.email ?? '—'}</Descriptions.Item>
          <Descriptions.Item label="Mobile">{user.mobile ?? '—'}</Descriptions.Item>
          <Descriptions.Item label="School">{school?.name ?? 'All schools'}</Descriptions.Item>
          <Descriptions.Item label="Two-factor authentication">
            {user.totp_enabled ? (
              <Tag color="green">On</Tag>
            ) : (
              <Flex gap="small" align="center">
                <Tag>Off</Tag>
                <Button size="small" onClick={() => navigate('/setup-2fa')}>
                  Set up
                </Button>
              </Flex>
            )}
          </Descriptions.Item>
        </Descriptions>
      </Card>
      <Card title="Logged-in devices">
        <Table<Session>
          rowKey="id"
          loading={loading}
          dataSource={sessions}
          pagination={false}
          scroll={{ x: 700 }}
          columns={[
            {
              title: 'Device',
              render: (_, s) => (
                <>
                  {s.device_name || 'Unknown'} {s.current && <Tag color="blue">This device</Tag>}
                </>
              ),
            },
            { title: 'IP', dataIndex: 'ip' },
            {
              title: 'Last active',
              dataIndex: 'last_used_at',
              render: (v: string) => new Date(v).toLocaleString(),
            },
            {
              title: 'Signed in',
              dataIndex: 'created_at',
              render: (v: string) => new Date(v).toLocaleString(),
            },
            {
              title: '',
              width: 120,
              render: (_, s) =>
                s.current ? null : (
                  <Popconfirm title="Log out this device?" onConfirm={() => revoke(s.id)}>
                    <Button size="small" danger>
                      Log out
                    </Button>
                  </Popconfirm>
                ),
            },
          ]}
        />
      </Card>
    </Flex>
  )
}
