import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  Alert,
  App,
  Avatar,
  Button,
  Card,
  Col,
  Descriptions,
  Divider,
  Dropdown,
  Flex,
  Row,
  Segmented,
  Space,
  Tabs,
  Tag,
  Typography,
  theme,
} from 'antd'
import {
  ArrowRightOutlined,
  BarChartOutlined,
  CalendarOutlined,
  CarOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  DownOutlined,
  EditOutlined,
  EnvironmentOutlined,
  FileTextOutlined,
  HistoryOutlined,
  NodeIndexOutlined,
  PushpinOutlined,
  TeamOutlined,
  ToolOutlined,
  UserOutlined,
  WifiOutlined,
} from '@ant-design/icons'
import LoadingOrError from '../components/LoadingOrError'
import RequireSchool from '../components/RequireSchool'
import StatusTag from '../components/StatusTag'
import TripDetailDrawer, { StatTile } from '../components/TripDetailDrawer'
import {
  AuditTimeline,
  InfoBit,
  PeriodBar,
  TodaySchedule,
  TripGroups,
  TripTable,
} from '../components/TripPanels'
import { useCurrentSchool } from '../context/SchoolContext'
import { AHEAD_DAYS, useEntityTrips } from '../hooks/useEntityTrips'
import { useSchoolClock } from '../hooks/useSchoolClock'
import { driverAssignmentsApi } from '../services/driverAssignments'
import { busesApi } from '../services/transport'
import {
  tripTypeLabels,
  type Bus,
  type BusStatus,
  type DriverRouteAssignment,
} from '../services/types'
import { avatarColor, initials } from '../utils/avatar'
import { formatDate, todayIn } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'
import { tripTypeIcons } from '../utils/tripIcons'
import { BusForm } from './BusesPage'

const STATUSES: BusStatus[] = ['active', 'maintenance', 'inactive']
const statusActions: Record<BusStatus, string> = {
  active: 'Mark active',
  maintenance: 'Send to maintenance',
  inactive: 'Deactivate',
}

type TabKey = 'overview' | 'trips' | 'routes' | 'activity'

export default function BusDetailPage() {
  return <RequireSchool>{(schoolId) => <BusDetail schoolId={schoolId} />}</RequireSchool>
}

/** The vehicle number styled like an Indian number plate. */
function NumberPlate({ number, size = 16 }: { number: string; size?: number }) {
  return (
    <span
      style={{
        display: 'inline-block',
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
        fontWeight: 700,
        fontSize: size,
        letterSpacing: 1,
        color: '#111827',
        background: '#fefce8',
        border: '2px solid #111827',
        borderRadius: 6,
        padding: '2px 10px',
        lineHeight: 1.3,
        whiteSpace: 'nowrap',
      }}
    >
      {number}
    </span>
  )
}

