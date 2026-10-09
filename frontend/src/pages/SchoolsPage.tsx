import { useCallback, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  App,
  Button,
  Checkbox,
  Col,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Space,
  Table,
  Typography,
} from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import ListToolbar from '../components/ListToolbar'
import PageHeader from '../components/PageHeader'
import StatusTag from '../components/StatusTag'
import { useCurrentSchool } from '../context/SchoolContext'
import { useServerList } from '../hooks/useServerList'
import { schoolsApi } from '../services/schools'
import type { School, SchoolInput, WeekDay } from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const WEEK_DAYS: { value: WeekDay; label: string }[] = [
  { value: 'mon', label: 'Mon' },
  { value: 'tue', label: 'Tue' },
  { value: 'wed', label: 'Wed' },
  { value: 'thu', label: 'Thu' },
  { value: 'fri', label: 'Fri' },
  { value: 'sat', label: 'Sat' },
  { value: 'sun', label: 'Sun' },
]

const emptySchool: Partial<SchoolInput> = {
  working_days: ['mon', 'tue', 'wed', 'thu', 'fri'],
  timezone: 'Asia/Kolkata',
}

// Super Admin: manage schools (tenants).
export default function SchoolsPage() {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const { setSchoolId, reloadSchools } = useCurrentSchool()
  const list = useServerList(schoolsApi.list)
  const [editing, setEditing] = useState<School | 'new' | null>(null)

  const { reload: reloadList } = list
  const reload = useCallback(() => {
    reloadList()
    void reloadSchools() // keep the header's school picker in sync
  }, [reloadList, reloadSchools])

  const [deactivating, setDeactivating] = useState<School | null>(null)

  async function toggleStatus(s: School, confirmCode?: string) {
    try {
      await schoolsApi.setStatus(s.id, s.status === 'active' ? 'inactive' : 'active', confirmCode)
      message.success(`${s.name} is now ${s.status === 'active' ? 'inactive' : 'active'}.`)
      reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <Flex vertical gap="middle">
      {deactivating && (
        <ConfirmDeactivate
          school={deactivating}
          onClose={() => setDeactivating(null)}
          onConfirm={async (code) => {
            await toggleStatus(deactivating, code)
            setDeactivating(null)
          }}
        />
      )}
      <PageHeader
        title="Schools"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
            Add school
          </Button>
        }
      />
      <ListToolbar
        placeholder="Search name or code"
        statuses={['active', 'inactive']}
        status={list.params.status}
        onSearch={(q) => list.update({ q })}
        onStatus={(status) => list.update({ status })}
      />
      <Table<School>
        rowKey="id"
        loading={list.loading}
        dataSource={list.rows}
        scroll={{ x: 800 }}
        pagination={list.pagination}
        columns={[
          { title: 'Name', dataIndex: 'name' },
          { title: 'Code', dataIndex: 'code', width: 110 },
          { title: 'City', dataIndex: 'city', width: 140 },
          { title: 'Contact', render: (_, s) => s.contact_phone || s.contact_email || '—' },
          {
            title: 'Status',
            dataIndex: 'status',
            width: 100,
            render: (v: School['status']) => <StatusTag status={v} />,
          },
          {
            title: 'Actions',
            width: 260,
            render: (_, s) => (
              <Space size="small">
                <Button size="small" onClick={() => setEditing(s)}>
                  Edit
                </Button>
                <Button
                  size="small"
                  onClick={() => {
                    setSchoolId(s.id)
                    navigate('/users')
                  }}
                >
                  Users
                </Button>
                {s.status === 'active' ? (
                  <Button size="small" danger onClick={() => setDeactivating(s)}>
                    Deactivate
                  </Button>
                ) : (
                  <Popconfirm
                    title="Activate this school?"
                    description="Its users can log in again."
                    onConfirm={() => toggleStatus(s)}
                  >
                    <Button size="small">Activate</Button>
                  </Popconfirm>
                )}
              </Space>
            ),
          },
        ]}
      />

      {editing && (
        <SchoolForm
          school={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
    </Flex>
  )
}

function SchoolForm({
  school,
  onClose,
  onSaved,
}: {
  school: School | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<SchoolInput>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(values: SchoolInput) {
    setSaving(true)
    try {
      const input = { ...values, transport_config: school?.transport_config ?? {} }
      if (school) await schoolsApi.update(school.id, input)
      else await schoolsApi.create(input)
      message.success(school ? 'School updated.' : 'School created.')
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
      title={school ? `Edit ${school.name}` : 'Add school'}
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
        initialValues={school ?? emptySchool}
        onFinish={save}
        requiredMark="optional"
      >
        <Row gutter={16}>
          <Col xs={24} md={16}>
            <Form.Item name="name" label="School name" rules={[{ required: true, max: 200 }]}>
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item
              name="code"
              label="Code"
              tooltip="Short unique code, e.g. DPS-CBE"
              rules={[
                {
                  required: true,
                  pattern: /^[A-Za-z0-9-]{2,20}$/,
                  message: '2-20 letters, digits or -',
                },
              ]}
            >
              <Input style={{ textTransform: 'uppercase' }} />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="address" label="Address">
              <Input.TextArea rows={2} />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item name="city" label="City">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item name="state" label="State">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item
              name="pincode"
              label="Pincode"
              rules={[{ pattern: /^\d{6}$/, message: '6 digits' }]}
            >
              <Input inputMode="numeric" maxLength={6} />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item name="contact_name" label="Contact person">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item name="contact_phone" label="Contact mobile">
              <Input inputMode="tel" />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item name="contact_email" label="Contact email" rules={[{ type: 'email' }]}>
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={16}>
            <Form.Item name="working_days" label="Working days" rules={[{ required: true }]}>
              <Checkbox.Group options={WEEK_DAYS} />
            </Form.Item>
          </Col>
          <Col xs={24} md={8}>
            <Form.Item name="timezone" label="Time zone" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
          </Col>
        </Row>
      </Form>
    </Modal>
  )
}

/** Critical action: the Super Admin types the school code to confirm (recorded in the audit log). */
function ConfirmDeactivate({
  school,
  onClose,
  onConfirm,
}: {
  school: School
  onClose: () => void
  onConfirm: (code: string) => Promise<void>
}) {
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <Modal
      open
      title={`Deactivate ${school.name}?`}
      okText="Deactivate school"
      okButtonProps={{
        danger: true,
        disabled: code.trim().toUpperCase() !== school.code.toUpperCase(),
      }}
      confirmLoading={busy}
      onCancel={onClose}
      onOk={async () => {
        setBusy(true)
        await onConfirm(code.trim())
        setBusy(false)
      }}
    >
      <Typography.Paragraph>
        Every user of this school (admins, drivers, parents) is logged out and cannot log in, and
        its buses stop being tracked. You can activate it again later.
      </Typography.Paragraph>
      <Typography.Paragraph>
        Type the school code <Typography.Text code>{school.code}</Typography.Text> to confirm.
      </Typography.Paragraph>
      <Input
        value={code}
        onChange={(e) => setCode(e.target.value)}
        autoFocus
        placeholder={school.code}
      />
    </Modal>
  )
}
