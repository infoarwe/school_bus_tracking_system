/** Today as YYYY-MM-DD in the given IANA time zone (the school's), not the browser's. */
export function todayIn(timeZone: string | undefined): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: timeZone || undefined }).format(new Date())
}

/** Adds days to a YYYY-MM-DD date. */
export function addDays(date: string, days: number): string {
  const d = new Date(`${date}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + days)
  return d.toISOString().slice(0, 10)
}

const weekdayKeys = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']

/** mon..sun key of a YYYY-MM-DD date. */
export function weekdayOf(date: string): string {
  return weekdayKeys[new Date(`${date}T00:00:00Z`).getUTCDay()]
}

export function formatDate(date: string): string {
  return new Date(`${date}T00:00:00Z`).toLocaleDateString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    timeZone: 'UTC',
  })
}
