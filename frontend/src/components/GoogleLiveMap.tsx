import { useEffect, useMemo, useRef, useState } from 'react'
import { APIProvider, InfoWindow, Map, Marker, Polyline, useMap } from '@vis.gl/react-google-maps'
import type { BusLocation } from '../services/live'
import type { StopStatus } from '../services/types'
import { haversine, type LatLng } from '../utils/geo'
import { busMarkerIcon } from './busMarker'
import { busState, busStateColors, pathColors, type LiveMapProps } from './liveMapTypes'
import { DEFAULT_CENTER } from './routeMapTypes'

export default function GoogleLiveMap({
  apiKey,
  height = 560,
  ...props
}: LiveMapProps & { apiKey: string }) {
  return (
    <APIProvider apiKey={apiKey}>
      <Map
        defaultCenter={DEFAULT_CENTER}
        defaultZoom={5}
        gestureHandling="greedy"
        clickableIcons={false}
        // The bus list floats over the top-left corner; keep Google's controls on the right.
        mapTypeControl={false}
        streetViewControl={false}
        style={{ height, width: '100%', borderRadius: 8, overflow: 'hidden' }}
      >
        <Overlay {...props} />
      </Map>
    </APIProvider>
  )
}

/** Stop marker fill by live status: reached/crossed stops are done. */
// One shared empty path, so "no route" does not look like a new route on every render.
const NO_PATH: LatLng[] = []

const stopFill: Record<StopStatus, string> = {
  upcoming: '#ffffff',
  approaching: '#f59e0b',
  reached: '#16a34a',
  crossed: '#94a3b8',
}

function Overlay({
  buses,
  selectedId,
  onSelect,
  routeStops = [],
  progressStops,
  plannedPath = NO_PATH,
  travelled = [],
  renderInfo,
  insetLeft = 0,
}: Omit<LiveMapProps, 'height'>) {
  // The info window opens with each newly selected bus; closing it keeps the selection.
  const [infoClosedFor, setInfoClosedFor] = useState<string | null>(null)
  const infoOpen = infoClosedFor !== selectedId
  const selected = buses.find((b) => b.trip_id === selectedId)

  const stops = useMemo(
    () =>
      progressStops?.length
        ? progressStops.map((s) => ({
            key: s.stop_id,
            pos: { lat: s.latitude, lng: s.longitude },
            label: s.stop_id === 'school' ? 'S' : String(s.sequence),
            title: s.stop_id === 'school' ? 'School' : `${s.sequence}. ${s.name}`,
            fill: s.missed ? pathColors.off : stopFill[s.status],
            done: s.status === 'reached' || s.status === 'crossed',
          }))
        : routeStops.map((s) => ({
            key: s.id,
            pos: { lat: s.latitude, lng: s.longitude },
            label: String(s.sequence),
            title: `${s.sequence}. ${s.name}`,
            fill: '#ffffff',
            done: false,
          })),
    [progressStops, routeStops],
  )

  return (
    <>
      <Camera buses={buses} selected={selected} plannedPath={plannedPath} insetLeft={insetLeft} />
      {plannedPath.length > 1 && (
        <>
          {/* A white casing under the route keeps it readable over busy map tiles. */}
          <Polyline path={plannedPath} strokeColor="#ffffff" strokeOpacity={0.9} strokeWeight={8} />
          <Polyline
            path={plannedPath}
            strokeColor={pathColors.planned}
            strokeOpacity={0.75}
            strokeWeight={5}
          />
        </>
      )}
      {travelled.map(
        (run, i) =>
          run.path.length > 1 && (
            <Polyline
              key={i}
              path={run.path}
              strokeColor={run.off ? pathColors.off : pathColors.travelled}
              strokeOpacity={0.95}
              strokeWeight={run.off ? 5 : 4}
              zIndex={10}
            />
          ),
      )}
      {stops.map((s) => (
        <Marker
          key={s.key}
          position={s.pos}
          title={s.title}
          zIndex={20}
          icon={{
            path: google.maps.SymbolPath.CIRCLE,
            scale: 11,
            fillColor: s.fill,
            fillOpacity: 1,
            strokeColor: pathColors.planned,
            strokeWeight: 2,
          }}
          label={{
            text: s.label,
            color: s.done || s.fill === pathColors.off ? '#ffffff' : pathColors.planned,
            fontWeight: '700',
            fontSize: '11px',
          }}
        />
      ))}
      {buses.map((b) => (
        <BusMarker
          key={b.trip_id}
          bus={b}
          selected={b.trip_id === selectedId}
          onClick={() => {
            if (b.trip_id === selectedId) setInfoClosedFor(null)
            else onSelect(b.trip_id)
          }}
        />
      ))}
      {selected && infoOpen && renderInfo && (
        <InfoWindow
          position={{ lat: selected.latitude, lng: selected.longitude }}
          pixelOffset={[0, -30]}
          headerContent={<strong>{selected.bus_number}</strong>}
          onCloseClick={() => setInfoClosedFor(selectedId)}
          shouldFocus={false}
          minWidth={220}
        >
          {renderInfo(selected)}
        </InfoWindow>
      )}
    </>
  )
}

