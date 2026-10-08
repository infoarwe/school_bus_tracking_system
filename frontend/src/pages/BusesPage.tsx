import { useCallback, useState } from 'react'
import {
  App,
  Button,
  Col,
  Dropdown,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Space,
  Table,
} from 'antd'
import { DownOutlined, PlusOutlined } from '@ant-design/icons'
import ListToolbar from '../components/ListToolbar'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import { useServerList } from '../hooks/useServerList'
import { busesApi } from '../services/transport'
import type { Bus, BusInput, BusStatus, ListParams } from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const STATUSES: BusStatus[] = ['active', 'maintenance', 'inactive']
const statusActions: Record<BusStatus, string> = {
  active: 'Mark active',
  maintenance: 'Send to maintenance',
  inactive: 'Deactivate',
}

export default function BusesPage() {
  return <RequireSchool>{(schoolId) => <Buses schoolId={schoolId} />}</RequireSchool>
}

function Buses({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const fetcher = useCallback((p: ListParams) => busesApi.list(schoolId, p), [schoolId])
  const list = useServerList(fetcher)
  const [editing, setEditing] = useState<Bus | 'new' | null>(null)

  async function changeStatus(b: Bus, status: BusStatus) {
    try {
      await busesApi.setStatus(schoolId, b.id, status)
      message.success(`${b.vehicle_number} is now ${status}.`)
      list.reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Buses"
        subtitle="Only active buses can be assigned to daily trips."
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
            Add bus
          </Button>
        }
      />
      <ListToolbar
        placeholder="Search vehicle number"
        statuses={STATUSES}
        status={list.params.status}
        onSearch={(q) => list.update({ q })}
        onStatus={(status) => list.update({ status })}
      />
      <Table<Bus>
        rowKey="id"
        loading={list.loading}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 800 }}
        columns={[
          { title: 'Vehicle number', dataIndex: 'vehicle_number' },
          { title: 'Seats', dataIndex: 'capacity', width: 90 },
          { title: 'Make / model', dataIndex: 'make_model', render: (v: string) => v || '—' },
          { title: 'GPS device', dataIndex: 'gps_device_id', render: (v: string) => v || '—' },
          {
            title: 'Status',
            dataIndex: 'status',
            width: 130,
            render: (s: BusStatus) => <StatusTag status={s} />,
          },
          {
            title: 'Actions',
            width: 200,
            render: (_, b) => (
              <Space size="small">
                <Button size="small" onClick={() => setEditing(b)}>
                  Edit
                </Button>
                <Dropdown
                  menu={{
                    items: STATUSES.filter((s) => s !== b.status).map((s) => ({
                      key: s,
                      label: statusActions[s],
                    })),
                    onClick: ({ key }) => void changeStatus(b, key as BusStatus),
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
        <BusForm
          schoolId={schoolId}
          bus={editing === 'new' ? null : editing}
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

function BusForm({
  schoolId,
  bus,
  onClose,
  onSaved,
}: {
  schoolId: string
  bus: Bus | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<BusInput>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(v: BusInput) {
    setSaving(true)
    try {
      if (bus) await busesApi.update(schoolId, bus.id, v)
      else await busesApi.create(schoolId, v)
      message.success(bus ? 'Bus updated.' : 'Bus added.')
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
      title={bus ? `Edit ${bus.vehicle_number}` : 'Add bus'}
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
        initialValues={bus ?? { capacity: 40 }}
        onFinish={save}
      >
        <Row gutter={16}>
          <Col xs={24} md={14}>
            <Form.Item name="vehicle_number" label="Vehicle number" rules={[{ required: true }]}>
              <Input placeholder="TN-38-AB-1234" style={{ textTransform: 'uppercase' }} />
            </Form.Item>
          </Col>
          <Col xs={24} md={10}>
            <Form.Item name="capacity" label="Seats" rules={[{ required: true }]}>
              <InputNumber min={1} max={100} style={{ width: '100%' }} />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="make_model" label="Make / model">
              <Input placeholder="Tata Starbus" />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item
              name="gps_device_id"
              label="GPS device ID"
              extra="Optional. Only if the bus has a fixed tracker; otherwise the Driver app's GPS is used."
            >
              <Input />
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
