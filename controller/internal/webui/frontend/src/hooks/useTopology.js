import { useState, useEffect, useCallback, useRef } from 'react'

const DEFAULT_INTERVAL = 15_000

export function useTopology() {
  const [data, setData] = useState(null)
  const [status, setStatus] = useState('connecting') // connecting | online | offline
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const [interval, setIntervalMs] = useState(DEFAULT_INTERVAL)
  const [activeFilter, setActiveFilter] = useState('true')
  const cursorRef = useRef(null)
  const machinesRef = useRef([])

  const fetchPage = useCallback(async (append = false) => {
    setLoading(true)
    const params = new URLSearchParams({ limit: '200' })
    if (activeFilter) params.set('active', activeFilter)
    if (append && cursorRef.current) params.set('cursor', cursorRef.current)

    try {
      const res = await fetch(`/v1/topology?${params}`, {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = await res.json()
      const machines = append
        ? [...machinesRef.current, ...(json.machines || [])]
        : (json.machines || [])
      machinesRef.current = machines
      cursorRef.current = json.page?.next_cursor ?? null
      setData({
        machines,
        latestCollection: json.latest_collection ?? null,
        latestErrors: json.latest_errors ?? [],
        hasMore: !!json.page?.next_cursor,
      })
      setStatus('online')
      setError(null)
    } catch (err) {
      setStatus('offline')
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [activeFilter])

  const refresh = useCallback(() => fetchPage(false), [fetchPage])
  const loadMore = useCallback(() => fetchPage(true), [fetchPage])

  // auto-refresh
  useEffect(() => {
    refresh()
    if (!interval) return
    const id = setInterval(refresh, interval)
    return () => clearInterval(id)
  }, [refresh, interval])

  // reset cursor when filter changes
  useEffect(() => {
    cursorRef.current = null
    machinesRef.current = []
  }, [activeFilter])

  return {
    data, status, error, loading,
    interval, setIntervalMs,
    activeFilter, setActiveFilter,
    refresh, loadMore,
  }
}
