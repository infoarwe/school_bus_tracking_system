import { useEffect, useMemo } from 'react'
import L from 'leaflet'
import {
  CircleMarker,
  MapContainer,
  Marker,
  Polyline,
  TileLayer,
  Tooltip,
  useMap,
} from 'react-leaflet'
import { busState, busStateColors, type LiveMapProps } from './liveMapTypes'
import { DEFAULT_CENTER } from './routeMapTypes'

const TILE_URL = 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png'
const ATTRIBUTION =
  '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'

function busIcon(label: string, color: string, selected: boolean) {
  return L.divIcon({
    className: `bus-marker${selected ? ' bus-marker--selected' : ''}`,
    html: `<span style="background:${color}">🚌 ${label}</span>`,
    iconSize: undefined,
    iconAnchor: [30, 14],
  })
}

export default function LeafletLiveMap({
  buses,
  selectedId,
  onSelect,
  routeStops = [],
  height = 560,
}: LiveMapProps) {
  const path = useMemo(
    () => routeStops.map((s) => [s.latitude, s.longitude] as L.LatLngTuple),
    [routeStops],
  )
  return (
    <MapContainer
      center={[DEFAULT_CENTER.lat, DEFAULT_CENTER.lng]}
      zoom={5}
      style={{ height, width: '100%', borderRadius: 8 }}
    >
      <TileLayer url={TILE_URL} attribution={ATTRIBUTION} />
      <Camera buses={buses} selectedId={selectedId} />
      {path.length > 1 && (
        <Polyline positions={path} pathOptions={{ color: '#1677ff', weight: 3, opacity: 0.6 }} />
      )}
      {routeStops.map((s) => (
        <CircleMarker
          key={s.id}
          center={[s.latitude, s.longitude]}
          radius={6}
          pathOptions={{ color: '#1677ff', fillOpacity: 1, fillColor: '#fff' }}
        >
          <Tooltip>
            {s.sequence}. {s.name}
          </Tooltip>
        </CircleMarker>
      ))}
      {buses.map((b) => (
        <Marker
          key={b.trip_id}
          position={[b.latitude, b.longitude]}
          icon={busIcon(b.route_code, busStateColors[busState(b)], b.trip_id === selectedId)}
          eventHandlers={{ click: () => onSelect(b.trip_id) }}
          zIndexOffset={b.trip_id === selectedId ? 1000 : 0}
        >
          <Tooltip>
            {b.bus_number} · {b.driver_name}
          </Tooltip>
        </Marker>
      ))}
    </MapContainer>
  )
}

/** Fits all buses when the set of buses changes; follows the selected bus. */
function Camera({ buses, selectedId }: Pick<LiveMapProps, 'buses' | 'selectedId'>) {
  const map = useMap()
  const ids = buses
    .map((b) => b.trip_id)
    .sort()
    .join(',')
  const selected = buses.find((b) => b.trip_id === selectedId)

  useEffect(() => {
    if (selectedId || buses.length === 0) return
    if (buses.length === 1) map.setView([buses[0].latitude, buses[0].longitude], 15)
    else
      map.fitBounds(
        buses.map((b) => [b.latitude, b.longitude] as L.LatLngTuple),
        { padding: [60, 60], maxZoom: 15 },
      )
    // Refit only when buses appear or disappear, not on every position update.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids, selectedId, map])

  useEffect(() => {
    if (selected) map.panTo([selected.latitude, selected.longitude])
  }, [selected, map])
  return null
}
