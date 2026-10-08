import { useCallback, useState } from 'react'
import { App, Button, Col, Dropdown, Flex, Form, Input, Modal, Row, Space, Table, Tag } from 'antd'
import { DownOutlined, PlusOutlined } from '@ant-design/icons'
import ListToolbar from '../components/ListToolbar'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import { useServerList } from '../hooks/useServerList'
import { driversApi } from '../services/transport'
import type { Driver, DriverInput, DriverStatus, ListParams } from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const STATUSES: DriverStatus[] = ['active', 'inactive', 'suspended']

export default function DriversPage() {
  return <RequireSchool>{(schoolId) => <Drivers schoolId={schoolId} />}</RequireSchool>
}

function Drivers({ schoolId }: { schoolId: string }) {
  const { message, modal } = App.useApp()
  const fetcher = useCallback((p: ListParams) => driversApi.list(schoolId, p), [schoolId])
  const list = useServerList(fetcher)
  const [editing, setEditing] = useState<Driver | 'new' | null>(null)

  function changeStatus(d: Driver, status: DriverStatus) {
    const apply = async () => {
      try {
        await driversApi.setStatus(schoolId, d.id, status)
        message.success(`${d.name} is now ${status}.`)
        list.reload()
      } catch (e) {
        message.error(errorMessage(e))
      }
    }
    if (status === 'active') return void apply()
    modal.confirm({
      title: `Mark ${d.name} as ${status}?`,
      content: 'The driver is logged out of the app and cannot log in until reactivated.',
      okText: status === 'suspended' ? 'Suspend' : 'Deactivate',
      okButtonProps: { danger: true },
      onOk: apply,
    })
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Drivers"
        subtitle="Each driver logs in to the Driver app with their mobile number and an OTP."
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
            Add driver
          </Button>
        }
      />
      <ListToolbar
        placeholder="Search name, mobile, licence"
        statuses={STATUSES}
        status={list.params.status}
        onSearch={(q) => list.update({ q })}
        onStatus={(status) => list.update({ status })}
      />
      <Table<Driver>
        rowKey="id"
        loading={list.loading}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 900 }}
        columns={[
          { title: 'Name', dataIndex: 'name' },
          { title: 'Mobile', dataIndex: 'mobile' },
          {
            title: 'Licence',
            render: (_, d) => (
              <Space size={4} wrap>
                {d.license_number || '—'}
                <LicenseExpiry date={d.license_expiry} />
              </Space>
            ),
          },
          {
            title: 'Status',
            dataIndex: 'status',
            width: 120,
            render: (s: DriverStatus) => <StatusTag status={s} />,
          },
          {
            title: 'Last app login',
            dataIndex: 'last_login_at',
            render: (v: string | null) => (v ? new Date(v).toLocaleString() : 'Never'),
          },
          {
            title: 'Actions',
            width: 200,
            render: (_, d) => (
              <Space size="small">
                <Button size="small" onClick={() => setEditing(d)}>
                  Edit
                </Button>
                <Dropdown
                  menu={{
                    items: STATUSES.filter((s) => s !== d.status).map((s) => ({
                      key: s,
                      label: { active: 'Activate', inactive: 'Deactivate', suspended: 'Suspend' }[
                        s
                      ],
                      danger: s !== 'active',
                    })),
                    onClick: ({ key }) => changeStatus(d, key as DriverStatus),
                  }}
                >
                  <Button size="small">
                    Status <DownOutlined />
                  </Button>
                </Dropdown>
              </Space>
            ),
          },
        ]}
      />
      {editing && (
        <DriverForm
          schoolId={schoolId}
          driver={editing === 'new' ? null : editing}
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

// ISO dates compare correctly as strings. Computed at load; fine for an admin page.
const today = new Date().toISOString().slice(0, 10)
const in30Days = new Date(Date.now() + 30 * 86_400_000).toISOString().slice(0, 10)

function LicenseExpiry({ date }: { date: string | null }) {
  if (!date) return null
  if (date < today) return <Tag color="red">Expired {date}</Tag>
  if (date <= in30Days) return <Tag color="orange">Expires {date}</Tag>
  return <Tag>Valid to {date}</Tag>
}

function DriverForm({
  schoolId,
  driver,
  onClose,
  onSaved,
}: {
  schoolId: string
  driver: Driver | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<DriverInput>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(v: DriverInput) {
    setSaving(true)
    try {
      const input = { ...v, license_expiry: v.license_expiry || null }
      if (driver) await driversApi.update(schoolId, driver.id, input)
      else await driversApi.create(schoolId, input)
      message.success(
        driver ? 'Driver updated.' : 'Driver added. They can now log in to the Driver app.',
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
      title={driver ? `Edit ${driver.name}` : 'Add driver'}
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
        initialValues={driver ?? {}}
        onFinish={save}
      >
        <Row gutter={16}>
          <Col xs={24} md={12}>
            <Form.Item name="name" label="Full name" rules={[{ required: true, max: 200 }]}>
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="mobile"
              label="Mobile (app login)"
              rules={[{ required: true }]}
              extra={
                driver ? 'Changing this changes the number the driver logs in with.' : undefined
              }
            >
              <Input inputMode="tel" placeholder="98765 43210" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="license_number" label="Driving licence number">
              <Input style={{ textTransform: 'uppercase' }} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="license_expiry" label="Licence valid until">
              <Input type="date" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="id_proof_type" label="ID proof type">
              <Input placeholder="Aadhaar, Voter ID, …" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="id_proof_number" label="ID proof number">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="emergency_contact_name" label="Emergency contact">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="emergency_contact_phone" label="Emergency contact mobile">
              <Input inputMode="tel" />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="address" label="Address">
              <Input.TextArea rows={2} />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="notes" label="Notes">
              <Input.TextArea rows={2} />
            </Form.Item>
          </Col>
        </Row>
      </Form>
    </Modal>
  )
}
