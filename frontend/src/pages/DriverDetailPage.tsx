import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
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
  IdcardOutlined,
  LoginOutlined,
  NodeIndexOutlined,
  PhoneOutlined,
  PushpinOutlined,
  UserOutlined,
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
import { driversApi } from '../services/transport'
import {
  tripTypeLabels,
  type Driver,
  type DriverRouteAssignment,
  type DriverStatus,
} from '../services/types'
import { avatarColor, initials } from '../utils/avatar'
import { formatDate, todayIn } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'
import { tripTypeIcons } from '../utils/tripIcons'
import { DriverForm } from './DriversPage'

const STATUSES: DriverStatus[] = ['active', 'inactive', 'suspended']

type TabKey = 'overview' | 'trips' | 'routes' | 'activity'

export default function DriverDetailPage() {
  return <RequireSchool>{(schoolId) => <DriverDetail schoolId={schoolId} />}</RequireSchool>
}

function DriverDetail({ schoolId }: { schoolId: string }) {
  const { driverId = '' } = useParams()
  const navigate = useNavigate()
  const { message, modal } = App.useApp()
  const { token } = theme.useToken()
  const { school } = useCurrentSchool()
  const timeZone = school?.timezone || undefined
  const today = todayIn(timeZone)
  const time = useSchoolClock(timeZone)
  const [driver, setDriver] = useState<Driver | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [period, setPeriod] = useState(30)
  const [assignments, setAssignments] = useState<DriverRouteAssignment[]>([])
  const [editing, setEditing] = useState(false)
  const [viewing, setViewing] = useState<string | null>(null)
  const [tab, setTab] = useState<TabKey>('overview')
  const [tripsView, setTripsView] = useState<'upcoming' | 'history'>('upcoming')
  const t = useEntityTrips(schoolId, { driver_id: driverId }, period, today)

  const loadDriver = useCallback(() => {
    driversApi
      .get(schoolId, driverId)
      .then(setDriver)
      .catch((e: unknown) => setError(errorMessage(e)))
  }, [schoolId, driverId])

  useEffect(loadDriver, [loadDriver])

  useEffect(() => {
    driverAssignmentsApi
      .list(schoolId, driverId)
      .then(setAssignments)
      .catch(() => setAssignments([]))
  }, [schoolId, driverId])

  if (!driver) return <LoadingOrError error={error} onRetry={loadDriver} />

  const periodLabel = `Last ${period} days`
  const assignmentsLink = `/driver-assignments?driver_id=${driver.id}`
  const shown = (n: number) => (t.loading ? '—' : n)

  function changeStatus(status: DriverStatus) {
    if (!driver) return
    const apply = async () => {
      try {
        setDriver(await driversApi.setStatus(schoolId, driver.id, status))
        message.success(`${driver.name} is now ${status}.`)
      } catch (e) {
        message.error(errorMessage(e))
      }
    }
    if (status === 'active') return void apply()
    modal.confirm({
      title: `Mark ${driver.name} as ${status}?`,
      content: 'The driver is logged out of the app and cannot log in until reactivated.',
      okText: status === 'suspended' ? 'Suspend' : 'Deactivate',
      okButtonProps: { danger: true },
      onOk: apply,
    })
  }

  const cardTitle = (icon: ReactNode, text: string) => (
    <Space>
      <span style={{ color: token.colorPrimary }}>{icon}</span>
      {text}
    </Space>
  )
  const manageLink = (
    <Link to={assignmentsLink}>
      Manage <ArrowRightOutlined />
    </Link>
  )

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
              show="bus"
              time={time}
              onOpen={setViewing}
              onLive={(id) => navigate(`/live?trip=${id}`)}
            />
          </Card>
        </Col>
        <Col xs={24} xl={10}>
          <Card
            size="small"
            title={cardTitle(<UserOutlined />, 'Driver information')}
            extra={
              <a onClick={() => setEditing(true)}>
                <EditOutlined /> Edit
              </a>
            }
            style={{ height: '100%' }}
          >
            <DriverInfo driver={driver} today={today} />
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
              show="bus"
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
        <Flex vertical>
          <Space wrap size="small">
            <Typography.Title level={3} style={{ margin: 0 }}>
              {driver.name}
            </Typography.Title>
            <StatusTag status={driver.status} />
            {t.running && (
              <Tag color="green" icon={<ClockCircleOutlined />}>
                On a trip now
              </Tag>
            )}
          </Space>
          <Typography.Text type="secondary">
            Logs in to the Driver app with {driver.mobile}
          </Typography.Text>
        </Flex>
        <Space wrap>
          <Link to={assignmentsLink}>
            <Button icon={<PushpinOutlined />}>Route assignments</Button>
          </Link>
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
              items: STATUSES.filter((s) => s !== driver.status).map((s) => ({
                key: s,
                label: { active: 'Activate', inactive: 'Deactivate', suspended: 'Suspend' }[s],
                danger: s !== 'active',
              })),
              onClick: ({ key }) => changeStatus(key as DriverStatus),
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

      <Card size="small" styles={{ body: { padding: 16 } }}>
        <Flex wrap gap="large" align="center">
          <Flex gap="middle" align="center" style={{ minWidth: 240 }}>
            <Avatar
              size={64}
              style={{ background: avatarColor(driver.id), fontSize: 24, flex: 'none' }}
            >
              {initials(driver.name)}
            </Avatar>
            <Flex vertical>
              <Typography.Text strong style={{ fontSize: 17 }}>
                {driver.name}
              </Typography.Text>
              <Typography.Link href={`tel:${driver.mobile}`}>
                <PhoneOutlined /> {driver.mobile}
              </Typography.Link>
            </Flex>
          </Flex>
          <Divider orientation="vertical" style={{ height: 40 }} />
          <InfoBit
            icon={<LoginOutlined />}
            label="Last app login"
            value={
              driver.last_login_at
                ? new Date(driver.last_login_at).toLocaleString(undefined, {
                    day: 'numeric',
                    month: 'short',
                    year: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit',
                    timeZone,
                  })
                : 'Never'
            }
          />
          <Divider orientation="vertical" style={{ height: 40 }} />
          <InfoBit
            icon={<CalendarOutlined />}
            label="Added on"
            value={new Date(driver.created_at).toLocaleDateString(undefined, {
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
            <CarOutlined style={{ fontSize: 26, color: token.blue6 }} />
            <Flex vertical>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {t.running ? 'Driving now' : 'Bus today'}
              </Typography.Text>
              {t.current ? (
                <>
                  <Link to={`/buses/${t.current.bus.id}`}>
                    <Typography.Text strong style={{ fontSize: 16, color: 'inherit' }}>
                      {t.current.bus.vehicle_number}
                    </Typography.Text>
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
          title="Trips driven"
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
          tone="cyan"
          icon={<BarChartOutlined />}
          title="Completion rate"
          value={t.completionRate != null ? `${t.completionRate}%` : '—'}
          hint={periodLabel}
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
            icon: <ClockCircleOutlined style={{ color: token.colorWarning }} />,
            label: 'Avg trip time',
            value: t.avgMinutes != null ? `${t.avgMinutes} min` : '—',
          },
          {
            icon: <PushpinOutlined style={{ color: token.purple6 }} />,
            label: 'Self-assigned',
            value: shown(t.selfAssigned),
          },
        ]}
      />

      <Tabs
        activeKey={tab}
        onChange={(k) => setTab(k as TabKey)}
        items={[
          { key: 'overview', label: 'Overview', icon: <UserOutlined />, children: overview },
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
                  show="bus"
                  today={today}
                  time={time}
                  onOpen={setViewing}
                />
              </Card>
            ),
          },
          {
            key: 'routes',
            label: 'Routes & buses',
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
                      title={cardTitle(<BarChartOutlined />, `Routes driven · ${periodLabel}`)}
                    >
                      <TripGroups trips={t.past} loading={t.loading} by="route" />
                    </Card>
                  </Col>
                  <Col xs={24} xl={10}>
                    <Card
                      size="small"
                      title={cardTitle(<CarOutlined />, `Buses driven · ${periodLabel}`)}
                    >
                      <TripGroups trips={t.past} loading={t.loading} by="bus" />
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
                <AuditTimeline
                  schoolId={schoolId}
                  entityId={driver.id}
                  actorUserId={driver.user_id}
                  timeZone={timeZone}
                />
              </Card>
            ),
          },
        ]}
      />

      {editing && (
        <DriverForm
          schoolId={schoolId}
          driver={driver}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            loadDriver()
          }}
        />
      )}
      {viewing && (
        <TripDetailDrawer schoolId={schoolId} tripId={viewing} onClose={() => setViewing(null)} />
      )}
    </Flex>
  )
}

