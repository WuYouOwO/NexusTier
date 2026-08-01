export function shortID(v) { return v ? String(v).slice(0, 8) : '—' }

export function formatDate(date) {
  if (!date || isNaN(date.getTime())) return 'Unknown'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

export function relativeTime(date) {
  if (!date || isNaN(date.getTime())) return 'Unknown'
  const seconds = Math.round((date.getTime() - Date.now()) / 1000)
  const fmt = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  if (Math.abs(seconds) < 60) return fmt.format(seconds, 'second')
  const minutes = Math.round(seconds / 60)
  if (Math.abs(minutes) < 60) return fmt.format(minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (Math.abs(hours) < 24) return fmt.format(hours, 'hour')
  return fmt.format(Math.round(hours / 24), 'day')
}

export function formatBytes(v) {
  if (v == null) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let n = Number(v), i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(i ? 1 : 0)} ${units[i]}`
}

export function formatLatency(ms) {
  if (ms == null) return '—'
  return `${Number(ms).toFixed(1)} ms`
}

export function formatLoss(rate) {
  if (rate == null) return '—'
  return `${(rate * 100).toFixed(2)}%`
}

export function deviceLabel(d) {
  if (!d) return 'Unknown'
  return [d.distribution, d.os_version].filter(Boolean).join(' ') || d.os_type || 'Unknown'
}

/** RTT → hue-based color: green(0ms)→yellow(50ms)→red(200ms+) */
export function latencyColor(ms) {
  if (ms == null) return '#94a3b8'
  const t = Math.min(ms / 200, 1)
  const h = Math.round((1 - t) * 120)
  return `hsl(${h}, 70%, 45%)`
}
