import { useCallback, useState } from 'react'
import { App, Button, Flex, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import ListToolbar from '../components/ListToolbar'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import { useAuth } from '../context/AuthContext'
import { useServerList } from '../hooks/useServerList'
import { roleLabels, type ListParams, type User } from '../services/types'
import { usersApi, type StaffRole, type UserInput } from '../services/users'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

// School Admins and Transport Managers of the current school.
export default function UsersPage() {
  return (
    <RequireSchool>
      {(schoolId, schoolName) => <UsersTable schoolId={schoolId} schoolName={schoolName} />}
    </RequireSchool>
  )
}

function UsersTable({ schoolId, schoolName }: { schoolId: string; schoolName: string }) {
  const { message } = App.useApp()
  const { me, hasRole } = useAuth()
  const fetcher = useCallback(
    (p: ListParams & { role?: StaffRole }) => usersApi.list(schoolId, p),
    [schoolId],
  )
  const list = useServerList(fetcher)
  const [editing, setEditing] = useState<User | 'new' | null>(null)
  const [resetting, setResetting] = useState<User | null>(null)

  // Mirrors the backend rule: Super Admin manages both roles, School Admin only Transport Managers.
  const manageableRoles: StaffRole[] = hasRole('super_admin')
    ? ['school_admin', 'transport_manager']
    : ['transport_manager']
  const canManage = (u: User) =>
    u.id !== me?.user.id && manageableRoles.includes(u.role as StaffRole)

  async function toggleStatus(u: User) {
    const next = u.status === 'active' ? 'suspended' : 'active'
    try {
      await usersApi.setStatus(schoolId, u.id, next)
      message.success(`${u.name} is now ${next}.`)
      list.reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="School users"
        subtitle={schoolName}
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
            Add user
          </Button>
        }
      />
      <ListToolbar
        placeholder="Search name, email, mobile"
        statuses={['active', 'suspended']}
        status={list.params.status}
        onSearch={(q) => list.update({ q })}
        onStatus={(status) => list.update({ status })}
      >
        <Select
          style={{ width: 200 }}
          value={list.params.role ?? ''}
          onChange={(role) => list.update({ role: (role || undefined) as StaffRole | undefined })}
          options={[
            { value: '', label: 'All roles' },
            { value: 'school_admin', label: roleLabels.school_admin },
            { value: 'transport_manager', label: roleLabels.transport_manager },
          ]}
        />
      </ListToolbar>
      <Table<User>
        rowKey="id"
        loading={list.loading}
        dataSource={list.rows}
        scroll={{ x: 900 }}
        pagination={list.pagination}
        columns={[
          { title: 'Name', dataIndex: 'name' },
          { title: 'Email', dataIndex: 'email' },
          { title: 'Mobile', dataIndex: 'mobile', render: (v: string | null) => v ?? '—' },
          {
            title: 'Role',
            dataIndex: 'role',
            render: (r: User['role']) => (
              <Tag color={r === 'school_admin' ? 'blue' : 'purple'}>{roleLabels[r]}</Tag>
            ),
          },
          {
            title: 'Status',
            dataIndex: 'status',
            width: 110,
            render: (v: User['status']) => <StatusTag status={v} />,
          },
          {
            title: 'Last login',
            dataIndex: 'last_login_at',
            render: (v: string | null) => (v ? new Date(v).toLocaleString() : 'Never'),
          },
          {
            title: 'Actions',
            width: 300,
            render: (_, u) =>
              canManage(u) ? (
                <Space size="small">
                  <Button size="small" onClick={() => setEditing(u)}>
                    Edit
                  </Button>
                  <Button size="small" onClick={() => setResetting(u)}>
                    Reset password
                  </Button>
                  <Popconfirm
                    title={u.status === 'active' ? `Suspend ${u.name}?` : `Reactivate ${u.name}?`}
                    description={
                      u.status === 'active' ? 'They are logged out of all devices.' : undefined
                    }
                    onConfirm={() => toggleStatus(u)}
                  >
                    <Button size="small" danger={u.status === 'active'}>
                      {u.status === 'active' ? 'Suspend' : 'Activate'}
                    </Button>
                  </Popconfirm>
                </Space>
              ) : null,
          },
        ]}
      />

      {editing && (
        <UserForm
          schoolId={schoolId}
          user={editing === 'new' ? null : editing}
          roles={manageableRoles}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            list.reload()
          }}
        />
      )}
      {resetting && (
        <ResetPasswordForm
          schoolId={schoolId}
          user={resetting}
          onClose={() => setResetting(null)}
        />
      )}
    </Flex>
  )
}

type UserFormValues = UserInput & { role: StaffRole; password: string }

function UserForm({
  schoolId,
  user,
  roles,
  onClose,
  onSaved,
}: {
  schoolId: string
  user: User | null
  roles: StaffRole[]
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<UserFormValues>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(v: UserFormValues) {
    setSaving(true)
    try {
      const profile = { name: v.name, email: v.email, mobile: v.mobile || null }
      if (user) await usersApi.update(schoolId, user.id, profile)
      else await usersApi.create(schoolId, { ...profile, role: v.role, password: v.password })
      message.success(user ? 'User updated.' : 'User created.')
      onSaved()
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      title={user ? `Edit ${user.name}` : 'Add user'}
      okText="Save"
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Form
        form={form}
        layout="vertical"
        requiredMark="optional"
        initialValues={user ?? { role: roles[roles.length - 1] }}
        onFinish={save}
      >
        <Form.Item name="name" label="Name" rules={[{ required: true, max: 200 }]}>
          <Input />
        </Form.Item>
        <Form.Item name="email" label="Email (login)" rules={[{ required: true, type: 'email' }]}>
          <Input />
        </Form.Item>
        <Form.Item name="mobile" label="Mobile">
          <Input inputMode="tel" placeholder="98765 43210" />
        </Form.Item>
        {!user && (
          <>
            <Form.Item name="role" label="Role" rules={[{ required: true }]}>
              <Select options={roles.map((r) => ({ value: r, label: roleLabels[r] }))} />
            </Form.Item>
            <Form.Item
              name="password"
              label="Initial password"
              extra="Share it with the user securely."
              rules={[{ required: true, min: 8 }]}
            >
              <Input.Password autoComplete="new-password" />
            </Form.Item>
          </>
        )}
      </Form>
    </Modal>
  )
}

function ResetPasswordForm({
  schoolId,
  user,
  onClose,
}: {
  schoolId: string
  user: User
  onClose: () => void
}) {
  const [form] = Form.useForm<{ password: string }>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(v: { password: string }) {
    setSaving(true)
    try {
      await usersApi.resetPassword(schoolId, user.id, v.password)
      message.success(`Password changed. ${user.name} has been logged out of all devices.`)
      onClose()
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      title={`Reset password for ${user.name}`}
      okText="Set password"
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" onFinish={save}>
        <Form.Item name="password" label="New password" rules={[{ required: true, min: 8 }]}>
          <Input.Password autoComplete="new-password" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
