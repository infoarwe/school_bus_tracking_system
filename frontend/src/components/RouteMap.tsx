import GoogleRouteMap from './GoogleRouteMap'
import MapProvider from './MapProvider'
import type { RouteMapProps } from './routeMapTypes'

export type { PickedPoint } from './routeMapTypes'

/**
 * Route map with numbered stops. Google Maps only, with the school's own key
 * (Settings → Maps) — see MapsConfigError for the no-key state.
 */
export default function RouteMap(props: RouteMapProps) {
  return (
    <MapProvider
      height={props.height ?? 480}
      google={(key) => <GoogleRouteMap apiKey={key} {...props} />}
    />
  )
}
