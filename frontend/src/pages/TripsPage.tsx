import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  Alert,
  App,
  Avatar,
  Button,
  Checkbox,
  Dropdown,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Progress,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import {
  CarOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  CopyOutlined,
  DownOutlined,
  FlagOutlined,
  LeftOutlined,
  MoonOutlined,
  PlayCircleOutlined,
  RightOutlined,
  StopOutlined,
  SunOutlined,
  TeamOutlined,
  UnorderedListOutlined,
  MinusCircleOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import LocationSearch from '../components/LocationSearch'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import TripDetailDrawer, { StatTile, TripStatusTag } from '../components/TripDetailDrawer'
import { useCurrentSchool } from '../context/SchoolContext'
import { busesApi, driversApi, routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type Bus,
  type BusRoute,
  type Driver,
  type RouteScheduleWindow,
  type Trip,
  type TripInput,
  type TripType,
  type WaypointInput,
} from '../services/types'
import { avatarColor, initials } from '../utils/avatar'
import { addDays, formatDate, todayIn, weekdayOf } from '../utils/dates'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const isOpen = (t: Trip) => t.status === 'scheduled' || t.status === 'confirmed'

type StatusFilter = 'all' | 'waiting' | 'started' | 'completed' | 'cancelled'

const matchesFilter = (t: Trip, f: StatusFilter) =>
  f === 'all' ? true : f === 'waiting' ? isOpen(t) : t.status === f

const tripTypeIcons: Record<TripType, ReactNode> = {
  morning_pickup: <SunOutlined />,
  evening_drop: <MoonOutlined />,
  special_trip: <FlagOutlined />,
}

export default function TripsPage() {
  return <RequireSchool>{(schoolId) => <Trips schoolId={schoolId} />}</RequireSchool>
}

interface Masters {
  routes: BusRoute[]
  buses: Bus[]
  drivers: Driver[]
}

function Trips({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const { school } = useCurrentSchool()
  const today = todayIn(school?.timezone)
  // ?date=YYYY-MM-DD opens that day (links from staff notifications).
  const [searchParams] = useSearchParams()
  const [date, setDate] = useState(() => {
    const d = searchParams.get('date') ?? ''
    return /^\d{4}-\d{2}-\d{2}$/.test(d) ? d : today
  })
  const [tripType, setTripType] = useState<TripType>('morning_pickup')
  const [dayTrips, setDayTrips] = useState<Trip[]>([])
  const [masters, setMasters] = useState<Masters>({ routes: [], buses: [], drivers: [] })
  const [loading, setLoading] = useState(true)
  const [reloadKey, setReloadKey] = useState(0)
  const [editing, setEditing] = useState<Trip | { route_id?: string } | null>(null)
  const [viewing, setViewing] = useState<string | null>(null)
  const [reasonFor, setReasonFor] = useState<{
    trip: Trip
    action: 'cancel' | 'started' | 'completed' | 'cancelled'
  } | null>(null)
  const [copying, setCopying] = useState(false)
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [unassigning, setUnassigning] = useState<Trip | null>(null)
  const [replacingDriver, setReplacingDriver] = useState<Trip | null>(null)
  const [replacingBus, setReplacingBus] = useState<Trip | null>(null)
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const [bulkUnassignOpen, setBulkUnassignOpen] = useState(false)
  const [bulkUnassigning, setBulkUnassigning] = useState(false)
  const clock = useMemo(
    () =>
      new Intl.DateTimeFormat('en-GB', {
        hour: '2-digit',
        minute: '2-digit',
        hourCycle: 'h23',
        timeZone: school?.timezone || undefined,
      }),
    [school?.timezone],
  )
  const time = (iso: string | null) => (iso ? clock.format(new Date(iso)) : null)
  const goTo = (d: string) => {
    setLoading(true)
    setDate(d)
  }

  // Active routes, buses and drivers for the form; inactive ones cannot be assigned anyway.
  useEffect(() => {
    Promise.all([
      routesApi.list(schoolId, { status: 'active', page_size: 100 }),
      busesApi.list(schoolId, { status: 'active', page_size: 100 }),
      driversApi.list(schoolId, { status: 'active', page_size: 100 }),
    ])
      .then(([r, b, d]) => setMasters({ routes: r.data, buses: b.data, drivers: d.data }))
      .catch((e: unknown) => message.error(errorMessage(e)))
  }, [schoolId, message])

  useEffect(() => {
    let cancelled = false
    tripsApi
      .list(schoolId, { date, page_size: 100 })
      .then((res) => !cancelled && setDayTrips(res.data))
      .catch((e: unknown) => !cancelled && message.error(errorMessage(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [schoolId, date, reloadKey, message])

  const reload = useCallback(() => {
    setLoading(true)
    setReloadKey((k) => k + 1)
  }, [])

  const typeTrips = dayTrips.filter((t) => t.trip_type === tripType)
  const trips = typeTrips.filter((t) => matchesFilter(t, statusFilter))
  const active = typeTrips.filter((t) => t.status !== 'cancelled')
  const count = (f: StatusFilter) => typeTrips.filter((t) => matchesFilter(t, f)).length
  const riders = active.reduce((n, t) => n + t.student_count, 0)
  const toggle = (f: StatusFilter) => setStatusFilter((cur) => (cur === f ? 'all' : f))
  const runsType = (r: BusRoute) =>
    tripType === 'morning_pickup'
      ? r.supports_pickup
      : tripType === 'evening_drop'
        ? r.supports_drop
        : r.supports_outing
  // Special trips are one-off, not a daily obligation per route, so there is no
  // "unassigned route" nudge for them.
  const unassigned =
    tripType === 'special_trip'
      ? []
      : masters.routes.filter(
          (r) => runsType(r) && r.stop_count > 0 && !active.some((t) => t.route?.id === r.id),
        )
  const past = date < today

  // Selected trips with something left to unassign, computed fresh from the
  // latest `trips` (re-derived from `dayTrips` on every reload) — never from
  // a snapshot taken when the row was checked. A trip already unassigned
  // (no bus and no driver, e.g. just unassigned via its own row action) no
  // longer qualifies, even while its checkbox is still ticked, so Bulk
  // Unassign can never re-offer to "unassign" a trip that has nothing left.
  const selectedTrips = trips.filter((t) => selectedKeys.includes(t.id))
  const unassignableSelected = selectedTrips.filter((t) => isOpen(t) && (t.bus || t.driver))

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Daily Trips"
        subtitle="Driver + Bus + Route for each date and trip type. Nothing is permanent: assign every day, or copy a day."
        extra={
          <Space wrap>
            <Button icon={<CopyOutlined />} onClick={() => setCopying(true)}>
              Copy this day
            </Button>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              disabled={past}
              onClick={() => setEditing({})}
            >
              Add trip
            </Button>
          </Space>
        }
      />
      <Flex gap="small" wrap align="center" justify="space-between">
        <Flex gap="small" wrap align="center">
          <Space.Compact>
            <Button
              icon={<LeftOutlined />}
              aria-label="Previous day"
              onClick={() => goTo(addDays(date, -1))}
            />
            <Input
              type="date"
              value={date}
              style={{ width: 160 }}
              onChange={(e) => e.target.value && goTo(e.target.value)}
            />
            <Button
              icon={<RightOutlined />}
              aria-label="Next day"
              onClick={() => goTo(addDays(date, 1))}
            />
          </Space.Compact>
          <Button onClick={() => goTo(today)} disabled={date === today}>
            Today
          </Button>
          <Typography.Text strong>{formatDate(date)}</Typography.Text>
          {date === today ? (
            <Tag color="green">Today</Tag>
          ) : past ? (
            <Tag>Past</Tag>
          ) : (
            <Tag color="blue">Upcoming</Tag>
          )}
        </Flex>
        <Segmented<TripType>
          value={tripType}
          onChange={setTripType}
          options={(['morning_pickup', 'evening_drop', 'special_trip'] as TripType[]).map(
            (type) => ({
              value: type,
              icon: tripTypeIcons[type],
              label: `${type === 'special_trip' ? 'Special Trip / Outing' : tripTypeLabels[type]} (${
                dayTrips.filter((t) => t.trip_type === type && t.status !== 'cancelled').length
              })`,
            }),
          )}
        />
      </Flex>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))',
          gap: 12,
        }}
      >
        <StatTile
          tone="geekblue"
          icon={<UnorderedListOutlined />}
          title="All trips"
          value={typeTrips.length}
          active={statusFilter === 'all'}
          onClick={() => setStatusFilter('all')}
        />
        <StatTile
          tone="gold"
          icon={<ClockCircleOutlined />}
          title="Not started"
          value={count('waiting')}
          active={statusFilter === 'waiting'}
          onClick={() => toggle('waiting')}
        />
        <StatTile
          tone="green"
          icon={<PlayCircleOutlined />}
          title="Running now"
          value={count('started')}
          active={statusFilter === 'started'}
          onClick={() => toggle('started')}
        />
        <StatTile
          tone="cyan"
          icon={<CheckCircleOutlined />}
          title="Completed"
          value={count('completed')}
          active={statusFilter === 'completed'}
          onClick={() => toggle('completed')}
        />
        <StatTile
          tone="red"
          icon={<StopOutlined />}
          title="Cancelled"
          value={count('cancelled')}
          active={statusFilter === 'cancelled'}
          onClick={() => toggle('cancelled')}
        />
        <StatTile tone="purple" icon={<TeamOutlined />} title="Students riding" value={riders} />
      </div>

      {!past && unassigned.length > 0 && (
        <Alert
          type="warning"
          showIcon
          title={`${unassigned.length} route(s) have no ${tripTypeLabels[tripType]} trip on this date`}
          description={
            <Space size={[4, 4]} wrap>
              {unassigned.map((r) => (
                <Button key={r.id} size="small" onClick={() => setEditing({ route_id: r.id })}>
                  Assign {r.code}
                </Button>
              ))}
            </Space>
          }
        />
      )}

      {selectedKeys.length > 0 && (
        <Alert
          type="info"
          showIcon
          title={`${selectedKeys.length} trip(s) selected`}
          description={
            <Flex vertical gap={4}>
              <Space size="small">
                {unassignableSelected.length > 0 ? (
                  <Button size="small" danger onClick={() => setBulkUnassignOpen(true)}>
                    Bulk unassign ({unassignableSelected.length})
                  </Button>
                ) : (
                  <Typography.Text type="secondary">
                    Selected trip(s) already have no bus or driver assigned — nothing to unassign.
                  </Typography.Text>
                )}
                <Button size="small" onClick={() => setSelectedKeys([])}>
                  Clear selection
                </Button>
              </Space>
              {unassignableSelected.length > 0 &&
                unassignableSelected.length < selectedTrips.length && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {selectedTrips.length - unassignableSelected.length} of the selected trip(s)
                    already have no bus or driver assigned and will be left out.
                  </Typography.Text>
                )}
            </Flex>
          }
        />
      )}

      <Table<Trip>
        rowKey="id"
        loading={loading}
        dataSource={trips}
        pagination={false}
        scroll={{ x: 1000 }}
        rowSelection={{
          selectedRowKeys: selectedKeys,
          onChange: (keys) => setSelectedKeys(keys as string[]),
          // Only an open trip (scheduled/confirmed) can be unassigned — a
          // started/completed/cancelled trip is never offered for bulk
          // selection, so "Select all" can never silently include one.
          getCheckboxProps: (t) => ({ disabled: !isOpen(t) }),
        }}
        locale={{
          emptyText:
            statusFilter !== 'all' && typeTrips.length ? (
              <>
                No trips match this filter. <a onClick={() => setStatusFilter('all')}>Show all</a>
              </>
            ) : (
              `No ${tripTypeLabels[tripType]} trips on this date.`
            ),
        }}
        rowClassName={(t) => (t.status === 'cancelled' ? 'row-muted' : '')}
        columns={[
          {
            title: 'Route',
            render: (_, t) => (
              <Flex vertical gap={2}>
                <a onClick={() => setViewing(t.id)}>
                  {t.route ? (
                    <Space size={6}>
                      <Tag color="geekblue" style={{ fontWeight: 600, marginInlineEnd: 0 }}>
                        {t.route.code}
                      </Tag>
                      <span>{t.route.name}</span>
                    </Space>
                  ) : (
                    <Space size={6}>
                      <Tag color="purple" icon={<FlagOutlined />} style={{ marginInlineEnd: 0 }}>
                        Outing
                      </Tag>
                      <strong>{t.title}</strong>
                    </Space>
                  )}
                </a>
                {t.route && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {t.route.stop_count} stop{t.route.stop_count === 1 ? '' : 's'}
                    {t.trip_type === 'special_trip' && ' · special trip on this route'}
                  </Typography.Text>
                )}
              </Flex>
            ),
          },
          {
            title: 'Time',
            width: 110,
            render: (_, t) =>
              t.scheduled_start_at && t.scheduled_end_at ? (
                <span>
                  {time(t.scheduled_start_at)}–{time(t.scheduled_end_at)}
                </span>
              ) : (
                <Typography.Text type="secondary">Not set</Typography.Text>
              ),
          },
          {
            title: 'Bus',
            width: 170,
            render: (_, t) =>
              t.bus ? (
                <Space size={6}>
                  <CarOutlined style={{ color: '#d97706' }} />
                  <Link to={`/buses/${t.bus.id}`} style={{ fontWeight: 500 }}>
                    {t.bus.vehicle_number}
                  </Link>
                </Space>
              ) : (
                <Tag>Unassigned</Tag>
              ),
          },
          {
            title: 'Driver',
            render: (_, t) =>
              t.driver ? (
                <Space size="small" align="center">
                  <Avatar size="small" style={{ background: avatarColor(t.driver.id) }}>
                    {initials(t.driver.name)}
                  </Avatar>
                  <Flex vertical>
                    <span>
                      <Link to={`/drivers/${t.driver.id}`}>{t.driver.name}</Link>
                      {t.assigned_by === 'driver' && (
                        <Tag
                          color="purple"
                          style={{ marginInlineStart: 6 }}
                          title="The driver took this route in the Driver app"
                        >
                          Self-assigned
                        </Tag>
                      )}
                    </span>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {t.driver.mobile}
                    </Typography.Text>
                  </Flex>
                </Space>
              ) : (
                <Tag>Unassigned</Tag>
              ),
          },
          {
            title: 'Students / seats',
            width: 150,
            render: (_, t) => {
              if (!t.bus) return <Typography.Text type="secondary">—</Typography.Text>
              const over = t.student_count > t.bus.capacity
              return (
                <Flex vertical>
                  <Typography.Text type={over ? 'danger' : undefined}>
                    {t.student_count} / {t.bus.capacity}
                    {over && ' · over capacity'}
                  </Typography.Text>
                  <Progress
                    percent={Math.min(100, (t.student_count / Math.max(t.bus.capacity, 1)) * 100)}
                    showInfo={false}
                    size="small"
                    status={over ? 'exception' : 'normal'}
                    strokeColor={over ? undefined : '#7c3aed'}
                  />
                </Flex>
              )
            },
          },
          {
            title: 'Status',
            width: 160,
            render: (_, t) => (
              <Flex vertical align="flex-start" gap={2}>
                <TripStatusTag status={t.status} />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {t.status === 'started'
                    ? `since ${time(t.started_at)}`
                    : t.status === 'completed'
                      ? `${time(t.started_at) ?? '—'} – ${time(t.ended_at)}`
                      : t.status === 'cancelled'
                        ? t.cancel_reason
                        : t.status === 'confirmed'
                          ? 'driver confirmed'
                          : 'waiting for driver'}
                </Typography.Text>
              </Flex>
            ),
          },
          {
            title: 'Actions',
            width: 230,
            render: (_, t) => (
              <Space size="small">
                <Button size="small" onClick={() => setViewing(t.id)}>
                  Details
                </Button>
                {isOpen(t) && (
                  <Button size="small" onClick={() => setEditing(t)}>
                    {t.bus || t.driver ? 'Edit' : 'Assign'}
                  </Button>
                )}
                {(isOpen(t) || t.status === 'started') && (
                  <Dropdown
                    menu={{
                      items: [
                        ...(isOpen(t) && (t.bus || t.driver)
                          ? [{ key: 'unassign', label: 'Unassign bus & driver' }]
                          : []),
                        ...(isOpen(t) && t.driver
                          ? [{ key: 'replace_driver', label: 'Replace driver' }]
                          : []),
                        ...(isOpen(t) && t.bus ? [{ key: 'replace_bus', label: 'Replace bus' }] : []),
                        ...(isOpen(t) || t.status === 'started'
                          ? [{ type: 'divider' as const }]
                          : []),
                        ...(isOpen(t)
                          ? [{ key: 'cancel', label: 'Cancel trip', danger: true }]
                          : []),
                        { type: 'divider' as const },
                        ...(isOpen(t) && t.trip_date === today
                          ? [{ key: 'started', label: 'Override: mark started' }]
                          : []),
                        { key: 'completed', label: 'Override: mark completed' },
                        ...(t.status === 'started'
                          ? [{ key: 'cancelled', label: 'Override: cancel', danger: true }]
                          : []),
                      ],
                      onClick: ({ key }) => {
                        if (key === 'unassign') setUnassigning(t)
                        else if (key === 'replace_driver') setReplacingDriver(t)
                        else if (key === 'replace_bus') setReplacingBus(t)
                        else setReasonFor({ trip: t, action: key as 'cancel' })
                      },
                    }}
                  >
                    <Button size="small">
                      More <DownOutlined />
                    </Button>
                  </Dropdown>
                )}
              </Space>
            ),
          },
        ]}
      />

      {editing && (
        <TripForm
          schoolId={schoolId}
          trip={'id' in editing ? (editing as Trip) : null}
          initialRouteId={'id' in editing ? undefined : editing.route_id}
          date={date}
          tripType={tripType}
          masters={masters}
          dayTrips={dayTrips}
          timeZone={school?.timezone}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      {reasonFor && (
        <ReasonModal
          schoolId={schoolId}
          trip={reasonFor.trip}
          action={reasonFor.action}
          onClose={() => setReasonFor(null)}
          onDone={() => {
            setReasonFor(null)
            reload()
          }}
        />
      )}
      {copying && (
        <CopyModal
          schoolId={schoolId}
          fromDate={date}
          today={today}
          workingDays={school?.working_days ?? []}
          onClose={() => setCopying(false)}
          onDone={(firstDate) => {
            setCopying(false)
            if (firstDate) {
              setLoading(true)
              setDate(firstDate)
            }
          }}
        />
      )}
      {viewing && (
        <TripDetailDrawer schoolId={schoolId} tripId={viewing} onClose={() => setViewing(null)} />
      )}
      {unassigning && (
        <UnassignModal
          schoolId={schoolId}
          trip={unassigning}
          onClose={() => setUnassigning(null)}
          onDone={() => {
            const id = unassigning.id
            setSelectedKeys((keys) => keys.filter((k) => k !== id))
            setUnassigning(null)
            reload()
          }}
        />
      )}
      {replacingDriver && (
        <ReplaceDriverModal
          schoolId={schoolId}
          trip={replacingDriver}
          drivers={masters.drivers}
          onClose={() => setReplacingDriver(null)}
          onDone={() => {
            setReplacingDriver(null)
            reload()
          }}
        />
      )}
      {replacingBus && (
        <ReplaceBusModal
          schoolId={schoolId}
          trip={replacingBus}
          buses={masters.buses}
          onClose={() => setReplacingBus(null)}
          onDone={() => {
            setReplacingBus(null)
            reload()
          }}
        />
      )}
      {bulkUnassignOpen && (
        <BulkUnassignModal
          schoolId={schoolId}
          trips={unassignableSelected}
          busy={bulkUnassigning}
          setBusy={setBulkUnassigning}
          onClose={() => setBulkUnassignOpen(false)}
          onDone={(successIds) => {
            setBulkUnassignOpen(false)
            setSelectedKeys((keys) => keys.filter((k) => !successIds.includes(k)))
            reload()
          }}
        />
      )}
    </Flex>
  )
}

function TripForm({
  schoolId,
  trip,
  initialRouteId,
  date,
  tripType,
  masters,
  dayTrips,
  timeZone,
  onClose,
  onSaved,
}: {
  schoolId: string
  trip: Trip | null
  initialRouteId?: string
  date: string
  tripType: TripType
  masters: Masters
  dayTrips: Trip[]
  timeZone?: string
  onClose: () => void
  onSaved: () => void
}) {
  const { message } = App.useApp()
  const [form] = Form.useForm<TripInput>()
  const [saving, setSaving] = useState(false)
  const type = trip?.trip_type ?? tripType
  const special = type === 'special_trip'
  const routeId = Form.useWatch('route_id', form)
  // HH:MM in the school's own time zone (not the admin's browser) — same
  // convention as Trips' own `clock`/`time` helpers above.
  const hhmm = useMemo(
    () =>
      new Intl.DateTimeFormat('en-GB', {
        hour: '2-digit',
        minute: '2-digit',
        hourCycle: 'h23',
        timeZone,
      }),
    [timeZone],
  )
  const toHHMM = (iso: string | null) => (iso ? hhmm.format(new Date(iso)) : null)

  // Prefill the schedule from the route's own stop times (never invented)
  // when creating a brand-new trip and a route is chosen — only for a new
  // trip: editing an existing one keeps its own already-saved schedule. The
  // admin can still edit either field afterwards; this only sets a starting
  // point, same as Trip Planner's Bulk Assign dialog.
  const [routeWindow, setRouteWindow] = useState<RouteScheduleWindow | null>(null)
  useEffect(() => {
    if (trip || !routeId || special) return
    let cancelled = false
    routesApi.schedule(schoolId, routeId).then((s) => {
      if (cancelled) return
      const w = type === 'morning_pickup' ? s.morning_pickup : s.evening_drop
      setRouteWindow(w)
      form.setFieldsValue({
        scheduled_start_time: w.start_time ?? undefined,
        scheduled_end_time: w.end_time ?? undefined,
      })
    })
    return () => {
      cancelled = true
    }
  }, [schoolId, routeId, type, trip, special, form])

  // Other trips of this type today, for same-bus/driver/route context — no longer
  // disabled (a bus/driver/route can now run more than one trip a day, as long as
  // their scheduled times don't overlap), just shown so the admin can see what
  // else that bus/driver/route is doing before picking a time. The backend's
  // own overlap check (trips_{route,bus,driver}_no_overlap) is the real guard;
  // see save()'s error handling below.
  const used = useMemo(() => {
    const slot = dayTrips.filter(
      (t) => t.trip_type === type && t.status !== 'cancelled' && t.id !== trip?.id,
    )
    const label = (t: Trip) => {
      const time = t.scheduled_start_at ? hhmm.format(new Date(t.scheduled_start_at)) : null
      const where = t.route?.code ?? t.title
      return time ? `${where} ${time}` : where
    }
    return {
      buses: new Map(slot.filter((t) => t.bus).map((t) => [t.bus!.id, label(t)])),
      drivers: new Map(slot.filter((t) => t.driver).map((t) => [t.driver!.id, label(t)])),
    }
  }, [dayTrips, type, trip, hhmm])

  async function save(v: TripInput) {
    setSaving(true)
    try {
      const input: TripInput = { ...v, trip_date: trip?.trip_date ?? date, trip_type: type }
      if (!special) {
        delete input.title
        delete input.notify_parents
        delete input.waypoints
      } else if (input.route_id) {
        // A route-based outing follows that route's own stops; waypoints don't apply.
        delete input.waypoints
      } else {
        delete input.route_id
        delete input.notify_parents // no template route: nothing to notify
      }
      if (trip) await tripsApi.update(schoolId, trip.id, input)
      else await tripsApi.create(schoolId, input)
      message.success(trip ? 'Trip updated.' : 'Trip assigned.')
      onSaved()
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const routes = masters.routes.filter((r) =>
    type === 'morning_pickup'
      ? r.supports_pickup
      : type === 'evening_drop'
        ? r.supports_drop
        : r.supports_outing,
  )

  return (
    <Modal
      open
      title={`${trip ? 'Edit' : 'Assign'} ${tripTypeLabels[type]} · ${formatDate(trip?.trip_date ?? date)}`}
      okText={trip ? 'Save' : 'Assign trip'}
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
      width={special ? 640 : undefined}
    >
      {trip?.status === 'confirmed' && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          title="The driver has confirmed this trip. Changing the driver sends it back for the new driver to confirm."
        />
      )}
      <Form
        form={form}
        layout="vertical"
        onFinish={save}
        initialValues={
          trip
            ? {
                route_id: trip.route?.id,
                bus_id: trip.bus?.id,
                driver_id: trip.driver?.id,
                notes: trip.notes,
                title: trip.title,
                notify_parents: trip.notify_parents,
                waypoints: [],
                scheduled_start_time: toHHMM(trip.scheduled_start_at) ?? undefined,
                scheduled_end_time: toHHMM(trip.scheduled_end_at) ?? undefined,
              }
            : { route_id: initialRouteId, notify_parents: true }
        }
      >
        {special && (
          <Typography.Paragraph type="secondary" style={{ marginTop: -8 }}>
            Pick a template route to reuse its stops and notify its usual parents, or leave it blank
            and add your own destinations below — School → destinations → School.
          </Typography.Paragraph>
        )}
        <Form.Item
          name="route_id"
          label={special ? 'Template route (optional)' : 'Route'}
          rules={special ? [] : [{ required: true, message: 'Choose a route.' }]}
          extra={
            routes.length === 0 && !special
              ? `No active route runs . Set it on the route (Routes & Stops).`
              : undefined
          }
        >
          <Select
            allowClear={special}
            showSearch={{ optionFilterProp: 'label' }}
            placeholder={special ? 'No template — ad-hoc destinations below' : undefined}
            options={routes.map((r) => ({
              value: r.id,
              label: `${r.code} · ${r.name}${!special && r.stop_count === 0 ? ' (no stops yet)' : ''}`,
              disabled: !special && r.stop_count === 0,
            }))}
          />
        </Form.Item>
        <Flex gap="small">
          <Form.Item
            name="scheduled_start_time"
            label="Scheduled start"
            style={{ flex: 1 }}
            rules={[{ required: true, message: 'Give a start time.' }]}
          >
            <Input type="time" />
          </Form.Item>
          <Form.Item
            name="scheduled_end_time"
            label="Scheduled end"
            style={{ flex: 1 }}
            rules={[{ required: true, message: 'Give an end time.' }]}
            extra="An end time at or before the start means the trip runs past midnight."
          >
            <Input type="time" />
          </Form.Item>
        </Flex>
        {!special && routeId && !trip && routeWindow && !routeWindow.start_time && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title={`This route's stops have no ${type === 'morning_pickup' ? 'pickup' : 'drop'} time configured yet — enter a schedule manually.`}
          />
        )}
        {special && !routeId && (
          <>
            <Form.Item
              name="title"
              label="Outing name"
              rules={[{ required: true, message: "Name this outing, e.g. 'Science Museum Trip'." }]}
            >
              <Input placeholder="e.g. Science Museum Trip" maxLength={200} />
            </Form.Item>
            <Form.List name="waypoints">
              {(fields, { add, remove }) => (
                <div style={{ marginBottom: 24 }}>
                  <Typography.Text strong>Destinations</Typography.Text>
                  <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
                    In travel order. The bus always starts and ends at school.
                  </Typography.Paragraph>
                  {fields.map((field, i) => (
                    <Flex key={field.key} gap="small" align="start" style={{ marginBottom: 8 }}>
                      <div style={{ flex: 1 }}>
                        <LocationSearch
                          placeholder={`Find destination ${i + 1}…`}
                          onSelect={(place) => {
                            const row = form.getFieldValue(['waypoints', field.name]) as
                              WaypointInput | undefined
                            form.setFields([
                              {
                                name: ['waypoints', field.name],
                                value: {
                                  ...row,
                                  name: row?.name || place.name,
                                  landmark: place.address,
                                  latitude: place.latitude,
                                  longitude: place.longitude,
                                },
                              },
                            ])
                          }}
                        />
                        <Flex gap="small" style={{ marginTop: 4 }}>
                          <Form.Item
                            name={[field.name, 'name']}
                            rules={[{ required: true, message: 'Name is required.' }]}
                            style={{ flex: 1, marginBottom: 0 }}
                          >
                            <Input placeholder="Name" maxLength={200} />
                          </Form.Item>
                          <Form.Item
                            name={[field.name, 'latitude']}
                            rules={[{ required: true, message: 'Pick a location.' }]}
                            style={{ width: 110, marginBottom: 0 }}
                          >
                            <InputNumber
                              placeholder="Lat"
                              precision={6}
                              style={{ width: '100%' }}
                            />
                          </Form.Item>
                          <Form.Item
                            name={[field.name, 'longitude']}
                            rules={[{ required: true, message: 'Pick a location.' }]}
                            style={{ width: 110, marginBottom: 0 }}
                          >
                            <InputNumber
                              placeholder="Lng"
                              precision={6}
                              style={{ width: '100%' }}
                            />
                          </Form.Item>
                        </Flex>
                      </div>
                      <Button
                        icon={<MinusCircleOutlined />}
                        onClick={() => remove(field.name)}
                        aria-label="Remove destination"
                      />
                    </Flex>
                  ))}
                  <Button
                    icon={<PlusOutlined />}
                    onClick={() => add({ name: '', landmark: '', geofence_radius_m: 100 })}
                  >
                    Add destination
                  </Button>
                </div>
              )}
            </Form.List>
          </>
        )}
        <Form.Item
          name="bus_id"
          label="Bus"
          rules={[{ required: true, message: 'Choose a bus.' }]}
          extra={
            masters.buses.length === 0
              ? 'No active buses. Add or activate one on the Buses page.'
              : 'A bus can run more than one trip a day if the times don’t overlap — others today are noted below.'
          }
        >
          <Select
            showSearch={{ optionFilterProp: 'label' }}
            options={masters.buses.map((b) => ({
              value: b.id,
              label: used.buses.has(b.id)
                ? `${b.vehicle_number} (also on ${used.buses.get(b.id)})`
                : `${b.vehicle_number} · ${b.capacity} seats`,
            }))}
          />
        </Form.Item>
        <Form.Item
          name="driver_id"
          label="Driver"
          rules={[{ required: true, message: 'Choose a driver.' }]}
          extra={
            masters.drivers.length === 0
              ? 'No active drivers. Add or activate one on the Drivers page.'
              : undefined
          }
        >
          <Select
            showSearch={{ optionFilterProp: 'label' }}
            options={masters.drivers.map((d) => ({
              value: d.id,
              label: used.drivers.has(d.id)
                ? `${d.name} (also on ${used.drivers.get(d.id)})`
                : `${d.name} · ${d.mobile}`,
            }))}
          />
        </Form.Item>
        {special && (
          <Form.Item
            name="notify_parents"
            label="Notify parents"
            valuePropName="checked"
            tooltip={
              routeId
                ? "Tell the template route's usual parents when this outing starts, is delayed or completes."
                : 'Pick a template route above to enable parent notifications for this outing. Staff can always view and track it either way.'
            }
          >
            <Switch disabled={!routeId} />
          </Form.Item>
        )}
        <Form.Item name="notes" label="Notes">
          <Input.TextArea rows={2} maxLength={500} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

function ReasonModal({
  schoolId,
  trip,
  action,
  onClose,
  onDone,
}: {
  schoolId: string
  trip: Trip
  action: 'cancel' | 'started' | 'completed' | 'cancelled'
  onClose: () => void
  onDone: () => void
}) {
  const { message } = App.useApp()
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const titles = {
    cancel: 'Cancel trip',
    started: 'Override: mark as started',
    completed: 'Override: mark as completed',
    cancelled: 'Override: cancel a trip in progress',
  }

  async function submit() {
    setBusy(true)
    try {
      if (action === 'cancel') await tripsApi.cancel(schoolId, trip.id, reason)
      else await tripsApi.override(schoolId, trip.id, action, reason)
      message.success('Trip updated.')
      onDone()
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title={`${titles[action]}: ${trip.route?.code ?? trip.title}`}
      okText="Confirm"
      okButtonProps={{
        danger: action === 'cancel' || action === 'cancelled',
        disabled: !reason.trim(),
      }}
      confirmLoading={busy}
      onOk={() => void submit()}
      onCancel={onClose}
    >
      {action !== 'cancel' && (
        <Typography.Paragraph type="secondary">
          Use an override only when the driver cannot update the trip themselves. It is recorded in
          the audit log.
        </Typography.Paragraph>
      )}
      <Input.TextArea
        rows={3}
        maxLength={500}
        placeholder="Reason (required), e.g. School holiday, driver's phone not working"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        autoFocus
      />
    </Modal>
  )
}

// UnassignModal: Part B, Single Trip Unassign. Shows the trip/date and its
// current driver/bus before confirming; removes only the assignment
// relationship — route, schedule, stops and students are untouched.
function UnassignModal({
  schoolId,
  trip,
  onClose,
  onDone,
}: {
  schoolId: string
  trip: Trip
  onClose: () => void
  onDone: () => void
}) {
  const { message } = App.useApp()
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      await tripsApi.unassign(schoolId, trip.id)
      message.success('Trip unassigned.')
      onDone()
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title={`Unassign: ${trip.route?.code ?? trip.title} · ${formatDate(trip.trip_date)}`}
      okText="Unassign"
      okButtonProps={{ danger: true }}
      confirmLoading={busy}
      onOk={() => void submit()}
      onCancel={onClose}
    >
      <Typography.Paragraph>
        This removes the bus and driver from this trip only. The route, schedule, stops and students
        stay exactly as they are, and the trip remains here to be reassigned.
      </Typography.Paragraph>
      <Space direction="vertical" size={4}>
        <Typography.Text>
          <strong>Current bus:</strong> {trip.bus?.vehicle_number ?? 'already unassigned'}
        </Typography.Text>
        <Typography.Text>
          <strong>Current driver:</strong> {trip.driver?.name ?? 'already unassigned'}
        </Typography.Text>
      </Space>
    </Modal>
  )
}

// ReplaceDriverModal: Part D/E, Replace Driver. One atomic swap — bus, route,
// schedule and everything else are untouched; the backend checks the new
// driver's availability for this trip's exact scheduled time and rejects a
// genuine overlap (preserving the original driver on failure).
function ReplaceDriverModal({
  schoolId,
  trip,
  drivers,
  onClose,
  onDone,
}: {
  schoolId: string
  trip: Trip
  drivers: Driver[]
  onClose: () => void
  onDone: () => void
}) {
  const { message } = App.useApp()
  const [form] = Form.useForm<{ driver_id: string }>()
  const [busy, setBusy] = useState(false)

  async function submit(v: { driver_id: string }) {
    setBusy(true)
    try {
      await tripsApi.reassignDriver(schoolId, trip.id, v.driver_id)
      message.success('Driver replaced.')
      onDone()
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title={`Replace driver: ${trip.route?.code ?? trip.title} · ${formatDate(trip.trip_date)}`}
      okText="Replace"
      confirmLoading={busy}
      onOk={() => form.submit()}
      onCancel={onClose}
    >
      <Typography.Paragraph type="secondary">
        Current driver: <strong>{trip.driver?.name ?? 'none'}</strong>.
        {trip.scheduled_start_at && trip.scheduled_end_at && (
          <>
            {' '}
            Scheduled {new Date(trip.scheduled_start_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}–
            {new Date(trip.scheduled_end_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}.
          </>
        )}{' '}
        The new driver is checked for a conflict at that exact time; the current driver is unaffected
        if the replacement fails.
      </Typography.Paragraph>
      <Form form={form} layout="vertical" onFinish={(v) => void submit(v)}>
        <Form.Item name="driver_id" label="New driver" rules={[{ required: true }]}>
          <Select
            showSearch
            optionFilterProp="label"
            placeholder="Choose a driver"
            options={drivers
              .filter((d) => d.id !== trip.driver?.id)
              .map((d) => ({ value: d.id, label: `${d.name} · ${d.mobile}` }))}
          />
        </Form.Item>
      </Form>
    </Modal>
  )
}

// ReplaceBusModal: Part F, Replace Bus. Same pattern as ReplaceDriverModal.
function ReplaceBusModal({
  schoolId,
  trip,
  buses,
  onClose,
  onDone,
}: {
  schoolId: string
  trip: Trip
  buses: Bus[]
  onClose: () => void
  onDone: () => void
}) {
  const { message } = App.useApp()
  const [form] = Form.useForm<{ bus_id: string }>()
  const [busy, setBusy] = useState(false)

  async function submit(v: { bus_id: string }) {
    setBusy(true)
    try {
      await tripsApi.reassignBus(schoolId, trip.id, v.bus_id)
      message.success('Bus replaced.')
      onDone()
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title={`Replace bus: ${trip.route?.code ?? trip.title} · ${formatDate(trip.trip_date)}`}
      okText="Replace"
      confirmLoading={busy}
      onOk={() => form.submit()}
      onCancel={onClose}
    >
      <Typography.Paragraph type="secondary">
        Current bus: <strong>{trip.bus?.vehicle_number ?? 'none'}</strong>. The new bus is checked for
        a conflict at this trip's exact scheduled time; the current bus is unaffected if the
        replacement fails.
      </Typography.Paragraph>
      <Form form={form} layout="vertical" onFinish={(v) => void submit(v)}>
        <Form.Item name="bus_id" label="New bus" rules={[{ required: true }]}>
          <Select
            showSearch
            optionFilterProp="label"
            placeholder="Choose a bus"
            options={buses
              .filter((b) => b.id !== trip.bus?.id)
              .map((b) => ({ value: b.id, label: `${b.vehicle_number} (${b.capacity} seats)` }))}
          />
        </Form.Item>
      </Form>
    </Modal>
  )
}

// BulkUnassignModal: Part C, Bulk Unassign. Shows a confirmation summary of
// exactly which selected trips will be unassigned, then reports a per-trip
// result — never a single pass/fail for the whole batch.
function BulkUnassignModal({
  schoolId,
  trips,
  busy,
  setBusy,
  onClose,
  onDone,
}: {
  schoolId: string
  trips: Trip[]
  busy: boolean
  setBusy: (b: boolean) => void
  onClose: () => void
  // Only the trip IDs that actually succeeded — the caller clears just
  // those from its selection, leaving any skipped one still selected (and,
  // once reloaded, still correctly offered for Bulk Unassign again).
  onDone: (successIds: string[]) => void
}) {
  const { message, modal } = App.useApp()

  async function submit() {
    setBusy(true)
    try {
      const res = await tripsApi.bulkUnassign(
        schoolId,
        trips.map((t) => t.id),
      )
      const ok = res.results.filter((r) => r.status === 'unassigned')
      const failed = res.results.filter((r) => r.status === 'skipped')
      if (failed.length === 0) {
        message.success(`${ok.length} trip(s) unassigned.`)
      } else {
        modal.info({
          title: `${ok.length} unassigned, ${failed.length} could not be`,
          width: 560,
          content: (
            <ul style={{ paddingLeft: 18, maxHeight: 320, overflow: 'auto' }}>
              {failed.map((f) => {
                const t = trips.find((x) => x.id === f.trip_id)
                return (
                  <li key={f.trip_id}>
                    {t ? `${t.route?.code ?? t.title} · ${formatDate(t.trip_date)}` : f.trip_id}:{' '}
                    {f.reason}
                  </li>
                )
              })}
            </ul>
          ),
        })
      }
      onDone(ok.map((r) => r.trip_id))
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title={`Unassign ${trips.length} trip(s)`}
      okText={`Unassign ${trips.length}`}
      okButtonProps={{ danger: true, disabled: trips.length === 0 }}
      confirmLoading={busy}
      onOk={() => void submit()}
      onCancel={onClose}
      width={560}
    >
      <Typography.Paragraph>
        Each trip's bus and driver will be cleared; route, schedule, stops and students stay
        unchanged. A trip already unassigned, or no longer open, is reported and skipped rather than
        failing the whole batch.
      </Typography.Paragraph>
      <ul style={{ paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
        {trips.map((t) => (
          <li key={t.id}>
            {t.route?.code ?? t.title} · {formatDate(t.trip_date)} · {tripTypeLabels[t.trip_type]}
            {' — '}
            {t.bus?.vehicle_number ?? 'no bus'} / {t.driver?.name ?? 'no driver'}
          </li>
        ))}
      </ul>
    </Modal>
  )
}

function CopyModal({
  schoolId,
  fromDate,
  today,
  workingDays,
  onClose,
  onDone,
}: {
  schoolId: string
  fromDate: string
  today: string
  workingDays: string[]
  onClose: () => void
  onDone: (firstDate: string | null) => void
}) {
  const { message, modal } = App.useApp()
  const [start, setStart] = useState(addDays(fromDate < today ? today : fromDate, 1))
  const [days, setDays] = useState(1)
  const [workingOnly, setWorkingOnly] = useState(true)
  const [busy, setBusy] = useState(false)

  const targets = Array.from({ length: days }, (_, i) => addDays(start, i)).filter(
    (d) =>
      d !== fromDate &&
      (!workingOnly || workingDays.length === 0 || workingDays.includes(weekdayOf(d))),
  )

  async function submit() {
    setBusy(true)
    try {
      const res = await tripsApi.copy(schoolId, { from_date: fromDate, to_dates: targets })
      if (res.skipped.length === 0) {
        message.success(`${res.created} trip(s) created.`)
      } else {
        modal.info({
          title: `${res.created} trip(s) created, ${res.skipped.length} skipped`,
          width: 600,
          content: (
            <ul style={{ paddingLeft: 18 }}>
              {res.skipped.map((s, i) => (
                <li key={i}>
                  {formatDate(s.trip_date)} · {tripTypeLabels[s.trip_type]} · {s.route_code}:{' '}
                  {s.reason}
                </li>
              ))}
            </ul>
          ),
        })
      }
      onDone(targets[0] ?? null)
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title={`Copy trips of ${formatDate(fromDate)}`}
      okText={`Copy to ${targets.length} day(s)`}
      okButtonProps={{ disabled: targets.length === 0 }}
      confirmLoading={busy}
      onOk={() => void submit()}
      onCancel={onClose}
    >
      <Typography.Paragraph type="secondary">
        Morning Pickup, Evening Drop and Special Trip/Outing are all copied with the same route (or
        outing details) and the same bus and driver. Trips that would clash with existing ones are
        skipped and listed.
      </Typography.Paragraph>
      <Flex gap="middle" wrap align="end">
        <div>
          <div>Starting</div>
          <Input
            type="date"
            min={today}
            value={start}
            onChange={(e) => e.target.value && setStart(e.target.value)}
          />
        </div>
        <div>
          <div>For</div>
          <InputNumber
            min={1}
            max={31}
            value={days}
            onChange={(v) => setDays(v ?? 1)}
            addonAfter="days"
          />
        </div>
      </Flex>
      <Checkbox
        checked={workingOnly}
        onChange={(e) => setWorkingOnly(e.target.checked)}
        style={{ marginTop: 12 }}
      >
        Working days only ({workingDays.join(', ') || 'not set'})
      </Checkbox>
      <Typography.Paragraph style={{ marginTop: 12 }}>
        {targets.length ? targets.map(formatDate).join(' · ') : 'No dates selected.'}
      </Typography.Paragraph>
    </Modal>
  )
}
