import { useEffect, useState } from 'react'
import { useCurrentSchool } from '../context/SchoolContext'
import { settingsApi } from '../services/settings'

// Each school has its own Google Maps browser key (Settings → Maps). Cached per school
// for the session; the Settings page updates the cache when a key is saved.
const cache = new Map<string, string>()

export function setCachedMapsKey(schoolId: string, key: string) {
  cache.set(schoolId, key)
}

/**
 * The current school's Google Maps browser key: undefined while loading, '' when the
 * school has none (callers fall back to OpenStreetMap).
 */
export function useSchoolMapsKey(): string | undefined {
  const { schoolId } = useCurrentSchool()
  const [loaded, setLoaded] = useState<{ schoolId: string; key: string } | null>(null)

  useEffect(() => {
    if (!schoolId || cache.has(schoolId)) return
    let cancelled = false
    settingsApi
      .getMaps(schoolId)
      .then((m) => m.browser_key)
      .catch(() => '') // no key readable: use OpenStreetMap
      .then((key) => {
        cache.set(schoolId, key)
        if (!cancelled) setLoaded({ schoolId, key })
      })
    return () => {
      cancelled = true
    }
  }, [schoolId])

  if (!schoolId) return ''
  if (cache.has(schoolId)) return cache.get(schoolId)
  return loaded?.schoolId === schoolId ? loaded.key : undefined
}
