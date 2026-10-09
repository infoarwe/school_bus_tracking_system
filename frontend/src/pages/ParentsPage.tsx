import { useCallback, useEffect, useRef, useState } from 'react'
import {
  App,
  Button,
  Col,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import ListToolbar from '../components/ListToolbar'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import { useAuth } from '../context/AuthContext'
import { useServerList } from '../hooks/useServerList'
import { parentsApi, studentsApi } from '../services/people'
import type { ChildLink, ListParams, Parent, ParentInput } from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const RELATIONSHIPS = [
  { value: 'father', label: 'Father' },
  { value: 'mother', label: 'Mother' },
  { value: 'guardian', label: 'Guardian' },
]

export default function ParentsPage() {
  return <RequireSchool>{(schoolId) => <Parents schoolId={schoolId} />}</RequireSchool>
}

function Parents({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const { hasRole } = useAuth()
  const canEdit = hasRole('super_admin', 'school_admin')
  const fetcher = useCallback((p: ListParams) => parentsApi.list(schoolId, p), [schoolId])
  const list = useServerList(fetcher)
  const [editing, setEditing] = useState<Parent | 'new' | null>(null)

  async function toggleStatus(p: Parent) {
    const next = p.status === 'active' ? 'inactive' : 'active'
    try {
      await parentsApi.setStatus(schoolId, p.id, next)
      message.success(`${p.name} is now ${next}.`)
      list.reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Parents"
        subtitle={
          canEdit
            ? 'Parents log in to the Parent app with their mobile number and an OTP, and see only their linked children.'
            : 'View only'
        }
        extra={
          canEdit && (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
              Add parent
            </Button>
          )
        }
      />
      <ListToolbar
        placeholder="Search parent, mobile or child"
        statuses={['active', 'inactive']}
        status={list.params.status}
        onSearch={(q) => list.update({ q })}
        onStatus={(status) => list.update({ status })}
      />
      <Table<Parent>
        rowKey="id"
        loading={list.loading}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 900 }}
        columns={[
          { title: 'Name', dataIndex: 'name' },
          { title: 'Mobile', dataIndex: 'mobile' },
          {
            title: 'Children',
            dataIndex: 'children',
            render: (children: ChildLink[]) =>
              children.length === 0 ? (
                <Tag color="red">None linked</Tag>
              ) : (
                <Space size={4} wrap>
                  {children.map((c) => (
                    <Tag key={c.student_id}>
                      {c.name} · {c.class}
                      {c.section && `-${c.section}`}
                    </Tag>
                  ))}
                </Space>
              ),
          },
          {
            title: 'App login',
            dataIndex: 'last_login_at',
            render: (v: string | null) => (v ? new Date(v).toLocaleDateString() : 'Never'),
          },
          {
            title: 'Status',
            dataIndex: 'status',
            width: 100,
            render: (v: string) => <StatusTag status={v} />,
          },
          ...(canEdit
            ? [
                {
                  title: 'Actions',
                  width: 190,
                  render: (_: unknown, p: Parent) => (
                    <Space size="small">
                      <Button size="small" onClick={() => setEditing(p)}>
                        Edit
                      </Button>
                      <Popconfirm
                        title={
                          p.status === 'active' ? `Deactivate ${p.name}?` : `Activate ${p.name}?`
                        }
                        description={
                          p.status === 'active'
                            ? 'They are logged out of the Parent app.'
                            : undefined
                        }
                        onConfirm={() => toggleStatus(p)}
                      >
                        <Button size="small" danger={p.status === 'active'}>
                          {p.status === 'active' ? 'Deactivate' : 'Activate'}
                        </Button>
                      </Popconfirm>
                    </Space>
                  ),
                },
              ]
            : []),
        ]}
      />
      {editing && (
        <ParentForm
          schoolId={schoolId}
          parent={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            list.reload()
          }}
        />
      )}
    </Flex>
  )
}

