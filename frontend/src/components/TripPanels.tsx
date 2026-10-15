// Building blocks shared by the driver and bus detail pages.
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  Button,
  Card,
  Divider,
  Empty,
  Flex,
  Select,
  Space,
  Table,
  Tag,
  Timeline,
  Typography,
  theme,
} from 'antd'
import {
  ArrowRightOutlined,
  CalendarOutlined,
  CarOutlined,
  EnvironmentOutlined,
  FlagOutlined,
  UserOutlined,
} from '@ant-design/icons'
import LoadingOrError from './LoadingOrError'
import { TripStatusTag } from './TripDetailDrawer'
import { auditApi } from '../services/admin'
import { tripTypeLabels, type AuditRow, type Trip, type TripDetail } from '../services/types'
import { PERIODS } from '../hooks/useEntityTrips'
import { formatDate } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'
import { tripTypeIcons } from '../utils/tripIcons'

type Clock = (iso: string | null) => string | null

/** The other side of a trip from the page's point of view: a bus page shows the driver. */
export type Counterpart = 'bus' | 'driver'

export function InfoBit({
  icon,
  label,
  value,
}: {
  icon: ReactNode
  label: string
  value: ReactNode
}) {
  const { token } = theme.useToken()
  return (
    <Flex gap="small" align="center">
      <span style={{ fontSize: 20, color: token.colorTextTertiary }}>{icon}</span>
      <Flex vertical>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {label}
        </Typography.Text>
        <Typography.Text>{value}</Typography.Text>
      </Flex>
    </Flex>
  )
}

/** A row of small figures with the period picker on the right. */
export function PeriodBar({
  items,
  period,
  onPeriod,
}: {
  items: { icon: ReactNode; label: string; value: ReactNode }[]
  period: number
  onPeriod: (days: number) => void
}) {
  return (
    <Card size="small" styles={{ body: { padding: '8px 16px' } }}>
      <Flex wrap gap="middle" align="center">
        {items.map((i, n) => (
          <Flex key={i.label} gap="middle" align="center">
            {n > 0 && <Divider orientation="vertical" />}
            <Space>
              <span style={{ fontSize: 18 }}>{i.icon}</span>
              <Typography.Text type="secondary">{i.label}</Typography.Text>
              <Typography.Text strong>{i.value}</Typography.Text>
            </Space>
          </Flex>
        ))}
        <Select
          value={period}
          onChange={onPeriod}
          aria-label="Period"
          style={{ marginInlineStart: 'auto', width: 150 }}
          prefix={<CalendarOutlined />}
          options={PERIODS.map((d) => ({ value: d, label: `Last ${d} days` }))}
        />
      </Flex>
    </Card>
  )
}

export function RouteBadge({ trip }: { trip: Trip }) {
  return trip.route ? (
    <Tag color="geekblue" style={{ fontWeight: 600 }}>
      {trip.route.code}
    </Tag>
  ) : (
    <strong>{trip.title}</strong>
  )
}

