/** Distance in metres between two points (haversine; plenty accurate for stops). */
export function distanceM(
  a: { latitude: number; longitude: number },
  b: { latitude: number; longitude: number },
): number {
  const R = 6371000
  const rad = Math.PI / 180
  const dLat = (b.latitude - a.latitude) * rad
  const dLng = (b.longitude - a.longitude) * rad
  const h =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(a.latitude * rad) * Math.cos(b.latitude * rad) * Math.sin(dLng / 2) ** 2
  return 2 * R * Math.asin(Math.sqrt(h))
}

/** Centre of a set of stops, to bias place search towards the route. */
export function centreOf(points: { latitude: number; longitude: number }[]) {
  if (points.length === 0) return null
  return {
    lat: points.reduce((s, p) => s + p.latitude, 0) / points.length,
    lng: points.reduce((s, p) => s + p.longitude, 0) / points.length,
  }
}

export function formatDistance(m: number): string {
  return m < 1000 ? `${Math.round(m)} m` : `${(m / 1000).toFixed(1)} km`
}

export interface LatLng {
  lat: number
  lng: number
}

const EARTH_M = 6371000
const rad = (d: number) => (d * Math.PI) / 180

/** distanceM for {lat, lng} points. */
export function haversine(a: LatLng, b: LatLng): number {
  return distanceM({ latitude: a.lat, longitude: a.lng }, { latitude: b.lat, longitude: b.lng })
}

/** Length of a path in metres. */
export function pathLength(path: LatLng[]): number {
  let m = 0
  for (let i = 1; i < path.length; i++) m += haversine(path[i - 1], path[i])
  return m
}

/** Compass bearing from a to b, degrees clockwise from north. */
export function bearing(a: LatLng, b: LatLng): number {
  const y = Math.sin(rad(b.lng - a.lng)) * Math.cos(rad(b.lat))
  const x =
    Math.cos(rad(a.lat)) * Math.sin(rad(b.lat)) -
    Math.sin(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.cos(rad(b.lng - a.lng))
  return ((Math.atan2(y, x) * 180) / Math.PI + 360) % 360
}

/**
 * Shortest distance in metres from p to the polyline (flat projection around p;
 * accurate to well under a metre at city scale).
 */
export function distanceToPath(p: LatLng, path: LatLng[]): number {
  if (path.length === 0) return Infinity
  const kx = rad(1) * EARTH_M * Math.cos(rad(p.lat))
  const ky = rad(1) * EARTH_M
  const xy = (q: LatLng) => [(q.lng - p.lng) * kx, (q.lat - p.lat) * ky]
  let best = Infinity
  let [ax, ay] = xy(path[0])
  if (path.length === 1) return Math.hypot(ax, ay)
  for (let i = 1; i < path.length; i++) {
    const [bx, by] = xy(path[i])
    const dx = bx - ax
    const dy = by - ay
    const len2 = dx * dx + dy * dy
    const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, -(ax * dx + ay * dy) / len2))
    best = Math.min(best, Math.hypot(ax + t * dx, ay + t * dy))
    ax = bx
    ay = by
  }
  return best
}

/**
 * Splits a travelled path into runs on and off the planned path (more than
 * `thresholdM` away). Neighbouring runs share their boundary point so the line
 * stays continuous.
 */
export function splitByDeviation(
  travelled: LatLng[],
  planned: LatLng[],
  thresholdM: number,
): { off: boolean; path: LatLng[] }[] {
  const runs: { off: boolean; path: LatLng[] }[] = []
  for (const p of travelled) {
    const off = distanceToPath(p, planned) > thresholdM
    const last = runs[runs.length - 1]
    if (last && last.off === off) last.path.push(p)
    else runs.push({ off, path: last ? [last.path[last.path.length - 1], p] : [p] })
  }
  return runs
}
