import { useState, useEffect, useCallback, useRef } from 'react'
import { api, type StatusResponse } from './lib/api'
import { useSSE } from './lib/sse'
import RunCard from './cards/RunCard'
import HistoryCard from './cards/HistoryCard'
import SettingsCard from './cards/SettingsCard'

type CardId = 'run' | 'history' | 'settings'

export default function App() {
  const [status, setStatus] = useState<StatusResponse | null>(null)
  const [expanded, setExpanded] = useState<CardId | null>(null)
  const [version] = useState('dev')
  const refreshRef = useRef<() => void>(() => {})

  const loadStatus = useCallback(() => {
    api.getStatus().then(setStatus).catch(console.error)
  }, [])

  useEffect(() => {
    loadStatus()
    const id = setInterval(loadStatus, 30_000)
    return () => clearInterval(id)
  }, [loadStatus])

  const wasRunning = useRef(false)
  useSSE(
    useCallback(() => {}, []),
    useCallback(() => {
      loadStatus()
      refreshRef.current()
    }, [loadStatus]),
  )

  useEffect(() => {
    if (status) {
      if (wasRunning.current && !status.running) {
        refreshRef.current()
      }
      wasRunning.current = status.running
    }
  }, [status])

  const expand = useCallback((id: CardId) => setExpanded(id), [])
  const collapse = useCallback(() => setExpanded(null), [])

  // Collapse on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') collapse()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [collapse])

  const globalHealth = status
    ? status.running
      ? 'running'
      : status.drives.some(d => d.health === 'error' || d.err)
        ? 'err'
        : status.drives.some(d => d.health === 'warn')
          ? 'warn'
          : status.drives.length > 0
            ? 'ok'
            : 'idle'
    : 'idle'

  return (
    <>
      <header className="app-header">
        <span className="app-logo">🔍</span>
        <span className="app-title">Bit Rot Detector</span>
        <span className="badge">{version}</span>
        <span className={`status-dot ${globalHealth}`} title={globalHealth} />
      </header>

      {expanded && (
        <div className="card-overlay" onClick={collapse} />
      )}

      <div className={`card-grid${expanded ? ' has-expanded' : ''}`}>
        <RunCard
          status={status}
          expanded={expanded === 'run'}
          onExpand={() => expand('run')}
          onCollapse={collapse}
          onRefresh={loadStatus}
          registerRefresh={(fn) => { refreshRef.current = fn }}
        />
        <HistoryCard
          expanded={expanded === 'history'}
          onExpand={() => expand('history')}
          onCollapse={collapse}
        />
        <SettingsCard
          expanded={expanded === 'settings'}
          onExpand={() => expand('settings')}
          onCollapse={collapse}
        />
      </div>
    </>
  )
}
