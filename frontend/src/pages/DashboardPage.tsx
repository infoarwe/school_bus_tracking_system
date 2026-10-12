import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, Button, Card, Col, Flex, Row, Skeleton, Space, Tooltip, Typography } from 'antd'
import {
  AlertOutlined,
  BarChartOutlined,
  CalendarOutlined,
  CarOutlined,
  EnvironmentOutlined,
  IdcardOutlined,
  MoonOutlined,
  NodeIndexOutlined,
  PlusOutlined,
  ReloadOutlined,
  SunOutlined,
  TeamOutlined,
  UploadOutlined,
  UserAddOutlined,
} from '@ant-design/icons'
import LoadingOrError from '../components/LoadingOrError'
import RequireSchool from '../components/RequireSchool'
import ActivityPanel from '../components/dashboard/ActivityPanel'
import AlertsPanel from '../components/dashboard/AlertsPanel'
import LiveBusesPanel from '../components/dashboard/LiveBusesPanel'
import OutingsPanel from '../components/dashboard/OutingsPanel'
import RoutesTodayPanel from '../components/dashboard/RoutesTodayPanel'
import StatCard from '../components/dashboard/StatCard'
import { clockTime } from '../components/dashboard/time'
import { useAuth } from '../context/AuthContext'
import { auditApi, dashboardApi } from '../services/admin'
import { routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type AuditRow,
  type BusRoute,
  type DashboardData,
  type Trip,
  type TripType,
} from '../services/types'
import { formatDate } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'

const REFRESH_MS = 30000

interface Loaded {
  dashboard: DashboardData
  /** Supporting data: the dashboard still works if one of these fails (null). */
  trips: Trip[] | null
  routes: Record<string, BusRoute>
  activity: AuditRow[] | null
  at: Date
}

export default function DashboardPage() {
  return (
    <RequireSchool>
      {(schoolId, schoolName) => <Dashboard schoolId={schoolId} schoolName={schoolName} />}
    </RequireSchool>
  )
}

