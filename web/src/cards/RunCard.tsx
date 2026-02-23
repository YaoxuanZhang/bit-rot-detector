import { useEffect, useState, useCallback, useRef } from 'react'
import {
  api,
  type StatusResponse,
  type DrivesResponse,
  type DriveStatus,
  type DriveHealth,
  type ProgressEvent as ProgEvt,
} from '../lib/api'
import { useSSE } from '../lib/sse'
import { formatDate, formatRelative, formatBytes, formatNumber } from '../lib/format'

interface Props {
  status: StatusResponse | null
  expanded: boolean
  onExpand: () => void
  onCollapse: () => void
  onRefresh: () => void
  registerRefresh: (fn: () => void) => void
}

// Per-drive progress state keyed by drive name.
// walkTotal is remembered from the walk phase to compute hash% denominator.
interface DriveProgress {
  phase: string  // 'walk' | 'hash' | 'scrub'
  count: number
  total: number
  walkTotal: number
}

function healthFromDrive(drive: DriveStatus): 'ok' | 'warn' | 'err' {
  if (drive.err) return 'err'
  const h: DriveHealth | undefined = drive.health
  if (!h) return 'ok'
  if (h.smart_status === 'FAILED') return 'err'
  if (h.total_space > 0) {
    const pct = (h.used_space / h.total_space) * 100
    if (pct >= 90) return 'err'
    if (pct >= 75) return 'warn'
  }
  return 'ok'
}

function labelFromPath(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/').filter(Boolean)
  return parts[parts.length - 1] || path
}

