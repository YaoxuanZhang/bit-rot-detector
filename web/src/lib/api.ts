// API types and fetch helpers
// All types must match backend JSON field names exactly.

// ── Drive health (domain.DriveHealth) ──────────────────────────────────────
export interface DriveHealth {
  drive_name: string
  total_space: number
  used_space: number
  free_space: number
  temperature?: number
  smart_status?: string
  smart_errors?: string[]
  is_rotational: boolean
}

// ── Scan result types (domain.SyncResult / domain.ScrubResult) ────────────
export interface SyncResult {
  files_scanned: number
  files_added: number
  files_modified: number
  files_moved: number
  files_removed: number
  errors?: string[]
}

export interface ScrubResult {
  files_validated: number
  files_corrupted?: string[]   // array of file paths
  errors?: string[]
}

// ── Per-drive status (api.DriveStatus) ────────────────────────────────────
export interface DriveStatus {
  drive: string
  path: string
  health?: DriveHealth            // object, absent until first run
  sync_result?: SyncResult
  scrub_result?: ScrubResult
  err?: string
}

export interface StatusResponse {
  last_run: string
  duration: string
  running: boolean
  drives: DriveStatus[]
}

export interface DrivesResponse {
  paths: string[]               // always populated from config on startup
  drives: DriveStatus[]         // populated after first run
}

// ── SSE progress (domain.ProgressEvent) ───────────────────────────────────
// phase: "walk" | "hash" | "scrub" | "done"
// count: files processed so far in this phase
// total: expected total (0 = unknown, e.g. during walk)
export interface ProgressEvent {
  phase: string
  drive: string
  count: number
  total: number
  message?: string
}

// ── Run history (domain.RunRecord) ────────────────────────────────────────
export interface RunRecord {
  id: number                  // int64
  drive_id: string
  drive_name: string
  started_at: string
  duration_ms: number
  files_scanned: number
  files_added: number
  files_modified: number
  files_removed: number
  files_moved: number
  files_validated: number
  files_corrupted: number     // count (not path list)
  sync_errors: number
  scrub_errors: number
}

// ── Corruption events (api.CorruptionEvent) ────────────────────────────────
// Each entry is a run summary where files_corrupted > 0.
export interface CorruptionEvent {
  drive_id: string
  drive_name: string
  started_at: string
  files_corrupted: number
  run_id: number
}

// ── Settings / schedule ───────────────────────────────────────────────────
export interface DiskThreshold {
  warn_pct: number
  error_pct: number
}

export interface NotificationRule {
  on_corruption: boolean
  on_error: boolean
  on_warning: boolean
  on_success: boolean
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

// ── Compare (api.handleCompare) ────────────────────────────────────────────
export interface CompareResponse {
  run_a: RunRecord
  run_b: RunRecord
  delta: {
    files_added: number
    files_modified: number
    files_removed: number
    files_corrupted: number
    duration_ms: number
  }
}

// ── Config (config.Config) ─────────────────────────────────────────────────
export interface SmtpConfig {
  host: string
  port: number
  sender: string
  recipient: string
  // username / password are secrets; never sent by backend
}

export interface ConfigResponse {
  target_paths?: string[]
  smtp?: SmtpConfig
  scrub_percentage?: number
  scrub_frequency?: string
  max_workers?: number
  log_level?: string
  log_retention_days?: number
  disk_thresholds?: DiskThreshold
  notification_rules?: NotificationRule
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

  getCorruption: () => request<CorruptionEvent[]>('/api/corruption'),

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

  compare: (a: number, b: number) =>
    request<CompareResponse>(`/api/compare?a=${a}&b=${b}`),

  exportUrl: (format: 'json' | 'csv') => `/api/export?format=${format}`,
}
