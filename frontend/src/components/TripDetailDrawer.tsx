import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Descriptions,
  Drawer,
  Flex,
  Space,
  Steps,
  Table,
  Tabs,
  Tag,
  Timeline,
  Tooltip,
  Typography,
  theme,
} from 'antd'
import {
  CarOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  EnvironmentOutlined,
  FieldTimeOutlined,
  HistoryOutlined,
  PhoneOutlined,
  TeamOutlined,
  UserOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import RouteSnapshotCard from './RouteSnapshotCard'
import TripReplay from './TripReplay'
import { useCurrentSchool } from '../context/SchoolContext'
import { useTripProgress } from '../hooks/useTripProgress'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type RouteSnapshot,
  type StopEventRow,
  type StopProgress,
  type TripDetail,
  type TripStatus,
} from '../services/types'
import { formatDate } from '../utils/dates'
import { formatDistance, formatEta } from '../utils/format'
import { errorMessage } from '../utils/formErrors'

const statusColors: Record<TripStatus, string> = {
  scheduled: 'default',
  confirmed: 'cyan',
  started: 'green',
  completed: 'blue',
  cancelled: 'red',
}

const capitalize = (s: string) => s[0].toUpperCase() + s.slice(1)

export function TripStatusTag({ status }: { status: TripStatus }) {
  return <Tag color={statusColors[status]}>{capitalize(status)}</Tag>
}

function minutesBetween(from: string, to: string) {
  return Math.round((new Date(to).getTime() - new Date(from).getTime()) / 60000)
}

function formatMinutes(min: number) {
  return min < 60 ? `${min} min` : `${Math.floor(min / 60)} h ${min % 60} min`
}

/** "07:45" or "07:45:00" → minutes after midnight. */
function clockMinutes(hhmm: string) {
  const [h, m] = hhmm.split(':').map(Number)
  return h * 60 + m
}

type RowStatus = 'upcoming' | 'approaching' | 'reached' | 'crossed' | 'missed' | 'no_record'

const rowStatusTags: Record<RowStatus, { color: string; label: string }> = {
  upcoming: { color: 'default', label: 'Upcoming' },
  approaching: { color: 'orange', label: 'Approaching' },
  reached: { color: 'green', label: 'At stop' },
  crossed: { color: 'blue', label: 'Served' },
  missed: { color: 'red', label: 'Missed' },
  no_record: { color: 'default', label: 'No GPS record' },
}

interface StopRow {
  key: string
  order: number | null
  name: string
  isSchool: boolean
  scheduled: string | null
  arrivedAt: string | null
  leftAt: string | null
  status: RowStatus
  etaSeconds: number | null
  distanceM: number | null
  students: number | null
}

/**
 * Staff view of one daily trip: summary, lifecycle, stop-by-stop schedule against
 * what GPS recorded, the travelled route and a combined activity log.
 */
