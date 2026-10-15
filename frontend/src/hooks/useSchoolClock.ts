import { useMemo } from 'react'

/** "HH:MM" (24 h) in the school's time zone, or null for a missing timestamp. */
export function useSchoolClock(timeZone: string | undefined) {
  return useMemo(() => {
    const clock = new Intl.DateTimeFormat('en-GB', {
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
      timeZone,
    })
    return (iso: string | null) => (iso ? clock.format(new Date(iso)) : null)
  }, [timeZone])
}