function ParentForm({
  schoolId,
  parent,
  onClose,
  onSaved,
}: {
  schoolId: string
  parent: Parent | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<ParentInput>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(v: ParentInput) {
    setSaving(true)
    try {
      const input = { ...v, children: v.children ?? [] }
      if (parent) await parentsApi.update(schoolId, parent.id, input)
      else await parentsApi.create(schoolId, input)
      message.success(
        parent ? 'Parent updated.' : 'Parent added. They can now log in to the Parent app.',
      )
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
      title={parent ? `Edit ${parent.name}` : 'Add parent'}
      okText="Save"
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      width={720}
      destroyOnHidden
    >
      <Form
        form={form}
        layout="vertical"
        requiredMark="optional"
        onFinish={save}
        initialValues={
          parent
            ? {
                ...parent,
                children: parent.children.map((c) => ({
                  student_id: c.student_id,
                  relationship: c.relationship,
                })),
              }
            : { children: [] }
        }
      >
        <Row gutter={16}>
          <Col xs={24} md={12}>
            <Form.Item name="name" label="Name" rules={[{ required: true, max: 200 }]}>
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="mobile"
              label="Mobile (app login)"
              rules={[{ required: true }]}
              extra={parent ? 'Changing this changes the number they log in with.' : undefined}
            >
              <Input inputMode="tel" placeholder="98765 43210" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="alternate_mobile" label="Alternate mobile">
              <Input inputMode="tel" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="email" label="Email" rules={[{ type: 'email' }]}>
              <Input />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="address" label="Address">
              <Input.TextArea rows={2} />
            </Form.Item>
          </Col>
        </Row>

        <Typography.Title level={5}>Children</Typography.Title>
        <Form.List name="children">
          {(fields, { add, remove }) => (
            <>
              {fields.map((field) => (
                <Flex key={field.key} gap="small" align="start">
                  <Form.Item
                    name={[field.name, 'student_id']}
                    rules={[{ required: true, message: 'Choose a student' }]}
                    style={{ flex: 1 }}
                  >
                    <StudentPicker schoolId={schoolId} known={parent?.children ?? []} />
                  </Form.Item>
                  <Form.Item name={[field.name, 'relationship']} initialValue="guardian">
                    <Select options={RELATIONSHIPS} style={{ width: 120 }} />
                  </Form.Item>
                  <Button
                    icon={<DeleteOutlined />}
                    aria-label="Remove child"
                    onClick={() => remove(field.name)}
                  />
                </Flex>
              ))}
              <Button icon={<PlusOutlined />} onClick={() => add()} block type="dashed">
                Link a child
              </Button>
            </>
          )}
        </Form.List>
      </Form>
    </Modal>
  )
}

function studentLabel(s: { name: string; admission_no: string; class: string; section: string }) {
  return `${s.name} (${s.admission_no}) · ${[s.class, s.section].filter(Boolean).join('-')}`
}

/** Searchable student select; `known` labels the already-linked children before any search. */
function StudentPicker({
  schoolId,
  known,
  value,
  onChange,
}: {
  schoolId: string
  known: ChildLink[]
  value?: string
  onChange?: (v: string) => void
}) {
  const [options, setOptions] = useState(
    known.map((c) => ({ value: c.student_id, label: studentLabel(c) })),
  )
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const search = useCallback(
    (q: string) => {
      clearTimeout(timer.current)
      timer.current = setTimeout(() => {
        studentsApi
          .list(schoolId, { q, page_size: 20, status: 'active' })
          .then((res) => setOptions(res.data.map((s) => ({ value: s.id, label: studentLabel(s) }))))
          .catch(() => {})
      }, 300)
    },
    [schoolId],
  )

  useEffect(() => {
    if (!value) search('')
    return () => clearTimeout(timer.current)
    // Load a first page once for a new, empty row.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <Select
      showSearch={{ filterOption: false, onSearch: search }}
      placeholder="Search name or admission no."
      value={value}
      onChange={onChange}
      options={options}
    />
  )
}
