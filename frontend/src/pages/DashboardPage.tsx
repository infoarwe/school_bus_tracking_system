import { useCallback, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Col, Flex, Row, Space, Statistic, Table, Tag, Typography } from 'antd'
import AlertList from '../components/AlertList'
import LoadingOrError from '../components/LoadingOrError'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import { useLoad } from '../hooks/useLoad'
import { dashboardApi } from '../services/admin'
import {
  tripTypeLabels,
  type DashboardData,
  type TripStatus,
  type TripType,
} from '../services/types'
import { formatDate } from '../utils/dates'

const REFRESH_MS = 30000

// Trip status with a text label, never colour alone.
const statusTag: Record<TripStatus, { color: string; label: string }> = {
  scheduled: { color: 'default', label: 'Scheduled' },
  confirmed: { color: 'cyan', label: 'Confirmed' },
  started: { color: 'green', label: 'On the road' },
  completed: { color: 'blue', label: 'Completed' },
  cancelled: { color: 'red', label: 'Cancelled' },
}

function TripCell({ status, bus }: { status: TripStatus | null; bus: string | null }) {
  if (!status) return <Tag color="orange">Not assigned</Tag>
  return (
    <Space size={4}>
      <Tag color={statusTag[status].color}>{statusTag[status].label}</Tag>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {bus}
      </Typography.Text>
    </Space>
  )
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
  const load = useCallback(() => dashboardApi.get(schoolId), [schoolId])
  const { data, error, retry } = useLoad<DashboardData>(load)

  // Keep the numbers fresh while the page is open.
  useEffect(() => {
    const t = setInterval(retry, REFRESH_MS)
    return () => clearInterval(t)
  }, [retry])

  if (!data) return <LoadingOrError error={error} onRetry={retry} />
  const { totals } = data
  const trips = (type: TripType) => data.trips[type] ?? {}
  const tripLine = (type: TripType) => {
    const t = trips(type)
    const total = Object.values(t).reduce((a, b) => a + (b ?? 0), 0)
    return { total, done: t.completed ?? 0, running: t.started ?? 0, cancelled: t.cancelled ?? 0 }
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Dashboard"
        subtitle={`${schoolName} · ${formatDate(data.today)} · updates every 30 seconds`}
      />

      <Row gutter={[16, 16]}>
        <Col xs={12} md={6}>
          <Card hoverable onClick={() => navigate('/live')}>
            <Statistic
              title="Buses on the road"
              value={data.live.on_road}
              suffix={`/ ${totals.buses_active}`}
            />
            <Typography.Text
              type={data.live.offline ? 'danger' : 'secondary'}
              style={{ fontSize: 12 }}
            >
              {data.live.offline ? `${data.live.offline} offline (no GPS)` : 'All sending GPS'}
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card hoverable onClick={() => navigate('/alerts')}>
            <Statistic title="Delayed trips today" value={data.delayed_trips} />
            <Typography.Text
              type={data.open_emergencies ? 'danger' : 'secondary'}
              style={{ fontSize: 12 }}
            >
              {data.open_emergencies
                ? `${data.open_emergencies} open emergency`
                : 'No open emergencies'}
            </Typography.Text>
          </Card>
        </Col>
        {(['morning_pickup', 'evening_drop'] as const).map((type) => {
          const l = tripLine(type)
          return (
            <Col xs={12} md={6} key={type}>
              <Card hoverable onClick={() => navigate('/trips')}>
                <Statistic
                  title={`${tripTypeLabels[type]} completed`}
                  value={l.done}
                  suffix={`/ ${l.total - l.cancelled}`}
                />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {l.running} on the road{l.cancelled ? ` · ${l.cancelled} cancelled` : ''}
                </Typography.Text>
              </Card>
            </Col>
          )
        })}
      </Row>

      <Row gutter={[16, 16]}>
        {[
          {
            title: 'Active buses',
            value: totals.buses_active,
            note: `${totals.buses_in_maintenance} in maintenance · ${totals.buses_total} total`,
            to: '/buses',
          },
          {
            title: 'Active drivers',
            value: totals.drivers_active,
            note: 'Can log in to the Driver app',
            to: '/drivers',
          },
          {
            title: 'Students on transport',
            value: totals.students_on_transport,
            note: totals.students_unassigned
              ? `${totals.students_unassigned} without a route`
              : 'All have a route',
            warn: totals.students_unassigned > 0,
            to: '/students',
          },
          {
            title: 'Active routes',
            value: totals.routes_active,
            note: 'With ordered stops',
            to: '/routes',
          },
        ].map((k) => (
          <Col xs={12} md={6} key={k.title}>
            <Card size="small" hoverable onClick={() => navigate(k.to)}>
              <Statistic title={k.title} value={k.value} />
              <Typography.Text type={k.warn ? 'warning' : 'secondary'} style={{ fontSize: 12 }}>
                {k.note}
              </Typography.Text>
            </Card>
          </Col>
        ))}
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={16}>
          <Card title="Routes today">
            <Table
              rowKey="route_id"
              size="small"
              dataSource={data.routes}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
              scroll={{ x: 700 }}
              onRow={(r) => ({
                onClick: () => navigate(`/routes/${r.route_id}`),
                style: { cursor: 'pointer' },
              })}
              columns={[
                {
                  title: 'Route',
                  render: (_, r) => (
                    <>
                      <strong>{r.code}</strong> · {r.name}
                    </>
                  ),
                },
                { title: 'Students', dataIndex: 'students', width: 90 },
                {
                  title: tripTypeLabels.morning_pickup,
                  render: (_, r) => <TripCell status={r.morning_status} bus={r.morning_bus} />,
                },
                {
                  title: tripTypeLabels.evening_drop,
                  render: (_, r) => <TripCell status={r.evening_status} bus={r.evening_bus} />,
                },
                {
                  title: 'Issues',
                  width: 140,
                  render: (_, r) =>
                    r.delays || r.missed_stops ? (
                      <Typography.Text type="warning" style={{ fontSize: 12 }}>
                        {[
                          r.delays && `${r.delays} delay(s)`,
                          r.missed_stops && `${r.missed_stops} missed stop(s)`,
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </Typography.Text>
                    ) : (
                      '—'
                    ),
                },
              ]}
            />
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="Alerts">
            <AlertList schoolId={schoolId} compact />
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
