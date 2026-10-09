import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Empty,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Result,
  Row,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from 'antd'
import {
  ArrowLeftOutlined,
  DeleteOutlined,
  EditOutlined,
  HolderOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from '@dnd-kit/core'
import type { DragEndEvent } from '@dnd-kit/core'
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import RouteMap from '../components/RouteMap'
import StatusTag from '../components/StatusTag'
import { ApiError } from '../services/api'
import { studentsApi } from '../services/people'
import { routesApi } from '../services/transport'
import type { BusRoute, RouteStudent, Stop, StopInput } from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'
import { RouteForm, TripTypeTags } from './RoutesPage'

export default function RouteDetailPage() {
  const { routeId } = useParams()
  return (
    <RequireSchool>
      {(schoolId) => <RouteDetail schoolId={schoolId} routeId={routeId!} />}
    </RequireSchool>
  )
}

function RouteDetail({ schoolId, routeId }: { schoolId: string; routeId: string }) {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const [route, setRoute] = useState<BusRoute | null>(null)
  const [stops, setStops] = useState<Stop[]>([])
  const [notFound, setNotFound] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)
  const [selected, setSelected] = useState<string | null>(null)
  const [editingRoute, setEditingRoute] = useState(false)
  const [editingStop, setEditingStop] = useState<Stop | 'new' | null>(null)
  const [savingOrder, setSavingOrder] = useState(false)
  const [students, setStudents] = useState<RouteStudent[]>([])

  useEffect(() => {
    let cancelled = false
    Promise.all([routesApi.get(schoolId, routeId), studentsApi.routeStudents(schoolId, routeId)])
      .then(([r, st]) => {
        if (cancelled) return
        setRoute(r)
        setStops(r.stops ?? [])
        setStudents(st)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 404) setNotFound(true)
        else message.error(errorMessage(e))
      })
    return () => {
      cancelled = true
    }
  }, [schoolId, routeId, reloadKey, message])

  const reload = useCallback(() => setReloadKey((k) => k + 1), [])

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  async function onDragEnd({ active, over }: DragEndEvent) {
    if (!over || active.id === over.id) return
    const before = stops
    const moved = arrayMove(
      stops,
      stops.findIndex((s) => s.id === active.id),
      stops.findIndex((s) => s.id === over.id),
    ).map((s, i) => ({ ...s, sequence: i + 1 }))
    setStops(moved) // optimistic; reverted if the save fails
    setSavingOrder(true)
    try {
      setStops(
        await routesApi.reorderStops(
          schoolId,
          routeId,
          moved.map((s) => s.id),
        ),
      )
      message.success('Stop order saved.')
    } catch (e) {
      setStops(before)
      message.error(errorMessage(e))
    } finally {
      setSavingOrder(false)
    }
  }

  async function deleteStop(s: Stop) {
    try {
      await routesApi.deleteStop(schoolId, routeId, s.id)
      message.success(`${s.name} deleted.`)
      reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  if (notFound) {
    return (
      <Result
        status="404"
        title="Route not found"
        extra={<Button onClick={() => navigate('/routes')}>Back to routes</Button>}
      />
    )
  }
  if (!route) return <Spin style={{ display: 'block', marginTop: 80 }} />

  return (
    <Flex vertical gap="middle">
      <Button
        type="link"
        icon={<ArrowLeftOutlined />}
        onClick={() => navigate('/routes')}
        style={{ alignSelf: 'flex-start', padding: 0 }}
      >
        All routes
      </Button>
      <PageHeader
        title={
          <Space wrap>
            {route.code} · {route.name} <StatusTag status={route.status} />
          </Space>
        }
        subtitle={
          <Space wrap>
            Starts at {route.start_point || '—'} <TripTypeTags route={route} />
          </Space>
        }
        extra={
          <Button icon={<EditOutlined />} onClick={() => setEditingRoute(true)}>
            Edit route
          </Button>
        }
      />

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={10}>
          <Card
            title={`Stops (${stops.length})`}
            extra={
              <Button
                type="primary"
                size="small"
                icon={<PlusOutlined />}
                onClick={() => setEditingStop('new')}
              >
                Add stop
              </Button>
            }
          >
            {stops.length === 0 ? (
              <Empty description="No stops yet. Add the first stop of this route." />
            ) : (
              <Spin spinning={savingOrder}>
                <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
                  Drag <HolderOutlined /> to change the travel order.
                </Typography.Paragraph>
                <DndContext
                  sensors={sensors}
                  collisionDetection={closestCenter}
                  onDragEnd={onDragEnd}
                >
                  <SortableContext
                    items={stops.map((s) => s.id)}
                    strategy={verticalListSortingStrategy}
                  >
                    {stops.map((s) => (
                      <StopRow
                        key={s.id}
                        stop={s}
                        studentCount={
                          students.filter(
                            (st) => st.pickup_stop_id === s.id || st.drop_stop_id === s.id,
                          ).length
                        }
                        selected={s.id === selected}
                        onSelect={() => setSelected(s.id === selected ? null : s.id)}
                        onEdit={() => setEditingStop(s)}
                        onDelete={() => deleteStop(s)}
                      />
                    ))}
                  </SortableContext>
                </DndContext>
              </Spin>
            )}
          </Card>
        </Col>
        <Col xs={24} lg={14}>
          <Card styles={{ body: { padding: 8 } }}>
            <RouteMap stops={stops} highlightId={selected} height={520} />
          </Card>
        </Col>
      </Row>

      <RouteStudentsCard
        students={students}
        stops={stops}
        selectedStop={stops.find((s) => s.id === selected) ?? null}
      />

      {editingRoute && (
        <RouteForm
          schoolId={schoolId}
          route={route}
          onClose={() => setEditingRoute(false)}
          onSaved={() => {
            setEditingRoute(false)
            reload()
          }}
        />
      )}
      {editingStop && (
        <StopForm
          schoolId={schoolId}
          route={route}
          stops={stops}
          stop={editingStop === 'new' ? null : editingStop}
          onClose={() => setEditingStop(null)}
          onSaved={() => {
            setEditingStop(null)
            reload()
          }}
        />
      )}
    </Flex>
  )
}

function RouteStudentsCard({
  students,
  stops,
  selectedStop,
}: {
  students: RouteStudent[]
  stops: Stop[]
  selectedStop: Stop | null
}) {
  const stopName = (id: string | null) => {
    const s = stops.find((x) => x.id === id)
    return s ? `${s.sequence}. ${s.name}` : '—'
  }
  const shown = selectedStop
    ? students.filter(
        (s) => s.pickup_stop_id === selectedStop.id || s.drop_stop_id === selectedStop.id,
      )
    : students
  return (
    <Card
      title={
        selectedStop
          ? `Students at ${selectedStop.name} (${shown.length})`
          : `Students on this route (${students.length})`
      }
      extra={
        <Typography.Text type="secondary">
          {selectedStop ? 'Click the stop again to show all.' : 'Click a stop to filter.'}
        </Typography.Text>
      }
    >
      <Table<RouteStudent>
        rowKey="student_id"
        size="small"
        dataSource={shown}
        pagination={{ pageSize: 20, hideOnSinglePage: true }}
        scroll={{ x: 600 }}
        locale={{ emptyText: 'No students assigned. Assign them on the Students page.' }}
        columns={[
          { title: 'Name', dataIndex: 'name' },
          { title: 'Adm. no.', dataIndex: 'admission_no', width: 110 },
          {
            title: 'Class',
            width: 90,
            render: (_, s) => [s.class, s.section].filter(Boolean).join('-') || '—',
          },
          { title: 'Pickup stop', render: (_, s) => stopName(s.pickup_stop_id) },
          { title: 'Drop stop', render: (_, s) => stopName(s.drop_stop_id) },
        ]}
      />
    </Card>
  )
}

function StopRow({
  stop,
  studentCount,
  selected,
  onSelect,
  onEdit,
  onDelete,
}: {
  stop: Stop
  studentCount: number
  selected: boolean
  onSelect: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: stop.id,
  })
  return (
    <div
      ref={setNodeRef}
      className={`stop-row${selected ? ' stop-row--selected' : ''}`}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.6 : 1,
      }}
      onClick={onSelect}
    >
      <span
        className="stop-row__handle"
        {...attributes}
        {...listeners}
        aria-label={`Move ${stop.name}`}
      >
        <HolderOutlined />
      </span>
      <span className="stop-row__seq">{stop.sequence}</span>
      <div className="stop-row__body">
        <Typography.Text strong>{stop.name}</Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {[
            stop.pickup_time && `Pickup ${stop.pickup_time}`,
            stop.drop_time && `Drop ${stop.drop_time}`,
            stop.landmark,
          ]
            .filter(Boolean)
            .join(' · ') || 'No times set'}
        </Typography.Text>
      </div>
      <Tag color={studentCount ? 'blue' : 'default'} title="Students using this stop">
        {studentCount} student{studentCount === 1 ? '' : 's'}
      </Tag>
      <Space size={0} onClick={(e) => e.stopPropagation()}>
        <Button
          type="text"
          size="small"
          icon={<EditOutlined />}
          onClick={onEdit}
          aria-label="Edit stop"
        />
        <Popconfirm
          title={`Delete ${stop.name}?`}
          description={
            studentCount
              ? `${studentCount} student(s) use this stop. Move them first.`
              : 'Later stops move up one place.'
          }
          onConfirm={onDelete}
        >
          <Button
            type="text"
            size="small"
            danger
            icon={<DeleteOutlined />}
            aria-label="Delete stop"
          />
        </Popconfirm>
      </Space>
    </div>
  )
}

