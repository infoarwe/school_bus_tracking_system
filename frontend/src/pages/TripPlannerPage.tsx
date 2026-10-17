import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  App,
  Button,
  Card,
  Flex,
  Segmented,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
  theme,
} from 'antd'
import {
  AppstoreAddOutlined,
  CalendarOutlined,
  FlagOutlined,
  LeftOutlined,
  MoonOutlined,
  RightOutlined,
  SunOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons'
import BulkAssignModal from '../components/BulkAssignModal'
import ListEmpty from '../components/ListEmpty'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import { TripStatusTag } from '../components/TripDetailDrawer'
import { useCurrentSchool } from '../context/SchoolContext'
import { useServerList } from '../hooks/useServerList'
import { busesApi, driversApi, routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type Bus,
  type BusRoute,
  type Driver,
  type Trip,
  type TripCalendarDay,
  type TripListParams,
  type TripType,
  type WeekDay,
} from '../services/types'
import { addDays, formatDate, todayIn, weekdayOf } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'

export default function TripPlannerPage() {
  return <RequireSchool>{(schoolId) => <Planner schoolId={schoolId} />}</RequireSchool>
}

interface Masters {
  routes: BusRoute[]
  buses: Bus[]
  drivers: Driver[]
}

type View = 'calendar' | 'list'

function Planner({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const { school } = useCurrentSchool()
  const today = todayIn(school?.timezone)
  const workingDays = (school?.working_days ?? []) as WeekDay[]
  const [view, setView] = useState<View>('calendar')
  const [masters, setMasters] = useState<Masters>({ routes: [], buses: [], drivers: [] })
  const [bulk, setBulk] = useState<{ from?: string } | null>(null)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    Promise.all([
      routesApi.list(schoolId, { status: 'active', page_size: 100 }),
      busesApi.list(schoolId, { status: 'active', page_size: 100 }),
      driversApi.list(schoolId, { status: 'active', page_size: 100 }),
    ])
      .then(([r, b, d]) => setMasters({ routes: r.data, buses: b.data, drivers: d.data }))
      .catch((e: unknown) => message.error(errorMessage(e)))
  }, [schoolId, message])

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Trip Planner"
        subtitle="See which days already have trips and assign upcoming days in bulk (up to 90 days ahead)."
        extra={
          <Button type="primary" icon={<AppstoreAddOutlined />} onClick={() => setBulk({})}>
            Bulk assign
          </Button>
        }
      />
      <Segmented<View>
        value={view}
        onChange={setView}
        options={[
          { value: 'calendar', label: 'Calendar', icon: <CalendarOutlined /> },
          { value: 'list', label: 'Upcoming trips', icon: <UnorderedListOutlined /> },
        ]}
        style={{ alignSelf: 'flex-start' }}
      />
      {view === 'calendar' ? (
        <MonthCalendar
          key={reloadKey}
          schoolId={schoolId}
          today={today}
          workingDays={workingDays}
          routes={masters.routes}
          onAssign={(from) => setBulk({ from })}
        />
      ) : (
        <UpcomingTrips key={reloadKey} schoolId={schoolId} today={today} masters={masters} />
      )}
      {bulk && (
        <BulkAssignModal
          schoolId={schoolId}
          today={today}
          workingDays={workingDays}
          routes={masters.routes}
          buses={masters.buses}
          drivers={masters.drivers}
          initial={bulk}
          onClose={() => setBulk(null)}
          onDone={() => {
            setBulk(null)
            setReloadKey((k) => k + 1)
          }}
        />
      )}
    </Flex>
  )
}

// --- Calendar ---

const weekHeader = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
const monthFmt = new Intl.DateTimeFormat(undefined, {
  month: 'long',
  year: 'numeric',
  timeZone: 'UTC',
})

/** First day (YYYY-MM-01) of the month containing date, moved by `months`. */
function monthStart(date: string, months = 0): string {
  const d = new Date(`${date.slice(0, 7)}-01T00:00:00Z`)
  d.setUTCMonth(d.getUTCMonth() + months)
  return d.toISOString().slice(0, 10)
}

/** The 6 × 7 grid of a month, Monday first. */
function monthGrid(first: string): string[] {
  const offset = (new Date(`${first}T00:00:00Z`).getUTCDay() + 6) % 7
  const start = addDays(first, -offset)
  return Array.from({ length: 42 }, (_, i) => addDays(start, i))
}

/** Colour of assigned vs. routes expected to run: full, partial or none. */
function coverColor(assigned: number, expected: number, past: boolean): string {
  if (past) return 'default'
  if (expected === 0) return assigned ? 'blue' : 'default'
  if (assigned >= expected) return 'green'
  return assigned > 0 ? 'orange' : 'red'
}

