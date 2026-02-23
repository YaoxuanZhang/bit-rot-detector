// API types and fetch helpers

export interface DriveStatus {
  drive: string
  path: string
  health: string
  sync_result?: SyncScrubResult
  scrub_result?: SyncScrubResult
  err?: string
}

export interface SyncScrubResult {
  scanned: number
  added: number
  modified: number
  removed: number
  validated: number
  corrupted: number
  duration_ms?: number
  error?: string
}

export interface StatusResponse {
  last_run: string
  duration: string
  running: boolean
  drives: DriveStatus[]
}

export interface DriveEntry {
  index: number
  path: string
  name: string
}

export interface DrivesResponse {
  paths: string[]
  drives: DriveEntry[]
}

export interface ProgressEvent {
  type: string
  path: string
  phase: string
  total: number
  done: number
  pct: number
}

export interface RunRecord {
  id: string
  started_at: string
  finished_at: string
  duration_ms: number
  drive: string
  path: string
  scanned: number
  added: number
  modified: number
  removed: number
  validated: number
  corrupted: number
  error?: string
}

export interface CorruptionEntry {
  id: string
  drive: string
  path: string
  detected_at: string
  hash_expected: string
  hash_actual: string
  run_id: string
}

export interface DiskThreshold {
  warn_pct: number
  error_pct: number
}

export interface NotificationRule {
  on_corruption: boolean
  on_error: boolean
  on_warn: boolean
  on_completion: boolean
}

export interface SettingsResponse {
  disk_thresholds: DiskThreshold
  notification_rules: NotificationRule
}

export interface ScheduleEntry {
  id: string
  label: string
  cron_expr: string
  enabled: boolean
}

export interface CompareResponse {
  a: RunRecord
  b: RunRecord
  delta: {
    scanned: number
    added: number
    modified: number
    removed: number
    validated: number
    corrupted: number
  }
}

export interface ConfigResponse {
  [key: string]: unknown
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(path, options)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`HTTP ${res.status}: ${text}`)
  }
  return res.json() as Promise<T>
}

export const api = {
  getStatus: () => request<StatusResponse>('/api/status'),

  getDrives: () => request<DrivesResponse>('/api/drives'),

  getHistory: (limit = 50) =>
    request<RunRecord[]>(`/api/history?limit=${limit}`),

  getCorruption: () => request<CorruptionEntry[]>('/api/corruption'),

  getSettings: () => request<SettingsResponse>('/api/settings'),

  postSettings: (body: SettingsResponse) =>
    request<SettingsResponse>('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),

  getSchedule: () => request<ScheduleEntry[]>('/api/schedule'),

  postSchedule: (entry: ScheduleEntry) =>
    request<ScheduleEntry>('/api/schedule', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(entry),
    }),

  getConfig: () => request<ConfigResponse>('/api/config'),

  postConfig: (body: Partial<ConfigResponse>) =>
    request<ConfigResponse>('/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),

  postSync: () =>
    fetch('/api/sync', { method: 'POST' }),

  postScrub: () =>
    fetch('/api/scrub', { method: 'POST' }),

  postDriveSync: (idx: number) =>
    fetch(`/api/drives/${idx}/sync`, { method: 'POST' }),

  postDriveScrub: (idx: number) =>
    fetch(`/api/drives/${idx}/scrub`, { method: 'POST' }),

  postTestEmail: () =>
    fetch('/api/test-email', { method: 'POST' }),

  compare: (a: string, b: string) =>
    request<CompareResponse>(`/api/compare?a=${a}&b=${b}`),

  exportUrl: (format: 'json' | 'csv') => `/api/export?format=${format}`,
}
