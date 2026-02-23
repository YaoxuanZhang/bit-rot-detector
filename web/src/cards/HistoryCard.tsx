import { useEffect, useState, useRef, useCallback } from 'react'
import { api, type RunRecord, type CorruptionEvent, type CompareResponse } from '../lib/api'
import { formatDate, formatDuration, formatNumber } from '../lib/format'
import { Chart, LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler } from 'chart.js'

Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler)

interface Props {
  expanded: boolean
  collapsing?: boolean
  onExpand: () => void
  onCollapse: () => void
  registerRefresh?: (fn: () => void) => void
}

type SortKey = 'started_at' | 'drive_name' | 'duration_ms' | 'files_scanned' | 'files_added' | 'files_modified' | 'files_removed' | 'files_validated' | 'files_corrupted'

export default function HistoryCard({ expanded, collapsing, onExpand, onCollapse, registerRefresh }: Props) {
  const [history, setHistory] = useState<RunRecord[]>([])
  const [corruption, setCorruption] = useState<CorruptionEvent[]>([])
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [compareResult, setCompareResult] = useState<CompareResponse | null>(null)
  const [sortKey, setSortKey] = useState<SortKey>('started_at')
  const [sortAsc, setSortAsc] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const sparkRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<Chart | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    Promise.all([api.getHistory(), api.getCorruption()])
      .then(([h, c]) => { setHistory(h); setCorruption(c) })
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  // Initial load
  useEffect(() => { load() }, [load])

  // Register with parent so runs trigger a refresh
  useEffect(() => {
    registerRefresh?.(load)
  }, [registerRefresh, load])

  // Reload whenever the card is expanded so the user always sees fresh data
  const wasExpanded = useRef(false)
  useEffect(() => {
    if (expanded && !wasExpanded.current) {
      load()
    }
    wasExpanded.current = expanded
  }, [expanded, load])

  // Draw spark chart (collapsed view — files_corrupted trend)
  useEffect(() => {
    if (!sparkRef.current || history.length === 0) return
    const ctx = sparkRef.current.getContext('2d')
    if (!ctx) return
    chartRef.current?.destroy()
    const slice = history.slice(-20)
    const labels = slice.map(r => r.started_at.slice(0, 10))
    const data = slice.map(r => r.files_corrupted)
    const hasCorruption = data.some(v => v > 0)
    chartRef.current = new Chart(ctx, {
      type: 'line',
      data: {
        labels,
        datasets: [{
          data,
          borderColor: hasCorruption ? '#ef4444' : '#6366f1',
          backgroundColor: hasCorruption ? 'rgba(239,68,68,0.15)' : 'rgba(99,102,241,0.15)',
          fill: true,
          tension: 0.4,
          pointRadius: 2,
        }],
      },
      options: {
        animation: false,
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { display: false }, tooltip: { enabled: false } },
        scales: {
          x: { display: false },
          y: { display: false, beginAtZero: true },
        },
      },
    })
    return () => { chartRef.current?.destroy(); chartRef.current = null }
  }, [history])

  const lastCorruption = corruption.length > 0
    ? corruption.slice().sort((a, b) => b.started_at.localeCompare(a.started_at))[0].started_at
    : null

  function toggleSort(key: SortKey) {
    if (sortKey === key) setSortAsc(a => !a)
    else { setSortKey(key); setSortAsc(false) }
  }

  const sorted = [...history].sort((a, b) => {
    const av = a[sortKey], bv = b[sortKey]
    const cmp = typeof av === 'number' ? (av as number) - (bv as number)
      : String(av).localeCompare(String(bv))
    return sortAsc ? cmp : -cmp
  })

  function toggleSelect(id: number) {
    setSelected(s => {
      const next = new Set(s)
      if (next.has(id)) next.delete(id)
      else if (next.size < 2) next.add(id)
      else { next.clear(); next.add(id) }
      return next
    })
  }

  async function runCompare() {
    const [a, b] = [...selected]
    try {
      const res = await api.compare(a, b)
      setCompareResult(res)
    } catch (e) { setError(String(e)) }
  }

  function SortTh({ col, label }: { col: SortKey; label: string }) {
    return (
      <th onClick={() => toggleSort(col)}>
        {label}{sortKey === col ? (sortAsc ? ' ↑' : ' ↓') : ''}
      </th>
    )
  }

  const cardClass = ['card', expanded ? 'expanded' : '', collapsing ? 'collapsing' : ''].filter(Boolean).join(' ')

  return (
    <div
      className={cardClass}
      onClick={!expanded && !collapsing ? onExpand : undefined}
    >
      <div className="card-header">
        <span className="card-title">History</span>
        {expanded && (
          <button className="card-close-btn" onClick={e => { e.stopPropagation(); onCollapse() }} title="Collapse (Esc)">✕</button>
        )}
      </div>

      {!expanded ? (
        /* ── Collapsed summary ── */
        <div>
          <div className="summary-row">
            <div className="summary-item">
              <span className="summary-val">{history.length}</span>
              <span className="summary-lbl">Runs</span>
            </div>
            {corruption.length > 0 && (
              <div className="summary-item">
                <span className="summary-val" style={{ color: 'var(--err)' }}>{corruption.length}</span>
                <span className="summary-lbl">Corruptions</span>
              </div>
            )}
          </div>
          <div className="spark-wrap">
            <canvas ref={sparkRef} style={{ width: '100%', height: '40px' }} />
          </div>
          <div style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
            Last corruption: {lastCorruption ? formatDate(lastCorruption) : 'never'}
          </div>
        </div>
      ) : (
        /* ── Expanded view ── */
        <div onClick={e => e.stopPropagation()}>
          {error && <div className="alert err">{error}<button className="sm" style={{ marginLeft: '0.5rem' }} onClick={() => setError('')}>✕</button></div>}

          <div className="toolbar">
            <button onClick={load} disabled={loading}>↺ Refresh</button>
            {selected.size === 2 && (
              <button className="primary" onClick={runCompare}>⇄ Compare selected runs</button>
            )}
            {selected.size > 0 && (
              <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
                {selected.size}/2 selected for compare
              </span>
            )}
            <span style={{ flex: 1 }} />
            <a href={api.exportUrl('csv')} download="bitrot-history.csv">
              <button className="sm">↓ CSV</button>
            </a>
            <a href={api.exportUrl('json')} download="bitrot-history.json">
              <button className="sm">↓ JSON</button>
            </a>
          </div>

          {/* Corruption events summary (shown prominently if any) */}
          {corruption.length > 0 && (
            <>
              <div className="section-title" style={{ color: 'var(--err)' }}>
                ⚠ Corruption Events ({corruption.length})
              </div>
              <div className="tbl-wrap" style={{ marginBottom: '1.5rem' }}>
                <table>
                  <thead>
                    <tr>
                      <th>Drive</th>
                      <th>Detected</th>
                      <th>Run #</th>
                      <th>Corrupted Files</th>
                    </tr>
                  </thead>
                  <tbody>
                    {corruption.slice().sort((a, b) => b.started_at.localeCompare(a.started_at)).map((c, i) => (
                      <tr key={i}>
                        <td>{c.drive_name || c.drive_id || '—'}</td>
                        <td>{formatDate(c.started_at)}</td>
                        <td style={{ fontFamily: 'var(--font-mono)' }}>#{c.run_id}</td>
                        <td className="red">{formatNumber(c.files_corrupted)} file{c.files_corrupted !== 1 ? 's' : ''}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}

          {/* Run history table */}
          <div className="section-title">Run History ({history.length})</div>
          {loading && <div className="empty">Loading…</div>}
          {!loading && history.length === 0 && <div className="empty">No runs recorded yet. Run a sync or scrub to see history here.</div>}
          {!loading && history.length > 0 && (
            <div className="tbl-wrap">
              <table>
                <thead>
                  <tr>
                    <th style={{ width: '2rem' }} />
                    <SortTh col="drive_name" label="Drive" />
                    <SortTh col="started_at" label="Started" />
                    <SortTh col="duration_ms" label="Duration" />
                    <SortTh col="files_scanned" label="Scanned" />
                    <SortTh col="files_added" label="Added" />
                    <SortTh col="files_modified" label="Modified" />
                    <SortTh col="files_removed" label="Removed" />
                    <SortTh col="files_validated" label="Validated" />
                    <SortTh col="files_corrupted" label="Corrupted" />
                  </tr>
                </thead>
                <tbody>
                  {sorted.map(r => (
                    <tr key={r.id} className={selected.has(r.id) ? 'selected' : ''} onClick={() => toggleSelect(r.id)} style={{ cursor: 'pointer' }}>
                      <td>
                        <input type="checkbox" checked={selected.has(r.id)} onChange={() => toggleSelect(r.id)} onClick={e => e.stopPropagation()} />
                      </td>
                      <td>{r.drive_name || '—'}</td>
                      <td style={{ whiteSpace: 'nowrap' }}>{formatDate(r.started_at)}</td>
                      <td style={{ fontFamily: 'var(--font-mono)' }}>{formatDuration(r.duration_ms)}</td>
                      <td>{formatNumber(r.files_scanned)}</td>
                      <td className={r.files_added > 0 ? 'ok' : ''}>{formatNumber(r.files_added)}</td>
                      <td className={r.files_modified > 0 ? 'warn' : ''}>{formatNumber(r.files_modified)}</td>
                      <td>{formatNumber(r.files_removed)}</td>
                      <td>{formatNumber(r.files_validated)}</td>
                      <td className={r.files_corrupted > 0 ? 'red' : ''}>{formatNumber(r.files_corrupted)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Compare modal */}
      {compareResult && (
        <div className="modal-backdrop" onClick={() => setCompareResult(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-header">
              <span className="modal-title">
                Compare Run #{compareResult.run_a.id} vs #{compareResult.run_b.id}
              </span>
              <button className="sm" onClick={() => setCompareResult(null)}>✕</button>
            </div>
            <div style={{ fontSize: '0.8rem', color: 'var(--muted)', marginBottom: '1rem' }}>
              {compareResult.run_a.drive_name} · {formatDate(compareResult.run_a.started_at)}
              {' → '}
              {compareResult.run_b.drive_name} · {formatDate(compareResult.run_b.started_at)}
            </div>
            <div className="tbl-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Metric</th>
                    <th>Run A (#{compareResult.run_a.id})</th>
                    <th>Run B (#{compareResult.run_b.id})</th>
                    <th>Δ Change</th>
                  </tr>
                </thead>
                <tbody>
                  {(['files_added', 'files_modified', 'files_removed', 'files_corrupted'] as const).map(k => {
                    const label = k.replace('files_', '').replace('_', ' ')
                    const delta = compareResult.delta[k] ?? 0
                    const aVal = (compareResult.run_a as unknown as Record<string, number>)[k] ?? 0
                    const bVal = (compareResult.run_b as unknown as Record<string, number>)[k] ?? 0
                    const isCorruption = k === 'files_corrupted'
                    return (
                      <tr key={k}>
                        <td style={{ textTransform: 'capitalize' }}>{label}</td>
                        <td>{formatNumber(aVal)}</td>
                        <td>{formatNumber(bVal)}</td>
                        <td className={delta > 0 ? (isCorruption ? 'red' : 'ok') : delta < 0 ? 'warn' : ''}>
                          {delta > 0 ? '+' : ''}{formatNumber(delta)}
                        </td>
                      </tr>
                    )
                  })}
                  <tr>
                    <td>Duration</td>
                    <td style={{ fontFamily: 'var(--font-mono)' }}>{formatDuration(compareResult.run_a.duration_ms)}</td>
                    <td style={{ fontFamily: 'var(--font-mono)' }}>{formatDuration(compareResult.run_b.duration_ms)}</td>
                    <td style={{ fontFamily: 'var(--font-mono)', color: compareResult.delta.duration_ms > 0 ? 'var(--warn)' : 'var(--ok)' }}>
                      {compareResult.delta.duration_ms > 0 ? '+' : ''}{formatDuration(Math.abs(compareResult.delta.duration_ms))}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
