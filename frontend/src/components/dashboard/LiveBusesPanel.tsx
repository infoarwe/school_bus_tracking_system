import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge, Button, Card, Col, Row, Typography } from 'antd'
import { ArrowRightOutlined, CarOutlined } from '@ant-design/icons'
import LiveMap from '../LiveMap'
import { busState, busStateColors, type BusState } from '../liveMapTypes'
import { useLiveBuses } from '../../hooks/useLiveBuses'
import { routesApi } from '../../services/transport'
import { tripTypeLabels, type Stop, type Trip } from '../../services/types'
import { timeAgo } from './time'

const stateLabels: Record<BusState, string> = {
  moving: 'Moving',
  stopped: 'Stopped',
  offline: 'Offline',
}

/**
 * Live buses on the dashboard, from the same WebSocket as Live Tracking. Only real GPS
 * is drawn: with no positions there is an empty state, never placeholder markers.
 */
export default function LiveBusesPanel({
  schoolId,
  routeNames,
  trips,
}: {
  schoolId: string
  /** route id → name (the live feed carries only the route code) */
  routeNames: Record<string, string>
  /** Today's trips, to explain an empty map (scheduled / waiting for GPS). */
  trips: Trip[] | null
}) {
  const navigate = useNavigate()
  const live = useLiveBuses(schoolId)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [routeStops, setRouteStops] = useState<{ routeId: string; stops: Stop[] } | null>(null)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 15000)
    return () => clearInterval(t)
  }, [])

  const buses = Object.values(live.buses).sort((a, b) => a.route_code.localeCompare(b.route_code))
  const selected = selectedId ? live.buses[selectedId] : undefined
  const selectedRouteId = selected?.route_id

  // Draw the selected bus's route (stops in order).
  useEffect(() => {
    if (!selectedRouteId) return
    let cancelled = false
    routesApi
      .get(schoolId, selectedRouteId)
      .then((r) => !cancelled && setRouteStops({ routeId: r.id, stops: r.stops ?? [] }))
      .catch(() => {}) // the bus is still shown without its route line
    return () => {
      cancelled = true
    }
  }, [schoolId, selectedRouteId])

  const started = (trips ?? []).filter((t) => t.status === 'started')
  const waiting = started.filter((t) => !live.buses[t.id])
  const upcoming = (trips ?? []).filter((t) => t.status === 'scheduled' || t.status === 'confirmed')
  const offline = buses.filter((b) => busState(b) === 'offline').length

  return (
    <Card
      className="dash-panel"
      title={
        <span>
          Live buses{' '}
          <Badge
            style={{ marginLeft: 8, fontWeight: 400 }}
            status={live.connected ? 'success' : 'warning'}
            text={
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {live.connected ? 'Live' : 'Reconnecting…'}
              </Typography.Text>
            }
          />
        </span>
      }
      extra={
        <Button type="link" onClick={() => navigate('/live')} style={{ paddingInline: 0 }}>
          Open Live Tracking <ArrowRightOutlined />
        </Button>
      }
      styles={{ body: { padding: buses.length ? 12 : 16 } }}
    >
      {buses.length === 0 ? (
        <div className="dash-empty">
          <CarOutlined className="dash-empty__icon" />
          <Typography.Paragraph strong style={{ margin: '8px 0 4px' }}>
            No buses are sending GPS right now
          </Typography.Paragraph>
          <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
            {waiting.length > 0
              ? `${waiting.length} trip(s) started and waiting for the first GPS update from the Driver app: ${waiting
                  .map((t) => `${t.bus?.vehicle_number ?? '?'} (${t.route?.code ?? t.title})`)
                  .join(', ')}.`
              : upcoming.length > 0
                ? `${upcoming.length} trip(s) planned today. Buses appear here when drivers start their trips.`
                : 'No trips are in progress. Buses appear here when a driver starts a trip.'}
          </Typography.Paragraph>
        </div>
      ) : (
        <Row gutter={[12, 12]}>
          <Col xs={24} xl={15}>
            <LiveMap
              buses={buses}
              selectedId={selectedId}
              onSelect={(id) => setSelectedId(id === selectedId ? null : id)}
              routeStops={
                selected && routeStops?.routeId === selected.route_id ? routeStops.stops : []
              }
              height={360}
            />
          </Col>
          <Col xs={24} xl={9}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {buses.length} on the road{offline ? ` · ${offline} offline` : ''}
              {waiting.length ? ` · ${waiting.length} waiting for GPS` : ''} · select one to see its
              route
            </Typography.Text>
            <div className="dash-list" style={{ maxHeight: 336, overflowY: 'auto', marginTop: 6 }}>
              {buses.map((b) => {
                const state = busState(b)
                const isSel = b.trip_id === selectedId
                return (
                  <button
                    type="button"
                    key={b.trip_id}
                    aria-pressed={isSel}
                    className={`dash-item dash-item--button${isSel ? ' dash-item--selected' : ''}`}
                    onClick={() => setSelectedId(isSel ? null : b.trip_id)}
                  >
                    <span className="dash-dot" style={{ background: busStateColors[state] }} />
                    <span className="dash-item__body">
                      <Typography.Text strong>{b.bus_number}</Typography.Text>
                      <Typography.Text style={{ fontSize: 13 }} ellipsis>
                        {b.route_code}
                        {routeNames[b.route_id] ? ` · ${routeNames[b.route_id]}` : ''}
                      </Typography.Text>
                      <span className="dash-item__meta">
                        {b.driver_name || 'Driver not named'} · GPS {timeAgo(b.received_at, now)}
                      </span>
                    </span>
                    <span
                      style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'flex-end',
                        gap: 4,
                      }}
                    >
                      <span
                        className={`pill pill--${
                          state === 'offline' ? 'slate' : state === 'moving' ? 'green' : 'orange'
                        }`}
                      >
                        {stateLabels[state]}
                      </span>
                      <span
                        className={`pill pill--${b.trip_type === 'morning_pickup' ? 'blue' : 'purple'}`}
                      >
                        {tripTypeLabels[b.trip_type]}
                      </span>
                    </span>
                  </button>
                )
              })}
            </div>
          </Col>
        </Row>
      )}
    </Card>
  )
}
