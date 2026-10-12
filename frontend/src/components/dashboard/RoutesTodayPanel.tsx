import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Empty, Flex, Grid, Segmented, Table, Typography } from 'antd'
import { ArrowRightOutlined } from '@ant-design/icons'
import {
  tripTypeLabels,
  type BusRoute,
  type DashboardData,
  type Trip,
  type TripStatus,
  type TripType,
} from '../../services/types'

type RouteRow = DashboardData['routes'][number]

// Readable trip status badges (text, not colour alone).
const statusBadge: Record<TripStatus, { pill: string; label: string }> = {
  scheduled: { pill: 'slate', label: 'Scheduled' },
  confirmed: { pill: 'slate', label: 'Confirmed' },
  started: { pill: 'blue', label: 'In progress' },
  completed: { pill: 'green', label: 'Completed' },
  cancelled: { pill: 'red', label: 'Cancelled' },
}

function Pill({ tone, children, title }: { tone: string; children: string; title?: string }) {
  return (
    <span className={`pill pill--${tone}`} title={title}>
      {children}
    </span>
  )
}

type Filter = 'all' | 'attention' | 'running'

interface Slot {
  runs: boolean
  status: TripStatus | null
  /** The driver took the route in the Driver app (self-assignment). */
  selfAssigned: boolean
  /** No driver: never taken, or the driver gave it back. */
  needsDriver: boolean
  bus: string | null
  driver: string | null
}

const RELEASED = 'Released by the driver'

/** Today's state of one route and trip type, joining the dashboard row with today's trips. */
function slot(r: RouteRow, type: TripType, trips: Trip[] | null, route?: BusRoute): Slot {
  const runs = route
    ? type === 'morning_pickup'
      ? route.supports_pickup
      : route.supports_drop
    : true
  const forSlot = (trips ?? []).filter((t) => t.route?.id === r.route_id && t.trip_type === type)
  const active = forSlot.find((t) => t.status !== 'cancelled')
  const trip = active ?? forSlot[0]
  // Prefer today's trip (it knows who assigned it); fall back to the dashboard row.
  const status = trip?.status ?? (type === 'morning_pickup' ? r.morning_status : r.evening_status)
  const released = !active && !!trip?.cancel_reason.startsWith(RELEASED)
  return {
    runs,
    status: released ? null : status,
    selfAssigned: !!active && active.assigned_by === 'driver',
    needsDriver: runs && (status === null || released),
    bus: active?.bus.vehicle_number ?? (type === 'morning_pickup' ? r.morning_bus : r.evening_bus),
    driver: active?.driver.name ?? null,
  }
}

function needsAttention(r: RouteRow, slots: Slot[]) {
  return (
    r.delays > 0 ||
    r.missed_stops > 0 ||
    slots.some((s) => s.needsDriver || (s.runs && s.status === 'cancelled'))
  )
}

function SlotCell({ s }: { s: Slot }) {
  if (!s.runs) return <Pill tone="muted">Not run</Pill>
  if (s.needsDriver || !s.status) return <Pill tone="orange">Needs driver</Pill>
  const b =
    s.selfAssigned && (s.status === 'scheduled' || s.status === 'confirmed')
      ? { pill: 'purple', label: 'Claimed' }
      : statusBadge[s.status]
  return (
    <Flex vertical gap={2}>
      <span>
        <Pill tone={b.pill} title={s.selfAssigned ? 'The driver took this route' : undefined}>
          {b.label}
        </Pill>
      </span>
      {(s.bus || s.driver) && (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {[s.bus, s.driver].filter(Boolean).join(' · ')}
        </Typography.Text>
      )}
    </Flex>
  )
}

function Issues({ r }: { r: RouteRow }) {
  if (!r.delays && !r.missed_stops) return <Pill tone="muted">None</Pill>
  return (
    <Flex gap={4} wrap>
      {r.delays > 0 && <Pill tone="orange">{`Delayed${r.delays > 1 ? ` ×${r.delays}` : ''}`}</Pill>}
      {r.missed_stops > 0 && (
        <Pill tone="red">{`${r.missed_stops} missed stop${r.missed_stops > 1 ? 's' : ''}`}</Pill>
      )}
    </Flex>
  )
}

