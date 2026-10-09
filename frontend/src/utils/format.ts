// Display formats for ETA and distance (Parent app copy: "ETA: 8 minutes | Distance: 2.4 km").

export function formatEta(seconds: number | null): string {
  if (seconds == null) return '—'
  if (seconds < 60) return 'under a minute'
  const m = Math.round(seconds / 60)
  return m < 60 ? `${m} min` : `${Math.floor(m / 60)} h ${m % 60} min`
}

export function formatDistance(m: number | null): string {
  if (m == null) return '—'
  return m < 1000 ? `${m} m` : `${(m / 1000).toFixed(1)} km`
}
