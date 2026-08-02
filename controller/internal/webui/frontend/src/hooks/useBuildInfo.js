import { useState, useEffect } from 'react'

// 构建信息在进程生命周期内不变，只取一次。
export function useBuildInfo() {
  const [build, setBuild] = useState(null)

  useEffect(() => {
    let cancelled = false
    fetch('/v1/build', { headers: { Accept: 'application/json' } })
      .then((res) => (res.ok ? res.json() : null))
      .then((json) => {
        if (!cancelled) setBuild(json)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  return build
}
