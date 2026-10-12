import { useCallback, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
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
  Tag,
} from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import ListToolbar from '../components/ListToolbar'
import ListEmpty from '../components/ListEmpty'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import { useServerList } from '../hooks/useServerList'
import { routesApi } from '../services/transport'
import type { BusRoute, ListParams, RouteInput } from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

export default function RoutesPage() {
  return <RequireSchool>{(schoolId) => <Routes schoolId={schoolId} />}</RequireSchool>
}

export function TripTypeTags({
  route,
}: {
  route: Pick<BusRoute, 'supports_pickup' | 'supports_drop' | 'supports_outing'>
}) {
  return (
    <Space size={4} wrap>
      {route.supports_pickup && <Tag color="gold">Morning Pickup</Tag>}
      {route.supports_drop && <Tag color="geekblue">Evening Drop</Tag>}
      {route.supports_outing && <Tag color="purple">Special Trip / Outing</Tag>}
    </Space>
  )
}

function Routes({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const fetcher = useCallback((p: ListParams) => routesApi.list(schoolId, p), [schoolId])
  const list = useServerList(fetcher)
  // ?new=1 (dashboard quick action) opens the form straight away.
  const [searchParams, setSearchParams] = useSearchParams()
  const [editing, setEditing] = useState<BusRoute | 'new' | null>(() =>
    searchParams.get('new') === '1' ? 'new' : null,
  )
  const closeForm = () => {
    setEditing(null)
    if (searchParams.has('new')) setSearchParams({}, { replace: true })
  }

  async function toggleStatus(r: BusRoute) {
    const next = r.status === 'active' ? 'inactive' : 'active'
    try {
      await routesApi.setStatus(schoolId, r.id, next)
      message.success(`${r.code} is now ${next}.`)
      list.reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Routes & Stops"
        subtitle="Open a route to add stops and set their order on the map."
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
            Add route
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
      <Table<BusRoute>
        rowKey="id"
        loading={list.loading}
        locale={{
          emptyText: (
            <ListEmpty
              list={list}
              empty="No routes yet. Create a route, then add its stops on the map."
            />
          ),
        }}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 900 }}
        onRow={(r) => ({ onDoubleClick: () => navigate(`/routes/${r.id}`) })}
        columns={[
          { title: 'Code', dataIndex: 'code', width: 110 },
          { title: 'Name', dataIndex: 'name' },
          { title: 'Starts at', dataIndex: 'start_point', render: (v: string) => v || '—' },
          { title: 'Trip types', render: (_, r) => <TripTypeTags route={r} /> },
          {
            title: 'Stops',
            dataIndex: 'stop_count',
            width: 80,
            render: (n: number) => (n === 0 ? <Tag color="red">0</Tag> : n),
          },
          {
            title: 'Status',
            dataIndex: 'status',
            width: 110,
            render: (s: string) => <StatusTag status={s} />,
          },
          {
            title: 'Actions',
            width: 260,
            render: (_, r) => (
              <Space size="small">
                <Button
                  size="small"
                  type="primary"
                  ghost
                  onClick={() => navigate(`/routes/${r.id}`)}
                >
                  Stops & map
                </Button>
                <Button size="small" onClick={() => setEditing(r)}>
                  Edit
                </Button>
                <Popconfirm
                  title={r.status === 'active' ? `Deactivate ${r.code}?` : `Activate ${r.code}?`}
                  description={
                    r.status === 'active' ? 'It can no longer be used for new trips.' : undefined
                  }
                  onConfirm={() => toggleStatus(r)}
                >
                  <Button size="small" danger={r.status === 'active'}>
                    {r.status === 'active' ? 'Deactivate' : 'Activate'}
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      {editing && (
        <RouteForm
          schoolId={schoolId}
          route={editing === 'new' ? null : editing}
          onClose={closeForm}
          onSaved={(saved) => {
            closeForm()
            if (editing === 'new') navigate(`/routes/${saved.id}?setup=1`)
            else list.reload()
          }}
        />
      )}
    </Flex>
  )
}

export function RouteForm({
  schoolId,
  route,
  onClose,
  onSaved,
}: {
  schoolId: string
  route: BusRoute | null
  onClose: () => void
  onSaved: (route: BusRoute) => void
}) {
  const [form] = Form.useForm<RouteInput>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)

  async function save(v: RouteInput) {
    if (!v.supports_pickup && !v.supports_drop && !v.supports_outing) {
      message.error('Choose at least one: Morning Pickup, Evening Drop or Special Trip/Outing.')
      return
    }
    setSaving(true)
    try {
      const saved = route
        ? await routesApi.update(schoolId, route.id, v)
        : await routesApi.create(schoolId, v)
      message.success(
        route ? 'Route updated.' : `Route ${saved.code} created. Next: add its stops.`,
      )
      onSaved(saved)
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
      title={route ? `Edit ${route.code}` : 'New route: step 1 of 3, details'}
      okText={route ? 'Save' : 'Save and add stops'}
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Form
        form={form}
        layout="vertical"
        requiredMark="optional"
        initialValues={
          route ?? { start_point: 'School', supports_pickup: true, supports_drop: true }
        }
        onFinish={save}
      >
        <Row gutter={16}>
          <Col xs={24} md={8}>
            <Form.Item name="code" label="Code" rules={[{ required: true }]}>
              <Input placeholder="RS-01" style={{ textTransform: 'uppercase' }} />
            </Form.Item>
          </Col>
          <Col xs={24} md={16}>
            <Form.Item name="name" label="Name" rules={[{ required: true, max: 200 }]}>
              <Input placeholder="Gandhipuram - Singanallur" />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="start_point" label="Starting point">
              <Input />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item
              label="Trip types"
              required
              extra="A route can support more than one — it is reusable across daily trips and special outings."
            >
              <Space direction="vertical" size={4}>
                <Form.Item name="supports_pickup" valuePropName="checked" noStyle>
                  <Checkbox>Morning Pickup — the daily school-run pickup</Checkbox>
                </Form.Item>
                <Form.Item name="supports_drop" valuePropName="checked" noStyle>
                  <Checkbox>Evening Drop — the daily school-run drop</Checkbox>
                </Form.Item>
                <Form.Item name="supports_outing" valuePropName="checked" noStyle>
                  <Checkbox>
                    Special Trip / Outing — reusable as a template for one-off trips (e.g. a
                    recurring museum or sports-day route)
                  </Checkbox>
                </Form.Item>
              </Space>
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item name="description" label="Description">
              <Input.TextArea rows={2} />
            </Form.Item>
          </Col>
        </Row>
      </Form>
    </Modal>
  )
}