/** "2 years left", "3 months left", "12 days left" or "Expired …". */
function timeLeft(expiry: string, today: string) {
  const days = Math.round(
    (new Date(`${expiry}T00:00:00Z`).getTime() - new Date(`${today}T00:00:00Z`).getTime()) /
      86_400_000,
  )
  if (days < 0)
    return {
      expired: true,
      text: `Expired ${-days} day${days === -1 ? '' : 's'} ago`,
      color: 'red',
    }
  const text =
    days >= 730
      ? `${Math.floor(days / 365)} years left`
      : days >= 60
        ? `${Math.floor(days / 30)} months left`
        : `${days} day${days === 1 ? '' : 's'} left`
  return { expired: false, text, color: days <= 30 ? 'orange' : 'green' }
}

function DriverInfo({ driver, today }: { driver: Driver; today: string }) {
  const left = driver.license_expiry ? timeLeft(driver.license_expiry, today) : null
  return (
    <Descriptions
      size="small"
      column={1}
      styles={{ label: { width: 150 } }}
      items={[
        {
          key: 'licence',
          label: 'Driving licence',
          children: (
            <Space size={6} wrap>
              {driver.license_number || '—'}
              {left && (
                <Tag
                  color={left.expired ? 'red' : 'green'}
                  icon={left.expired ? <CloseCircleOutlined /> : <CheckCircleOutlined />}
                >
                  {left.expired ? 'Expired' : 'Valid'}
                </Tag>
              )}
            </Space>
          ),
        },
        {
          key: 'expiry',
          label: 'Licence expiry',
          children: driver.license_expiry ? (
            <Space size={6} wrap>
              {formatDate(driver.license_expiry)}
              {left && <Tag color={left.color}>{left.text}</Tag>}
            </Space>
          ) : (
            '—'
          ),
        },
        {
          key: 'id',
          label: (
            <>
              <IdcardOutlined /> ID proof
            </>
          ),
          children: driver.id_proof_type
            ? `${driver.id_proof_type}${driver.id_proof_number ? ` · ${driver.id_proof_number}` : ''}`
            : '—',
        },
        {
          key: 'phone',
          label: 'Phone number',
          children: (
            <Typography.Link href={`tel:${driver.mobile}`}>{driver.mobile}</Typography.Link>
          ),
        },
        { key: 'address', label: 'Address', children: driver.address || '—' },
        {
          key: 'emergency',
          label: 'Emergency contact',
          children:
            driver.emergency_contact_name || driver.emergency_contact_phone ? (
              <Space size={4} wrap>
                {driver.emergency_contact_name}
                {driver.emergency_contact_phone && (
                  <Typography.Link href={`tel:${driver.emergency_contact_phone}`}>
                    {driver.emergency_contact_phone}
                  </Typography.Link>
                )}
              </Space>
            ) : (
              '—'
            ),
        },
        ...(driver.notes ? [{ key: 'notes', label: 'Notes', children: driver.notes }] : []),
      ]}
    />
  )
}