export default function TripDetailDrawer({
  schoolId,
  tripId,
  onClose,
}: {
  schoolId: string
  tripId: string
  onClose: () => void
}) {
  const { message } = App.useApp()
  const { token } = theme.useToken()
  const { school } = useCurrentSchool()
  const timeZone = school?.timezone || undefined
  const [trip, setTrip] = useState<TripDetail | null>(null)
  const [snapshot, setSnapshot] = useState<RouteSnapshot | null>(null)
  const [replaying, setReplaying] = useState(false)
  const live = useTripProgress(schoolId, tripId)

  useEffect(() => {
    tripsApi
      .get(schoolId, tripId)
      .then(setTrip)
      .catch((e: unknown) => message.error(errorMessage(e)))
  }, [schoolId, tripId, message])

  const fmt = useMemo(() => {
    const clock = new Intl.DateTimeFormat('en-GB', {
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
      timeZone,
    })
    const clockSec = new Intl.DateTimeFormat('en-GB', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
      timeZone,
    })
    const dateTime = new Intl.DateTimeFormat(undefined, {
      day: 'numeric',
      month: 'short',
      hour: '2-digit',
      minute: '2-digit',
      timeZone,
    })
    return {
      time: (iso: string | null) => (iso ? clock.format(new Date(iso)) : null),
      timeSec: (iso: string) => clockSec.format(new Date(iso)),
      dateTime: (iso: string) => dateTime.format(new Date(iso)),
    }
  }, [timeZone])

  const rows = useMemo(
    () => (trip ? stopRows(trip, live?.progress?.stops ?? null, live?.events ?? []) : []),
    [trip, live],
  )

  if (!trip) return <Drawer open onClose={onClose} size="large" loading />

  const statusTint = {
    scheduled: token.colorFillQuaternary,
    confirmed: token.cyan1,
    started: token.green1,
    completed: token.blue1,
    cancelled: token.red1,
  }[trip.status]
  const name = trip.route ? `${trip.route.code} · ${trip.route.name}` : trip.title
  const stopRowsOnly = rows.filter((r) => !r.isSchool)
  const served = stopRowsOnly.filter((r) => r.status === 'crossed' || r.status === 'reached')
  const missed = stopRowsOnly.filter((r) => r.status === 'missed')
  const lateness = rows.map((r) => punctuality(r, fmt.time)).filter((d): d is number => d != null)
  const onTime = lateness.filter((d) => d <= 5).length
  const endOrNow = trip.ended_at ?? (trip.status === 'started' ? new Date().toISOString() : null)
  const duration = trip.started_at && endOrNow ? minutesBetween(trip.started_at, endOrNow) : null
  const timeColumn =
    trip.trip_type === 'morning_pickup'
      ? 'Pickup'
      : trip.trip_type === 'evening_drop'
        ? 'Drop'
        : 'Scheduled'

  return (
    <Drawer
      open
      onClose={onClose}
      size="large"
      title={
        <Flex vertical>
          <Space size="small" wrap>
            <span>{name}</span>
            <TripStatusTag status={trip.status} />
          </Space>
          <Typography.Text type="secondary" style={{ fontSize: 13, fontWeight: 'normal' }}>
            {tripTypeLabels[trip.trip_type]} · {formatDate(trip.trip_date)}
          </Typography.Text>
        </Flex>
      }
      extra={
        trip.started_at && (
          <Button icon={<HistoryOutlined />} onClick={() => setReplaying(true)}>
            Replay GPS
          </Button>
        )
      }
    >
      <Flex vertical gap="middle">
        {trip.status === 'cancelled' && (
          <Alert
            type="error"
            showIcon
            title="This trip was cancelled"
            description={trip.cancel_reason || undefined}
          />
        )}

        <Card size="small" style={{ background: statusTint }}>
          <Steps size="small" {...lifecycle(trip, fmt.time)} />
        </Card>

        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))',
            gap: 12,
          }}
        >
          <StatTile
            tone="blue"
            icon={<ClockCircleOutlined />}
            title={trip.status === 'started' ? 'Duration so far' : 'Duration'}
            value={duration != null ? formatMinutes(duration) : '—'}
          />
          <StatTile
            tone="cyan"
            icon={<EnvironmentOutlined />}
            title="Distance"
            value={snapshot && snapshot.point_count > 1 ? formatDistance(snapshot.distance_m) : '—'}
          />
          <StatTile
            tone="green"
            icon={<CheckCircleOutlined />}
            title="Stops served"
            value={`${served.length} / ${stopRowsOnly.length}`}
          />
          <StatTile
            tone={missed.length ? 'red' : 'green'}
            icon={<WarningOutlined />}
            title="Missed stops"
            value={missed.length}
          />
          <StatTile
            tone="gold"
            icon={<FieldTimeOutlined />}
            title={
              <Tooltip title="Stops reached no more than 5 minutes after the scheduled time">
                On time
              </Tooltip>
            }
            value={lateness.length ? `${onTime} / ${lateness.length}` : '—'}
          />
          <StatTile
            tone="purple"
            icon={<TeamOutlined />}
            title="Students / seats"
            value={`${trip.student_count} / ${trip.bus.capacity}`}
          />
        </div>

        <Descriptions
          size="small"
          bordered
          column={{ xs: 1, sm: 2 }}
          items={[
            {
              key: 'bus',
              label: (
                <>
                  <CarOutlined /> Bus
                </>
              ),
              children: `${trip.bus.vehicle_number} · ${trip.bus.capacity} seats`,
            },
            {
              key: 'driver',
              label: (
                <>
                  <UserOutlined /> Driver
                </>
              ),
              children: (
                <Flex vertical>
                  <span>{trip.driver.name}</span>
                  <Typography.Link href={`tel:${trip.driver.mobile}`} style={{ fontSize: 12 }}>
                    <PhoneOutlined /> {trip.driver.mobile}
                  </Typography.Link>
                </Flex>
              ),
            },
            {
              key: 'assigned',
              label: 'Assigned by',
              children:
                trip.assigned_by === 'driver' ? (
                  <Tag color="purple">Driver (self-assigned in app)</Tag>
                ) : (
                  <Tag>School staff</Tag>
                ),
            },
            {
              key: 'created',
              label: 'Created',
              children: fmt.dateTime(trip.created_at),
            },
            ...(trip.trip_type === 'special_trip'
              ? [
                  {
                    key: 'notify',
                    label: 'Notify parents',
                    span: 2,
                    children: trip.route
                      ? trip.notify_parents
                        ? "Yes — the template route's usual parents"
                        : 'No'
                      : 'No — this outing has no template route',
                  },
                ]
              : []),
            ...(trip.notes
              ? [{ key: 'notes', label: 'Notes', span: 2, children: trip.notes }]
              : []),
          ]}
        />

        <Tabs
          defaultActiveKey="stops"
          items={[
            {
              key: 'stops',
              label: `Stops (${stopRowsOnly.length})`,
              children: (
                <Flex vertical gap="small">
                  {live?.progress && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      Live from GPS · ETA{' '}
                      {live.progress.eta_source === 'google' ? 'by Google' : 'estimated'}
                    </Typography.Text>
                  )}
                  <Table<StopRow>
                    rowKey="key"
                    size="small"
                    pagination={false}
                    dataSource={rows}
                    scroll={{ x: 560 }}
                    columns={[
                      {
                        title: '#',
                        width: 44,
                        render: (_, r) => (r.isSchool ? '🏫' : r.order),
                      },
                      {
                        title: 'Stop',
                        render: (_, r) => (r.isSchool ? <strong>{r.name}</strong> : r.name),
                      },
                      {
                        title: timeColumn,
                        width: 80,
                        render: (_, r) => r.scheduled?.slice(0, 5) ?? '—',
                      },
                      {
                        title: 'Arrived',
                        width: 110,
                        render: (_, r) => {
                          const t = fmt.time(r.arrivedAt)
                          if (!t) return '—'
                          const late = punctuality(r, fmt.time)
                          return (
                            <Flex vertical>
                              <span>{t}</span>
                              {late != null && <PunctualityText minutes={late} />}
                            </Flex>
                          )
                        },
                      },
                      {
                        title: 'Left',
                        width: 70,
                        render: (_, r) => fmt.time(r.leftAt) ?? '—',
                      },
                      {
                        title: 'Status',
                        width: 130,
                        render: (_, r) => (
                          <Flex vertical align="flex-start">
                            <Tag color={rowStatusTags[r.status].color}>
                              {rowStatusTags[r.status].label}
                            </Tag>
                            {r.etaSeconds != null && (
                              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                                ETA {formatEta(r.etaSeconds)} · {formatDistance(r.distanceM)}
                              </Typography.Text>
                            )}
                          </Flex>
                        ),
                      },
                      {
                        title: 'Students',
                        width: 80,
                        align: 'right',
                        render: (_, r) => r.students ?? '—',
                      },
                    ]}
                  />
                  {!trip.started_at && trip.status !== 'cancelled' && (
                    <Typography.Text type="secondary">
                      Arrival times and stop status appear once the trip starts and the bus sends
                      GPS.
                    </Typography.Text>
                  )}
                </Flex>
              ),
            },
            ...(trip.status === 'completed'
              ? [
                  {
                    key: 'map',
                    label: 'Travelled route',
                    // Rendered up front so the distance above is known without opening the tab.
                    forceRender: true,
                    children: (
                      <RouteSnapshotCard
                        schoolId={schoolId}
                        tripId={trip.id}
                        onLoaded={setSnapshot}
                        fileName={`${trip.trip_date}_${trip.route?.code ?? 'trip'}_${trip.trip_type}.png`.replace(
                          /[^\w.-]+/g,
                          '-',
                        )}
                      />
                    ),
                  },
                ]
              : []),
            {
              key: 'activity',
              label: 'Activity',
              children: (
                <ActivityLog trip={trip} events={live?.events ?? []} timeSec={fmt.timeSec} />
              ),
            },
          ]}
        />
      </Flex>

      {replaying && (
        <TripReplay
          schoolId={schoolId}
          tripId={trip.id}
          title={`${trip.route?.code ?? trip.title} · ${tripTypeLabels[trip.trip_type]} · ${formatDate(trip.trip_date)}`}
          stops={trip.stops}
          onClose={() => setReplaying(false)}
        />
      )}
    </Drawer>
  )
}

