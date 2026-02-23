# Web UI Reference

The web dashboard is a single-page application served at the root path (`/`) when
the server is started with `-web`. It requires no build step — all JavaScript and
CSS are embedded in the binary.

---

## Layout

The page is a single scrolling column. Sections appear in this order:

```
┌──────────────────────────────────────────┐
│ Header (logo, title, version badge)       │
├──────────────────────────────────────────┤
│ Toolbar                                   │
├──────────────────────────────────────────┤
│ Status bar                                │
├──────────────────────────────────────────┤
│ Live progress card (hidden when idle)     │
├──────────────────────────────────────────┤
│ Last failures panel (hidden when clean)  │
├──────────────────────────────────────────┤
│ Drive cards grid                          │
├──────────────────────────────────────────┤
│ Charts section                            │
├──────────────────────────────────────────┤
│ Run history table                         │
├──────────────────────────────────────────┤
│ Comparison modal (overlay, on demand)     │
├──────────────────────────────────────────┤
│ Corruption drill-down panel              │
├──────────────────────────────────────────┤
│ Settings card                             │
├──────────────────────────────────────────┤
│ Schedule card                             │
└──────────────────────────────────────────┘
```

---

## Sections

### Header

Displays the shield icon, the application name "Bit Rot Detector", and a `v1` version
badge. No interactive controls.

---

### Toolbar

| Button | Action |
|---|---|
| **⟳ Sync All** | `POST /api/sync` — starts a sync for all configured drives |
| **🔍 Scrub All** | `POST /api/scrub` — starts a scrub for all configured drives |
| **↺ Refresh** | `GET /api/status` — re-fetches and re-renders status |
| **Export CSV** | `GET /api/export?format=csv` — downloads history as a CSV file |
| **Export JSON** | `GET /api/export?format=json` — downloads history as a JSON file |

All buttons are disabled while an operation is running. They re-enable when the
operation completes (detected via SSE `done` event or the status-poll loop).

---

### Status Bar

Four informational items displayed horizontally:

| Item | Source field | Description |
|---|---|---|
| **State** | `running`, `drives[].err`, `drives[].scrub_result` | Dot + label (see [Status States](#status-states)) |
| **Last Run** | `last_run` | Formatted local timestamp; "Never" before first run |
| **Duration** | `duration` | Last operation wall-clock time |
| **Drives** | `drives.length` | Count of configured drives |

#### Status States

| Dot colour | Label | Condition |
|---|---|---|
| Grey (idle) | `Idle` | Server never ran an operation |
| Indigo (pulsing) | `Running` | `running === true` |
| Red | `Error` | Any drive has a non-empty `err` field |
| Red | `Bit Rot Detected` | Any drive's `scrub_result.files_corrupted` is non-empty |
| Green | `OK` | None of the above; at least one completed run |

"Bit Rot Detected" takes precedence over "Error" — both dot colour and label are red
to ensure the critical state is unmissable.

When an operation completes, a toast notification appears:
- `⚠ Bit rot detected!` (error style) — when corruption was found
- `Operation completed with errors` (error style) — when `err` fields are present
- `Operation completed successfully` (success style) — clean run

---

### Live Progress Card

Hidden when no operation is in progress. Appears immediately after a sync or scrub
is triggered via the toolbar or per-drive buttons.

| Element | Description |
|---|---|
| Spinner + title | "Scanning in progress" |
| Progress label | e.g., `Hash: 12,500 / 48,231 files · nas` |
| Fill bar | Animated fill: Walk = 0–33%, Hash = 33–66%, Scrub = 66–99%, Done = 100% |
| Phase badges | `Walk`, `Hash`, `Scrub` — styled as `active` (indigo) or `done` (green) |

The card is hidden again when the `done` SSE event is received.

---

### Last Failures Panel

Hidden when all drives are clean. Appears when any of the following are non-empty:

- `drives[].err` (fatal drive error)
- `drives[].sync_result.errors` (non-fatal sync errors)
- `drives[].scrub_result.errors` (non-fatal scrub errors)

Displays a compact list of error strings with the associated drive label. Intended as
a quick summary; no action buttons.

---

### Drive Cards

One card per configured drive, displayed in a responsive grid (min 320px per card).

Each card contains:

| Element | Description |
|---|---|
| Drive name (h2) | Base name of the monitored path |
| Path | Full absolute path (monospace, wraps) |
| Drive type badge | `💽 HDD` or `⚡ SSD` based on `health.is_rotational` |
| SMART badge | `PASSED` (green) or `FAILED`/`WARNING` (red) from `health.smart_status` |
| Stat grid (2×3) | Scanned / Added / Modified / Removed / Validated / Corrupted |
| Disk-usage bar | Used / Total with percentage; warn/error thresholds from Settings |
| Corrupted files | Expandable `<details>` list of corrupted file paths (shown only when non-empty) |
| Error box | Red box showing fatal error text (shown only when non-empty) |
| Per-drive buttons | **Sync** (`POST /api/drives/{idx}/sync`) and **Scrub** (`POST /api/drives/{idx}/scrub`) |

Disk-usage bar colours:
- Default (indigo): usage below the warning threshold
- `warn` (amber): usage ≥ `settings.disk_thresholds.warn_pct`
- `err` (red): usage ≥ `settings.disk_thresholds.error_pct`

Thresholds are read from the live settings state and applied dynamically whenever
settings are saved.

---

### Charts Section

Powered by [Chart.js](https://www.chartjs.org/) loaded from CDN. Charts are silently
skipped if Chart.js fails to load (e.g., offline).

Data source: `GET /api/history?limit=30`, sorted ascending by `started_at`.

#### Combined Files + Corruption Chart

Type: stacked line graph.

| Series | Colour | Field |
|---|---|---|
| Added | Green | `files_added` |
| Modified | Amber | `files_modified` |
| Removed | Red | `files_removed` |
| Corrupted | Dark red | `files_corrupted` |

X-axis labels are the `started_at` date formatted as a local date string.

#### Run Duration Chart

Type: bar chart with two series.

| Series | Colour | Logic |
|---|---|---|
| Sync duration | Indigo | Runs where `files_scanned > 0` |
| Scrub duration | Red | Runs where `files_validated > 0` |

A run where both `files_scanned > 0` and `files_validated > 0` (sync+scrub combined)
appears in both series.

**Show/hide toggles:** Two checkboxes above the chart (`Show Sync`, `Show Scrub`)
independently show or hide each series. State is persisted in `localStorage` under
the keys `showSyncDuration` and `showScrubDuration`.

---

### Run History Table

Columns: Drive, Started, Duration, Scanned, Added, Modified, Removed, Validated,
Corrupted.

| Feature | Description |
|---|---|
| Default sort | Most-recent first |
| Corrupted column | Value shown in red when > 0 |
| Compare button | Click one row to select run A; click a second to select run B; a "Compare" button appears |

History is fetched from `GET /api/history?limit=30` and refreshed alongside status.

---

### Comparison Modal

Triggered by selecting two rows in the history table and clicking "Compare".

Displays:
- Run A details (drive, started, duration, all file counts)
- Run B details
- Delta column (run B minus run A per field; positive = more, negative = fewer)

The modal is dismissed by clicking outside it or pressing Escape.

---

### Corruption Drill-Down Panel

Fetches `GET /api/corruption` on page load and after each operation.

Displays a table of runs that detected bit rot:

| Column | Description |
|---|---|
| Drive | `drive_name` |
| Run ID | `run_id` (links conceptually to the history table) |
| Detected | `started_at` formatted as local date/time |
| Files Corrupted | `files_corrupted` count |

Hidden when the array is empty.

---

### Settings Card

Allows runtime modification of disk-usage thresholds and notification rules.

| Control | Setting | Default |
|---|---|---|
| Warn % (number input) | `disk_thresholds.warn_pct` | 75 |
| Error % (number input) | `disk_thresholds.error_pct` | 90 |
| On Success (checkbox) | `notification_rules.on_success` | false |
| On Warning (checkbox) | `notification_rules.on_warning` | true |
| On Corruption (checkbox) | `notification_rules.on_corruption` | true |
| On Error (checkbox) | `notification_rules.on_error` | true |
| **Save** button | `POST /api/settings` | — |
| **Test Email** button | `POST /api/test-email` | — |

On Save, a success or error toast is shown. The disk-usage thresholds are applied
immediately to all drive-card bars on the current page.

The Test Email button is disabled while the request is in-flight and shows a
success/error toast on completion.

---

### Schedule Card

Displays all auto-run schedule entries from `GET /api/schedule`.

Each row:
| Control | Field |
|---|---|
| Enabled toggle (checkbox) | `enabled` |
| Label (text) | `label` |
| Cron expression (text input) | `cron_expr` |
| **Save** button | `POST /api/schedule` |

Supported expressions: `@hourly`, `@daily`, `@weekly`, `HH:MM` (24-hour).

The scheduler runner checks entries every minute; changes take effect within 60 seconds.

---

## LocalStorage Keys

| Key | Type | Description |
|---|---|---|
| `showSyncDuration` | `"true"` / `"false"` | Duration chart sync-series visibility |
| `showScrubDuration` | `"true"` / `"false"` | Duration chart scrub-series visibility |

---

## Design Tokens

```css
--bg:      #0f1117   /* page background */
--surface: #1a1d27   /* cards, header */
--border:  #2e3147   /* card borders */
--accent:  #6366f1   /* primary actions, active states */
--ok:      #22c55e   /* success, clean */
--warn:    #f59e0b   /* warning threshold */
--err:     #ef4444   /* error, corruption, critical */
--text:    #e2e8f0   /* primary text */
--muted:   #6b7280   /* secondary text, labels */
```