function AssignmentList({ assignments }: { assignments: DriverRouteAssignment[] }) {
  const { token } = theme.useToken()
  if (!assignments.length)
    return (
      <Typography.Text type="secondary">
        No regular route. Trips are assigned day by day, or the driver takes a route in the Driver
        app.
      </Typography.Text>
    )
  // One line per route, listing the trip types this driver covers on it.
  const byRoute = new Map<string, DriverRouteAssignment[]>()
  for (const a of assignments) byRoute.set(a.route.id, [...(byRoute.get(a.route.id) ?? []), a])
  return (
    <Flex vertical gap="small">
      {[...byRoute.values()].map((list) => {
        const a = list[0]
        const buses = [...new Map(list.map((x) => [x.bus.id, x.bus])).values()]
        return (
          <Flex
            key={a.route.id}
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
              <Space size={8} wrap>
                {list.map((x) => (
                  <Typography.Text key={x.id} type="secondary" style={{ fontSize: 12 }}>
                    {tripTypeIcons[x.trip_type]} {tripTypeLabels[x.trip_type]}
                  </Typography.Text>
                ))}
              </Space>
            </Flex>
            <Space size={4} wrap>
              <CarOutlined />
              {buses.map((b) => (
                <Link key={b.id} to={`/buses/${b.id}`}>
                  {b.vehicle_number}
                </Link>
              ))}
            </Space>
          </Flex>
        )
      })}
    </Flex>
  )
}