function Dashboard({ schoolId, schoolName }: { schoolId: string; schoolName: string }) {
  const navigate = useNavigate()
  const { hasRole } = useAuth()
  const canManageStudents = hasRole('super_admin', 'school_admin')

  const load = useCallback(async (): Promise<Loaded> => {
    const dashboard = await dashboardApi.get(schoolId)
    // "Today" is the school's date from the server, never the browser's.
    const [trips, routes, activity] = await Promise.all([
      tripsApi
        .list(schoolId, { date: dashboard.today, page_size: 100 })
        .then((r) => r.data)
        .catch(() => null),
      routesApi
        .list(schoolId, { status: 'active', page_size: 100 })
        .then((r) => Object.fromEntries(r.data.map((x) => [x.id, x])))
        .catch(() => ({})),
      auditApi
        .school(schoolId, { page_size: 25 })
        .then((r) => r.data)
        .catch(() => null),
    ])
    return { dashboard, trips, routes, activity, at: new Date() }
  }, [schoolId])

  const [state, setState] = useState<{
    data: Loaded | null
    error: string | null
    loading: boolean
  }>({ data: null, error: null, loading: true })
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    load()
      .then((data) => !cancelled && setState({ data, error: null, loading: false }))
      .catch(
        // A failed refresh keeps the last good numbers on screen (with a warning).
        (e: unknown) =>
          !cancelled && setState((s) => ({ ...s, error: errorMessage(e), loading: false })),
      )
    return () => {
      cancelled = true
    }
  }, [load, tick])

  const refresh = useCallback(() => {
    setState((s) => ({ ...s, loading: true }))
    setTick((t) => t + 1)
  }, [])

  useEffect(() => {
    const t = setInterval(refresh, REFRESH_MS)
    return () => clearInterval(t)
  }, [refresh])

  const { data, error, loading } = state
  if (!data) {
    return (
      <LoadingOrError
        error={error}
        onRetry={refresh}
        title="Could not load the dashboard"
        placeholder={<DashboardSkeleton />}
      />
    )
  }

  const { dashboard: d, trips, routes, activity } = data
  const { totals } = d
  const tripLine = (type: TripType) => {
    const t = d.trips[type] ?? {}
    const total = Object.values(t).reduce((a, b) => a + (b ?? 0), 0)
    const cancelled = t.cancelled ?? 0
    const planned = total - cancelled
    const done = t.completed ?? 0
    return {
      planned,
      done,
      running: t.started ?? 0,
      cancelled,
      percent: planned > 0 ? Math.round((done / planned) * 100) : undefined,
    }
  }
  const routeNames = Object.fromEntries(d.routes.map((r) => [r.route_id, r.name]))

  return (
    <Flex vertical gap={20}>
      {/* Header: title, school and date, freshness, actions */}
      <div className="dash-header">
        <div>
          <Typography.Title level={2} className="dash-header__title">
            Dashboard
          </Typography.Title>
          <div className="dash-header__meta">
            <span>
              {schoolName} · {formatDate(d.today)}
            </span>
            <span aria-live="polite" className={error ? undefined : 'dash-header__live'}>
              {loading ? 'Refreshing…' : `Updated ${clockTime(data.at)} · refreshes every 30 s`}
            </span>
            <Tooltip title="Refresh now">
              <Button
                size="small"
                type="text"
                icon={<ReloadOutlined spin={loading} />}
                onClick={refresh}
                aria-label="Refresh the dashboard"
              />
            </Tooltip>
          </div>
        </div>
        <Space wrap>
          <Button icon={<EnvironmentOutlined />} onClick={() => navigate('/live')}>
            Live tracking
          </Button>
          <Button icon={<NodeIndexOutlined />} onClick={() => navigate('/routes?new=1')}>
            Add route
          </Button>
          {canManageStudents && (
            <Button icon={<UserAddOutlined />} onClick={() => navigate('/students?new=1')}>
              Register student
            </Button>
          )}
          <Button type="primary" icon={<CalendarOutlined />} onClick={() => navigate('/trips')}>
            Assign trips
          </Button>
        </Space>
      </div>

      {error && (
        <Alert
          type="warning"
          showIcon
          title={`Could not refresh: ${error}`}
          description={`Showing the numbers from ${clockTime(data.at)}.`}
          action={
            <Button size="small" onClick={refresh}>
              Retry
            </Button>
          }
        />
      )}

      {/* Today's operations */}
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={12} xl={6}>
          <StatCard
            icon={<CarOutlined />}
            label="Buses on the road"
            value={d.live.on_road}
            suffix={`/ ${totals.buses_active}`}
            accent="green"
            noteTone={d.live.offline ? 'danger' : 'neutral'}
            note={
              d.live.offline
                ? `${d.live.offline} offline (no recent GPS)`
                : d.live.on_road
                  ? 'All sending GPS'
                  : 'None on a trip right now'
            }
            to="/live"
          />
        </Col>
        <Col xs={12} sm={12} xl={6}>
          <StatCard
            icon={<AlertOutlined />}
            label="Delayed trips today"
            value={d.delayed_trips}
            accent={d.open_emergencies ? 'red' : 'orange'}
            noteTone={d.open_emergencies ? 'danger' : 'neutral'}
            note={
              d.open_emergencies
                ? `${d.open_emergencies} open emergenc${d.open_emergencies === 1 ? 'y' : 'ies'}`
                : 'No open emergencies'
            }
            to="/alerts"
          />
        </Col>
        {(['morning_pickup', 'evening_drop'] as const).map((type) => {
          const l = tripLine(type)
          return (
            <Col xs={12} sm={12} xl={6} key={type}>
              <StatCard
                icon={type === 'morning_pickup' ? <SunOutlined /> : <MoonOutlined />}
                accent={type === 'morning_pickup' ? 'blue' : 'purple'}
                label={`${tripTypeLabels[type]} completed`}
                value={l.done}
                suffix={`/ ${l.planned}`}
                percent={l.percent}
                noteTone={l.planned > 0 && l.done === l.planned ? 'success' : 'neutral'}
                note={
                  l.planned === 0
                    ? 'No trips planned today'
                    : [
                        `${l.percent}% done`,
                        l.running ? `${l.running} on the road` : null,
                        l.cancelled ? `${l.cancelled} cancelled` : null,
                      ]
                        .filter(Boolean)
                        .join(' · ')
                }
                to="/trips"
              />
            </Col>
          )
        })}
      </Row>

      {/* Fleet and people */}
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={12} xl={6}>
          <StatCard
            icon={<CarOutlined />}
            label="Active buses"
            value={totals.buses_active}
            note={`${totals.buses_in_maintenance} in maintenance · ${totals.buses_total} total`}
            accent="orange"
            noteTone={totals.buses_in_maintenance ? 'warning' : 'neutral'}
            to="/buses"
          />
        </Col>
        <Col xs={12} sm={12} xl={6}>
          <StatCard
            icon={<IdcardOutlined />}
            accent="purple"
            label="Active drivers"
            value={totals.drivers_active}
            note="Can log in to the Driver app"
            to="/drivers"
          />
        </Col>
        <Col xs={12} sm={12} xl={6}>
          <StatCard
            icon={<TeamOutlined />}
            label="Students on transport"
            value={totals.students_on_transport}
            note={
              totals.students_unassigned
                ? `${totals.students_unassigned} without a route`
                : 'All have a route'
            }
            accent="green"
            noteTone={totals.students_unassigned ? 'warning' : 'neutral'}
            to="/students"
          />
        </Col>
        <Col xs={12} sm={12} xl={6}>
          <StatCard
            icon={<NodeIndexOutlined />}
            accent="blue"
            label="Active routes"
            value={totals.routes_active}
            note="With ordered stops"
            to="/routes"
          />
        </Col>
      </Row>

      {/* Live buses + alerts */}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={16}>
          <LiveBusesPanel schoolId={schoolId} routeNames={routeNames} trips={trips} />
        </Col>
        <Col xs={24} lg={8}>
          <AlertsPanel />
        </Col>
      </Row>

      {/* Routes today + activity and setup shortcuts */}
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={16}>
          <RoutesTodayPanel routes={d.routes} trips={trips} routeMasters={routes} />
        </Col>
        <Col xs={24} xl={8}>
          <Flex vertical gap={16} style={{ height: '100%' }}>
            <OutingsPanel outings={d.outings} />
            <Card
              className="dash-panel"
              title="Quick actions"
              styles={{ body: { paddingTop: 14 } }}
            >
              {/* Only actions not already in the header. */}
              <div className="quick-actions">
                <button
                  type="button"
                  className="quick-action"
                  onClick={() => navigate('/buses?new=1')}
                >
                  <span className="icon-tile icon-tile--orange" aria-hidden>
                    <PlusOutlined />
                  </span>
                  Add bus
                </button>
                <button
                  type="button"
                  className="quick-action"
                  onClick={() => navigate('/drivers?new=1')}
                >
                  <span className="icon-tile icon-tile--purple" aria-hidden>
                    <IdcardOutlined />
                  </span>
                  Register driver
                </button>
                {canManageStudents && (
                  <button
                    type="button"
                    className="quick-action"
                    onClick={() => navigate('/students/import')}
                  >
                    <span className="icon-tile icon-tile--green" aria-hidden>
                      <UploadOutlined />
                    </span>
                    Import students
                  </button>
                )}
                <button type="button" className="quick-action" onClick={() => navigate('/reports')}>
                  <span className="icon-tile icon-tile--blue" aria-hidden>
                    <BarChartOutlined />
                  </span>
                  View reports
                </button>
              </div>
            </Card>
            <ActivityPanel rows={activity} />
          </Flex>
        </Col>
      </Row>
    </Flex>
  )
}

function DashboardSkeleton() {
  return (
    <Flex vertical gap={20} aria-busy="true" aria-label="Loading the dashboard">
      <Skeleton active title={{ width: 200 }} paragraph={{ rows: 1, width: 320 }} />
      {[0, 1].map((row) => (
        <Row gutter={[16, 16]} key={row}>
          {[0, 1, 2, 3].map((i) => (
            <Col xs={12} sm={12} xl={6} key={i}>
              <Card className="dash-panel" styles={{ body: { padding: 16 } }}>
                <Skeleton
                  active
                  avatar={{ shape: 'square' }}
                  title={false}
                  paragraph={{ rows: 2 }}
                />
              </Card>
            </Col>
          ))}
        </Row>
      ))}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={16}>
          <Card className="dash-panel">
            <Skeleton.Node active style={{ width: '100%', height: 300 }} />
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card className="dash-panel">
            <Skeleton active paragraph={{ rows: 6 }} />
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
