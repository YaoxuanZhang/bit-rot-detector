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
  const [collapsing, setCollapsing] = useState<CardId | null>(null)
  const [version] = useState('dev')
  const runRefreshRef = useRef<() => void>(() => {})
  const historyRefreshRef = useRef<() => void>(() => {})

  const loadStatus = useCallback(() => {
    api.getStatus().then(setStatus).catch(console.error)
  }, [])

  useEffect(() => {
    loadStatus()
    const id = setInterval(loadStatus, 30_000)
    return () => clearInterval(id)
  }, [loadStatus])

  // Called when any SSE 'done' event arrives — refresh everything
  const onRunDone = useCallback(() => {
    loadStatus()
    runRefreshRef.current()
    historyRefreshRef.current()
  }, [loadStatus])

  useSSE(useCallback(() => {}, []), onRunDone)

  // Also detect running→idle transition via status poll
  const wasRunning = useRef(false)
  useEffect(() => {
    if (status) {
      if (wasRunning.current && !status.running) {
        runRefreshRef.current()
        historyRefreshRef.current()
      }
      wasRunning.current = status.running
    }
  }, [status])

  // Collapse with animation: add .collapsing class, wait for animation, then clear expanded
  const expand = useCallback((id: CardId) => {
    setCollapsing(null)
    setExpanded(id)
  }, [])

  const collapse = useCallback(() => {
    setCollapsing(expanded)
    // After animation (~250ms) clear both
    setTimeout(() => {
      setExpanded(null)
      setCollapsing(null)
    }, 260)
  }, [expanded])

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
      : status.drives.some(d => d.err)
        ? 'err'
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

      <div className="card-grid">
        <RunCard
          status={status}
          expanded={expanded === 'run'}
          collapsing={collapsing === 'run'}
          onExpand={() => expand('run')}
          onCollapse={collapse}
          onRefresh={loadStatus}
          registerRefresh={(fn) => { runRefreshRef.current = fn }}
        />
        <HistoryCard
          expanded={expanded === 'history'}
          collapsing={collapsing === 'history'}
          onExpand={() => expand('history')}
          onCollapse={collapse}
          registerRefresh={(fn) => { historyRefreshRef.current = fn }}
        />
        <SettingsCard
          expanded={expanded === 'settings'}
          collapsing={collapsing === 'settings'}
          onExpand={() => expand('settings')}
          onCollapse={collapse}
        />
      </div>
    </>
  )
}
