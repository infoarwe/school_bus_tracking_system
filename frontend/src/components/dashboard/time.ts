/** "just now", "4 min ago", "2 h ago" */
export function timeAgo(iso: string, now: number = Date.now()): string {
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 45) return 'just now'
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  return new Date(iso).toLocaleDateString()
}

/** "08:42" in the viewer's locale */
export const clockTime = (d: Date | string) =>
  new Date(d).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