export function TripTable({
  rows,
  loading,
  compact,
  show,
  today,
  time,
  onOpen,
}: {
  rows: Trip[]
  loading: boolean
  compact?: boolean
  show: Counterpart
  today: string
  time: Clock
  onOpen: (id: string) => void
}) {
  return (
    <Table<Trip>
      rowKey="id"
      size="small"
      loading={loading}
      dataSource={rows}
      pagination={compact ? false : { pageSize: 10, hideOnSinglePage: true }}
      scroll={{ x: compact ? 620 : 820 }}
      rowClassName={(t) => (t.status === 'cancelled' ? 'row-muted' : '')}
      onRow={(t) => ({ onClick: () => onOpen(t.id), style: { cursor: 'pointer' } })}
      locale={{
        emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No trips." />,
      }}
      columns={[
        {
          title: 'Date',
          width: 150,
          render: (_, t) => (
            <Space size={4}>
              {formatDate(t.trip_date)}
              {t.trip_date === today && <Tag color="green">Today</Tag>}
            </Space>
          ),
        },
        {
          title: 'Trip',
          width: 150,
          render: (_, t) => (
            <Space size={6}>
              {tripTypeIcons[t.trip_type]}
              {tripTypeLabels[t.trip_type]}
            </Space>
          ),
        },
        {
          title: 'Times',
          width: 110,
          render: (_, t) =>
            t.started_at ? `${time(t.started_at)} – ${time(t.ended_at) ?? 'now'}` : '—',
        },
        { title: 'Route', render: (_, t) => <RouteBadge trip={t} /> },
        show === 'bus'
          ? { title: 'Bus', width: 130, render: (_, t: Trip) => t.bus.vehicle_number }
          : {
              title: 'Driver',
              width: 170,
              render: (_, t: Trip) => (
                <Link to={`/drivers/${t.driver.id}`} onClick={(e) => e.stopPropagation()}>
                  {t.driver.name}
                </Link>
              ),
            },
        ...(show === 'driver' && !compact
          ? [
              {
                title: 'Students',
                width: 90,
                align: 'right' as const,
                render: (_: unknown, t: Trip) => `${t.student_count} / ${t.bus.capacity}`,
              },
            ]
          : []),
        {
          title: 'Status',
          width: 130,
          render: (_, t) => (
            <Space size={4}>
              <TripStatusTag status={t.status} />
              {t.assigned_by === 'driver' && !compact && (
                <Tag color="purple" title="The driver took this route in the Driver app">
                  Self
                </Tag>
              )}
            </Space>
          ),
        },
      ]}
    />
  )
}

/** Stops of a trip in travel order. */
function travelOrder(t: TripDetail) {
  return [...t.stops].sort((a, b) =>
    t.trip_type === 'evening_drop'
      ? (a.drop_sequence ?? 999) - (b.drop_sequence ?? 999) || a.sequence - b.sequence
      : a.sequence - b.sequence,
  )
}

