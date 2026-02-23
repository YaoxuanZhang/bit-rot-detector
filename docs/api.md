# REST API Reference

All endpoints are served by the HTTP server started with `-web`. The base URL
depends on the `-addr` flag (default `http://localhost:8080`).

All request and response bodies are JSON unless noted otherwise.
Timestamps are RFC 3339 strings (`"2024-01-15T02:00:00Z"`).

---

## Table of Contents

- [Status](#status)
- [Drives](#drives)
- [Progress (SSE)](#progress-sse)
- [History](#history)
- [Export](#export)
- [Compare](#compare)
- [Corruption](#corruption)
- [Settings](#settings)
- [Schedule](#schedule)
- [Retries](#retries)
- [Sync / Scrub (all drives)](#sync--scrub-all-drives)
- [Sync / Scrub (per drive)](#sync--scrub-per-drive)
- [Test Email](#test-email)
- [Error responses](#error-responses)

---

## Status

### `GET /api/status`

Returns the most recent aggregated run result and whether an operation is running.

**Response `200 OK`**

```json
{
  "last_run": "2024-01-15T02:05:31Z",
  "duration": "1m23.4s",
  "running": false,
  "drives": [
    {
      "drive": "nas",
      "path": "/mnt/nas",
      "health": {
        "drive_name": "nas",
        "total_space": 4000000000000,
        "used_space": 1800000000000,
        "free_space": 2200000000000,
        "temperature": 38,
        "smart_status": "PASSED",
        "smart_errors": [],
        "is_rotational": true
      },
      "sync_result": {
        "files_scanned": 48231,
        "files_added": 12,
        "files_modified": 3,
        "files_moved": 0,
        "files_removed": 1,
        "errors": []
      },
      "scrub_result": {
        "files_validated": 483,
        "files_corrupted": [],
        "errors": []
      }
    }
  ]
}
```

**Fields**

| Field | Type | Description |
|---|---|---|
| `last_run` | RFC 3339 | Time the most recent operation completed; zero value = never run |
| `duration` | string | Wall-clock duration of the last operation (Go `Duration.String()` format) |
| `running` | bool | `true` while an operation is in progress |
| `drives[].drive` | string | Human-readable label derived from the directory base name |
| `drives[].path` | string | Absolute path of the monitored directory |
| `drives[].health` | object | Disk-usage and SMART statistics; absent when not collected |
| `drives[].sync_result` | object | Sync statistics; absent when no sync was run |
| `drives[].scrub_result` | object | Scrub statistics; absent when no scrub was run |
| `drives[].err` | string | Non-empty when a fatal error occurred for this drive |

---

## Drives

### `GET /api/drives`

Returns the configured paths and the most recent drive-health data.

**Response `200 OK`**

```json
{
  "paths": ["/mnt/nas", "/mnt/backup"],
  "drives": [ /* same DriveStatus objects as /api/status */ ]
}
```

---

## Progress (SSE)

### `GET /api/progress`

Server-Sent Events stream. Each event is:

```
data: {"phase":"walk","drive":"nas","count":5000,"total":0}\n\n
data: {"phase":"hash","drive":"nas","count":250,"total":48231}\n\n
data: {"phase":"scrub","drive":"nas","count":50,"total":483}\n\n
data: {"phase":"done","drive":"nas","count":483,"total":483,"message":"Complete"}\n\n
```

**Event fields**

| Field | Type | Description |
|---|---|---|
| `phase` | string | One of `walk`, `hash`, `scrub`, `done` |
| `drive` | string | Human-readable drive label |
| `count` | int | Files processed so far in this phase |
| `total` | int | Expected total (0 during walk when total is unknown) |
| `message` | string | Optional human-readable status; present on `done` |

The connection remains open until the client disconnects or the server shuts down.
Write-deadline is cleared on the SSE handler so long scans do not time out.

---

## History

### `GET /api/history`

Returns run history records from each configured drive's database, most-recent first.

**Query parameters**

| Parameter | Default | Description |
|---|---|---|
| `limit` | `50` | Maximum number of records per drive |

**Response `200 OK`** — array of `RunRecord`:

```json
[
  {
    "id": 42,
    "drive_id": "/mnt/nas",
    "drive_name": "nas",
    "started_at": "2024-01-15T02:00:00Z",
    "duration_ms": 83400,
    "files_scanned": 48231,
    "files_added": 12,
    "files_modified": 3,
    "files_removed": 1,
    "files_moved": 0,
    "files_validated": 483,
    "files_corrupted": 0,
    "sync_errors": 0,
    "scrub_errors": 0
  }
]
```

Returns `[]` (empty array) when no history exists.

---

## Export

### `GET /api/export`

Exports run history in the specified format.

**Query parameters**

| Parameter | Default | Values | Description |
|---|---|---|---|
| `format` | `json` | `json`, `csv` | Output format |

**JSON response `200 OK`** — same array as `/api/history` (no limit applied).

**CSV response `200 OK`**
- `Content-Type: text/csv`
- `Content-Disposition: attachment; filename="bitrot-history.csv"`
- Header row: `id,drive_id,drive_name,started_at,duration_ms,files_scanned,files_added,files_modified,files_removed,files_moved,files_validated,files_corrupted,sync_errors,scrub_errors`

---

## Compare

### `GET /api/compare`

Computes the field-by-field delta between two run records.

**Query parameters**

| Parameter | Required | Description |
|---|---|---|
| `a` | yes | `id` of run A (integer) |
| `b` | yes | `id` of run B (integer) |

**Response `200 OK`**

```json
{
  "run_a": { /* RunRecord */ },
  "run_b": { /* RunRecord */ },
  "delta": {
    "files_added": 5,
    "files_modified": -2,
    "files_removed": 0,
    "files_corrupted": 0,
    "duration_ms": 1200
  }
}
```

Positive delta values mean run B had more; negative means run B had less.

**Error responses**

| Status | Condition |
|---|---|
| `400 Bad Request` | `a` or `b` missing or not valid integers |
| `404 Not Found` | One or both run IDs do not exist |

---

## Corruption

### `GET /api/corruption`

Returns all run records where `files_corrupted > 0`, across all configured drives.

**Response `200 OK`**

```json
[
  {
    "drive_id": "/mnt/nas",
    "drive_name": "nas",
    "started_at": "2024-01-10T03:12:00Z",
    "files_corrupted": 2,
    "run_id": 37
  }
]
```

Returns `[]` when no corruption has ever been detected. Up to 100 events per drive
are returned (most recent first).

---

## Settings

### `GET /api/settings`

Returns the current runtime settings.

**Response `200 OK`**

```json
{
  "disk_thresholds": {
    "warn_pct": 75,
    "error_pct": 90
  },
  "notification_rules": {
    "on_success": false,
    "on_warning": true,
    "on_corruption": true,
    "on_error": true
  }
}
```

When no settings store has been injected (bare coordinator mode), defaults are
returned.

---

### `POST /api/settings`

Updates runtime settings. The body must be a complete `Settings` object.

**Request body** — same shape as `GET /api/settings` response.

**Response `200 OK`** — the updated settings.

**Error responses**

| Status | Condition |
|---|---|
| `400 Bad Request` | Invalid JSON body |
| `503 Service Unavailable` | Settings store not configured |

---

## Schedule

### `GET /api/schedule`

Returns the current auto-run schedule entries.

**Response `200 OK`**

```json
[
  {
    "id": "sync",
    "label": "Daily Sync",
    "enabled": true,
    "cron_expr": "@daily",
    "last_run": "2024-01-15T00:00:00Z",
    "next_run": "2024-01-16T00:00:00Z"
  }
]
```

**`cron_expr` values**

| Expression | Meaning |
|---|---|
| `@hourly` | Every hour at minute 0 |
| `@daily` | Every day at 00:00 |
| `@weekly` | Every Sunday at 00:00 |
| `HH:MM` | A specific time each day (24-hour format) |

---

### `POST /api/schedule`

Inserts or replaces a schedule entry (matched by `id`).

**Request body**

```json
{
  "id": "scrub",
  "label": "Weekly Scrub",
  "enabled": true,
  "cron_expr": "@weekly"
}
```

**Response `200 OK`** — the full updated list of schedule entries.

**Error responses**

| Status | Condition |
|---|---|
| `400 Bad Request` | Invalid JSON body |
| `503 Service Unavailable` | Scheduler store not configured |

---

## Retries

### `GET /api/retries`

Returns a snapshot of the in-memory retry queue.

**Response `200 OK`**

```json
[
  {
    "id": "1",
    "path": "/mnt/nas",
    "op": "sync",
    "err": "permission denied",
    "attempts": 2,
    "next_at": "2024-01-15T02:30:00Z",
    "done": false
  }
]
```

Returns `[]` when the queue is empty.

| Field | Description |
|---|---|
| `id` | Sequential identifier |
| `path` | Affected drive path |
| `op` | `sync` or `scrub` |
| `err` | Most recent error message |
| `attempts` | Number of failed attempts so far |
| `next_at` | Earliest time the item will be retried (exponential backoff, max 24h) |
| `done` | `true` when the item was resolved successfully |

---

## Sync / Scrub (all drives)

### `POST /api/sync`

Triggers a sync operation across all configured drives. Non-blocking.

**Response `202 Accepted`**

```json
{ "status": "accepted" }
```

**Response `409 Conflict`** — when an operation is already running:

```json
{ "error": "an operation is already in progress" }
```

---

### `POST /api/scrub`

Same as `POST /api/sync` but triggers a scrub operation.

---

## Sync / Scrub (per drive)

### `POST /api/drives/{idx}/sync`
### `POST /api/drives/{idx}/scrub`

Triggers an operation for a single drive. `{idx}` is the 0-based index into the
`paths` slice as returned by `GET /api/drives`.

**Response `202 Accepted`** on success.

**Error responses**

| Status | Condition |
|---|---|
| `400 Bad Request` | `{idx}` is not a valid integer or is out of range |
| `409 Conflict` | Another operation is already running |

---

## Test Email

### `POST /api/test-email`

Sends a test email via the configured SMTP server to verify connectivity.

**Response `200 OK`**

```json
{ "status": "sent" }
```

**Error responses**

| Status | Condition |
|---|---|
| `500 Internal Server Error` | SMTP error; body contains `{ "error": "..." }` |
| `503 Service Unavailable` | No mailer configured; body contains `{ "error": "no mailer configured" }` |

---

## Error Responses

All error responses follow the same shape:

```json
{ "error": "human-readable description" }
```

| Status | Meaning |
|---|---|
| `400 Bad Request` | Invalid request parameters or body |
| `404 Not Found` | Referenced resource (run ID, etc.) does not exist |
| `409 Conflict` | An operation is already in progress |
| `500 Internal Server Error` | Unexpected server-side error |
| `503 Service Unavailable` | Optional dependency (mailer, settings store) not configured |