function BusDetail({ schoolId }: { schoolId: string }) {
  const { busId = '' } = useParams()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const { token } = theme.useToken()
  const { school } = useCurrentSchool()
  const timeZone = school?.timezone || undefined
  const today = todayIn(timeZone)
  const time = useSchoolClock(timeZone)
  const [bus, setBus] = useState<Bus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [period, setPeriod] = useState(30)
  const [assignments, setAssignments] = useState<DriverRouteAssignment[]>([])
  const [editing, setEditing] = useState(false)
  const [viewing, setViewing] = useState<string | null>(null)
  const [tab, setTab] = useState<TabKey>('overview')
  const [tripsView, setTripsView] = useState<'upcoming' | 'history'>('upcoming')
  const t = useEntityTrips(schoolId, { bus_id: busId }, period, today)

  const loadBus = useCallback(() => {
    busesApi
      .get(schoolId, busId)
      .then(setBus)
      .catch((e: unknown) => setError(errorMessage(e)))
  }, [schoolId, busId])

  useEffect(loadBus, [loadBus])

  // The assignments endpoint filters by driver only; a school has few, so filter here.
  useEffect(() => {
    driverAssignmentsApi
      .list(schoolId)
      .then((all) => setAssignments(all.filter((a) => a.bus.id === busId)))
      .catch(() => setAssignments([]))
  }, [schoolId, busId])

  const load = useMemo(() => {
    const ran = t.past.filter((x) => x.status !== 'cancelled')
    const peak = ran.reduce((m, x) => Math.max(m, x.student_count), 0)
    return { peak, drivers: new Set(ran.map((x) => x.driver.id)).size }
  }, [t.past])

  if (!bus) return <LoadingOrError error={error} onRetry={loadBus} />

  const periodLabel = `Last ${period} days`
  const shown = (n: number) => (t.loading ? '—' : n)

  async function changeStatus(status: BusStatus) {
    if (!bus) return
    try {
      setBus(await busesApi.setStatus(schoolId, bus.id, status))
      message.success(`${bus.vehicle_number} is now ${status}.`)
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  const cardTitle = (icon: ReactNode, text: string) => (
    <Space>
      <span style={{ color: token.colorPrimary }}>{icon}</span>
      {text}
    </Space>
  )
  const manageLink = (
    <Link to="/driver-assignments">
      Manage <ArrowRightOutlined />
    </Link>
  )
  const capacityPct = bus.capacity ? Math.round((load.peak / bus.capacity) * 100) : 0

  const overview = (
    <Flex vertical gap="middle">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={14}>
          <Card
            size="small"
            title={cardTitle(<CalendarOutlined />, "Today's schedule")}
            extra={
              <Space>
                <Tag>{formatDate(today)}</Tag>
                <a onClick={() => setTab('trips')}>
                  View all <ArrowRightOutlined />
                </a>
              </Space>
            }
            style={{ height: '100%' }}
          >
            <TodaySchedule
              details={t.todayDetails}
              show="driver"
              time={time}
              onOpen={setViewing}
              onLive={(id) => navigate(`/live?trip=${id}`)}
            />
          </Card>
        </Col>
        <Col xs={24} xl={10}>
          <Card
            size="small"
            title={cardTitle(<CarOutlined />, 'Bus information')}
            extra={
              <a onClick={() => setEditing(true)}>
                <EditOutlined /> Edit
              </a>
            }
            style={{ height: '100%' }}
          >
            <Descriptions
              size="small"
              column={1}
              styles={{ label: { width: 150 } }}
              items={[
                {
                  key: 'number',
                  label: 'Vehicle number',
                  children: <NumberPlate number={bus.vehicle_number} size={13} />,
                },
                { key: 'seats', label: 'Seats', children: bus.capacity },
                { key: 'make', label: 'Make / model', children: bus.make_model || '—' },
                {
                  key: 'gps',
                  label: 'GPS device',
                  children: bus.gps_device_id ? (
                    <Space size={6}>
                      {bus.gps_device_id}
                      <Tag color="green" icon={<WifiOutlined />}>
                        Linked
                      </Tag>
                    </Space>
                  ) : (
                    <Typography.Text type="secondary">
                      None — location comes from the Driver app
                    </Typography.Text>
                  ),
                },
                { key: 'status', label: 'Status', children: <StatusTag status={bus.status} /> },
                {
                  key: 'updated',
                  label: 'Last updated',
                  children: new Date(bus.updated_at).toLocaleDateString(undefined, {
                    day: 'numeric',
                    month: 'short',
                    year: 'numeric',
                    timeZone,
                  }),
                },
                ...(bus.notes ? [{ key: 'notes', label: 'Notes', children: bus.notes }] : []),
              ]}
            />
          </Card>
        </Col>
      </Row>
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card
            size="small"
            title={cardTitle(<NodeIndexOutlined />, 'Regular routes')}
            extra={manageLink}
            style={{ height: '100%' }}
          >
            <AssignmentList assignments={assignments} />
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card
            size="small"
            title={cardTitle(<HistoryOutlined />, 'Recent trips')}
            extra={
              <a
                onClick={() => {
                  setTripsView('history')
                  setTab('trips')
                }}
              >
                View all <ArrowRightOutlined />
              </a>
            }
            style={{ height: '100%' }}
          >
            <TripTable
              rows={t.past.slice(0, 5)}
              loading={t.loading}
              compact
              show="driver"
              today={today}
              time={time}
              onOpen={setViewing}
            />
          </Card>
        </Col>
      </Row>
    </Flex>
  )

  return (
    <Flex vertical gap="middle">
      <Flex justify="space-between" align="center" wrap gap="small">
        <Flex vertical gap={4}>
          <Space wrap size="small">
            <NumberPlate number={bus.vehicle_number} size={22} />
            <StatusTag status={bus.status} />
            {t.running && (
              <Tag color="green" icon={<ClockCircleOutlined />}>
                On a trip now
              </Tag>
            )}
          </Space>
          <Typography.Text type="secondary">
            {[bus.make_model, `${bus.capacity} seats`].filter(Boolean).join(' · ')}
          </Typography.Text>
        </Flex>
        <Space wrap>
          {t.running && (
            <Button
              icon={<EnvironmentOutlined />}
              onClick={() => navigate(`/live?trip=${t.running!.id}`)}
            >
              View live location
            </Button>
          )}
          <Dropdown
            menu={{
              items: STATUSES.filter((s) => s !== bus.status).map((s) => ({
                key: s,
                label: statusActions[s],
                danger: s === 'inactive',
              })),
              onClick: ({ key }) => void changeStatus(key as BusStatus),
            }}
          >
            <Button>
              Status <DownOutlined />
            </Button>
          </Dropdown>
          <Button type="primary" icon={<EditOutlined />} onClick={() => setEditing(true)}>
            Edit
          </Button>
        </Space>
      </Flex>

      {bus.status === 'maintenance' && (
        <Alert
          type="warning"
          showIcon
          icon={<ToolOutlined />}
          title="In maintenance"
          description="This bus cannot be assigned to new trips until it is marked active."
        />
      )}
      {bus.status === 'inactive' && (
        <Alert
          type="info"
          showIcon
          title="Inactive"
          description="This bus cannot be assigned to trips. Its trip history is kept."
        />
      )}

      <Card size="small" styles={{ body: { padding: 16 } }}>
        <Flex wrap gap="large" align="center">
          <Flex gap="middle" align="center" style={{ minWidth: 240 }}>
            <Flex
              align="center"
              justify="center"
              style={{
                width: 64,
                height: 64,
                flex: 'none',
                borderRadius: '50%',
                background: '#f59e0b',
                color: '#fff',
                fontSize: 30,
              }}
            >
              <CarOutlined />
            </Flex>
            <Flex vertical gap={4}>
              <NumberPlate number={bus.vehicle_number} />
              <Typography.Text type="secondary">{bus.make_model || 'Make not set'}</Typography.Text>
            </Flex>
          </Flex>
          <Divider orientation="vertical" style={{ height: 40 }} />
          <InfoBit icon={<TeamOutlined />} label="Seats" value={bus.capacity} />
          <Divider orientation="vertical" style={{ height: 40 }} />
          <InfoBit
            icon={<WifiOutlined />}
            label="GPS device"
            value={bus.gps_device_id || 'Driver app'}
          />
          <Divider orientation="vertical" style={{ height: 40 }} />
          <InfoBit
            icon={<CalendarOutlined />}
            label="Added on"
            value={new Date(bus.created_at).toLocaleDateString(undefined, {
              day: 'numeric',
              month: 'short',
              year: 'numeric',
              timeZone,
            })}
          />
          <Flex
            gap="middle"
            align="center"
            style={{
              marginInlineStart: 'auto',
              background: token.blue1,
              border: `1px solid ${token.blue3}`,
              borderRadius: token.borderRadiusLG,
              padding: '10px 16px',
              minWidth: 220,
            }}
          >
            {t.current ? (
              <Avatar size={40} style={{ background: avatarColor(t.current.driver.id) }}>
                {initials(t.current.driver.name)}
              </Avatar>
            ) : (
              <UserOutlined style={{ fontSize: 26, color: token.blue6 }} />
            )}
            <Flex vertical>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {t.running ? 'Driven now by' : 'Driver today'}
              </Typography.Text>
              {t.current ? (
                <>
                  <Link to={`/drivers/${t.current.driver.id}`} style={{ fontWeight: 600 }}>
                    {t.current.driver.name}
                  </Link>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {t.current.route ? `Route ${t.current.route.code}` : t.current.title} ·{' '}
                    {tripTypeLabels[t.current.trip_type]}
                  </Typography.Text>
                </>
              ) : (
                <Typography.Text type="secondary">
                  {t.loading ? '…' : 'No trip today'}
                </Typography.Text>
              )}
            </Flex>
          </Flex>
        </Flex>
      </Card>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))',
          gap: 12,
        }}
      >
        <StatTile
          tone="blue"
          icon={<FileTextOutlined />}
          title="Trips run"
          value={shown(t.past.length)}
          hint={periodLabel}
        />
        <StatTile
          tone="green"
          icon={<CheckCircleOutlined />}
          title="Completed"
          value={shown(t.completed.length)}
          hint={periodLabel}
        />
        <StatTile
          tone={capacityPct > 100 ? 'red' : 'gold'}
          icon={<TeamOutlined />}
          title="Peak load"
          value={t.loading || !load.peak ? '—' : `${load.peak} / ${bus.capacity}`}
          hint={load.peak ? `${capacityPct}% of seats · ${periodLabel}` : periodLabel}
        />
        <StatTile
          tone="purple"
          icon={<CalendarOutlined />}
          title="Upcoming trips"
          value={shown(t.upcoming.length)}
          hint={`Next ${AHEAD_DAYS} days`}
        />
      </div>

      <PeriodBar
        period={period}
        onPeriod={setPeriod}
        items={[
          {
            icon: <CloseCircleOutlined style={{ color: token.colorError }} />,
            label: 'Cancelled',
            value: shown(t.cancelled.length),
          },
          {
            icon: <BarChartOutlined style={{ color: token.cyan6 }} />,
            label: 'Completion rate',
            value: t.completionRate != null ? `${t.completionRate}%` : '—',
          },
          {
            icon: <ClockCircleOutlined style={{ color: token.colorWarning }} />,
            label: 'Avg trip time',
            value: t.avgMinutes != null ? `${t.avgMinutes} min` : '—',
          },
          {
            icon: <UserOutlined style={{ color: token.purple6 }} />,
            label: 'Drivers',
            value: shown(load.drivers),
          },
        ]}
      />

      <Tabs
        activeKey={tab}
        onChange={(k) => setTab(k as TabKey)}
        items={[
          { key: 'overview', label: 'Overview', icon: <CarOutlined />, children: overview },
          {
            key: 'trips',
            label: 'Trips',
            icon: <CalendarOutlined />,
            children: (
              <Card
                size="small"
                title={tripsView === 'upcoming' ? `Next ${AHEAD_DAYS} days` : periodLabel}
                extra={
                  <Segmented<'upcoming' | 'history'>
                    size="small"
                    value={tripsView}
                    onChange={setTripsView}
                    options={[
                      { value: 'upcoming', label: `Upcoming (${t.upcoming.length})` },
                      { value: 'history', label: `${periodLabel} (${t.past.length})` },
                    ]}
                  />
                }
              >
                <TripTable
                  rows={tripsView === 'upcoming' ? t.upcoming : t.past}
                  loading={t.loading}
                  show="driver"
                  today={today}
                  time={time}
                  onOpen={setViewing}
                />
              </Card>
            ),
          },
          {
            key: 'routes',
            label: 'Routes & drivers',
            icon: <NodeIndexOutlined />,
            children: (
              <Flex vertical gap="middle">
                <Card
                  size="small"
                  title={cardTitle(<PushpinOutlined />, 'Regular routes')}
                  extra={manageLink}
                >
                  <AssignmentList assignments={assignments} />
                </Card>
                <Row gutter={[16, 16]}>
                  <Col xs={24} xl={14}>
                    <Card
                      size="small"
                      title={cardTitle(<BarChartOutlined />, `Routes run · ${periodLabel}`)}
                    >
                      <TripGroups trips={t.past} loading={t.loading} by="route" />
                    </Card>
                  </Col>
                  <Col xs={24} xl={10}>
                    <Card
                      size="small"
                      title={cardTitle(<UserOutlined />, `Drivers · ${periodLabel}`)}
                    >
                      <TripGroups trips={t.past} loading={t.loading} by="driver" />
                    </Card>
                  </Col>
                </Row>
              </Flex>
            ),
          },
          {
            key: 'activity',
            label: 'Activity',
            icon: <HistoryOutlined />,
            children: (
              <Card size="small">
                <AuditTimeline schoolId={schoolId} entityId={bus.id} timeZone={timeZone} />
              </Card>
            ),
          },
        ]}
      />

      {editing && (
        <BusForm
          schoolId={schoolId}
          bus={bus}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            loadBus()
          }}
        />
      )}
      {viewing && (
        <TripDetailDrawer schoolId={schoolId} tripId={viewing} onClose={() => setViewing(null)} />
      )}
    </Flex>
  )
}