type StopFormValues = Omit<StopInput, 'latitude' | 'longitude'> & {
  latitude?: number
  longitude?: number
  position?: number
}

function StopForm({
  schoolId,
  route,
  stops,
  stop,
  onClose,
  onSaved,
}: {
  schoolId: string
  route: BusRoute
  stops: Stop[]
  stop: Stop | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<StopFormValues>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)
  const latitude = Form.useWatch('latitude', form)
  const longitude = Form.useWatch('longitude', form)
  const radius = Form.useWatch('geofence_radius_m', form) ?? 100
  const picked = latitude != null && longitude != null ? { latitude, longitude, radius } : null

  async function save(v: StopFormValues) {
    if (v.latitude == null || v.longitude == null) {
      message.error('Click the map to place the stop.')
      return
    }
    setSaving(true)
    try {
      const input: StopInput = {
        name: v.name,
        landmark: v.landmark ?? '',
        latitude: v.latitude,
        longitude: v.longitude,
        pickup_time: v.pickup_time || null,
        drop_time: v.drop_time || null,
        geofence_radius_m: v.geofence_radius_m,
      }
      if (stop) await routesApi.updateStop(schoolId, route.id, stop.id, input)
      else await routesApi.createStop(schoolId, route.id, { ...input, position: v.position })
      message.success(stop ? 'Stop updated.' : 'Stop added.')
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
      title={stop ? `Edit stop: ${stop.name}` : `Add stop to ${route.code}`}
      okText="Save"
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      width={960}
      destroyOnHidden
    >
      <Row gutter={16}>
        <Col xs={24} md={10}>
          <Form
            form={form}
            layout="vertical"
            requiredMark="optional"
            initialValues={stop ?? { geofence_radius_m: 100, position: undefined }}
            onFinish={save}
          >
            <Form.Item name="name" label="Stop name" rules={[{ required: true, max: 200 }]}>
              <Input placeholder="Gandhipuram" />
            </Form.Item>
            <Form.Item name="landmark" label="Landmark">
              <Input placeholder="Near bus stand" />
            </Form.Item>
            {!stop && stops.length > 0 && (
              <Form.Item name="position" label="Position">
                <Select
                  allowClear
                  placeholder={`At the end (after stop ${stops.length})`}
                  options={stops.map((s) => ({
                    value: s.sequence,
                    label: `Before ${s.sequence}. ${s.name}`,
                  }))}
                />
              </Form.Item>
            )}
            <Row gutter={8}>
              <Col span={12}>
                <Form.Item
                  name="latitude"
                  label="Latitude"
                  rules={[{ required: true, message: 'Click the map' }]}
                >
                  <InputNumber min={-90} max={90} step={0.0001} style={{ width: '100%' }} />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item
                  name="longitude"
                  label="Longitude"
                  rules={[{ required: true, message: 'Click the map' }]}
                >
                  <InputNumber min={-180} max={180} step={0.0001} style={{ width: '100%' }} />
                </Form.Item>
              </Col>
              {route.supports_pickup && (
                <Col span={12}>
                  <Form.Item name="pickup_time" label="Pickup time">
                    <Input type="time" />
                  </Form.Item>
                </Col>
              )}
              {route.supports_drop && (
                <Col span={12}>
                  <Form.Item name="drop_time" label="Drop time">
                    <Input type="time" />
                  </Form.Item>
                </Col>
              )}
            </Row>
            <Form.Item
              name="geofence_radius_m"
              label="Arrival radius (metres)"
              tooltip="The bus counts as Reached when it is inside this circle."
              rules={[{ required: true }]}
            >
              <InputNumber min={25} max={1000} step={25} style={{ width: '100%' }} />
            </Form.Item>
          </Form>
        </Col>
        <Col xs={24} md={14}>
          <Alert
            type="info"
            showIcon
            title="Click the map to place the stop."
            style={{ marginBottom: 8 }}
          />
          <RouteMap
            stops={stops.filter((s) => s.id !== stop?.id)}
            picked={picked}
            onPick={(lat, lng) => form.setFieldsValue({ latitude: lat, longitude: lng })}
            height={420}
          />
        </Col>
      </Row>
    </Modal>
  )
}
