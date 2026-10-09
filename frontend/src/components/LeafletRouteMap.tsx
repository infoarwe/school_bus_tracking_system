import { useEffect, useMemo } from 'react'
import L from 'leaflet'
import {
  Circle,
  MapContainer,
  Marker,
  Polyline,
  TileLayer,
  Tooltip,
  useMap,
  useMapEvents,
} from 'react-leaflet'
import { DEFAULT_CENTER, type RouteMapProps as Props } from './routeMapTypes'

// OpenStreetMap tiles: free, no key. Their usage policy limits heavy production use;
// switch the TileLayer URL to a paid provider before go-live.
const TILE_URL = 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png'
const ATTRIBUTION =
  '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
const INDIA_CENTER: L.LatLngTuple = [DEFAULT_CENTER.lat, DEFAULT_CENTER.lng]

// Numbered markers drawn with HTML/CSS, so no image assets are needed.
function numberIcon(n: number | string, highlighted = false) {
  return L.divIcon({
    className: highlighted ? 'stop-marker stop-marker--active' : 'stop-marker',
    html: `<span>${n}</span>`,
    iconSize: [28, 28],
    iconAnchor: [14, 14],
  })
}

export default function LeafletRouteMap({
  stops,
  highlightId,
  onPick,
  picked,
  height = 480,
}: Props) {
  const path = useMemo(() => stops.map((s) => [s.latitude, s.longitude] as L.LatLngTuple), [stops])

  return (
    <MapContainer
      center={INDIA_CENTER}
      zoom={5}
      style={{ height, width: '100%', borderRadius: 8, cursor: onPick ? 'crosshair' : undefined }}
    >
      <TileLayer url={TILE_URL} attribution={ATTRIBUTION} />
      <FitToPoints points={picked ? [...path, [picked.latitude, picked.longitude]] : path} />
      {onPick && <ClickToPick onPick={onPick} />}

      {path.length > 1 && (
        <Polyline positions={path} pathOptions={{ color: '#f5a623', weight: 4 }} />
      )}
      {stops.map((s) => (
        <Marker
          key={s.id}
          position={[s.latitude, s.longitude]}
          icon={numberIcon(s.sequence, s.id === highlightId)}
        >
          <Tooltip>
            {s.sequence}. {s.name}
            {s.pickup_time && ` · Pickup ${s.pickup_time}`}
            {s.drop_time && ` · Drop ${s.drop_time}`}
          </Tooltip>
          {s.id === highlightId && (
            <Circle
              center={[s.latitude, s.longitude]}
              radius={s.geofence_radius_m}
              pathOptions={{ color: '#1677ff' }}
            />
          )}
        </Marker>
      ))}

      {picked && (
        <>
          <Marker position={[picked.latitude, picked.longitude]} icon={numberIcon('★', true)} />
          <Circle
            center={[picked.latitude, picked.longitude]}
            radius={picked.radius}
            pathOptions={{ color: '#1677ff', fillOpacity: 0.15 }}
          />
        </>
      )}
    </MapContainer>
  )
}

/** Zooms to show all points whenever the set of points changes. */
function FitToPoints({ points }: { points: L.LatLngTuple[] }) {
  const map = useMap()
  const key = points.map((p) => p.join(',')).join(';')
  useEffect(() => {
    if (points.length === 1) map.setView(points[0], Math.max(map.getZoom(), 15))
    else if (points.length > 1) map.fitBounds(points, { padding: [40, 40], maxZoom: 16 })
    // Only refit when the coordinates change, not on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, map])
  return null
}

function ClickToPick({ onPick }: { onPick: (lat: number, lng: number) => void }) {
  useMapEvents({
    click: (e) => onPick(Number(e.latlng.lat.toFixed(6)), Number(e.latlng.lng.toFixed(6))),
  })
  return null
}
