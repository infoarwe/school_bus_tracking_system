import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Empty,
  Flex,
  Popconfirm,
  Result,
  Row,
  Segmented,
  Skeleton,
  Space,
  Spin,
  Steps,
  Table,
  Tag,
  Typography,
} from 'antd'
import {
  CheckCircleOutlined,
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
import StopForm from '../components/StopForm'
import { ApiError } from '../services/api'
import { studentsApi } from '../services/people'
import { routesApi } from '../services/transport'
import type { BusRoute, RouteStudent, Stop } from '../services/types'
import { errorMessage } from '../utils/formErrors'
import { distanceM, formatDistance } from '../utils/geo'
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
  // ?setup=1: just created from "Add route": guide through stops and a final review.
  const [searchParams, setSearchParams] = useSearchParams()
  const setup = searchParams.get('setup') === '1'
  const [reviewing, setReviewing] = useState(false)
  const [route, setRoute] = useState<BusRoute | null>(null)
  const [stops, setStops] = useState<Stop[]>([])
  const [loadError, setLoadError] = useState<string | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)
  const [selected, setSelected] = useState<string | null>(null)
  const [editingRoute, setEditingRoute] = useState(false)
  const [editingStop, setEditingStop] = useState<Stop | 'new' | null>(null)
  const [savingOrder, setSavingOrder] = useState(false)
  const [students, setStudents] = useState<RouteStudent[]>([])
  // Evening Drop has its own, independently configured travel order.
  const [orderMode, setOrderMode] = useState<'pickup' | 'drop'>('pickup')

  useEffect(() => {
    let cancelled = false
    Promise.all([routesApi.get(schoolId, routeId), studentsApi.routeStudents(schoolId, routeId)])
      .then(([r, st]) => {
        if (cancelled) return
        setRoute(r)
        setStops(r.stops ?? [])
        setStudents(st)
        setLoadError(null)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 404) setNotFound(true)
        else setLoadError(errorMessage(e))
      })
    return () => {
      cancelled = true
    }
  }, [schoolId, routeId, reloadKey])

  const reload = useCallback(() => setReloadKey((k) => k + 1), [])

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  // Stops shown in the current mode: every stop (Morning Pickup order) or only the
  // ones that run Evening Drop (its own order — not necessarily all stops, and not
  // assumed to be the pickup order reversed).
  const dropStops = [...stops]
    .filter((s) => s.drop_time)
    .sort((a, b) => (a.drop_sequence ?? 999) - (b.drop_sequence ?? 999) || a.sequence - b.sequence)
  const visibleStops = orderMode === 'drop' ? dropStops : stops

  async function onDragEnd({ active, over }: DragEndEvent) {
    if (!over || active.id === over.id) return
    if (orderMode === 'drop') {
      const before = stops
      const movedDrop = arrayMove(
        dropStops,
        dropStops.findIndex((s) => s.id === active.id),
        dropStops.findIndex((s) => s.id === over.id),
      )
      const byId = new Map(movedDrop.map((s, i) => [s.id, i + 1]))
      setStops(stops.map((s) => (byId.has(s.id) ? { ...s, drop_sequence: byId.get(s.id)! } : s)))
      setSavingOrder(true)
      try {
        await routesApi.reorderDropSequence(
          schoolId,
          routeId,
          movedDrop.map((s) => s.id),
        )
        message.success('Evening Drop order saved.')
        reload()
      } catch (e) {
        setStops(before)
        message.error(`The order was not saved: ${errorMessage(e)}`)
      } finally {
        setSavingOrder(false)
      }
      return
    }
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
      message.error(`The order was not saved: ${errorMessage(e)}`)
    } finally {
      setSavingOrder(false)
    }
  }

  async function deleteStop(s: Stop) {
    try {
      await routesApi.deleteStop(schoolId, routeId, s.id)
      message.success(`${s.name} removed. Later stops moved up one place.`)
      reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  function finishSetup() {
    setSearchParams({}, { replace: true })
    setReviewing(false)
    message.success(`${route?.code} is ready. Assign students to it on the Students page.`)
  }

  if (notFound) {
    return (
      <Result
        status="404"
        title="Route not found"
        subTitle="It may have been removed, or it belongs to another school."
        extra={<Button onClick={() => navigate('/routes')}>Back to routes</Button>}
      />
    )
  }
  if (loadError) {
    return (
      <Result
        status="warning"
        title="Could not load this route"
        subTitle={loadError}
        extra={<Button onClick={reload}>Try again</Button>}
      />
    )
  }
  if (!route) return <Skeleton active paragraph={{ rows: 8 }} />

  const step = reviewing ? 2 : 1

  return (
    <Flex vertical gap="middle">
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
          <Space wrap>
            <Button icon={<EditOutlined />} onClick={() => setEditingRoute(true)}>
              Edit details
            </Button>
            {reviewing ? (
              <Button onClick={() => setReviewing(false)}>Back to stops</Button>
            ) : (
              <Button
                type={setup ? 'primary' : 'default'}
                icon={<CheckCircleOutlined />}
                onClick={() => setReviewing(true)}
              >
                {setup ? 'Review & finish' : 'Check route'}
              </Button>
            )}
          </Space>
        }
      />

      {setup && (
        <Card size="small">
          <Steps
            size="small"
            current={step}
            items={[
              { title: 'Route details', content: 'Saved' },
              { title: 'Add stops', content: 'Search, place and order them' },
              { title: 'Review & finish' },
            ]}
          />
        </Card>
      )}

      {reviewing ? (
        <RouteReview
          route={route}
          stops={stops}
          onBack={() => setReviewing(false)}
          onFinish={setup ? finishSetup : () => setReviewing(false)}
          finishLabel={setup ? 'Finish' : 'Done'}
        />
      ) : (
        <>
          <Row gutter={[16, 16]}>
            <Col xs={24} lg={10}>
              <Card
                title={`Stops (${stops.length})`}
                extra={
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => setEditingStop('new')}
                  >
                    Add stop
                  </Button>
                }
              >
                {stops.length === 0 ? (
                  <Empty
                    description={
                      <>
                        No stops yet. Add them in travel order for Morning Pickup:
                        <br />
                        first pickup point first, the school last.
                      </>
                    }
                  >
                    <Button
                      type="primary"
                      icon={<PlusOutlined />}
                      onClick={() => setEditingStop('new')}
                    >
                      Add the first stop
                    </Button>
                  </Empty>
                ) : (
                  <Spin spinning={savingOrder}>
                    {route.supports_drop && (
                      <Segmented
                        value={orderMode}
                        onChange={(v) => setOrderMode(v as 'pickup' | 'drop')}
                        style={{ marginBottom: 12 }}
                        options={[
                          { label: 'Pickup order', value: 'pickup' },
                          { label: 'Drop order', value: 'drop' },
                        ]}
                      />
                    )}
                    <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
                      {orderMode === 'drop'
                        ? "Evening Drop's own order — independent of the pickup order above. Drag to change it."
                        : 'Drag'}{' '}
                      <HolderOutlined /> (or focus it and use the arrow keys) to change the order.
                      Click a stop to see it on the map.
                    </Typography.Paragraph>
                    {orderMode === 'drop' && dropStops.length === 0 ? (
                      <Empty description="No stops have a drop time yet. Set one on a stop to include it in the evening order." />
                    ) : (
                      <DndContext
                        sensors={sensors}
                        collisionDetection={closestCenter}
                        onDragEnd={onDragEnd}
                      >
                        <SortableContext
                          items={visibleStops.map((s) => s.id)}
                          strategy={verticalListSortingStrategy}
                        >
                          {visibleStops.map((s, i) => (
                            <StopRow
                              key={s.id}
                              stop={s}
                              displaySequence={orderMode === 'drop' ? i + 1 : s.sequence}
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
                    )}
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

          {!setup && (
            <RouteStudentsCard
              students={students}
              stops={stops}
              selectedStop={stops.find((s) => s.id === selected) ?? null}
            />
          )}
        </>
      )}

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

/** Problems worth fixing before the route is used (none of them block saving). */
function routeIssues(route: BusRoute, stops: Stop[]): string[] {
  const issues: string[] = []
  if (stops.length === 0) return ['The route has no stops yet.']
  if (stops.length === 1) issues.push('A route usually needs at least two stops.')
  const names = (list: Stop[]) => list.map((s) => `${s.sequence}. ${s.name}`).join(', ')
  if (route.supports_pickup) {
    const missing = stops.filter((s) => !s.pickup_time)
    if (missing.length) issues.push(`No pickup time: ${names(missing)}.`)
    const timed = stops.filter((s) => s.pickup_time)
    if (timed.some((s, i) => i > 0 && s.pickup_time! < timed[i - 1].pickup_time!)) {
      issues.push(
        'Morning Pickup runs from stop 1 to the last stop, so pickup times should go up in that order.',
      )
    }
  }
  if (route.supports_drop) {
    const missing = stops.filter((s) => !s.drop_time)
    if (missing.length) issues.push(`No drop time: ${names(missing)}.`)
    const withDropTime = stops.filter((s) => s.drop_time)
    const unordered = withDropTime.filter((s) => s.drop_sequence == null)
    if (unordered.length) {
      issues.push(
        `Evening Drop order is not set yet: ${names(unordered)}. Set it in "Drop order" on the stops list.`,
      )
    }
    const ordered = withDropTime
      .filter((s) => s.drop_sequence != null)
      .sort((a, b) => a.drop_sequence! - b.drop_sequence!)
    if (ordered.some((s, i) => i > 0 && s.drop_time! > ordered[i - 1].drop_time!)) {
      issues.push(
        'Drop times do not increase through the Evening Drop order — check the order matches when the bus actually reaches each stop.',
      )
    }
  }
  for (let i = 0; i < stops.length; i++) {
    for (let j = i + 1; j < stops.length; j++) {
      const d = distanceM(stops[i], stops[j])
      if (d < 50) {
        issues.push(
          `Stops ${stops[i].sequence} and ${stops[j].sequence} are only ${formatDistance(d)} apart.`,
        )
      }
    }
  }
  return issues
}

function RouteReview({
  route,
  stops,
  onBack,
  onFinish,
  finishLabel,
}: {
  route: BusRoute
  stops: Stop[]
  onBack: () => void
  onFinish: () => void
  finishLabel: string
}) {
  const issues = routeIssues(route, stops)
  const legs = stops.map((s, i) => (i === 0 ? 0 : distanceM(stops[i - 1], s)))
  const total = legs.reduce((a, b) => a + b, 0)

  return (
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={14}>
        <Card
          title="Review"
          extra={
            stops.length > 1 && (
              <Typography.Text type="secondary">
                {stops.length} stops · about {formatDistance(total)} in a straight line
              </Typography.Text>
            )
          }
        >
          <Flex vertical gap="middle">
            {issues.length === 0 ? (
              <Alert type="success" showIcon title="Everything looks good." />
            ) : (
              <Alert
                type="warning"
                showIcon
                title="Worth checking before the route is used"
                description={
                  <ul style={{ margin: 0, paddingLeft: 18 }}>
                    {issues.map((i) => (
                      <li key={i}>{i}</li>
                    ))}
                  </ul>
                }
              />
            )}
            <Table<Stop>
              rowKey="id"
              size="small"
              pagination={false}
              dataSource={stops}
              scroll={{ x: 640 }}
              columns={[
                { title: '#', dataIndex: 'sequence', width: 48 },
                {
                  title: 'Stop',
                  render: (_, s) => (
                    <Flex vertical>
                      <Typography.Text strong>{s.name}</Typography.Text>
                      {s.landmark && (
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          {s.landmark}
                        </Typography.Text>
                      )}
                    </Flex>
                  ),
                },
                ...(route.supports_pickup
                  ? [
                      {
                        title: 'Pickup',
                        width: 80,
                        render: (_: unknown, s: Stop) =>
                          s.pickup_time || <Tag color="orange">—</Tag>,
                      },
                    ]
                  : []),
                ...(route.supports_drop
                  ? [
                      {
                        title: 'Drop',
                        width: 80,
                        render: (_: unknown, s: Stop) => s.drop_time || <Tag color="orange">—</Tag>,
                      },
                    ]
                  : []),
                { title: 'Radius', width: 80, render: (_, s) => `${s.geofence_radius_m} m` },
                {
                  title: 'From previous',
                  width: 120,
                  render: (_v, _s, i) => (i === 0 ? '—' : formatDistance(legs[i])),
                },
              ]}
            />
            <Flex gap="small" justify="flex-end" wrap>
              <Button onClick={onBack}>Back to stops</Button>
              <Button type="primary" disabled={stops.length === 0} onClick={onFinish}>
                {finishLabel}
              </Button>
            </Flex>
          </Flex>
        </Card>
      </Col>
      <Col xs={24} xl={10}>
        <Card styles={{ body: { padding: 8 } }}>
          <RouteMap stops={stops} height={480} />
        </Card>
      </Col>
    </Row>
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
  displaySequence,
  studentCount,
  selected,
  onSelect,
  onEdit,
  onDelete,
}: {
  stop: Stop
  /** The number shown in the order badge: pickup sequence, or position in the drop order. */
  displaySequence: number
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
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      aria-label={`Stop ${displaySequence}: ${stop.name}. Show on the map`}
      onKeyDown={(e) => {
        if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault()
          onSelect()
        }
      }}
    >
      <span
        className="stop-row__handle"
        {...attributes}
        {...listeners}
        aria-label={`Move ${stop.name}`}
      >
        <HolderOutlined />
      </span>
      <span className="stop-row__seq">{displaySequence}</span>
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
