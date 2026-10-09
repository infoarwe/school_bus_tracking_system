import { useCallback, useEffect, useState } from 'react'
import { errorMessage } from '../utils/formErrors'

/**
 * Loads data once (and on retry). Unlike a bare effect, a failure is kept as
 * `error` so the UI can say what went wrong instead of loading forever.
 * `load` must be stable (useCallback).
 */
export function useLoad<T>(load: () => Promise<T>) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    load()
      .then((d) => {
        if (cancelled) return
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => !cancelled && setError(errorMessage(e)))
    return () => {
      cancelled = true
    }
  }, [load, attempt])

  const retry = useCallback(() => {
    setError(null)
    setAttempt((n) => n + 1)
  }, [])

  return { data, setData, error, retry }
}
