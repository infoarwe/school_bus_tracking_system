import { useCallback, useEffect, useState } from 'react'
import { App } from 'antd'
import type { ListParams, Paged, PageMeta } from '../services/types'
import { errorMessage } from '../utils/formErrors'

/**
 * Server-side paged list with search and filters, shared by every master-data page.
 * `fetcher` must be stable (wrap in useCallback) or the list refetches every render.
 */
export function useServerList<T, F extends ListParams = ListParams>(
  fetcher: (params: F) => Promise<Paged<T>>,
  initial: F = {} as F,
) {
  const { message } = App.useApp()
  const [rows, setRows] = useState<T[]>([])
  const [meta, setMeta] = useState<PageMeta>({ page: 1, page_size: 20, total: 0 })
  const [params, setParams] = useState<F>({ page: 1, ...initial })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    fetcher(params)
      .then((res) => {
        if (cancelled) return
        setRows(res.data)
        setMeta(res.meta)
        setError(null)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        // Shown in the table with a Retry button (ListEmpty); one toast as well.
        setRows([])
        setError(errorMessage(e))
        message.error(errorMessage(e))
      })
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [fetcher, params, reloadKey, message])

  /** Change filters; anything other than `page` resets to page 1. */
  const update = useCallback((patch: Partial<F>) => {
    setLoading(true)
    setParams((p) => ({ ...p, page: 1, ...patch }))
  }, [])

  const reload = useCallback(() => {
    setLoading(true)
    setReloadKey((k) => k + 1)
  }, [])

  const pagination = {
    current: meta.page,
    pageSize: meta.page_size,
    total: meta.total,
    showSizeChanger: false,
    onChange: (page: number) => update({ page } as Partial<F>),
  }

  /** True when a search or filter (not just the page) is applied. */
  const filtered = Object.entries(params).some(([k, v]) => k !== 'page' && v !== '' && v != null)

  return { rows, meta, params, loading, error, filtered, update, reload, pagination }
}
