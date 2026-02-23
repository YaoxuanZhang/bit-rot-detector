import { useEffect, useRef, useCallback } from 'react'
import type { ProgressEvent } from './api'

type SSEHandler = (event: ProgressEvent) => void
type DoneHandler = () => void

const MIN_BACKOFF_MS = 1_000
const MAX_BACKOFF_MS = 30_000

export function useSSE(onEvent: SSEHandler, onDone?: DoneHandler) {
  const esRef = useRef<EventSource | null>(null)
  const onEventRef = useRef(onEvent)
  const onDoneRef = useRef(onDone)
  const backoffRef = useRef(MIN_BACKOFF_MS)

  useEffect(() => {
    onEventRef.current = onEvent
  }, [onEvent])

  useEffect(() => {
    onDoneRef.current = onDone
  }, [onDone])

  const connect = useCallback(() => {
    if (esRef.current) {
      esRef.current.close()
    }
    const es = new EventSource('/api/progress')
    esRef.current = es

    es.onmessage = (e: MessageEvent) => {
      backoffRef.current = MIN_BACKOFF_MS // reset on successful message
      try {
        const data = JSON.parse(e.data) as ProgressEvent
        onEventRef.current(data)
        // Backend signals completion with phase = "done"
        if (data.phase === 'done') {
          onDoneRef.current?.()
        }
      } catch {
        // ignore parse errors
      }
    }

    es.onerror = () => {
      es.close()
      esRef.current = null
      const delay = backoffRef.current
      backoffRef.current = Math.min(delay * 2, MAX_BACKOFF_MS)
      setTimeout(connect, delay)
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      esRef.current?.close()
    }
  }, [connect])
}