export default function RoutesTodayPanel({
  routes,
  trips,
  routeMasters,
}: {
  routes: RouteRow[]
  trips: Trip[] | null
  /** Active routes, to know which trip types each route runs. */
  routeMasters: Record<string, BusRoute>
}) {
  const navigate = useNavigate()
  const screens = Grid.useBreakpoint()
  const [filter, setFilter] = useState<Filter>('all')

  const rows = routes.map((r) => {
    const m = slot(r, 'morning_pickup', trips, routeMasters[r.route_id])
    const e = slot(r, 'evening_drop', trips, routeMasters[r.route_id])
    return { r, m, e, attention: needsAttention(r, [m, e]) }
  })
  const counts = {
    attention: rows.filter((x) => x.attention).length,
    running: rows.filter((x) => x.m.status === 'started' || x.e.status === 'started').length,
  }
  const shown = rows.filter((x) =>
    filter === 'attention'
      ? x.attention
      : filter === 'running'
        ? x.m.status === 'started' || x.e.status === 'started'
        : true,
  )

  const open = (r: RouteRow, s: Slot) =>
    s.needsDriver ? navigate('/trips') : navigate(`/routes/${r.route_id}`)

  return (
    <Card
      className="dash-panel"
      title="Today's routes"
      extra={
        <Button type="link" onClick={() => navigate('/trips')} style={{ paddingInline: 0 }}>
          Daily trips <ArrowRightOutlined />
        </Button>
      }
    >
      <Flex vertical gap="middle">
        <Segmented<Filter>
          aria-label="Filter routes"
          value={filter}
          onChange={setFilter}
          options={[
            { value: 'all', label: `All (${rows.length})` },
            { value: 'attention', label: `Needs attention (${counts.attention})` },
            { value: 'running', label: `In progress (${counts.running})` },
          ]}
        />
        {routes.length === 0 ? (
          <Empty description="No active routes yet. Create routes in Routes & Stops.">
            <Button onClick={() => navigate('/routes?new=1')}>Add route</Button>
          </Empty>
        ) : shown.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              filter === 'attention' ? 'Nothing needs attention.' : 'No trips in progress.'
            }
          />
        ) : screens.md ? (
          <Table
            rowKey={(x) => x.r.route_id}
            size="middle"
            dataSource={shown}
            pagination={{ pageSize: 8, hideOnSinglePage: true }}
            scroll={{ x: 820 }}
            columns={[
              {
                title: 'Route',
                render: (_, { r }) => (
                  <Flex vertical>
                    <Typography.Text strong>{r.code}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {r.name}
                    </Typography.Text>
                  </Flex>
                ),
              },
              {
                title: 'Students',
                width: 90,
                align: 'right' as const,
                render: (_, { r }) => r.students,
              },
              {
                title: tripTypeLabels.morning_pickup,
                render: (_, { m }) => <SlotCell s={m} />,
              },
              {
                title: tripTypeLabels.evening_drop,
                render: (_, { e }) => <SlotCell s={e} />,
              },
              { title: 'Issues', width: 150, render: (_, { r }) => <Issues r={r} /> },
              {
                title: '',
                width: 110,
                render: (_, { r, m, e }) =>
                  m.needsDriver || e.needsDriver ? (
                    <Button size="small" onClick={() => navigate('/trips')}>
                      Assign
                    </Button>
                  ) : (
                    <Button
                      size="small"
                      type="link"
                      onClick={() => navigate(`/routes/${r.route_id}`)}
                    >
                      View route
                    </Button>
                  ),
              },
            ]}
          />
        ) : (
          <div className="route-cards">
            {shown.map(({ r, m, e }) => (
              <div key={r.route_id} className="route-card">
                <Flex justify="space-between" align="baseline" gap={8}>
                  <Typography.Text strong>
                    {r.code} <Typography.Text type="secondary">· {r.name}</Typography.Text>
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12, flex: 'none' }}>
                    {r.students} students
                  </Typography.Text>
                </Flex>
                <Flex vertical gap={8} style={{ marginTop: 8 }}>
                  {(
                    [
                      ['morning_pickup', m],
                      ['evening_drop', e],
                    ] as const
                  ).map(([type, s]) => (
                    <Flex key={type} justify="space-between" align="flex-start" gap={8}>
                      <Typography.Text style={{ fontSize: 13 }}>
                        {tripTypeLabels[type]}
                      </Typography.Text>
                      <SlotCell s={s} />
                    </Flex>
                  ))}
                  <Flex justify="space-between" align="center" gap={8}>
                    <Issues r={r} />
                    <Button size="small" onClick={() => open(r, m.needsDriver ? m : e)}>
                      {m.needsDriver || e.needsDriver ? 'Assign' : 'View route'}
                    </Button>
                  </Flex>
                </Flex>
              </div>
            ))}
          </div>
        )}
      </Flex>
    </Card>
  )
}