export default function RunCard({ status, expanded, onExpand, onCollapse, onRefresh, registerRefresh }: Props) {
  const [progMap, setProgMap] = useState<Record<string, DriveProgress>>({})
  const [busy, setBusy] = useState(false)
  const [drivesData, setDrivesData] = useState<DrivesResponse | null>(null)

  // Eagerly load configured drive paths on mount — visible before any scan.
  useEffect(() => {
    api.getDrives().then(setDrivesData).catch(console.error)
  }, [])

  const clearProg = useCallback(() => setProgMap({}), [])

  useEffect(() => {
    registerRefresh(() => {
      clearProg()
      api.getDrives().then(setDrivesData).catch(console.error)
    })
  }, [registerRefresh, clearProg])

  const handleSSEEvent = useCallback((e: ProgEvt) => {
    if (e.phase === 'done') {
      // Remove this drive's progress entry; refresh status
      setProgMap(m => {
        const next = { ...m }
        delete next[e.drive]
        return next
      })
      onRefresh()
    } else {
      setProgMap(m => {
        const prev = m[e.drive]
        // Freeze walkTotal once we leave the walk phase
        const walkTotal = e.phase === 'walk' ? e.count : (prev?.walkTotal ?? prev?.count ?? 0)
        return { ...m, [e.drive]: { phase: e.phase, count: e.count, total: e.total, walkTotal } }
      })
    }
  }, [onRefresh])

  useSSE(handleSSEEvent)

  // Merge drive paths (always known from config) with run results (available after scan)
  const runDrives: DriveStatus[] = (() => {
    if (status?.drives && status.drives.length > 0) return status.drives
    if (drivesData?.paths && drivesData.paths.length > 0)
      return drivesData.paths.map(p => ({ drive: labelFromPath(p), path: p }))
    return []
  })()

  const globalHealth = status
    ? status.running ? 'running'
      : runDrives.some(d => healthFromDrive(d) === 'err') ? 'err'
      : runDrives.some(d => healthFromDrive(d) === 'warn') ? 'warn'
      : runDrives.length > 0 ? 'ok' : 'idle'
    : 'idle'

  const totalScanned = status?.drives.reduce((s, d) => s + (d.sync_result?.files_scanned ?? 0), 0) ?? 0

  async function triggerSync() {
    setBusy(true)
    try { await api.postSync() } finally { setBusy(false); onRefresh() }
  }

  async function triggerScrub() {
    setBusy(true)
    try { await api.postScrub() } finally { setBusy(false); onRefresh() }
  }

  const cardRef = useRef<HTMLDivElement>(null)
  const activeProgress = Object.entries(progMap).filter(([, p]) => p.phase !== 'done')

  return (
    <div
      ref={cardRef}
      className={`card${expanded ? ' expanded' : ''}`}
      onClick={!expanded ? onExpand : undefined}
    >
      <div className="card-header">
        <span className="card-title">Run</span>
        {expanded && (
          <button className="card-close-btn" onClick={e => { e.stopPropagation(); onCollapse() }} title="Collapse (Esc)">✕</button>
        )}
      </div>

      {!expanded ? (
        /* ── Collapsed summary ── */
        <div>
          <div className="summary-row">
            <div className="summary-item">
              <span className={`status-dot ${globalHealth}`} />
            </div>
            <div className="summary-item">
              <span className="summary-val">{runDrives.length}</span>
              <span className="summary-lbl">Drives</span>
            </div>
            <div className="summary-item">
              <span className="summary-val">{formatNumber(totalScanned)}</span>
              <span className="summary-lbl">Scanned</span>
            </div>
          </div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem', color: 'var(--muted)' }}>
            Last run: {status?.last_run ? formatRelative(status.last_run) : '—'}
          </div>
          {status?.duration && (
            <div style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>Duration: {status.duration}</div>
          )}
        </div>
      ) : (
        /* ── Expanded view ── */
        <div onClick={e => e.stopPropagation()}>
          {/* Toolbar */}
          <div className="toolbar">
            <button className="primary" disabled={busy || status?.running} onClick={triggerSync}>⟳ Sync All</button>
            <button disabled={busy || status?.running} onClick={triggerScrub}>🔬 Scrub All</button>
            <a href={api.exportUrl('csv')} download="export.csv">
              <button>↓ CSV</button>
            </a>
            <a href={api.exportUrl('json')} download="export.json">
              <button>↓ JSON</button>
            </a>
            <span style={{ flex: 1 }} />
            <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
              Last: {status?.last_run ? formatDate(status.last_run) : '—'}
              {status?.duration && ` · ${status.duration}`}
            </span>
          </div>

          {/* ── Live progress section ── */}
          {(status?.running || activeProgress.length > 0) && (
            <div style={{ marginBottom: '1.25rem' }}>
              <div className="section-title">Progress</div>
              {activeProgress.length === 0 && status?.running && (
                <div style={{ color: 'var(--muted)', fontSize: '0.85rem' }}>Starting…</div>
              )}
              {activeProgress.map(([driveName, p]) => {
                let pct = 0, rightLabel = '', barClass = '', indeterminate = false
                if (p.phase === 'walk') {
                  // Discovery phase — total unknown, show count on right
                  indeterminate = true
                  rightLabel = `${formatNumber(p.count)} found`
                } else if (p.phase === 'hash') {
                  // Use walk-discovered total as denominator
                  const total = p.walkTotal > 0 ? p.walkTotal : p.total
                  pct = total > 0 ? Math.min((p.count / total) * 100, 100) : 0
                  rightLabel = total > 0
                    ? `${formatNumber(p.count)} / ${formatNumber(total)}`
                    : `${formatNumber(p.count)} hashed`
                } else if (p.phase === 'scrub') {
                  pct = p.total > 0 ? Math.min((p.count / p.total) * 100, 100) : 0
                  rightLabel = p.total > 0
                    ? `${formatNumber(p.count)} / ${formatNumber(p.total)}`
                    : `${formatNumber(p.count)} verified`
                  barClass = ' ok'
                }
                return (
                  <div key={driveName} style={{ marginBottom: '0.9rem' }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.8rem', marginBottom: '0.3rem' }}>
                      <span>
                        {(['walk', 'hash', 'scrub'] as const).map(ph => (
                          <span key={ph} className={`phase-badge${p.phase === ph ? ' active' : ''}`} style={{ marginRight: '0.4rem' }}>
                            {ph.charAt(0).toUpperCase() + ph.slice(1)}
                          </span>
                        ))}
                        <span style={{ marginLeft: '0.6rem', color: 'var(--muted)' }}>{driveName}</span>
                      </span>
                      <span style={{ fontFamily: 'var(--font-mono)' }}>
                        {rightLabel}{!indeterminate && pct > 0 && ` · ${pct.toFixed(1)}%`}
                      </span>
                    </div>
                    <div className="progress-wrap">
                      {indeterminate
                        ? <div className="progress-bar indeterminate" />
                        : <div className={`progress-bar${barClass}`} style={{ width: `${pct}%` }} />}
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {/* ── Per-drive rows ── */}
          <div className="section-title">Drives ({runDrives.length})</div>
          {runDrives.length === 0 && <div className="empty">No drives configured.</div>}
          {runDrives.map((drive, idx) => {
            const sr = drive.sync_result
            const scr = drive.scrub_result
            const health = healthFromDrive(drive)
            const h = drive.health
            const usedPct = h && h.total_space > 0 ? (h.used_space / h.total_space) * 100 : null
            return (
              <div key={drive.path} className="drive-row">
                <div className="drive-row-header">
                  <span className={`status-dot ${health}`} />
                  <span className="drive-name">{drive.drive || labelFromPath(drive.path)}</span>
                  <span className="drive-path">{drive.path}</span>
                  {h && h.total_space > 0 && (
                    <span style={{ fontSize: '0.75rem', color: 'var(--muted)', marginLeft: '0.5rem' }}>
                      {formatBytes(h.free_space)} free
                    </span>
                  )}
                  <span style={{ flex: 1 }} />
                  <button className="sm" onClick={e => { e.stopPropagation(); api.postDriveSync(idx).then(onRefresh) }}>Sync</button>
                  <button className="sm" style={{ marginLeft: '0.4rem' }} onClick={e => { e.stopPropagation(); api.postDriveScrub(idx).then(onRefresh) }}>Scrub</button>
                </div>
                {usedPct !== null && (
                  <div style={{ margin: '0.35rem 0 0.5rem', display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
                    <div className="progress-wrap" style={{ flex: 1 }}>
                      <div
                        className={`progress-bar${usedPct >= 90 ? ' err' : usedPct >= 75 ? ' warn' : ''}`}
                        style={{ width: `${usedPct.toFixed(1)}%` }}
                      />
                    </div>
                    <span style={{ fontSize: '0.72rem', color: 'var(--muted)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap' }}>
                      {formatBytes(h!.used_space)} / {formatBytes(h!.total_space)} ({usedPct.toFixed(1)}%)
                    </span>
                  </div>
                )}
                {drive.err && <div className="alert err">{drive.err}</div>}
                {(sr || scr) ? (
                  <div className="stat-grid">
                    {[
                      { lbl: 'Scanned',   val: sr?.files_scanned  ?? 0 },
                      { lbl: 'Added',     val: sr?.files_added    ?? 0, cls: (sr?.files_added    ?? 0) > 0 ? 'ok'   : '' },
                      { lbl: 'Modified',  val: sr?.files_modified ?? 0, cls: (sr?.files_modified ?? 0) > 0 ? 'warn' : '' },
                      { lbl: 'Removed',   val: sr?.files_removed  ?? 0 },
                      { lbl: 'Validated', val: scr?.files_validated ?? 0 },
                      { lbl: 'Corrupted', val: scr?.files_corrupted?.length ?? 0, cls: (scr?.files_corrupted?.length ?? 0) > 0 ? 'red' : '' },
                    ].map(item => (
                      <div key={item.lbl} className={`stat-item ${item.cls ?? ''}`}>
                        <div className="stat-val">{formatNumber(item.val)}</div>
                        <div className="stat-lbl">{item.lbl}</div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div style={{ fontSize: '0.8rem', color: 'var(--muted)', marginTop: '0.4rem' }}>
                    No run data yet — click Sync or Scrub to start.
                  </div>
                )}
              </div>
            )
          })}

          {/* Failures panel */}
          {runDrives.some(d => d.err) && (
            <div style={{ marginTop: '1rem' }}>
              <div className="section-title">Failures</div>
              {runDrives.filter(d => d.err).map(d => (
                <div key={d.path} className="alert err">
                  <strong>{d.drive || d.path}</strong>: {d.err}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
