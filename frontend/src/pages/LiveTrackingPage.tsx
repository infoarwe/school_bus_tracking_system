import { useEffect, useState } from 'react'
import {
  App,
  Badge,
  Card,
  Col,
  Empty,
  Flex,
  Input,
  Row,
  Segmented,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
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
import { tripTypeLabels, type Stop, type Trip, type TripType } from '../services/types'
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
  const [typeFilter, setTypeFilter] = useState<TripType | 'all'>('all')
  const [stateFilter, setStateFilter] = useState<'all' | 'moving' | 'stopped' | 'offline'>('all')
  const [query, setQuery] = useState('')

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

  const allBuses = Object.values(live.buses)
  const q = query.trim().toLowerCase()
  const matches = (bus: string, route: string, driver: string) =>
    !q || [bus, route, driver].some((x) => x.toLowerCase().includes(q))
  const buses = allBuses.filter(
    (b) =>
      (typeFilter === 'all' || b.trip_type === typeFilter) &&
      (stateFilter === 'all' || busState(b) === stateFilter) &&
      matches(b.bus_number, b.route_code, b.driver_name),
  )
  const selected = selectedId ? live.buses[selectedId] : undefined
  const selectedRouteId =
    selected?.route_id ?? startedTrips.find((t) => t.id === selectedId)?.route?.id

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
  const waiting = startedTrips.filter(
    (t) =>
      !live.buses[t.id] &&
      (typeFilter === 'all' || t.trip_type === typeFilter) &&
      stateFilter === 'all' &&
      matches(t.bus.vehicle_number, t.route?.code ?? t.title, t.driver.name),
  )

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Live Tracking"
        subtitle={`${allBuses.length} bus(es) sending GPS · updates arrive automatically`}
        extra={
          <Badge
            status={live.connected ? 'success' : 'error'}
            text={live.connected ? 'Live' : 'Reconnecting…'}
          />
        }
      />
      <Flex gap="small" wrap align="center">
        <Segmented
          aria-label="Trip type"
          value={typeFilter}
          onChange={(v) => setTypeFilter(v as TripType | 'all')}
          options={[
            { value: 'all', label: 'All trips' },
            { value: 'morning_pickup', label: tripTypeLabels.morning_pickup },
            { value: 'evening_drop', label: tripTypeLabels.evening_drop },
          ]}
        />
        <Select
          aria-label="Bus status"
          style={{ width: 150 }}
          value={stateFilter}
          onChange={setStateFilter}
          options={[
            { value: 'all', label: 'Any status' },
            { value: 'moving', label: 'Moving' },
            { value: 'stopped', label: 'Stopped' },
            { value: 'offline', label: 'Offline' },
          ]}
        />
        <Input.Search
          allowClear
          aria-label="Search bus, route or driver"
          placeholder="Search bus, route or driver"
          style={{ width: 240 }}
          onChange={(e) => setQuery(e.target.value)}
        />
      </Flex>
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8}>
          <Card
            title="Buses on trips"
            styles={{ body: { padding: 8, maxHeight: 560, overflowY: 'auto' } }}
          >
            {buses.length === 0 && waiting.length === 0 && (
              <Empty
                description={
                  allBuses.length || startedTrips.length
                    ? 'No buses match these filters.'
                    : 'No trips in progress. Buses appear here when a driver starts a trip.'
                }
              />
            )}
            {buses
              .sort((a, b) => a.route_code.localeCompare(b.route_code))
              .map((b) => (
                <div
                  key={b.trip_id}
                  className={`stop-row${b.trip_id === selectedId ? ' stop-row--selected' : ''}`}
                  role="button"
                  tabIndex={0}
                  aria-pressed={b.trip_id === selectedId}
                  onClick={() => setSelectedId(b.trip_id === selectedId ? null : b.trip_id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      setSelectedId(b.trip_id === selectedId ? null : b.trip_id)
                    }
                  }}
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
                      <Tag color={b.trip_type === 'morning_pickup' ? 'gold' : 'geekblue'}>
                        {tripTypeLabels[b.trip_type]}
                      </Tag>
                      {b.driver_name}
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
                    Bus: {t.bus.vehicle_number} · {t.route?.code ?? t.title}
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
                Click a bus to see its route, stop status and ETA.
              </Typography.Text>
            </Space>
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
