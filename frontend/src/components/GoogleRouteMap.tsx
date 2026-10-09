import { useEffect, useMemo } from 'react'
import { APIProvider, Circle, Map, Marker, Polyline, useMap } from '@vis.gl/react-google-maps'
import { DEFAULT_CENTER, type RouteMapProps } from './routeMapTypes'

const ORANGE = '#f5a623'
const BLUE = '#1677ff'

// Numbered circle markers, matching the OpenStreetMap version.
function numberMarker(
  label: string,
  highlighted: boolean,
): Pick<google.maps.MarkerOptions, 'icon' | 'label'> {
  return {
    icon: {
      path: google.maps.SymbolPath.CIRCLE,
      scale: 13,
      fillColor: highlighted ? BLUE : ORANGE,
      fillOpacity: 1,
      strokeColor: '#ffffff',
      strokeWeight: 2,
    },
    label: { text: label, color: '#ffffff', fontWeight: '600', fontSize: '12px' },
  }
}

export default function GoogleRouteMap({
  apiKey,
  stops,
  highlightId,
  onPick,
  picked,
  height = 480,
  track,
  bus,
}: RouteMapProps & { apiKey: string }) {
  return (
    <APIProvider apiKey={apiKey}>
      <Map
        defaultCenter={DEFAULT_CENTER}
        defaultZoom={5}
        gestureHandling="greedy"
        clickableIcons={false}
        draggableCursor={onPick ? 'crosshair' : undefined}
        style={{ height, width: '100%', borderRadius: 8, overflow: 'hidden' }}
        onClick={(e) => {
          const p = e.detail.latLng
          if (onPick && p) onPick(Number(p.lat.toFixed(6)), Number(p.lng.toFixed(6)))
        }}
      >
        <Overlay stops={stops} highlightId={highlightId} picked={picked} track={track} bus={bus} />
      </Map>
    </APIProvider>
  )
}

function Overlay({
  stops,
  highlightId,
  picked,
  track,
  bus,
}: Pick<RouteMapProps, 'stops' | 'highlightId' | 'picked' | 'track' | 'bus'>) {
  const path = useMemo(() => stops.map((s) => ({ lat: s.latitude, lng: s.longitude })), [stops])
  const highlighted = stops.find((s) => s.id === highlightId)

  return (
    <>
      <FitToPoints
        points={
          track?.length
            ? track
            : picked
              ? [...path, { lat: picked.latitude, lng: picked.longitude }]
              : path
        }
      />
      {track && track.length > 1 && (
        <Polyline path={track} strokeColor={BLUE} strokeOpacity={0.7} strokeWeight={3} />
      )}
      {bus && <Marker position={bus} zIndex={1000} {...numberMarker('🚌', true)} />}
      {path.length > 1 && <Polyline path={path} strokeColor={ORANGE} strokeWeight={4} />}
      {stops.map((s) => (
        <Marker
          key={s.id}
          position={{ lat: s.latitude, lng: s.longitude }}
          title={[
            `${s.sequence}. ${s.name}`,
            s.pickup_time && `Pickup ${s.pickup_time}`,
            s.drop_time && `Drop ${s.drop_time}`,
          ]
            .filter(Boolean)
            .join(' · ')}
          {...numberMarker(String(s.sequence), s.id === highlightId)}
        />
      ))}
      {highlighted && (
        <Circle
          center={{ lat: highlighted.latitude, lng: highlighted.longitude }}
          radius={highlighted.geofence_radius_m}
          strokeColor={BLUE}
          fillOpacity={0.1}
          clickable={false}
        />
      )}
      {picked && (
        <>
          <Marker
            position={{ lat: picked.latitude, lng: picked.longitude }}
            {...numberMarker('★', true)}
          />
          <Circle
            center={{ lat: picked.latitude, lng: picked.longitude }}
            radius={picked.radius}
            strokeColor={BLUE}
            fillColor={BLUE}
            fillOpacity={0.15}
            clickable={false}
          />
        </>
      )}
    </>
  )
}

/** Zooms to show all points whenever the set of points changes. */
function FitToPoints({ points }: { points: google.maps.LatLngLiteral[] }) {
  const map = useMap()
  const key = points.map((p) => `${p.lat},${p.lng}`).join(';')
  useEffect(() => {
    if (!map || points.length === 0) return
    if (points.length === 1) {
      map.setCenter(points[0])
      map.setZoom(Math.max(map.getZoom() ?? 0, 15))
      return
    }
    const bounds = new google.maps.LatLngBounds()
    points.forEach((p) => bounds.extend(p))
    map.fitBounds(bounds, 40)
    // Only refit when the coordinates change, not on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, map])
  return null
}
