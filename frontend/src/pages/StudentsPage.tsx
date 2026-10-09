import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  App,
  Button,
  Col,
  Descriptions,
  Drawer,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Radio,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Timeline,
  Typography,
} from 'antd'
import { PlusOutlined, UploadOutlined } from '@ant-design/icons'
import AssignRouteModal from '../components/AssignRouteModal'
import ListToolbar from '../components/ListToolbar'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import { useAuth } from '../context/AuthContext'
import { useServerList } from '../hooks/useServerList'
import { studentsApi } from '../services/people'
import { routesApi } from '../services/transport'
import type {
  BusRoute,
  Student,
  StudentAssignment,
  StudentInput,
  StudentListParams,
} from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

export default function StudentsPage() {
  return <RequireSchool>{(schoolId) => <Students schoolId={schoolId} />}</RequireSchool>
}

function stopLabel(a: StudentAssignment | null, which: 'pickup_stop' | 'drop_stop') {
  const s = a?.[which]
  return s ? `${s.sequence}. ${s.name}` : '—'
}

function Students({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const { hasRole } = useAuth()
  // Transport Managers have a read-only view (permission matrix).
  const canEdit = hasRole('super_admin', 'school_admin')
  const fetcher = useCallback((p: StudentListParams) => studentsApi.list(schoolId, p), [schoolId])
  const list = useServerList(fetcher)
  const [classes, setClasses] = useState<string[]>([])
  const [routes, setRoutes] = useState<BusRoute[]>([])
  const [editing, setEditing] = useState<Student | 'new' | null>(null)
  const [assigning, setAssigning] = useState<Student | null>(null)
  const [viewing, setViewing] = useState<string | null>(null)

  useEffect(() => {
    studentsApi
      .classes(schoolId)
      .then(setClasses)
      .catch(() => setClasses([]))
    routesApi
      .list(schoolId, { page_size: 100 })
      .then((r) => setRoutes(r.data))
      .catch(() => setRoutes([]))
  }, [schoolId, list.meta.total]) // refresh filter options when students are added

  async function toggleStatus(s: Student) {
    const next = s.status === 'active' ? 'inactive' : 'active'
    try {
      await studentsApi.setStatus(schoolId, s.id, next)
      message.success(`${s.name} is now ${next}.`)
      list.reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Students"
        subtitle={canEdit ? undefined : 'View only'}
        extra={
          canEdit && (
            <Space wrap>
              <Button icon={<UploadOutlined />} onClick={() => navigate('/students/import')}>
                Import CSV
              </Button>
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
                Add student
              </Button>
            </Space>
          )
        }
      />
      <ListToolbar
        placeholder="Search name or admission no."
        statuses={['active', 'inactive']}
        status={list.params.status}
        onSearch={(q) => list.update({ q })}
        onStatus={(status) => list.update({ status })}
      >
        <Select
          style={{ width: 130 }}
          value={list.params.class ?? ''}
          onChange={(c) => list.update({ class: c })}
          options={[
            { value: '', label: 'All classes' },
            ...classes.map((c) => ({ value: c, label: `Class ${c}` })),
          ]}
        />
        <Select
          style={{ width: 220 }}
          value={list.params.route_id ?? ''}
          onChange={(route_id) => list.update({ route_id })}
          options={[
            { value: '', label: 'All routes' },
            ...routes.map((r) => ({ value: r.id, label: `${r.code} · ${r.name}` })),
          ]}
        />
        <Select
          style={{ width: 170 }}
          value={list.params.assigned ?? ''}
          onChange={(assigned) => list.update({ assigned })}
          options={[
            { value: '', label: 'Assigned or not' },
            { value: 'yes', label: 'Has a route' },
            { value: 'no', label: 'No route yet' },
          ]}
        />
      </ListToolbar>
      <Table<Student>
        rowKey="id"
        loading={list.loading}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 1100 }}
        onRow={(s) => ({ onDoubleClick: () => setViewing(s.id) })}
        columns={[
          { title: 'Adm. no.', dataIndex: 'admission_no', width: 110 },
          {
            title: 'Name',
            dataIndex: 'name',
            render: (name: string, s) => <a onClick={() => setViewing(s.id)}>{name}</a>,
          },
          {
            title: 'Class',
            width: 90,
            render: (_, s) => [s.class, s.section].filter(Boolean).join('-') || '—',
          },
          {
            title: 'Route',
            render: (_, s) =>
              s.transport_status === 'not_using' ? (
                <Tag>No transport</Tag>
              ) : s.assignment ? (
                <Tag color="gold">{s.assignment.route_code}</Tag>
              ) : (
                <Tag color="red">Not assigned</Tag>
              ),
          },
          { title: 'Pickup stop', render: (_, s) => stopLabel(s.assignment, 'pickup_stop') },
          { title: 'Drop stop', render: (_, s) => stopLabel(s.assignment, 'drop_stop') },
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
                  width: 290,
                  render: (_: unknown, s: Student) => (
                    <Space size="small">
                      <Button
                        size="small"
                        type="primary"
                        ghost
                        disabled={s.transport_status === 'not_using' || s.status !== 'active'}
                        onClick={() => setAssigning(s)}
                      >
                        Route & stop
                      </Button>
                      <Button size="small" onClick={() => setEditing(s)}>
                        Edit
                      </Button>
                      <Popconfirm
                        title={
                          s.status === 'active' ? `Deactivate ${s.name}?` : `Activate ${s.name}?`
                        }
                        onConfirm={() => toggleStatus(s)}
                      >
                        <Button size="small" danger={s.status === 'active'}>
                          {s.status === 'active' ? 'Deactivate' : 'Activate'}
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
        <StudentForm
          schoolId={schoolId}
          student={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={(saved, isNew) => {
            setEditing(null)
            list.reload()
            if (isNew && saved.transport_status === 'uses_transport') setAssigning(saved)
          }}
        />
      )}
      {assigning && (
        <AssignRouteModal
          schoolId={schoolId}
          student={assigning}
          onClose={() => setAssigning(null)}
          onSaved={() => {
            setAssigning(null)
            list.reload()
          }}
        />
      )}
      {viewing && (
        <StudentDrawer schoolId={schoolId} studentId={viewing} onClose={() => setViewing(null)} />
      )}
    </Flex>
  )
}

function StudentForm({
  schoolId,
  student,
  onClose,
  onSaved,
}: {
  schoolId: string
  student: Student | null
  onClose: () => void
  onSaved: (s: Student, isNew: boolean) => void
}) {
  const [form] = Form.useForm<StudentInput>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(v: StudentInput) {
    setSaving(true)
    try {
      const saved = student
        ? await studentsApi.update(schoolId, student.id, v)
        : await studentsApi.create(schoolId, v)
      message.success(
        student ? 'Student updated.' : 'Student added. Now choose their route and stop.',
      )
      onSaved(saved, !student)
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
      title={student ? `Edit ${student.name}` : 'Add student'}
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
        initialValues={student ?? { transport_status: 'uses_transport' }}
        onFinish={save}
      >
        <Row gutter={16}>
          <Col xs={24} md={10}>
            <Form.Item
              name="admission_no"
              label="Admission no."
              rules={[{ required: true, max: 30 }]}
            >
              <Input style={{ textTransform: 'uppercase' }} />
            </Form.Item>
          </Col>
          <Col xs={24} md={14}>
            <Form.Item name="name" label="Student name" rules={[{ required: true, max: 200 }]}>
              <Input />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name="class" label="Class">
              <Input placeholder="5" />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name="section" label="Section">
              <Input placeholder="A" style={{ textTransform: 'uppercase' }} />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item
              name="transport_status"
              label="School transport"
              extra={
                student?.assignment
                  ? 'Choosing "Not using" removes the student from their route.'
                  : undefined
              }
            >
              <Radio.Group
                options={[
                  { value: 'uses_transport', label: 'Uses the school bus' },
                  { value: 'not_using', label: 'Not using' },
                ]}
              />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="notes" label="Notes (not shown to Transport Managers)">
              <Input.TextArea rows={2} />
            </Form.Item>
          </Col>
        </Row>
      </Form>
    </Modal>
  )
}

function StudentDrawer({
  schoolId,
  studentId,
  onClose,
}: {
  schoolId: string
  studentId: string
  onClose: () => void
}) {
  const { message } = App.useApp()
  const [student, setStudent] = useState<Student | null>(null)
  const [history, setHistory] = useState<StudentAssignment[]>([])

  useEffect(() => {
    Promise.all([studentsApi.get(schoolId, studentId), studentsApi.history(schoolId, studentId)])
      .then(([s, h]) => {
        setStudent(s)
        setHistory(h)
      })
      .catch((e: unknown) => message.error(errorMessage(e)))
  }, [schoolId, studentId, message])

  return (
    <Drawer
      open
      onClose={onClose}
      title={student?.name ?? 'Student'}
      size="large"
      loading={!student}
    >
      {student && (
        <Flex vertical gap="large">
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="Admission no.">{student.admission_no}</Descriptions.Item>
            <Descriptions.Item label="Class">
              {[student.class, student.section].filter(Boolean).join('-') || '—'}
            </Descriptions.Item>
            <Descriptions.Item label="Status">
              <StatusTag status={student.status} />
            </Descriptions.Item>
            <Descriptions.Item label="Route">
              {student.assignment
                ? `${student.assignment.route_code} · ${student.assignment.route_name}`
                : student.transport_status === 'not_using'
                  ? 'Not using school transport'
                  : 'Not assigned'}
            </Descriptions.Item>
            <Descriptions.Item label="Pickup stop">
              {stopLabel(student.assignment, 'pickup_stop')}
              {student.assignment?.pickup_stop?.pickup_time &&
                ` at ${student.assignment.pickup_stop.pickup_time}`}
            </Descriptions.Item>
            <Descriptions.Item label="Drop stop">
              {stopLabel(student.assignment, 'drop_stop')}
              {student.assignment?.drop_stop?.drop_time &&
                ` at ${student.assignment.drop_stop.drop_time}`}
            </Descriptions.Item>
            {student.notes !== undefined && (
              <Descriptions.Item label="Notes">{student.notes || '—'}</Descriptions.Item>
            )}
          </Descriptions>

          <div>
            <Typography.Title level={5}>Parents</Typography.Title>
            {student.parents?.length ? (
              student.parents.map((p) => (
                <div key={p.parent_id}>
                  {p.name} ({p.relationship}) · {p.mobile}
                </div>
              ))
            ) : (
              <Typography.Text type="secondary">
                No parent linked. Add one on the Parents page.
              </Typography.Text>
            )}
          </div>

          <div>
            <Typography.Title level={5}>Route history</Typography.Title>
            {history.length === 0 ? (
              <Typography.Text type="secondary">Never assigned.</Typography.Text>
            ) : (
              <Timeline
                items={history.map((h) => ({
                  color: h.ended_at ? 'gray' : 'green',
                  content: (
                    <>
                      <strong>{h.route_code}</strong> · pickup {h.pickup_stop?.name ?? '—'}, drop{' '}
                      {h.drop_stop?.name ?? '—'}
                      <br />
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {new Date(h.assigned_at).toLocaleDateString()} –{' '}
                        {h.ended_at ? new Date(h.ended_at).toLocaleDateString() : 'now'}
                        {h.assigned_by_name && ` · by ${h.assigned_by_name}`}
                      </Typography.Text>
                    </>
                  ),
                }))}
              />
            )}
          </div>
        </Flex>
      )}
    </Drawer>
  )
}
