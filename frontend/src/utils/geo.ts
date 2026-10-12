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