export type Tone = 'blue' | 'cyan' | 'green' | 'red' | 'gold' | 'purple' | 'geekblue'

/** A small tinted tile: coloured icon, label and value. Clickable when onClick is given. */
export function StatTile({
  tone,
  icon,
  title,
  value,
  hint,
  active,
  onClick,
}: {
  tone: Tone
  icon: ReactNode
  title: ReactNode
  value: ReactNode
  /** A small line under the value, e.g. the period it covers. */
  hint?: ReactNode
  /** Highlighted as the selected filter. */
  active?: boolean
  onClick?: () => void
}) {
  const { token } = theme.useToken()
  const fg = token[`${tone}7`]
  return (
    <Flex
      gap={10}
      align="center"
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      aria-pressed={onClick ? !!active : undefined}
      onClick={onClick}
      onKeyDown={(e) => {
        if (onClick && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault()
          onClick()
        }
      }}
      style={{
        background: token[`${tone}1`],
        border: `${active ? 2 : 1}px solid ${active ? fg : token[`${tone}3`]}`,
        borderRadius: token.borderRadiusLG,
        padding: active ? '9px 11px' : '10px 12px',
        cursor: onClick ? 'pointer' : undefined,
        boxShadow: active ? token.boxShadowTertiary : undefined,
      }}
    >
      <Flex
        align="center"
        justify="center"
        style={{
          width: 34,
          height: 34,
          flex: 'none',
          borderRadius: '50%',
          background: fg,
          color: '#fff',
          fontSize: 16,
        }}
      >
        {icon}
      </Flex>
      <Flex vertical style={{ minWidth: 0 }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {title}
        </Typography.Text>
        <span style={{ fontSize: 18, fontWeight: 600, color: fg, lineHeight: 1.3 }}>{value}</span>
        {hint && (
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {hint}
          </Typography.Text>
        )}
      </Flex>
    </Flex>
  )
}

/** Minutes late (negative = early) at a stop, against its scheduled time. */
function punctuality(r: StopRow, time: (iso: string | null) => string | null): number | null {
  const arrived = time(r.arrivedAt)
  if (!arrived || !r.scheduled) return null
  return clockMinutes(arrived) - clockMinutes(r.scheduled)
}

function PunctualityText({ minutes }: { minutes: number }) {
  if (minutes > 5)
    return (
      <Typography.Text type="danger" style={{ fontSize: 12 }}>
        {minutes} min late
      </Typography.Text>
    )
  if (minutes < -2)
    return (
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {-minutes} min early
      </Typography.Text>
    )
  return (
    <Typography.Text type="success" style={{ fontSize: 12 }}>
      on time
    </Typography.Text>
  )
}

/** Stops in travel order, merged with live progress or the recorded stop events. */
function stopRows(
  trip: TripDetail,
  progress: StopProgress[] | null,
  events: StopEventRow[],
): StopRow[] {
  const students = new Map(trip.stops.map((s) => [s.id, s.student_count]))
  const isMorning = trip.trip_type === 'morning_pickup'
  const scheduledOf = (s: TripDetail['stops'][number]) =>
    trip.trip_type === 'evening_drop'
      ? s.drop_time
      : isMorning
        ? s.pickup_time
        : (s.pickup_time ?? s.drop_time)

  if (progress) {
    return progress.map((p, i) => ({
      key: p.stop_id,
      order: p.stop_id === 'school' ? null : i + 1,
      name: p.stop_id === 'school' ? 'School' : p.name,
      isSchool: p.stop_id === 'school',
      scheduled: p.scheduled_time,
      arrivedAt: p.reached_at,
      leftAt: p.missed ? null : p.crossed_at,
      status: p.missed ? 'missed' : p.status,
      etaSeconds: p.status === 'upcoming' || p.status === 'approaching' ? p.eta_seconds : null,
      distanceM: p.distance_m,
      students: p.stop_id === 'school' ? null : (students.get(p.stop_id) ?? 0),
    }))
  }

  const ordered = [...trip.stops].sort((a, b) =>
    trip.trip_type === 'evening_drop'
      ? (a.drop_sequence ?? 999) - (b.drop_sequence ?? 999) || a.sequence - b.sequence
      : a.sequence - b.sequence,
  )
  const ended = trip.status === 'completed' || trip.status === 'cancelled'
  const rows: StopRow[] = ordered.map((s, i) => {
    const mine = events.filter((e) => e.stop_id === s.id)
    const at = (type: StopEventRow['event_type']) =>
      mine.find((e) => e.event_type === type && !e.missed)?.occurred_at ?? null
    const reached = at('reached')
    const crossed = at('crossed')
    const status: RowStatus = mine.some((e) => e.missed)
      ? 'missed'
      : crossed
        ? 'crossed'
        : reached
          ? 'reached'
          : at('approaching')
            ? 'approaching'
            : ended && trip.started_at
              ? 'no_record'
              : 'upcoming'
    return {
      key: s.id,
      order: i + 1,
      name: s.name,
      isSchool: false,
      scheduled: scheduledOf(s),
      arrivedAt: reached,
      leftAt: crossed,
      status,
      etaSeconds: null,
      distanceM: null,
      students: s.student_count,
    }
  })
  const school = events.find((e) => e.event_type === 'school_reached')
  if (isMorning && school) {
    rows.push({
      key: 'school',
      order: null,
      name: 'School',
      isSchool: true,
      scheduled: null,
      arrivedAt: school.occurred_at,
      leftAt: null,
      status: 'reached',
      etaSeconds: null,
      distanceM: null,
      students: null,
    })
  }
  return rows
}

/** Scheduled → Confirmed → Started → Completed (or Cancelled), with the time of each step. */
function lifecycle(trip: TripDetail, time: (iso: string | null) => string | null) {
  const steps = [
    { title: 'Scheduled', at: trip.created_at },
    { title: 'Confirmed', at: trip.confirmed_at },
    { title: 'Started', at: trip.started_at },
    { title: 'Completed', at: trip.ended_at },
  ]
  const reached = steps.filter((s) => s.at).length
  if (trip.status === 'cancelled') {
    const done = steps.slice(0, reached)
    return {
      current: done.length,
      status: 'error' as const,
      items: [
        ...done.map((s) => ({ title: s.title, content: time(s.at) })),
        { title: 'Cancelled', content: time(trip.cancelled_at) },
      ],
    }
  }
  return {
    current: trip.status === 'completed' ? steps.length : Math.max(reached - 1, 0),
    status: trip.status === 'started' ? ('process' as const) : ('finish' as const),
    items: steps.map((s) => ({ title: s.title, content: time(s.at) ?? undefined })),
  }
}

/** Status changes and stop events in one timeline, oldest first. */
function ActivityLog({
  trip,
  events,
  timeSec,
}: {
  trip: TripDetail
  events: StopEventRow[]
  timeSec: (iso: string) => string
}) {
  const items = [
    ...trip.history.map((h) => ({
      at: h.created_at,
      color: h.to_status === 'cancelled' ? 'red' : h.to_status === 'started' ? 'green' : 'blue',
      text: (
        <>
          Trip <strong>{h.to_status}</strong>
          {h.reason && ` · ${h.reason}`}
        </>
      ),
      by: h.changed_by_name && `${h.changed_by_name} (${h.changed_by_role.replace('_', ' ')})`,
    })),
    ...events.map((e) => ({
      at: e.occurred_at,
      color: e.missed ? 'red' : e.event_type === 'approaching' ? 'gray' : 'green',
      text:
        e.event_type === 'school_reached'
          ? 'Bus reached school'
          : `${e.missed ? 'Missed' : capitalize(e.event_type)} ${e.stop_name ?? ''}`,
      by: 'GPS',
    })),
  ].sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime())

  return (
    <Timeline
      items={items.map((i) => ({
        color: i.color,
        content: (
          <>
            {i.text}
            <br />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {timeSec(i.at)}
              {i.by && ` · ${i.by}`}
            </Typography.Text>
          </>
        ),
      }))}
    />
  )
}