/**
 * A bus on the map. It glides to each new fix instead of jumping, and points in
 * the direction of travel (the phone's heading, or its last move: useLiveBuses).
 */
function BusMarker({
  bus,
  selected,
  onClick,
}: {
  bus: BusLocation
  selected: boolean
  onClick: () => void
}) {
  const target = useMemo(
    () => ({ lat: bus.latitude, lng: bus.longitude }),
    [bus.latitude, bus.longitude],
  )
  const pos = useGlide(target)
  const heading = bus.heading
  const state = busState(bus)
  const icon = useMemo(
    () =>
      busMarkerIcon({
        label: bus.bus_number,
        color: busStateColors[state],
        heading: state === 'offline' || heading == null ? null : Math.round(heading / 5) * 5,
        selected,
      }),
    [bus.bus_number, state, heading, selected],
  )
  return (
    <Marker
      position={pos}
      title={`${bus.bus_number} · ${bus.route_code} · ${bus.driver_name}`}
      zIndex={selected ? 1000 : 100}
      onClick={onClick}
      icon={icon}
    />
  )
}

/** Animates from the previous position to `to` (skipped for big jumps, e.g. after signal loss). */
function useGlide(to: LatLng): LatLng {
  const [pos, setPos] = useState(to)
  const current = useRef(to)
  useEffect(() => {
    const from = current.current
    const duration = haversine(from, to) > 2000 ? 0 : 900
    const start = performance.now()
    let frame = 0
    const step = (now: number) => {
      const k = duration ? Math.min(1, (now - start) / duration) : 1
      const e = k * (2 - k) // ease out
      const p = { lat: from.lat + (to.lat - from.lat) * e, lng: from.lng + (to.lng - from.lng) * e }
      current.current = p
      setPos(p)
      if (k < 1) frame = requestAnimationFrame(step)
    }
    frame = requestAnimationFrame(step)
    return () => cancelAnimationFrame(frame)
  }, [to])
  return pos
}

function Camera({
  buses,
  selected,
  plannedPath,
  insetLeft,
}: {
  buses: BusLocation[]
  selected: BusLocation | undefined
  plannedPath: LatLng[]
  insetLeft: number
}) {
  const map = useMap()
  const ids = buses
    .map((b) => b.trip_id)
    .sort()
    .join(',')
  const selectedId = selected?.trip_id

  // Nothing selected: show every bus. Refit only when buses appear or disappear.
  useEffect(() => {
    if (!map || selectedId || buses.length === 0) return
    if (buses.length === 1) {
      map.setCenter({ lat: buses[0].latitude, lng: buses[0].longitude })
      map.setZoom(15)
      return
    }
    const bounds = new google.maps.LatLngBounds()
    buses.forEach((b) => bounds.extend({ lat: b.latitude, lng: b.longitude }))
    map.fitBounds(bounds, 60)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids, selectedId, map])

  // A bus was selected (or its route loaded): show the whole route with the bus.
  useEffect(() => {
    if (!map || !selected) return
    if (plannedPath.length > 1) {
      const bounds = new google.maps.LatLngBounds()
      plannedPath.forEach((p) => bounds.extend(p))
      bounds.extend({ lat: selected.latitude, lng: selected.longitude })
      // Leave room for the bus panel floating on the left.
      map.fitBounds(bounds, { top: 60, bottom: 60, right: 60, left: 60 + insetLeft })
    } else {
      map.panTo({ lat: selected.latitude, lng: selected.longitude })
      if ((map.getZoom() ?? 0) < 14) map.setZoom(15)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId, plannedPath, map])

  // Follow the selected bus only when it drives out of view, so the user can pan around.
  useEffect(() => {
    if (!map || !selected) return
    const p = { lat: selected.latitude, lng: selected.longitude }
    if (!map.getBounds()?.contains(p)) map.panTo(p)
  }, [selected, map])
  return null
}