/** Today's trips as tinted cards: times, route, first → last stop, status. */
export function TodaySchedule({
  details,
  show,
  time,
  onOpen,
  onLive,
}: {
  details: TripDetail[] | null
  show: Counterpart
  time: Clock
  onOpen: (id: string) => void
  onLive: (id: string) => void
}) {
  const { token } = theme.useToken()
  if (details === null) return <Typography.Text type="secondary">Loading…</Typography.Text>
  if (!details.length)
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No trips today." />
  const sorted = [...details].sort((a, b) => b.trip_type.localeCompare(a.trip_type))
  return (
    <Timeline
      items={sorted.map((t) => {
        const stops = travelOrder(t)
        const timeOf = (s: (typeof stops)[number] | undefined) =>
          s && (t.trip_type === 'evening_drop' ? s.drop_time : (s.pickup_time ?? s.drop_time))
        const first = stops[0]
        const last = stops[stops.length - 1]
        const from = time(t.started_at) ?? timeOf(first)?.slice(0, 5) ?? '—'
        const to = time(t.ended_at) ?? timeOf(last)?.slice(0, 5) ?? '—'
        const tint =
          t.trip_type === 'morning_pickup'
            ? token.gold1
            : t.trip_type === 'evening_drop'
              ? token.geekblue1
              : token.purple1
        return {
          color: t.status === 'started' ? 'green' : t.status === 'completed' ? 'blue' : 'gray',
          content: (
            <Flex gap="middle" align="stretch" wrap>
              <Flex vertical style={{ width: 64 }}>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  {t.started_at ? 'Started' : 'From'}
                </Typography.Text>
                <Typography.Text strong>{from}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  {t.ended_at ? 'Ended' : 'To'}
                </Typography.Text>
                <Typography.Text strong>{to}</Typography.Text>
              </Flex>
              <Flex
                vertical
                gap={4}
                role="button"
                tabIndex={0}
                onClick={() => onOpen(t.id)}
                onKeyDown={(e) => e.key === 'Enter' && onOpen(t.id)}
                style={{
                  flex: 1,
                  minWidth: 220,
                  cursor: 'pointer',
                  background: tint,
                  borderRadius: token.borderRadiusLG,
                  padding: '10px 12px',
                }}
              >
                <Flex justify="space-between" wrap gap="small">
                  <Space>
                    <span style={{ fontSize: 18 }}>{tripTypeIcons[t.trip_type]}</span>
                    <Typography.Text strong>{tripTypeLabels[t.trip_type]}</Typography.Text>
                  </Space>
                  {show === 'bus' ? (
                    <Space size={4}>
                      <CarOutlined /> {t.bus.vehicle_number}
                    </Space>
                  ) : (
                    <Space size={4}>
                      <UserOutlined /> {t.driver.name}
                    </Space>
                  )}
                </Flex>
                <Space size={6} wrap>
                  {t.route ? (
                    <>
                      <Tag color="geekblue" style={{ fontWeight: 600, marginInlineEnd: 0 }}>
                        {t.route.code}
                      </Tag>
                      {t.route.name}
                    </>
                  ) : (
                    <strong>{t.title}</strong>
                  )}
                  {show === 'driver' && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      · {t.student_count} students
                    </Typography.Text>
                  )}
                </Space>
                <Flex justify="space-between" align="center" wrap gap="small">
                  {first && last ? (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      <EnvironmentOutlined /> {first.name} <ArrowRightOutlined /> {last.name}
                    </Typography.Text>
                  ) : (
                    <span />
                  )}
                  <Space size={4}>
                    <TripStatusTag status={t.status} />
                    {t.status === 'started' && (
                      <Button
                        size="small"
                        type="link"
                        icon={<EnvironmentOutlined />}
                        onClick={(e) => {
                          e.stopPropagation()
                          onLive(t.id)
                        }}
                      >
                        Live
                      </Button>
                    )}
                  </Space>
                </Flex>
              </Flex>
            </Flex>
          ),
        }
      })}
    />
  )
}

interface GroupRow {
  key: string
  trip: Trip
  trips: number
  completed: number
  last: string
}

function groupTrips(trips: Trip[], keyOf: (t: Trip) => string) {
  const map = new Map<string, GroupRow>()
  for (const t of trips) {
    const key = keyOf(t)
    const row = map.get(key) ?? { key, trip: t, trips: 0, completed: 0, last: t.trip_date }
    row.trips++
    if (t.status === 'completed') row.completed++
    if (t.trip_date > row.last) row.last = t.trip_date
    map.set(key, row)
  }
  return [...map.values()].sort((a, b) => b.trips - a.trips)
}

/** Trips grouped by route, or by the counterpart (bus or driver): count, completed, last. */
export function TripGroups({
  trips,
  loading,
  by,
}: {
  trips: Trip[]
  loading: boolean
  by: 'route' | Counterpart
}) {
  const rows = useMemo(
    () =>
      groupTrips(trips, (t) =>
        by === 'route'
          ? (t.route?.id ?? `outing:${t.title}`)
          : by === 'bus'
            ? t.bus.id
            : t.driver.id,
      ),
    [trips, by],
  )
  const label = (t: Trip) =>
    by === 'route' ? (
      t.route ? (
        <Space size={6}>
          <Tag color="geekblue" style={{ fontWeight: 600, marginInlineEnd: 0 }}>
            {t.route.code}
          </Tag>
          <Link to={`/routes/${t.route.id}`}>{t.route.name}</Link>
        </Space>
      ) : (
        <Space size={6}>
          <FlagOutlined /> {t.title}
        </Space>
      )
    ) : by === 'bus' ? (
      <Link to={`/buses/${t.bus.id}`}>
        <CarOutlined /> {t.bus.vehicle_number}
      </Link>
    ) : (
      <Link to={`/drivers/${t.driver.id}`}>
        <UserOutlined /> {t.driver.name}
      </Link>
    )
  return (
    <Table<GroupRow>
      rowKey="key"
      size="small"
      loading={loading}
      pagination={false}
      dataSource={rows}
      locale={{
        emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No trips." />,
      }}
      columns={[
        {
          title: { route: 'Route', bus: 'Bus', driver: 'Driver' }[by],
          render: (_, r) => label(r.trip),
        },
        { title: 'Trips', dataIndex: 'trips', width: 80, align: 'right' },
        { title: 'Completed', dataIndex: 'completed', width: 100, align: 'right' },
        {
          title: 'Last trip',
          dataIndex: 'last',
          width: 150,
          render: (d: string) => formatDate(d),
        },
      ]}
    />
  )
}

