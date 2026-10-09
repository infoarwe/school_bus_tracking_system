import GoogleRouteMap from './GoogleRouteMap'
import LeafletRouteMap from './LeafletRouteMap'
import MapProvider from './MapProvider'
import type { RouteMapProps } from './routeMapTypes'

export type { PickedPoint } from './routeMapTypes'

/**
 * Route map with numbered stops. Uses Google Maps with the school's own key
 * (Settings → Maps), or OpenStreetMap when the school has no key.
 */
export default function RouteMap(props: RouteMapProps) {
  return (
    <MapProvider
      height={props.height ?? 480}
      google={(key) => <GoogleRouteMap apiKey={key} {...props} />}
      osm={() => <LeafletRouteMap {...props} />}
    />
  )
}
