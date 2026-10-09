import GoogleLiveMap from './GoogleLiveMap'
import LeafletLiveMap from './LeafletLiveMap'
import MapProvider from './MapProvider'
import type { LiveMapProps } from './liveMapTypes'

/** Live bus map: Google Maps with the school's key, or OpenStreetMap without one. */
export default function LiveMap(props: LiveMapProps) {
  return (
    <MapProvider
      height={props.height ?? 560}
      google={(key) => <GoogleLiveMap apiKey={key} {...props} />}
      osm={() => <LeafletLiveMap {...props} />}
    />
  )
}
