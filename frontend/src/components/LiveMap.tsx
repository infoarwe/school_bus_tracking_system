import GoogleLiveMap from './GoogleLiveMap'
import MapProvider from './MapProvider'
import type { LiveMapProps } from './liveMapTypes'

/** Live bus map: Google Maps only, with the school's key (Settings → Maps). */
export default function LiveMap(props: LiveMapProps) {
  return (
    <MapProvider
      height={props.height ?? 560}
      google={(key) => <GoogleLiveMap apiKey={key} {...props} />}
    />
  )
}
