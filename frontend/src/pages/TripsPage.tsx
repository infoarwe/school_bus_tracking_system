import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Alert,
  App,
  Button,
  Checkbox,
  Descriptions,
  Drawer,
  Dropdown,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Timeline,
  Typography,
} from 'antd'
import {
  CopyOutlined,
  DownOutlined,
  HistoryOutlined,
  MinusCircleOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import LocationSearch from '../components/LocationSearch'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StopProgressList from '../components/StopProgressList'
import TripReplay from '../components/TripReplay'
import { useCurrentSchool } from '../context/SchoolContext'
import { useTripProgress } from '../hooks/useTripProgress'
import { busesApi, driversApi, routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type Bus,
  type BusRoute,
  type Driver,
  type Trip,
  type TripDetail,
  type TripInput,
  type TripStatus,
  type TripType,
  type WaypointInput,
} from '../services/types'
import { addDays, formatDate, todayIn, weekdayOf } from '../utils/dates'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const statusColors: Record<TripStatus, string> = {
  scheduled: 'default',
  confirmed: 'cyan',
  started: 'green',
  completed: 'blue',
  cancelled: 'red',
}

function TripStatusTag({ status }: { status: TripStatus }) {
  return <Tag color={statusColors[status]}>{status[0].toUpperCase() + status.slice(1)}</Tag>
}

const isOpen = (t: Trip) => t.status === 'scheduled' || t.status === 'confirmed'

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

  const trips = dayTrips.filter((t) => t.trip_type === tripType)
  const active = trips.filter((t) => t.status !== 'cancelled')
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
      <Flex gap="small" wrap align="center">
        <Input
          type="date"
          value={date}
          style={{ width: 170 }}
          onChange={(e) => {
            if (!e.target.value) return
            setLoading(true)
            setDate(e.target.value)
          }}
        />
        <Button
          onClick={() => {
            setLoading(true)
            setDate(today)
          }}
          disabled={date === today}
        >
          Today
        </Button>
        <Typography.Text type="secondary">{formatDate(date)}</Typography.Text>
        <Segmented<TripType>
          value={tripType}
          onChange={setTripType}
          options={[
            {
              value: 'morning_pickup',
              label: `Morning Pickup (${dayTrips.filter((t) => t.trip_type === 'morning_pickup' && t.status !== 'cancelled').length})`,
            },
            {
              value: 'evening_drop',
              label: `Evening Drop (${dayTrips.filter((t) => t.trip_type === 'evening_drop' && t.status !== 'cancelled').length})`,
            },
            {
              value: 'special_trip',
              label: `Special Trip / Outing (${dayTrips.filter((t) => t.trip_type === 'special_trip' && t.status !== 'cancelled').length})`,
            },
          ]}
        />
      </Flex>

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

      <Table<Trip>
        rowKey="id"
        loading={loading}
        dataSource={trips}
        pagination={false}
        scroll={{ x: 1000 }}
        locale={{ emptyText: `No ${tripTypeLabels[tripType]} trips on this date.` }}
        rowClassName={(t) => (t.status === 'cancelled' ? 'row-muted' : '')}
        columns={[
          {
            title: 'Route',
            render: (_, t) => (
              <a onClick={() => setViewing(t.id)}>
                {t.trip_type === 'special_trip' && (
                  <Tag color="purple" style={{ marginInlineEnd: 6 }}>
                    Outing
                  </Tag>
                )}
                {t.route ? (
                  <>
                    <strong>{t.route.code}</strong> · {t.route.name}
                  </>
                ) : (
                  <strong>{t.title}</strong>
                )}
              </a>
            ),
          },
          {
            title: 'Bus',
            render: (_, t) => t.bus.vehicle_number,
          },
          {
            title: 'Driver',
            render: (_, t) => (
              <>
                {t.driver.name}
                {t.assigned_by === 'driver' && (
                  <Tag
                    color="purple"
                    style={{ marginInlineStart: 6 }}
                    title="The driver took this route in the Driver app"
                  >
                    Self-assigned
                  </Tag>
                )}
                <br />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {t.driver.mobile}
                </Typography.Text>
              </>
            ),
          },
          {
            title: 'Students / seats',
            width: 130,
            render: (_, t) => (
              <Typography.Text type={t.student_count > t.bus.capacity ? 'danger' : undefined}>
                {t.student_count} / {t.bus.capacity}
              </Typography.Text>
            ),
          },
          {
            title: 'Status',
            width: 120,
            render: (_, t) => <TripStatusTag status={t.status} />,
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
                    Edit
                  </Button>
                )}
                {(isOpen(t) || t.status === 'started') && (
                  <Dropdown
                    menu={{
                      items: [
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
                      onClick: ({ key }) => setReasonFor({ trip: t, action: key as 'cancel' }),
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
        <TripDrawer schoolId={schoolId} tripId={viewing} onClose={() => setViewing(null)} />
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
  onClose: () => void
  onSaved: () => void
}) {
  const { message } = App.useApp()
  const [form] = Form.useForm<TripInput>()
  const [saving, setSaving] = useState(false)
  const type = trip?.trip_type ?? tripType
  const special = type === 'special_trip'
  const routeId = Form.useWatch('route_id', form)

  // Buses and drivers already used in this slot (other than by this trip) cannot be chosen.
  const used = useMemo(() => {
    const slot = dayTrips.filter(
      (t) => t.trip_type === type && t.status !== 'cancelled' && t.id !== trip?.id,
    )
    const label = (t: Trip) => t.route?.code ?? t.title
    return {
      routes: new Set(slot.map((t) => t.route?.id).filter((id): id is string => !!id)),
      buses: new Map(slot.map((t) => [t.bus.id, label(t)])),
      drivers: new Map(slot.map((t) => [t.driver.id, label(t)])),
    }
  }, [dayTrips, type, trip])

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
                bus_id: trip.bus.id,
                driver_id: trip.driver.id,
                notes: trip.notes,
                title: trip.title,
                notify_parents: trip.notify_parents,
                waypoints: [],
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
              label: `${r.code} · ${r.name}${used.routes.has(r.id) ? ' (already has this trip)' : !special && r.stop_count === 0 ? ' (no stops yet)' : ''}`,
              disabled: (!special && r.stop_count === 0) || used.routes.has(r.id),
            }))}
          />
        </Form.Item>
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
              : 'Buses already on another route in this slot are greyed out.'
          }
        >
          <Select
            showSearch={{ optionFilterProp: 'label' }}
            options={masters.buses.map((b) => ({
              value: b.id,
              label: used.buses.has(b.id)
                ? `${b.vehicle_number} (on ${used.buses.get(b.id)})`
                : `${b.vehicle_number} · ${b.capacity} seats`,
              disabled: used.buses.has(b.id),
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
                ? `${d.name} (on ${used.drivers.get(d.id)})`
                : `${d.name} · ${d.mobile}`,
              disabled: used.drivers.has(d.id),
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

function TripDrawer({
  schoolId,
  tripId,
  onClose,
}: {
  schoolId: string
  tripId: string
  onClose: () => void
}) {
  const { message } = App.useApp()
  const [trip, setTrip] = useState<TripDetail | null>(null)
  const live = useTripProgress(schoolId, tripId)
  const [replaying, setReplaying] = useState(false)

  useEffect(() => {
    tripsApi
      .get(schoolId, tripId)
      .then(setTrip)
      .catch((e: unknown) => message.error(errorMessage(e)))
  }, [schoolId, tripId, message])

  return (
    <Drawer
      open
      onClose={onClose}
      size="large"
      loading={!trip}
      title={trip && `${trip.route?.code ?? trip.title} · ${tripTypeLabels[trip.trip_type]}`}
    >
      {trip && (
        <Flex vertical gap="large">
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="Date">{formatDate(trip.trip_date)}</Descriptions.Item>
            <Descriptions.Item label="Status">
              <TripStatusTag status={trip.status} />
              {trip.cancel_reason && ` ${trip.cancel_reason}`}
            </Descriptions.Item>
            <Descriptions.Item label={trip.route ? 'Route' : 'Outing'}>
              {trip.route ? `${trip.route.code} · ${trip.route.name}` : trip.title}
            </Descriptions.Item>
            <Descriptions.Item label="Bus">{`${trip.bus.vehicle_number} (${trip.bus.capacity} seats)`}</Descriptions.Item>
            <Descriptions.Item label="Driver">{`${trip.driver.name} · ${trip.driver.mobile}`}</Descriptions.Item>
            <Descriptions.Item label="Assigned by">
              {trip.assigned_by === 'driver'
                ? 'The driver (took it in the Driver app)'
                : 'School staff'}
            </Descriptions.Item>
            <Descriptions.Item label="Students">{trip.student_count}</Descriptions.Item>
            {trip.trip_type === 'special_trip' && (
              <Descriptions.Item label="Notify parents">
                {trip.route
                  ? trip.notify_parents
                    ? "Yes — the template route's usual parents"
                    : 'No'
                  : 'No — this outing has no template route'}
              </Descriptions.Item>
            )}
            {trip.notes && <Descriptions.Item label="Notes">{trip.notes}</Descriptions.Item>}
          </Descriptions>
          {trip.started_at && (
            <Button
              icon={<HistoryOutlined />}
              onClick={() => setReplaying(true)}
              style={{ alignSelf: 'flex-start' }}
            >
              Replay GPS
            </Button>
          )}
          {replaying && (
            <TripReplay
              schoolId={schoolId}
              tripId={trip.id}
              title={`${trip.route?.code ?? trip.title} · ${tripTypeLabels[trip.trip_type]} · ${formatDate(trip.trip_date)}`}
              stops={trip.stops}
              onClose={() => setReplaying(false)}
            />
          )}

          <div>
            <Typography.Title level={5}>Stop progress</Typography.Title>
            {live?.progress ? (
              <>
                <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
                  Live from GPS · ETA{' '}
                  {live.progress.eta_source === 'google' ? 'by Google' : 'estimated'}
                </Typography.Paragraph>
                <StopProgressList stops={live.progress.stops} />
              </>
            ) : live?.events.length ? (
              <Timeline
                items={live.events.map((e) => ({
                  color: e.missed ? 'red' : e.event_type === 'reached' ? 'green' : 'blue',
                  content: (
                    <>
                      {e.event_type === 'school_reached'
                        ? 'Reached school'
                        : `${e.missed ? 'Missed' : e.event_type[0].toUpperCase() + e.event_type.slice(1)} ${e.stop_name ?? ''}`}
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {' · '}
                        {new Date(e.occurred_at).toLocaleTimeString()}
                      </Typography.Text>
                    </>
                  ),
                }))}
              />
            ) : (
              <Typography.Text type="secondary">
                Stop status appears here once the trip starts and the bus sends GPS.
              </Typography.Text>
            )}
          </div>

          <div>
            <Typography.Title level={5}>Stops</Typography.Title>
            <Table
              rowKey="id"
              size="small"
              pagination={false}
              dataSource={trip.stops}
              columns={[
                { title: '#', dataIndex: 'sequence', width: 50 },
                { title: 'Stop', dataIndex: 'name' },
                {
                  title:
                    trip.trip_type === 'morning_pickup'
                      ? 'Pickup'
                      : trip.trip_type === 'evening_drop'
                        ? 'Drop'
                        : 'Scheduled',
                  render: (_, s) =>
                    (trip.trip_type === 'morning_pickup' ? s.pickup_time : s.drop_time) ?? '—',
                },
                { title: 'Students', dataIndex: 'student_count', width: 90 },
              ]}
            />
          </div>

          <div>
            <Typography.Title level={5}>Status history</Typography.Title>
            <Timeline
              items={trip.history.map((h) => ({
                color:
                  h.to_status === 'cancelled'
                    ? 'red'
                    : h.to_status === 'started'
                      ? 'green'
                      : 'blue',
                content: (
                  <>
                    <strong>{h.to_status}</strong>
                    {h.reason && ` · ${h.reason}`}
                    <br />
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {new Date(h.created_at).toLocaleString()}
                      {h.changed_by_name &&
                        ` · ${h.changed_by_name} (${h.changed_by_role.replace('_', ' ')})`}
                    </Typography.Text>
                  </>
                ),
              }))}
            />
          </div>
        </Flex>
      )}
    </Drawer>
  )
}