function AssignmentList({ assignments }: { assignments: DriverRouteAssignment[] }) {
  const { token } = theme.useToken()
  if (!assignments.length)
    return (
      <Typography.Text type="secondary">
        No regular route. The bus is assigned day by day, or a driver picks it in the Driver app.
      </Typography.Text>
    )
  return (
    <Flex vertical gap="small">
      {assignments.map((a) => (
        <Flex
          key={a.id}
          gap="middle"
          align="center"
          wrap
          style={{
            background: token.colorFillQuaternary,
            borderRadius: token.borderRadiusLG,
            padding: '10px 12px',
          }}
        >
          <Tag color="geekblue" style={{ fontSize: 16, padding: '4px 10px', fontWeight: 600 }}>
            {a.route.code}
          </Tag>
          <Flex vertical style={{ flex: 1, minWidth: 140 }}>
            <Link to={`/routes/${a.route.id}`}>{a.route.name}</Link>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {tripTypeIcons[a.trip_type]} {tripTypeLabels[a.trip_type]}
            </Typography.Text>
          </Flex>
          <Space size={6}>
            <Avatar size="small" style={{ background: avatarColor(a.driver.id) }}>
              {initials(a.driver.name)}
            </Avatar>
            <Link to={`/drivers/${a.driver.id}`}>{a.driver.name}</Link>
          </Space>
        </Flex>
      ))}
    </Flex>
  )
}
