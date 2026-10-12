import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Alert,
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
import ListEmpty from '../components/ListEmpty'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import { useAuth } from '../context/AuthContext'
import { useServerList } from '../hooks/useServerList'
import { parentsApi, studentsApi } from '../services/people'
import type { ChildLink, ListParams, Parent, ParentInput } from '../services/types'
import { mobileDigits } from '../utils/mobile'
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
        // A parent's "children" are students, not nested table rows: turn off antd tree data.
        expandable={{ childrenColumnName: '__no_tree_rows' }}
        rowKey="id"
        loading={list.loading}
        locale={{
          emptyText: (
            <ListEmpty
              list={list}
              empty="No parents yet. Parents are usually added while registering a student."
            />
          ),
        }}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 900 }}
        columns={[
          { title: 'Name', dataIndex: 'name' },
          { title: 'Mobile', dataIndex: 'mobile' },
          {
            title: 'Children',
            dataIndex: 'children',
            render: (children: ChildLink[] | undefined) =>
              !children?.length ? (
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
          key={editing === 'new' ? 'new' : editing.id}
          schoolId={schoolId}
          parent={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onOpenExisting={setEditing}
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
  onOpenExisting,
}: {
  schoolId: string
  parent: Parent | null
  onClose: () => void
  onSaved: () => void
  /** Switch to editing a parent that already has the typed mobile number. */
  onOpenExisting: (p: Parent) => void
}) {
  const [form] = Form.useForm<ParentInput>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)
  const digits = mobileDigits(Form.useWatch('mobile', form))
  const [duplicate, setDuplicate] = useState<{ digits: string; parent: Parent | null } | null>(null)
  const existing =
    digits && duplicate?.digits === digits && duplicate.parent?.id !== parent?.id
      ? duplicate.parent
      : null

  // A mobile number is one login: find a parent who already has it before creating another.
  useEffect(() => {
    if (!digits) return
    let cancelled = false
    const t = setTimeout(() => {
      parentsApi
        .list(schoolId, { q: digits, page_size: 5 })
        .then((res) => {
          const match = res.data.find((p) => mobileDigits(p.mobile) === digits) ?? null
          if (!cancelled) setDuplicate({ digits, parent: match })
        })
        .catch(() => {}) // the server still refuses a duplicate on save
    }, 350)
    return () => {
      cancelled = true
      clearTimeout(t)
    }
  }, [digits, schoolId])

  async function save(v: ParentInput) {
    if (existing) {
      message.error(`${existing.name} already has this mobile number. Open that parent instead.`)
      return
    }
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
              rules={[
                { required: true, message: 'Enter the mobile number.' },
                {
                  validator: (_, v: string | undefined) =>
                    !v || mobileDigits(v)
                      ? Promise.resolve()
                      : Promise.reject(new Error('Enter a valid 10-digit mobile number.')),
                },
              ]}
              extra={parent ? 'Changing this changes the number they log in with.' : undefined}
            >
              <Input inputMode="tel" placeholder="98765 43210" prefix="+91" />
            </Form.Item>
          </Col>
          {existing && (
            <Col span={24}>
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 16 }}
                title={`${existing.name} already uses this mobile number.`}
                description={
                  existing.children.length
                    ? `Linked children: ${existing.children.map((c) => c.name).join(', ')}. Add more children to that parent instead of creating a second account.`
                    : 'Add children to that parent instead of creating a second account.'
                }
                action={
                  <Button size="small" type="primary" onClick={() => onOpenExisting(existing)}>
                    Open {existing.name}
                  </Button>
                }
              />
            </Col>
          )}
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

        <div className="form-section-title">Linked children</div>
        <Typography.Paragraph type="secondary" style={{ marginTop: -4 }}>
          The parent sees each child's route, stop and bus in the Parent app. Routes come from the
          student's assignment, not from the parent.
        </Typography.Paragraph>
        <Form.List name="children">
          {(fields, { add, remove }) => (
            <>
              {fields.map((field) => {
                const studentId = form.getFieldValue(['children', field.name, 'student_id']) as
                  string | undefined
                const linked = parent?.children.find((c) => c.student_id === studentId)
                return (
                  <Flex key={field.key} gap="small" align="start" wrap>
                    <Form.Item
                      name={[field.name, 'student_id']}
                      rules={[{ required: true, message: 'Choose a student' }]}
                      style={{ flex: 1, minWidth: 220 }}
                    >
                      <StudentPicker schoolId={schoolId} known={parent?.children ?? []} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'relationship']} initialValue="guardian">
                      <Select
                        options={RELATIONSHIPS}
                        style={{ width: 120 }}
                        aria-label="Relationship"
                      />
                    </Form.Item>
                    {linked ? (
                      // Unlinking hides the child from this parent's app: confirm first.
                      <Popconfirm
                        title={`Unlink ${linked.name}?`}
                        description="After saving, this parent no longer sees the child in the app."
                        okText="Unlink"
                        okButtonProps={{ danger: true }}
                        onConfirm={() => remove(field.name)}
                      >
                        <Button icon={<DeleteOutlined />} aria-label={`Unlink ${linked.name}`} />
                      </Popconfirm>
                    ) : (
                      <Button
                        icon={<DeleteOutlined />}
                        aria-label="Remove this row"
                        onClick={() => remove(field.name)}
                      />
                    )}
                  </Flex>
                )
              })}
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