function MonthCalendar({
  schoolId,
  today,
  workingDays,
  routes,
  onAssign,
}: {
  schoolId: string
  today: string
  workingDays: WeekDay[]
  routes: BusRoute[]
  onAssign: (from: string) => void
}) {
  const { message } = App.useApp()
  const { token } = theme.useToken()
  const navigate = useNavigate()
  const [month, setMonth] = useState(() => monthStart(today))
  const [days, setDays] = useState<TripCalendarDay[]>([])
  const [loading, setLoading] = useState(true)
  const grid = useMemo(() => monthGrid(month), [month])
  const goMonth = (m: string) => {
    setLoading(true)
    setMonth(m)
  }

  useEffect(() => {
    let cancelled = false
    tripsApi
      .calendar(schoolId, grid[0], grid[grid.length - 1])
      .then((res) => !cancelled && setDays(res))
      .catch((e: unknown) => !cancelled && message.error(errorMessage(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [schoolId, grid, message])

  // Routes that should get a trip each working day (active, with stops).
  const expected = {
    morning_pickup: routes.filter((r) => r.stop_count > 0 && r.supports_pickup).length,
    evening_drop: routes.filter((r) => r.stop_count > 0 && r.supports_drop).length,
  }
  const counts = useMemo(() => {
    const m = new Map<string, TripCalendarDay>()
    for (const d of days) m.set(`${d.trip_date} ${d.trip_type}`, d)
    return m
  }, [days])
  const assigned = (date: string, t: TripType) => counts.get(`${date} ${t}`)?.assigned ?? 0
  const thisMonth = month.slice(0, 7)

  const line = (date: string, t: 'morning_pickup' | 'evening_drop', working: boolean) => {
    const n = assigned(date, t)
    const exp = working ? expected[t] : 0
    if (!n && !exp) return null
    return (
      <Tooltip title={`${tripTypeLabels[t]}: ${n} of ${exp} route(s) assigned`}>
        <Tag
          color={coverColor(n, exp, date < today)}
          icon={t === 'morning_pickup' ? <SunOutlined /> : <MoonOutlined />}
          style={{ marginInlineEnd: 0, fontSize: 11 }}
        >
          {exp ? `${n}/${exp}` : n}
        </Tag>
      </Tooltip>
    )
  }

  return (
    <Card size="small">
      <Flex justify="space-between" align="center" wrap gap="small" style={{ marginBottom: 12 }}>
        <Space>
          <Button
            icon={<LeftOutlined />}
            aria-label="Previous month"
            onClick={() => goMonth(monthStart(month, -1))}
          />
          <Typography.Title level={5} style={{ margin: 0, minWidth: 150, textAlign: 'center' }}>
            {monthFmt.format(new Date(`${month}T00:00:00Z`))}
          </Typography.Title>
          <Button
            icon={<RightOutlined />}
            aria-label="Next month"
            onClick={() => goMonth(monthStart(month, 1))}
          />
          <Button
            onClick={() => goMonth(monthStart(today))}
            disabled={thisMonth === today.slice(0, 7)}
          >
            This month
          </Button>
          {loading && <Spin size="small" />}
        </Space>
        <Space wrap size="small">
          <Tag color="green">All routes assigned</Tag>
          <Tag color="orange">Some</Tag>
          <Tag color="red">None</Tag>
          <Typography.Text type="secondary">
            <SunOutlined /> Morning · <MoonOutlined /> Evening · <FlagOutlined /> Special
          </Typography.Text>
        </Space>
      </Flex>
      <div style={{ overflowX: 'auto' }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(7, minmax(84px, 1fr))',
            gap: 4,
            minWidth: 620,
          }}
        >
          {weekHeader.map((d) => (
            <Typography.Text key={d} type="secondary" strong style={{ textAlign: 'center' }}>
              {d}
            </Typography.Text>
          ))}
          {grid.map((date) => {
            const inMonth = date.slice(0, 7) === thisMonth
            const working =
              workingDays.length === 0 || workingDays.includes(weekdayOf(date) as WeekDay)
            const special = assigned(date, 'special_trip')
            const isToday = date === today
            const canAssign = date >= today && date <= addDays(today, 90)
            return (
              <div
                key={date}
                role="button"
                tabIndex={0}
                aria-label={`Open trips of ${formatDate(date)}`}
                onClick={() => navigate(`/trips?date=${date}`)}
                onKeyDown={(e) => e.key === 'Enter' && navigate(`/trips?date=${date}`)}
                style={{
                  minHeight: 92,
                  padding: 6,
                  borderRadius: 8,
                  cursor: 'pointer',
                  border: `1px solid ${isToday ? token.colorPrimary : token.colorBorderSecondary}`,
                  background: !working ? token.colorFillQuaternary : undefined,
                  opacity: inMonth ? 1 : 0.45,
                }}
              >
                <Flex justify="space-between" align="center">
                  <Typography.Text
                    strong={isToday}
                    style={isToday ? { color: token.colorPrimary } : {}}
                  >
                    {Number(date.slice(8))}
                  </Typography.Text>
                  {canAssign && working && (
                    <Tooltip title="Bulk assign from this day">
                      <Button
                        type="text"
                        size="small"
                        icon={<AppstoreAddOutlined />}
                        aria-label={`Bulk assign from ${formatDate(date)}`}
                        onClick={(e) => {
                          e.stopPropagation()
                          onAssign(date)
                        }}
                      />
                    </Tooltip>
                  )}
                </Flex>
                <Flex vertical gap={2} align="flex-start" style={{ marginTop: 4 }}>
                  {line(date, 'morning_pickup', working)}
                  {line(date, 'evening_drop', working)}
                  {special > 0 && (
                    <Tag icon={<FlagOutlined />} style={{ marginInlineEnd: 0, fontSize: 11 }}>
                      {special}
                    </Tag>
                  )}
                  {!working && (
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      Off
                    </Typography.Text>
                  )}
                </Flex>
              </div>
            )
          })}
        </div>
      </div>
      <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
        Each day shows trips assigned out of the active routes that run that trip type. Click a day
        to open it in Daily Trips.
      </Typography.Paragraph>
    </Card>
  )
}

// --- Upcoming list ---

function UpcomingTrips({
  schoolId,
  today,
  masters,
}: {
  schoolId: string
  today: string
  masters: Masters
}) {
  const fetcher = useCallback(
    (p: TripListParams) => tripsApi.list(schoolId, { ...p, from: today, sort: 'asc' }),
    [schoolId, today],
  )
  const list = useServerList<Trip, TripListParams>(fetcher, { page_size: 50 })

  return (
    <Card size="small">
      <Flex gap="small" wrap style={{ marginBottom: 12 }}>
        <Select
          allowClear
          placeholder="Trip type"
          style={{ width: 170 }}
          value={list.params.trip_type}
          onChange={(v) => list.update({ trip_type: v })}
          options={(Object.keys(tripTypeLabels) as TripType[]).map((t) => ({
            value: t,
            label: tripTypeLabels[t],
          }))}
        />
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="Route"
          style={{ width: 200 }}
          value={list.params.route_id}
          onChange={(v) => list.update({ route_id: v })}
          options={masters.routes.map((r) => ({ value: r.id, label: `${r.code} · ${r.name}` }))}
        />
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="Bus"
          style={{ width: 170 }}
          value={list.params.bus_id}
          onChange={(v) => list.update({ bus_id: v })}
          options={masters.buses.map((b) => ({ value: b.id, label: b.vehicle_number }))}
        />
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="Driver"
          style={{ width: 170 }}
          value={list.params.driver_id}
          onChange={(v) => list.update({ driver_id: v })}
          options={masters.drivers.map((d) => ({ value: d.id, label: d.name }))}
        />
        <Select
          allowClear
          placeholder="Status"
          style={{ width: 150 }}
          value={list.params.status}
          onChange={(v) => list.update({ status: v })}
          options={['scheduled', 'confirmed', 'started', 'completed', 'cancelled'].map((s) => ({
            value: s,
            label: s[0].toUpperCase() + s.slice(1),
          }))}
        />
      </Flex>
      <Table<Trip>
        rowKey="id"
        size="middle"
        loading={list.loading}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 900 }}
        locale={{
          emptyText: (
            <ListEmpty list={list} empty="No upcoming trips yet. Use Bulk assign to plan ahead." />
          ),
        }}
        columns={[
          {
            title: 'Date',
            dataIndex: 'trip_date',
            width: 150,
            render: (d: string) => (
              <Link to={`/trips?date=${d}`}>
                {formatDate(d)}
                {d === today && (
                  <Tag color="green" style={{ marginInlineStart: 6 }}>
                    Today
                  </Tag>
                )}
              </Link>
            ),
          },
          {
            title: 'Trip type',
            dataIndex: 'trip_type',
            width: 150,
            render: (t: TripType) => tripTypeLabels[t],
          },
          {
            title: 'Route',
            render: (_, t) =>
              t.route ? (
                <Link to={`/routes/${t.route.id}`}>
                  {t.route.code} · {t.route.name}
                </Link>
              ) : (
                t.title
              ),
          },
          { title: 'Bus', render: (_, t) => t.bus.vehicle_number },
          { title: 'Driver', render: (_, t) => t.driver.name },
          {
            title: 'Assigned by',
            dataIndex: 'assigned_by',
            width: 120,
            render: (a: Trip['assigned_by']) =>
              a === 'driver' ? <Tag color="purple">Driver</Tag> : <Tag>School</Tag>,
          },
          {
            title: 'Status',
            dataIndex: 'status',
            width: 120,
            render: (s: Trip['status']) => <TripStatusTag status={s} />,
          },
        ]}
      />
    </Card>
  )
}
