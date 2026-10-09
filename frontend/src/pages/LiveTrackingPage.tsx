import { useEffect, useState } from 'react'
import { App, Badge, Card, Col, Empty, Flex, Row, Space, Tag, Typography } from 'antd'
import AlertList from '../components/AlertList'
import LiveMap from '../components/LiveMap'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StopProgressList from '../components/StopProgressList'
import { busState, busStateColors } from '../components/liveMapTypes'
import { useCurrentSchool } from '../context/SchoolContext'
import { useLiveBuses } from '../hooks/useLiveBuses'
import type { BusLocation } from '../services/live'
import { routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import { tripTypeLabels, type Stop, type Trip } from '../services/types'
import { todayIn } from '../utils/dates'
import { formatDistance, formatEta } from '../utils/format'
import { errorMessage } from '../utils/formErrors'

export default function LiveTrackingPage() {
  return <RequireSchool>{(schoolId) => <LiveTracking schoolId={schoolId} />}</RequireSchool>
}

function ago(iso: string, now: number) {
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  return `${Math.floor(s / 3600)} h ago`
}

/** "Moving · 32 km/h", "Stopped", "Offline · last seen 4 min ago" */
function statusLine(b: BusLocation, now: number) {
  const state = busState(b)
  if (state === 'offline') return `Offline · last seen ${ago(b.received_at, now)}`
  if (state === 'stopped') return `Stopped · updated ${ago(b.received_at, now)}`
  return `Moving · ${Math.round((b.speed_mps ?? 0) * 3.6)} km/h`
}

function LiveTracking({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const { school } = useCurrentSchool()
  const live = useLiveBuses(schoolId)
  const [startedTrips, setStartedTrips] = useState<Trip[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [routeStops, setRouteStops] = useState<Stop[]>([])
  const [now, setNow] = useState(() => Date.now())

  // Trips in progress today, refreshed whenever a trip changes status.
  useEffect(() => {
    tripsApi
      .list(schoolId, { date: todayIn(school?.timezone), status: 'started', page_size: 100 })
      .then((res) => setStartedTrips(res.data))
      .catch((e: unknown) => message.error(errorMessage(e)))
  }, [schoolId, school?.timezone, live.statusVersion, message])

  // "Last seen" labels tick every 5 s.
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 5000)
    return () => clearInterval(t)
  }, [])

  const buses = Object.values(live.buses)
  const selected = selectedId ? live.buses[selectedId] : undefined
  const selectedRouteId =
    selected?.route_id ?? startedTrips.find((t) => t.id === selectedId)?.route.id

  useEffect(() => {
    if (!selectedRouteId) return
    let cancelled = false
    routesApi
      .get(schoolId, selectedRouteId)
      .then((r) => !cancelled && setRouteStops(r.stops ?? []))
      .catch(() => !cancelled && setRouteStops([]))
    return () => {
      cancelled = true
    }
  }, [schoolId, selectedRouteId])

  const selectedProgress = selectedId ? live.progress[selectedId] : undefined
  const nextStop = (tripId: string) =>
    live.progress[tripId]?.stops.find((s) => s.status === 'upcoming' || s.status === 'approaching')

  // Started trips that have not sent any GPS yet.
  const waiting = startedTrips.filter((t) => !live.buses[t.id])

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Live Tracking"
        subtitle={`${buses.length} bus(es) on the map · updates arrive automatically`}
        extra={
          <Badge
            status={live.connected ? 'success' : 'error'}
            text={live.connected ? 'Live' : 'Reconnecting…'}
          />
        }
      />
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8}>
          <Card
            title="Buses on trips"
            styles={{ body: { padding: 8, maxHeight: 560, overflowY: 'auto' } }}
          >
            {buses.length === 0 && waiting.length === 0 && (
              <Empty description="No trips in progress. Buses appear here when a driver starts a trip." />
            )}
            {buses
              .sort((a, b) => a.route_code.localeCompare(b.route_code))
              .map((b) => (
                <div
                  key={b.trip_id}
                  className={`stop-row${b.trip_id === selectedId ? ' stop-row--selected' : ''}`}
                  onClick={() => setSelectedId(b.trip_id === selectedId ? null : b.trip_id)}
                >
                  <span
                    className="stop-row__seq"
                    style={{ background: busStateColors[busState(b)], width: 12, height: 12 }}
                  />
                  <div className="stop-row__body">
                    <Typography.Text strong>
                      Bus: {b.bus_number} · {b.route_code}
                    </Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {tripTypeLabels[b.trip_type]} · {b.driver_name}
                    </Typography.Text>
                    <Typography.Text style={{ fontSize: 12 }} type={b.stale ? 'danger' : undefined}>
                      {statusLine(b, now)}
                    </Typography.Text>
                    {nextStop(b.trip_id) && (
                      <Typography.Text style={{ fontSize: 12 }}>
                        Next: {nextStop(b.trip_id)!.name} · ETA{' '}
                        {formatEta(nextStop(b.trip_id)!.eta_seconds)} ·{' '}
                        {formatDistance(nextStop(b.trip_id)!.distance_m)}
                      </Typography.Text>
                    )}
                  </div>
                </div>
              ))}
            {waiting.map((t) => (
              <div key={t.id} className="stop-row" style={{ cursor: 'default' }}>
                <span
                  className="stop-row__seq"
                  style={{ background: '#d9d9d9', width: 12, height: 12 }}
                />
                <div className="stop-row__body">
                  <Typography.Text strong>
                    Bus: {t.bus.vehicle_number} · {t.route.code}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {tripTypeLabels[t.trip_type]} · {t.driver.name}
                  </Typography.Text>
                  <Typography.Text type="warning" style={{ fontSize: 12 }}>
                    Started, waiting for GPS from the Driver app
                  </Typography.Text>
                </div>
              </div>
            ))}
          </Card>
          <Card title="Alerts" size="small" style={{ marginTop: 16 }}>
            <AlertList schoolId={schoolId} compact />
          </Card>
          {selected && (
            <Card
              title={`${selected.route_code} · stops`}
              style={{ marginTop: 16 }}
              styles={{ body: { maxHeight: 420, overflowY: 'auto' } }}
              extra={
                selectedProgress && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    ETA {selectedProgress.eta_source === 'google' ? 'by Google' : 'estimated'}
                  </Typography.Text>
                )
              }
            >
              {selectedProgress ? (
                <StopProgressList stops={selectedProgress.stops} />
              ) : (
                <Typography.Text type="secondary">
                  Stop status appears with the next GPS update.
                </Typography.Text>
              )}
            </Card>
          )}
        </Col>
        <Col xs={24} lg={16}>
          <Card styles={{ body: { padding: 8 } }}>
            <LiveMap
              buses={buses}
              selectedId={selectedId}
              onSelect={(id) => setSelectedId(id === selectedId ? null : id)}
              routeStops={selectedId ? routeStops : []}
            />
            <Space size="middle" wrap style={{ marginTop: 8, paddingLeft: 4 }}>
              {(['moving', 'stopped', 'offline'] as const).map((s) => (
                <Tag key={s} color={busStateColors[s]}>
                  {s[0].toUpperCase() + s.slice(1)}
                </Tag>
              ))}
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                Click a bus to see its route. ETA and stop status arrive in Sprint 6.
              </Typography.Text>
            </Space>
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