const actionText: Record<string, string> = {
  'driver.create': 'Driver added',
  'driver.update': 'Profile updated',
  'driver.status_change': 'Status changed',
  'bus.create': 'Bus added',
  'bus.update': 'Bus details updated',
  'bus.status_change': 'Status changed',
  'trip.create': 'Trip assigned',
  'trip.update': 'Trip changed',
  'trip.claim': 'Took a route in the Driver app',
  'trip.release': 'Released a route',
  'trip.cancel': 'Trip cancelled',
}

const describe = (action: string) =>
  actionText[action] ?? action.replace(/[._]/g, ' ').replace(/^\w/, (c) => c.toUpperCase())

/**
 * Audit entries about a record (entityId), plus, for a person with a login, what they
 * did themselves (actorUserId), newest first.
 */
export function AuditTimeline({
  schoolId,
  entityId,
  actorUserId,
  timeZone,
}: {
  schoolId: string
  entityId: string
  actorUserId?: string
  timeZone: string | undefined
}) {
  const [rows, setRows] = useState<AuditRow[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const fetchRows = useCallback(() => {
    Promise.all([
      auditApi.school(schoolId, { entity_id: entityId, page_size: 50 }),
      actorUserId
        ? auditApi.school(schoolId, { actor_id: actorUserId, page_size: 50 })
        : Promise.resolve({ data: [] as AuditRow[] }),
    ])
      .then(([about, by]) => {
        const seen = new Set<string>()
        setRows(
          [...about.data, ...by.data]
            .filter((r) => (seen.has(r.id) ? false : (seen.add(r.id), true)))
            .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
            .slice(0, 50),
        )
      })
      .catch((e: unknown) => setError(errorMessage(e)))
  }, [schoolId, entityId, actorUserId])

  useEffect(fetchRows, [fetchRows])
  const retry = () => {
    setError(null)
    fetchRows()
  }

  if (!rows) return <LoadingOrError error={error} onRetry={retry} />
  if (!rows.length)
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No activity recorded yet." />
  const when = (iso: string) =>
    new Date(iso).toLocaleString(undefined, {
      day: 'numeric',
      month: 'short',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      timeZone,
    })
  return (
    <Timeline
      items={rows.map((r) => {
        const bySelf = !!actorUserId && r.actor_id === actorUserId
        return {
          color:
            r.action.includes('cancel') || r.action.includes('emergency')
              ? 'red'
              : bySelf
                ? 'green'
                : 'blue',
          content: (
            <>
              <Typography.Text strong>{describe(r.action)}</Typography.Text>
              {bySelf && (
                <Tag color="green" style={{ marginInlineStart: 8 }}>
                  by driver
                </Tag>
              )}
              <br />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {when(r.created_at)}
                {!bySelf &&
                  r.actor_name &&
                  ` · ${r.actor_name} (${r.actor_role.replace('_', ' ')})`}
              </Typography.Text>
            </>
          ),
        }
      })}
    />
  )
}
