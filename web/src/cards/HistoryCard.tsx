import { useEffect, useState, useRef, useCallback } from 'react'
import { api, type RunRecord, type CorruptionEntry, type CompareResponse } from '../lib/api'
import { formatDate, formatDuration, formatNumber } from '../lib/format'
import { Chart, LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler } from 'chart.js'

Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler)

interface Props {
  expanded: boolean
  onExpand: () => void
  onCollapse: () => void
}

type SortKey = 'started_at' | 'drive' | 'duration_ms' | 'scanned' | 'added' | 'modified' | 'removed' | 'validated' | 'corrupted'

export default function HistoryCard({ expanded, onExpand, onCollapse }: Props) {
  const [history, setHistory] = useState<RunRecord[]>([])
  const [corruption, setCorruption] = useState<CorruptionEntry[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
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

  useEffect(() => { load() }, [load])

  // Draw spark chart
  useEffect(() => {
    if (!sparkRef.current || history.length === 0) return
    const ctx = sparkRef.current.getContext('2d')
    if (!ctx) return
    chartRef.current?.destroy()
    const labels = history.slice(-20).map(r => r.started_at.slice(0, 10))
    const data = history.slice(-20).map(r => r.added)
    chartRef.current = new Chart(ctx, {
      type: 'line',
      data: {
        labels,
        datasets: [{
          data,
          borderColor: '#6366f1',
          backgroundColor: 'rgba(99,102,241,0.15)',
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
          y: { display: false },
        },
      },
    })
    return () => { chartRef.current?.destroy(); chartRef.current = null }
  }, [history])

  const lastCorruption = corruption.length > 0
    ? corruption.sort((a, b) => b.detected_at.localeCompare(a.detected_at))[0].detected_at
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

  function toggleSelect(id: string) {
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

  return (
    <div
      className={`card${expanded ? ' expanded' : ''}`}
      onClick={!expanded ? onExpand : undefined}
      onContextMenu={e => { if (expanded) { e.preventDefault(); onCollapse() } }}
    >
      <div className="card-header">
        <span className="card-title">History</span>
        <button className="card-close-btn" onClick={e => { e.stopPropagation(); onCollapse() }} title="Collapse (Esc)">✕</button>
      </div>

      {!expanded ? (
        /* ── Collapsed summary ── */
        <div>
          <div className="summary-row">
            <div className="summary-item">
              <span className="summary-val">{history.length}</span>
              <span className="summary-lbl">Runs</span>
            </div>
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
          {error && <div className="alert err">{error}</div>}

          <div className="toolbar">
            <button onClick={load} disabled={loading}>↺ Refresh</button>
            {selected.size === 2 && (
              <button className="primary" onClick={runCompare}>⇄ Compare</button>
            )}
            {selected.size > 0 && (
              <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
                {selected.size}/2 selected
              </span>
            )}
          </div>

          {/* Run history table */}
          <div className="section-title">Run History ({history.length})</div>
          {loading && <div className="empty">Loading…</div>}
          {!loading && history.length === 0 && <div className="empty">No runs recorded yet.</div>}
          {!loading && history.length > 0 && (
            <div className="tbl-wrap">
              <table>
                <thead>
                  <tr>
                    <th />
                    <SortTh col="drive" label="Drive" />
                    <SortTh col="started_at" label="Started" />
                    <SortTh col="duration_ms" label="Duration" />
                    <SortTh col="scanned" label="Scanned" />
                    <SortTh col="added" label="Added" />
                    <SortTh col="modified" label="Modified" />
                    <SortTh col="removed" label="Removed" />
                    <SortTh col="validated" label="Validated" />
                    <SortTh col="corrupted" label="Corrupted" />
                  </tr>
                </thead>
                <tbody>
                  {sorted.map(r => (
                    <tr key={r.id} className={selected.has(r.id) ? 'selected' : ''} onClick={() => toggleSelect(r.id)}>
                      <td>
                        <input type="checkbox" checked={selected.has(r.id)} onChange={() => toggleSelect(r.id)} onClick={e => e.stopPropagation()} />
                      </td>
                      <td>{r.drive || '—'}</td>
                      <td>{formatDate(r.started_at)}</td>
                      <td>{formatDuration(r.duration_ms)}</td>
                      <td>{formatNumber(r.scanned)}</td>
                      <td className={r.added > 0 ? 'ok' : ''}>{formatNumber(r.added)}</td>
                      <td className={r.modified > 0 ? 'warn' : ''}>{formatNumber(r.modified)}</td>
                      <td>{formatNumber(r.removed)}</td>
                      <td>{formatNumber(r.validated)}</td>
                      <td className={r.corrupted > 0 ? 'red' : ''}>{formatNumber(r.corrupted)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Corruption panel */}
          {corruption.length > 0 && (
            <>
              <div className="section-title" style={{ marginTop: '1.5rem' }}>
                Corruption Events ({corruption.length})
              </div>
              <div className="tbl-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Drive</th>
                      <th>Path</th>
                      <th>Detected</th>
                      <th>Expected Hash</th>
                      <th>Actual Hash</th>
                    </tr>
                  </thead>
                  <tbody>
                    {corruption.map(c => (
                      <tr key={c.id}>
                        <td>{c.drive}</td>
                        <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem' }}>{c.path}</td>
                        <td>{formatDate(c.detected_at)}</td>
                        <td className="red" style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem' }}>{c.hash_expected?.slice(0, 12)}…</td>
                        <td className="red" style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem' }}>{c.hash_actual?.slice(0, 12)}…</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      )}

      {/* Compare modal */}
      {compareResult && (
        <div className="modal-backdrop" onClick={() => setCompareResult(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-header">
              <span className="modal-title">Compare Runs</span>
              <button className="sm" onClick={() => setCompareResult(null)}>✕</button>
            </div>
            <div className="tbl-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Metric</th>
                    <th>Run A</th>
                    <th>Run B</th>
                    <th>Delta</th>
                  </tr>
                </thead>
                <tbody>
                  {(['scanned', 'added', 'modified', 'removed', 'validated', 'corrupted'] as const).map(k => {
                    const delta = compareResult.delta[k]
                    return (
                      <tr key={k}>
                        <td style={{ textTransform: 'capitalize' }}>{k}</td>
                        <td>{formatNumber(compareResult.a[k])}</td>
                        <td>{formatNumber(compareResult.b[k])}</td>
                        <td className={delta > 0 ? (k === 'corrupted' ? 'red' : 'ok') : delta < 0 ? 'warn' : ''}>
                          {delta > 0 ? '+' : ''}{formatNumber(delta)}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
