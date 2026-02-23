export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const val = bytes / Math.pow(1024, i)
  return `${val.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rem = s % 60
  if (m < 60) return `${m}m ${rem}s`
  const h = Math.floor(m / 60)
  const remM = m % 60
  return `${h}h ${remM}m`
}

export function formatDate(iso: string): string {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    if (d.getFullYear() < 2000) return '—'
    return d.toLocaleString()
  } catch {
    return iso
  }
}

export function formatRelative(iso: string): string {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    // Zero/empty time (Go zero value is year 1)
    if (d.getFullYear() < 2000) return '—'
    const diff = Date.now() - d.getTime()
    const s = Math.floor(diff / 1000)
    if (s < 60) return 'just now'
    const m = Math.floor(s / 60)
    if (m < 60) return `${m}m ago`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h}h ago`
    const days = Math.floor(h / 24)
    return `${days}d ago`
  } catch {
    return iso
  }
}

export function formatNumber(n: number): string {
  return n.toLocaleString()
}
