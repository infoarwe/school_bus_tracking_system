import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  App,
  Badge,
  Button,
  Checkbox,
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
  ClearOutlined,
  LeftOutlined,
  NodeIndexOutlined,
  PhoneOutlined,
  RightOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { BusGlyph } from '../components/BusGlyph'
import LiveMap from '../components/LiveMap'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import StopProgressList from '../components/StopProgressList'
import { busState, busStateColors, pathColors, type BusState } from '../components/liveMapTypes'
import { useCurrentSchool } from '../context/SchoolContext'
import { useLiveBuses, type TrailPoint } from '../hooks/useLiveBuses'
import { trackApi } from '../services/admin'
import type { BusLocation } from '../services/live'
import { routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type PlannedPath,
  type Stop,
  type Trip,
  type TripType,
} from '../services/types'
import { todayIn } from '../utils/dates'
import { formatDistance, formatEta } from '../utils/format'
import { errorMessage } from '../utils/formErrors'
import { distanceToPath, pathLength, splitByDeviation, type LatLng } from '../utils/geo'

const MAP_HEIGHT = 680
const PANEL_WIDTH = 380
/** Further than this from the planned road route counts as off route (GPS is good to ~30 m). */
const OFF_ROUTE_M = 150
const NO_PATH: LatLng[] = []

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
  // ?trip=<id> (e.g. from a driver's page) opens with that bus selected.
  const [searchParams] = useSearchParams()
  const [selectedId, setSelectedId] = useState<string | null>(() => searchParams.get('trip'))
  const [routeStops, setRouteStops] = useState<Stop[]>([])
  const [now, setNow] = useState(() => Date.now())
  const [typeFilter, setTypeFilter] = useState<TripType | 'all'>('all')
  const [stateFilter, setStateFilter] = useState<BusState | 'all'>('all')
  const [query, setQuery] = useState('')
  const [panelOpen, setPanelOpen] = useState(true)
  // Lines drawn for the selected bus: the planned road route and where it really went.
  const [showPlanned, setShowPlanned] = useState(true)
  const [showTravelled, setShowTravelled] = useState(true)
  const [planned, setPlanned] = useState<{ tripId: string; path: PlannedPath } | null>(null)
  const [history, setHistory] = useState<{ tripId: string; points: TrailPoint[] } | null>(null)

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
  // Only a bus still on its trip: when the trip ends, its route leaves the map by itself.
  const selected = selectedId ? live.buses[selectedId] : undefined
  const selectedTrip = startedTrips.find((t) => t.id === selectedId)
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

  // Planned road path and stored GPS track of the selected trip.
  useEffect(() => {
    if (!selectedId) return
    let cancelled = false
    tripsApi
      .plannedPath(schoolId, selectedId)
      .then((path) => !cancelled && setPlanned({ tripId: selectedId, path }))
      .catch(() => !cancelled && setPlanned(null))
    trackApi
      .get(schoolId, selectedId)
      .then(
        (pts) =>
          !cancelled &&
          setHistory({
            tripId: selectedId,
            points: pts.map((p) => ({
              lat: p.latitude,
              lng: p.longitude,
              t: new Date(p.recorded_at).getTime(),
            })),
          }),
      )
      .catch(() => !cancelled && setHistory({ tripId: selectedId, points: [] }))
    return () => {
      cancelled = true
    }
  }, [schoolId, selectedId])

  // Tell the user why the bus they were looking at left the map.
  useEffect(() => {
    const s = live.lastStatus
    if (s && s.tripId === selectedId && (s.status === 'completed' || s.status === 'cancelled')) {
      message.info(
        s.status === 'completed'
          ? 'The trip is complete. Its route has been cleared from the map.'
          : 'The trip was cancelled. Its route has been cleared from the map.',
      )
    }
  }, [live.lastStatus, selectedId, message])

  // Until the planned path arrives (or if it cannot be loaded), join the stops in
  // travel order: the live progress has them with the school, else the route's stops.
  const progressStops = selected ? live.progress[selected.trip_id]?.stops : undefined
  const plannedPath = useMemo<LatLng[]>(() => {
    if (planned && planned.tripId === selected?.trip_id)
      return planned.path.points.map((p) => ({ lat: p.latitude, lng: p.longitude }))
    if (!selected) return []
    const stops = progressStops?.length ? progressStops : routeStops
    return stops.map((s) => ({ lat: s.latitude, lng: s.longitude }))
  }, [planned, selected, progressStops, routeStops])
  // Off-route checks only make sense against a path that follows the roads.
  const roadPath = useMemo(
    () =>
      planned?.tripId === selected?.trip_id && planned?.path.source === 'google'
        ? plannedPath
        : NO_PATH,
    [planned, plannedPath, selected?.trip_id],
  )
  const storedPoints = selected && history?.tripId === selected.trip_id ? history.points : null
  const trail = selected ? live.trails[selected.trip_id] : undefined
  const travelledPath = useMemo<LatLng[]>(() => {
    if (!selected) return []
    const stored = storedPoints ?? []
    const lastT = stored.length ? stored[stored.length - 1].t : -Infinity
    const pts: LatLng[] = [...stored, ...(trail ?? []).filter((p) => p.t > lastT)]
    const now = { lat: selected.latitude, lng: selected.longitude }
    const last = pts[pts.length - 1]
    return last && last.lat === now.lat && last.lng === now.lng ? pts : [...pts, now]
  }, [selected, storedPoints, trail])
  const travelledRuns = useMemo(
    () =>
      roadPath.length > 1
        ? splitByDeviation(travelledPath, roadPath, OFF_ROUTE_M)
        : [{ off: false, path: travelledPath }],
    [travelledPath, roadPath],
  )
  const offRouteM =
    selected && roadPath.length > 1
      ? distanceToPath({ lat: selected.latitude, lng: selected.longitude }, roadPath)
      : null
  const offRouteNow = offRouteM != null && offRouteM > OFF_ROUTE_M
  const routeShown = showPlanned || showTravelled

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
            <BusGlyph color={busStateColors[state]} />
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
          <BusGlyph color={token.colorTextQuaternary} />
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
        <BusGlyph color={busStateColors[busState(selected)]} />
        <div style={{ minWidth: 0 }}>
          <Typography.Text strong>{selected.bus_number}</Typography.Text>
          <br />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {selected.route_code} · {tripTypeLabels[selected.trip_type]}
          </Typography.Text>
        </div>
      </Flex>
      <Typography.Text style={{ fontSize: 13 }}>
        Driver: <strong>{selected.driver_name}</strong>
        {selectedTrip?.driver.mobile && (
          <>
            {' · '}
            <a href={`tel:${selectedTrip.driver.mobile}`}>
              <PhoneOutlined /> {selectedTrip.driver.mobile}
            </a>
          </>
        )}
      </Typography.Text>
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
      <Flex
        vertical
        gap={6}
        style={{
          padding: 10,
          borderRadius: token.borderRadius,
          background: token.colorFillQuaternary,
        }}
      >
        <Flex align="center" justify="space-between" gap="small" wrap>
          <Button
            size="small"
            type={routeShown ? 'default' : 'primary'}
            icon={routeShown ? <ClearOutlined /> : <NodeIndexOutlined />}
            onClick={() => {
              setShowPlanned(!routeShown)
              setShowTravelled(!routeShown)
            }}
          >
            {routeShown ? 'Clear route' : 'Show route'}
          </Button>
          {offRouteM != null && (
            <Tag color={offRouteNow ? 'red' : 'green'} style={{ margin: 0 }}>
              {offRouteNow ? `Off route · ${formatDistance(offRouteM)} away` : 'On route'}
            </Tag>
          )}
        </Flex>
        <Flex gap="middle" wrap>
          <Checkbox checked={showPlanned} onChange={(e) => setShowPlanned(e.target.checked)}>
            <LineKey color={pathColors.planned} /> Planned route
          </Checkbox>
          <Checkbox checked={showTravelled} onChange={(e) => setShowTravelled(e.target.checked)}>
            <LineKey color={pathColors.travelled} /> Travelled
            {roadPath.length > 1 && (
              <>
                {' / '}
                <LineKey color={pathColors.off} /> off route
              </>
            )}
          </Checkbox>
        </Flex>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {[
            travelledPath.length > 1 && `Travelled ${formatDistance(pathLength(travelledPath))}`,
            plannedPath.length > 1 && `planned ${formatDistance(pathLength(plannedPath))}`,
          ]
            .filter(Boolean)
            .join(' · ')}
          {plannedPath.length > 1 &&
            roadPath.length === 0 &&
            '. The planned route joins the stops with straight lines (following the roads needs the Google server key in Settings → Maps); off-route checks need the road route.'}
          {storedPoints?.length === 0 &&
            ' The school keeps no GPS history, so the travelled path starts from when this page was opened.'}
        </Typography.Text>
      </Flex>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        Stops ·{' '}
        {selectedProgress
          ? `ETA ${selectedProgress.eta_source === 'google' ? 'by Google' : 'estimated'}`
          : 'status appears with the next GPS update'}
      </Typography.Text>
      {selectedProgress && <StopProgressList stops={selectedProgress.stops} />}
    </Flex>
  )

  const renderInfo = (b: BusLocation) => {
    const state = busState(b)
    const next = nextStop(b.trip_id)
    const mobile = startedTrips.find((t) => t.id === b.trip_id)?.driver.mobile
    return (
      <div style={{ fontSize: 13, lineHeight: 1.7, color: '#1f2937' }}>
        <div>
          <Tag color={stateTagColors[state]}>{stateLabels[state]}</Tag>
          <span style={{ color: '#6b7280' }}>{stateDetail(b, now)}</span>
        </div>
        <div>
          Route <strong>{b.route_code}</strong> · {tripTypeLabels[b.trip_type]}
        </div>
        <div>
          Driver <strong>{b.driver_name}</strong>
        </div>
        {mobile && (
          <div>
            <PhoneOutlined /> <a href={`tel:${mobile}`}>{mobile}</a>
          </div>
        )}
        {next && (
          <div>
            Next <strong>{next.name}</strong> · ETA {formatEta(next.eta_seconds)}
          </div>
        )}
        {offRouteNow && (
          <div style={{ color: pathColors.off, fontWeight: 600 }}>
            Off route · {formatDistance(offRouteM!)} from the planned route
          </div>
        )}
      </div>
    )
  }

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
      {selected && showPlanned && plannedPath.length > 1 && (
        <Flex align="center" gap={6}>
          <LineKey color={pathColors.planned} /> Planned
        </Flex>
      )}
      {selected && showTravelled && (
        <Flex align="center" gap={6}>
          <LineKey color={pathColors.travelled} /> Travelled
        </Flex>
      )}
      {selected && showTravelled && roadPath.length > 1 && (
        <Flex align="center" gap={6}>
          <LineKey color={pathColors.off} /> Off route
        </Flex>
      )}
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
          routeStops={selected && showPlanned ? routeStops : []}
          progressStops={selected && showPlanned ? selectedProgress?.stops : undefined}
          plannedPath={showPlanned ? plannedPath : NO_PATH}
          travelled={selected && showTravelled ? travelledRuns : []}
          renderInfo={renderInfo}
          insetLeft={floating && panelOpen ? PANEL_WIDTH + 16 : 0}
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

/** A short line in a path colour, for the legend and the route checkboxes. */
function LineKey({ color }: { color: string }) {
  return (
    <span
      aria-hidden
      style={{
        display: 'inline-block',
        width: 16,
        height: 4,
        borderRadius: 2,
        background: color,
        verticalAlign: 'middle',
      }}
    />
  )
}
