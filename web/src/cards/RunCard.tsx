import { useEffect, useState, useCallback, useRef } from 'react'
import { api, type StatusResponse, type ProgressEvent as ProgEvt } from '../lib/api'
import { useSSE } from '../lib/sse'
import { formatDate, formatRelative, formatDuration, formatNumber } from '../lib/format'

interface Props {
  status: StatusResponse | null
  expanded: boolean
  onExpand: () => void
  onCollapse: () => void
  onRefresh: () => void
  registerRefresh: (fn: () => void) => void
}

interface Progress {
  phase: string
  path: string
  pct: number
  done: number
  total: number
}

export default function RunCard({ status, expanded, onExpand, onCollapse, onRefresh, registerRefresh }: Props) {
  const [progMap, setProgMap] = useState<Record<string, Progress>>({})
  const [busy, setBusy] = useState(false)

  const clearProg = useCallback(() => setProgMap({}), [])

  useEffect(() => {
    registerRefresh(clearProg)
  }, [registerRefresh, clearProg])

  const handleSSEEvent = useCallback((e: ProgEvt) => {
    if (e.type === 'progress') {
      setProgMap(m => ({ ...m, [e.path]: { phase: e.phase, path: e.path, pct: e.pct, done: e.done, total: e.total } }))
    } else if (e.type === 'done') {
      setProgMap({})
      onRefresh()
    }
  }, [onRefresh])

  useSSE(handleSSEEvent)

  const globalHealth = status
    ? status.running
      ? 'running'
      : status.drives.some(d => d.health === 'error' || d.err)
        ? 'err'
        : status.drives.some(d => d.health === 'warn')
          ? 'warn'
          : 'ok'
    : 'idle'

  const totalScanned = status?.drives.reduce((s, d) => s + (d.sync_result?.scanned ?? 0) + (d.scrub_result?.scanned ?? 0), 0) ?? 0

  async function triggerSync() {
    setBusy(true)
    try { await api.postSync() } finally { setBusy(false); onRefresh() }
  }

  async function triggerScrub() {
    setBusy(true)
    try { await api.postScrub() } finally { setBusy(false); onRefresh() }
  }

  const cardRef = useRef<HTMLDivElement>(null)

  function handleClick() {
    if (!expanded) onExpand()
  }

  return (
    <div
      ref={cardRef}
      className={`card${expanded ? ' expanded' : ''}`}
      onClick={handleClick}
      onContextMenu={e => { if (expanded) { e.preventDefault(); onCollapse() } }}
    >
      <div className="card-header">
        <span className="card-title">Run</span>
        <button className="card-close-btn" onClick={e => { e.stopPropagation(); onCollapse() }} title="Collapse (Esc)">✕</button>
      </div>

      {!expanded ? (
        /* ── Collapsed summary ── */
        <div>
          <div className="summary-row">
            <div className="summary-item">
              <span className={`status-dot ${globalHealth}`} />
            </div>
            <div className="summary-item">
              <span className="summary-val">{status?.drives.length ?? 0}</span>
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

          {/* Progress section */}
          {(status?.running || Object.keys(progMap).length > 0) && (
            <div style={{ marginBottom: '1.25rem' }}>
              <div className="section-title">Progress</div>
              {Object.values(progMap).map(p => (
                <div key={p.path} style={{ marginBottom: '0.75rem' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.8rem', marginBottom: '0.3rem' }}>
                    <span>
                      <span className={`phase-badge${p.phase === 'walk' ? ' active' : ''}`} style={{ marginRight: '0.4rem' }}>Walk</span>
                      <span className={`phase-badge${p.phase === 'hash' ? ' active' : ''}`} style={{ marginRight: '0.4rem' }}>Hash</span>
                      <span className={`phase-badge${p.phase === 'scrub' ? ' active' : ''}`}>Scrub</span>
                      <span style={{ marginLeft: '0.6rem', color: 'var(--muted)' }}>{p.path}</span>
                    </span>
                    <span style={{ fontFamily: 'var(--font-mono)' }}>{p.pct.toFixed(1)}%</span>
                  </div>
                  <div className="progress-wrap">
                    <div className="progress-bar" style={{ width: `${p.pct}%` }} />
                  </div>
                </div>
              ))}
              {status?.running && Object.keys(progMap).length === 0 && (
                <div style={{ color: 'var(--muted)', fontSize: '0.85rem' }}>Running…</div>
              )}
            </div>
          )}

          {/* Per-drive rows */}
          <div className="section-title">Drives ({status?.drives.length ?? 0})</div>
          {status?.drives.length === 0 && (
            <div className="empty">No drives configured.</div>
          )}
          {status?.drives.map((drive, idx) => {
            const sr = drive.sync_result
            const scr = drive.scrub_result
            const health = drive.health === 'error' || drive.err ? 'err' : drive.health === 'warn' ? 'warn' : 'ok'
            return (
              <div key={drive.path} className="drive-row">
                <div className="drive-row-header">
                  <span className={`status-dot ${health}`} />
                  <span className="drive-name">{drive.drive || `Drive ${idx + 1}`}</span>
                  <span className="drive-path">{drive.path}</span>
                  <span style={{ flex: 1 }} />
                  <button className="sm" onClick={() => api.postDriveSync(idx).then(onRefresh)}>Sync</button>
                  <button className="sm" style={{ marginLeft: '0.4rem' }} onClick={() => api.postDriveScrub(idx).then(onRefresh)}>Scrub</button>
                </div>
                {drive.err && <div className="alert err">{drive.err}</div>}
                {(sr || scr) && (
                  <div className="stat-grid">
                    {[
                      { lbl: 'Scanned',   val: (sr?.scanned   ?? 0) + (scr?.scanned   ?? 0) },
                      { lbl: 'Added',     val: (sr?.added     ?? 0), cls: (sr?.added ?? 0) > 0 ? 'ok' : '' },
                      { lbl: 'Modified',  val: (sr?.modified  ?? 0), cls: (sr?.modified ?? 0) > 0 ? 'warn' : '' },
                      { lbl: 'Removed',   val: (sr?.removed   ?? 0) },
                      { lbl: 'Validated', val: (scr?.validated ?? 0) + (sr?.validated ?? 0) },
                      { lbl: 'Corrupted', val: (scr?.corrupted ?? 0) + (sr?.corrupted ?? 0), cls: ((scr?.corrupted ?? 0) + (sr?.corrupted ?? 0)) > 0 ? 'red' : '' },
                    ].map(item => (
                      <div key={item.lbl} className={`stat-item ${item.cls ?? ''}`}>
                        <div className="stat-val">{formatNumber(item.val)}</div>
                        <div className="stat-lbl">{item.lbl}</div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )
          })}

          {/* Failures panel */}
          {status?.drives.some(d => d.err) && (
            <div style={{ marginTop: '1rem' }}>
              <div className="section-title">Failures</div>
              {status.drives.filter(d => d.err).map(d => (
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
