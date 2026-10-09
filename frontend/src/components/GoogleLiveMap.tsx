import { useEffect, useMemo } from 'react'
import { APIProvider, Map, Marker, Polyline, useMap } from '@vis.gl/react-google-maps'
import { busState, busStateColors, type LiveMapProps } from './liveMapTypes'
import { DEFAULT_CENTER } from './routeMapTypes'

export default function GoogleLiveMap({
  apiKey,
  buses,
  selectedId,
  onSelect,
  routeStops = [],
  height = 560,
}: LiveMapProps & { apiKey: string }) {
  return (
    <APIProvider apiKey={apiKey}>
      <Map
        defaultCenter={DEFAULT_CENTER}
        defaultZoom={5}
        gestureHandling="greedy"
        clickableIcons={false}
        style={{ height, width: '100%', borderRadius: 8, overflow: 'hidden' }}
      >
        <Overlay
          buses={buses}
          selectedId={selectedId}
          onSelect={onSelect}
          routeStops={routeStops}
        />
      </Map>
    </APIProvider>
  )
}

function Overlay({ buses, selectedId, onSelect, routeStops = [] }: LiveMapProps) {
  const path = useMemo(
    () => routeStops.map((s) => ({ lat: s.latitude, lng: s.longitude })),
    [routeStops],
  )
  return (
    <>
      <Camera buses={buses} selectedId={selectedId} onSelect={onSelect} />
      {path.length > 1 && (
        <Polyline path={path} strokeColor="#1677ff" strokeOpacity={0.6} strokeWeight={3} />
      )}
      {routeStops.map((s) => (
        <Marker
          key={s.id}
          position={{ lat: s.latitude, lng: s.longitude }}
          title={`${s.sequence}. ${s.name}`}
          icon={{
            path: google.maps.SymbolPath.CIRCLE,
            scale: 6,
            fillColor: '#ffffff',
            fillOpacity: 1,
            strokeColor: '#1677ff',
            strokeWeight: 2,
          }}
        />
      ))}
      {buses.map((b) => {
        const selected = b.trip_id === selectedId
        return (
          <Marker
            key={b.trip_id}
            position={{ lat: b.latitude, lng: b.longitude }}
            title={`${b.bus_number} · ${b.driver_name}`}
            zIndex={selected ? 1000 : 1}
            onClick={() => onSelect(b.trip_id)}
            icon={{
              path: google.maps.SymbolPath.CIRCLE,
              scale: selected ? 18 : 15,
              fillColor: busStateColors[busState(b)],
              fillOpacity: 1,
              strokeColor: selected ? '#1677ff' : '#ffffff',
              strokeWeight: 3,
            }}
            label={{ text: b.route_code, color: '#ffffff', fontWeight: '600', fontSize: '10px' }}
          />
        )
      })}
    </>
  )
}

function Camera({ buses, selectedId }: LiveMapProps) {
  const map = useMap()
  const ids = buses
    .map((b) => b.trip_id)
    .sort()
    .join(',')
  const selected = buses.find((b) => b.trip_id === selectedId)

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
    // Refit only when buses appear or disappear, not on every position update.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids, selectedId, map])

  useEffect(() => {
    if (map && selected) map.panTo({ lat: selected.latitude, lng: selected.longitude })
  }, [selected, map])
  return null
}
