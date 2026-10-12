import { useEffect, useState, type ReactNode } from 'react'
import {
  App,
  Badge,
  Button,
  Empty,
  Flex,
  Grid,
  Input,
  Segmented,
  Tag,
  Tooltip,
  Typography,
  theme,
} from 'antd'
import {
  ArrowLeftOutlined,
  CarOutlined,
  LeftOutlined,
  RightOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import LiveMap from '../components/LiveMap'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StopProgressList from '../components/StopProgressList'
import { busState, busStateColors, type BusState } from '../components/liveMapTypes'
import { useCurrentSchool } from '../context/SchoolContext'
import { useLiveBuses } from '../hooks/useLiveBuses'
import type { BusLocation } from '../services/live'
import { routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import { tripTypeLabels, type Stop, type Trip, type TripType } from '../services/types'
import { todayIn } from '../utils/dates'
import { formatDistance, formatEta } from '../utils/format'
import { errorMessage } from '../utils/formErrors'

const MAP_HEIGHT = 680
const PANEL_WIDTH = 380

export default function LiveTrackingPage() {
  return <RequireSchool>{(schoolId) => <LiveTracking schoolId={schoolId} />}</RequireSchool>
}

function ago(iso: string, now: number) {
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  return `${Math.floor(s / 3600)} h ago`
}

const stateLabels: Record<BusState, string> = {
  moving: 'Moving',
  stopped: 'Stopped',
  offline: 'Offline',
}
const stateTagColors: Record<BusState, string> = {
  moving: 'green',
  stopped: 'orange',
  offline: 'default',
}

/** "32 km/h" while moving, otherwise how long ago the last fix arrived. */
function stateDetail(b: BusLocation, now: number) {
  return busState(b) === 'moving'
    ? `${Math.round((b.speed_mps ?? 0) * 3.6)} km/h`
    : ago(b.received_at, now)
}

function LiveTracking({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const { token } = theme.useToken()
  const screens = Grid.useBreakpoint()
  // Wide screens: the panel floats over the map. Narrow ones: it sits under the map.
  const floating = !!screens.lg
  const { school } = useCurrentSchool()
  const live = useLiveBuses(schoolId)
  const [startedTrips, setStartedTrips] = useState<Trip[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [routeStops, setRouteStops] = useState<Stop[]>([])
  const [now, setNow] = useState(() => Date.now())
  const [typeFilter, setTypeFilter] = useState<TripType | 'all'>('all')
  const [stateFilter, setStateFilter] = useState<BusState | 'all'>('all')
  const [query, setQuery] = useState('')
  const [panelOpen, setPanelOpen] = useState(true)

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
  // Trip type and search narrow the list; the status chips count within that.
  const inScope = allBuses.filter(
    (b) =>
      (typeFilter === 'all' || b.trip_type === typeFilter) &&
      matches(b.bus_number, b.route_code, b.driver_name),
  )
  const counts = { moving: 0, stopped: 0, offline: 0 }
  inScope.forEach((b) => counts[busState(b)]++)
  const buses = inScope
    .filter((b) => stateFilter === 'all' || busState(b) === stateFilter)
    .sort((a, b) => a.route_code.localeCompare(b.route_code))
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

  const select = (id: string) => {
    setSelectedId(id === selectedId ? null : id)
    setPanelOpen(true)
  }

  const chip = (value: BusState | 'all', label: string, n: number) => (
    <Tag.CheckableTag
      key={value}
      checked={stateFilter === value}
      onChange={() => setStateFilter(value)}
      style={{ padding: '2px 10px', borderRadius: 999 }}
    >
      {label} ({n})
    </Tag.CheckableTag>
  )

  const busList = (
    <>
      <Flex gap={4} wrap style={{ padding: '0 4px 8px' }}>
        {chip('all', 'All', inScope.length)}
        {chip('moving', 'Moving', counts.moving)}
        {chip('stopped', 'Stopped', counts.stopped)}
        {chip('offline', 'Offline', counts.offline)}
      </Flex>
      {buses.length === 0 && waiting.length === 0 && (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={
            allBuses.length || startedTrips.length
              ? 'No buses match these filters.'
              : 'No trips in progress. Buses appear here when a driver starts a trip.'
          }
        />
      )}
      {buses.map((b) => {
        const state = busState(b)
        return (
          <div
            key={b.trip_id}
            className="stop-row"
            role="button"
            tabIndex={0}
            onClick={() => select(b.trip_id)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                select(b.trip_id)
              }
            }}
          >
            <BusIcon color={busStateColors[state]} />
            <div className="stop-row__body">
              <Typography.Text strong>{b.bus_number}</Typography.Text>
              <Typography.Text type="secondary" ellipsis style={{ fontSize: 12 }}>
                {b.route_code} · {tripTypeLabels[b.trip_type]}
              </Typography.Text>
            </div>
            <Flex vertical align="flex-end" gap={2} style={{ flex: 'none' }}>
              <Tag color={stateTagColors[state]} style={{ margin: 0 }}>
                {stateLabels[state]}
              </Tag>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {stateDetail(b, now)}
              </Typography.Text>
            </Flex>
            <RightOutlined style={{ color: token.colorTextQuaternary, fontSize: 12 }} />
          </div>
        )
      })}
      {waiting.map((t) => (
        <div key={t.id} className="stop-row" style={{ cursor: 'default' }}>
          <BusIcon color={token.colorTextQuaternary} />
          <div className="stop-row__body">
            <Typography.Text strong>{t.bus.vehicle_number}</Typography.Text>
            <Typography.Text type="secondary" ellipsis style={{ fontSize: 12 }}>
              {t.route?.code ?? t.title} · {tripTypeLabels[t.trip_type]}
            </Typography.Text>
            <Typography.Text type="warning" style={{ fontSize: 12 }}>
              Started, waiting for GPS from the Driver app
            </Typography.Text>
          </div>
        </div>
      ))}
    </>
  )

  const busDetail = selected && (
    <Flex vertical gap="small">
      <Flex align="center" gap="small">
        <Button
          type="text"
          icon={<ArrowLeftOutlined />}
          aria-label="Back to all buses"
          onClick={() => setSelectedId(null)}
        />
        <BusIcon color={busStateColors[busState(selected)]} />
        <div style={{ minWidth: 0 }}>
          <Typography.Text strong>{selected.bus_number}</Typography.Text>
          <br />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {selected.route_code} · {tripTypeLabels[selected.trip_type]} · {selected.driver_name}
          </Typography.Text>
        </div>
      </Flex>
      <Flex gap="small" wrap align="center">
        <Tag color={stateTagColors[busState(selected)]}>{stateLabels[busState(selected)]}</Tag>
        <Typography.Text type={selected.stale ? 'danger' : 'secondary'} style={{ fontSize: 12 }}>
          {busState(selected) === 'moving'
            ? stateDetail(selected, now)
            : `Last GPS ${ago(selected.received_at, now)}`}
        </Typography.Text>
      </Flex>
      {nextStop(selected.trip_id) && (
        <Typography.Text style={{ fontSize: 13 }}>
          Next: <strong>{nextStop(selected.trip_id)!.name}</strong> · ETA{' '}
          {formatEta(nextStop(selected.trip_id)!.eta_seconds)} ·{' '}
          {formatDistance(nextStop(selected.trip_id)!.distance_m)}
        </Typography.Text>
      )}
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        Stops ·{' '}
        {selectedProgress
          ? `ETA ${selectedProgress.eta_source === 'google' ? 'by Google' : 'estimated'}`
          : 'status appears with the next GPS update'}
      </Typography.Text>
      {selectedProgress && <StopProgressList stops={selectedProgress.stops} />}
    </Flex>
  )

  const panel = (
    <div style={{ padding: '0 12px' }}>
      <Flex align="center" gap={8} style={{ padding: '14px 4px 10px' }}>
        <CarOutlined style={{ color: token.colorPrimary }} />
        <Typography.Text strong>Buses on trips</Typography.Text>
        <Badge count={allBuses.length} showZero color="blue" />
      </Flex>
      {selected ? busDetail : busList}
    </div>
  )

  const legend = (
    <Flex
      gap="middle"
      align="center"
      style={{
        position: 'absolute',
        right: 64,
        bottom: 24,
        padding: '6px 14px',
        background: token.colorBgContainer,
        borderRadius: 999,
        boxShadow: token.boxShadowSecondary,
        fontSize: 12,
      }}
    >
      {(['moving', 'stopped', 'offline'] as const).map((s) => (
        <Flex key={s} align="center" gap={6}>
          <span
            style={{ width: 8, height: 8, borderRadius: '50%', background: busStateColors[s] }}
          />
          {stateLabels[s]}
        </Flex>
      ))}
    </Flex>
  )

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title={
          <Flex align="center" gap="small" wrap>
            Live Tracking
            <Tag
              color={live.connected ? 'green' : 'red'}
              style={{ fontWeight: 400, borderRadius: 999 }}
            >
              <Badge status={live.connected ? 'success' : 'error'} />{' '}
              {live.connected ? 'Live' : 'Reconnecting…'}
            </Tag>
          </Flex>
        }
        subtitle={`${allBuses.length} bus${allBuses.length === 1 ? '' : 'es'} on trips · updates arrive automatically`}
        extra={
          <Flex gap="small" wrap align="center" justify="flex-end">
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
            <Input
              allowClear
              prefix={<SearchOutlined />}
              aria-label="Search bus, route or driver"
              placeholder="Search bus, route or driver…"
              style={{ width: 260 }}
              onChange={(e) => setQuery(e.target.value)}
            />
          </Flex>
        }
      />

      <div
        style={{
          position: 'relative',
          borderRadius: token.borderRadiusLG,
          overflow: 'hidden',
          boxShadow: token.boxShadowTertiary,
        }}
      >
        <LiveMap
          buses={buses}
          selectedId={selectedId}
          onSelect={select}
          routeStops={selectedId ? routeStops : []}
          height={floating ? MAP_HEIGHT : 420}
        />

        {floating && (
          <>
            {panelOpen && <FloatingPanel>{panel}</FloatingPanel>}
            <Tooltip title={panelOpen ? 'Hide list' : 'Show buses'} placement="right">
              <Button
                shape="circle"
                icon={panelOpen ? <LeftOutlined /> : <RightOutlined />}
                aria-label={panelOpen ? 'Hide list' : 'Show buses'}
                aria-expanded={panelOpen}
                onClick={() => setPanelOpen((o) => !o)}
                style={{
                  position: 'absolute',
                  top: 24,
                  left: panelOpen ? 16 + PANEL_WIDTH + 8 : 16,
                  boxShadow: token.boxShadowSecondary,
                }}
              />
            </Tooltip>
            {legend}
          </>
        )}
      </div>

      {!floating && (
        <div
          style={{
            background: token.colorBgContainer,
            borderRadius: token.borderRadiusLG,
            paddingBottom: 8,
          }}
        >
          {panel}
        </div>
      )}
    </Flex>
  )
}

function FloatingPanel({ children }: { children: ReactNode }) {
  const { token } = theme.useToken()
  return (
    <div
      style={{
        position: 'absolute',
        top: 16,
        left: 16,
        bottom: 16,
        width: PANEL_WIDTH,
        background: token.colorBgContainer,
        borderRadius: token.borderRadiusLG,
        boxShadow: token.boxShadowSecondary,
        overflowY: 'auto',
        paddingBottom: 8,
      }}
    >
      {children}
    </div>
  )
}

function BusIcon({ color }: { color: string }) {
  return (
    <Flex
      align="center"
      justify="center"
      style={{
        flex: 'none',
        width: 36,
        height: 36,
        borderRadius: 10,
        background: `${color}22`,
        color,
        fontSize: 18,
      }}
    >
      <CarOutlined />
    </Flex>
  )
}
