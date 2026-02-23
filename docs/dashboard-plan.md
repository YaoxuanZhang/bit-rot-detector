# Bit Rot Detector — Dashboard Plan

> **Branch**: `copilot/redesign-config-architecture`  
> **Stack**: React 18 · Vite 5 · TypeScript 5 · Chart.js 4  
> **Build**: `make ui` (npm build → `internal/api/static/`) then `make build-go`

---

## Goals

1. **Fully-featured dashboard** for the nominal use-case: monitor N drives, trigger scans, review history, tweak config — without leaving the browser.
2. **No modals / popups** — cards physically expand in-place with animated CSS transitions.
3. **Live progress** — sync and scrub progress visible in real-time via Server-Sent Events.
4. **Drives visible on page load** — disk list is eagerly loaded from `GET /api/drives` before any scan runs.
5. **Editable config** — all non-secret settings are editable from the Settings card and persisted to `config.yaml`.
6. **Auto-refresh** — History card and status refresh automatically when a run completes (SSE `done` event).

---

## Architecture

```
Browser (React / Vite)
  ├── App.tsx              — grid shell, SSE orchestration, global state
  ├── cards/RunCard.tsx    — drive grid, scan controls, live progress, failures
  ├── cards/HistoryCard.tsx — run history table, corruption events, compare modal
  └── cards/SettingsCard.tsx — thresholds, notifications, schedule, config editor

Go backend (internal/api/server.go)
  ├── GET  /api/status          — aggregated drive status (last run + running flag)
  ├── GET  /api/drives          — drive paths + health (available before first run)
  ├── GET  /api/progress        — SSE stream of ProgressEvent during scan/scrub
  ├── GET  /api/history         — RunRecord[] from each drive's SQLite DB
  ├── GET  /api/corruption      — runs with files_corrupted > 0
  ├── GET  /api/compare?a=&b=   — delta between two run IDs
  ├── GET  /api/export          — download run history (format=json|csv)
  ├── GET  /api/config          — full config (secrets redacted)
  ├── POST /api/config          — patch runtime-mutable config keys → saved to YAML
  ├── GET  /api/settings        — disk thresholds + notification rules
  ├── POST /api/settings        — update thresholds + notifications
  ├── GET  /api/schedule        — schedule entries
  ├── POST /api/schedule        — upsert a schedule entry
  ├── POST /api/sync            — trigger sync (all drives)
  ├── POST /api/scrub           — trigger scrub (all drives)
  ├── POST /api/drives/{idx}/sync  — per-drive sync
  └── POST /api/drives/{idx}/scrub — per-drive scrub
```

---

## Card layout

```
┌─────────────────────────────────────────────────────────────────────┐
│  App header  🔍 Bit Rot Detector  [v]  ● status dot                │
└─────────────────────────────────────────────────────────────────────┘
┌──────────────────┐  ┌──────────────────┐  ┌───────────────────────┐
│  Run Card        │  │  History Card    │  │  Settings Card        │
│  ● status dot    │  │  42 Runs         │  │  Next run: @daily     │
│  3 Drives        │  │  [spark chart]   │  │  Notify: corruption   │
│  1,234 Scanned   │  │  Last corruption:│  │  Disk: warn 75% err   │
│  Last run: 2h ago│  │  never           │  │  90%                  │
└──────────────────┘  └──────────────────┘  └───────────────────────┘

When a card is clicked it expands inline to span the full grid width:

┌─────────────────────────────────────────────────────────────────────┐
│  Run Card (expanded)                                            [✕] │
│  [⟳ Sync All] [🔬 Scrub All] [↓ CSV] [↓ JSON]                     │
│  ── Progress ───────────────────────────────────────────────────── │
│  Walk → [▒▒▒▒▒▒▒░░░░░] indeterminate      500 found               │
│  Hash → [▓▓▓▓▓░░░░░░░] 50%          500 / 1000 · 50.0%            │
│  ── Drives (2) ─────────────────────────────────────────────────── │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ ● Movies  /mnt/movies  Free: 1.2 TB          [Sync] [Scrub]  │  │
│  │ [used: ▓▓▓▓░░░░░░░░] 60%  3.0 TB / 5.0 TB (60.0%)          │  │
│  │ Scanned:1234  Added:5  Modified:0  Removed:0  Corrupted:0    │  │
│  └──────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
┌──────────────────┐  ┌─────────────────────────────────────────────┐
│  History (coll.) │  │  Settings (collapsed)                        │
└──────────────────┘  └─────────────────────────────────────────────┘
```

---

## Progress event protocol

The backend (`domain.ProgressEvent`) emits events during sync and scrub:

```json
{ "phase": "walk|hash|scrub|done", "drive": "Movies", "count": 250, "total": 0 }
```

| Phase   | `count`               | `total`                | UI behaviour                                |
| ------- | --------------------- | ---------------------- | ------------------------------------------- |
| `walk`  | Files discovered so far | 0 → finalCount (last event) | Indeterminate shimmer + "N found" right |
| `hash`  | Files hashed so far   | Walk total (set in fix) | Determinate bar + "N / Total · X%" right   |
| `scrub` | Files verified        | Files selected for scrub | Determinate bar (green) + "N / Total · X%" |
| `done`  | Final count           | Final total             | Clear progress entry; refresh history + status |

Walk events are emitted every 250 files **plus** a final event after `WalkDir` finishes (so even small directories with < 250 files get a walk-total event used as the hash denominator).

---

## Bug fixes (completed this session)

### ✅ 1. HistoryCard auto-refresh
**Symptom**: After a scan completes, History card showed stale data until the user manually clicked ↺ Refresh.

**Root cause**: No refresh callback was wired from the SSE `done` handler to `HistoryCard`.

**Fix** (`App.tsx` + `HistoryCard.tsx`):
- Added `historyRefreshRef` to `App.tsx`.
- `onRunDone` now calls `runRefreshRef.current()` + `historyRefreshRef.current()`.
- `HistoryCard` accepts `registerRefresh` prop, calls `load()` on expand.

### ✅ 2. Collapse animation missing
**Symptom**: Collapsing a card was instantaneous (no animation).

**Fix** (`App.tsx` + `cards.css`):
- Added `@keyframes cardCollapse` CSS.
- `App.tsx` sets a `collapsing` state flag for 260 ms before clearing `expanded`.

### ✅ 3. Progress bar denominator (hash phase)
**Symptom**: Hash progress bar showed indeterminate for directories with < 250 files (no walk events were emitted). For larger directories the denominator was the last walk count emitted, not the actual file total.

**Fix** (`internal/scanner/scanner.go`):
- Emit a final walk event after `WalkDir` finishes with the complete file count.
- Hash events now include `Total: walkTotal` for accurate progress percentages.

---

## Phased delivery status

### ✅ Phase 0 — Config Architecture
- [x] `gopkg.in/yaml.v3` dependency
- [x] `config.Load(path)` / `config.Save(cfg, path)` / `config.Store`
- [x] `POST /api/config` merges & persists mutable key patches
- [x] `internal/settings` merged into `internal/config`
- [x] Schedule entries in `config.Schedule`
- [x] `.env.example` trimmed to secrets only
- [x] `config.yaml.example` with all non-secret keys

### ✅ Phase 1 — Foundation
- [x] `web/` scaffold: Vite 5 + React 18 + TypeScript 5 + Chart.js
- [x] `vite.config.ts`: output to `internal/api/static/`, `/api` proxy in dev
- [x] Makefile: `ui`, `ui-dev`, `build-go`, `clean-ui` targets
- [x] Dockerfile: three-stage build (ui-builder → go-builder → scratch)
- [x] Card shell with physical in-place expand/collapse (grid-column: 1/-1)
- [x] Expand animation (`cardExpand`) + collapse animation (`cardCollapse`)
- [x] SSE `done` event + 30 s status poll auto-refresh both Run and History

### ✅ Phase 2 — Run Card
- [x] Drive grid eagerly populated from `GET /api/drives` on page load
- [x] Live SSE progress bars: Walk (indeterminate + count), Hash (% bar + count/total), Scrub (green % bar)
- [x] Per-drive disk usage bar (colour: normal / warn / err based on thresholds)
- [x] Per-drive Sync / Scrub buttons
- [x] Sync All / Scrub All / Export CSV / Export JSON toolbar
- [x] Failures panel for drives with errors

### ✅ Phase 3 — History Card
- [x] Run history table (sortable by any column, most-recent first default)
- [x] Auto-refresh after SSE `done` event
- [x] Reload when card is expanded (always fresh data)
- [x] Multi-select two rows → Compare modal with delta table + duration
- [x] Corruption events panel (sorted newest-first, shown prominently in red)
- [x] Spark line chart (collapsed view, files_corrupted trend)
- [x] Export CSV / JSON buttons in toolbar

### ✅ Phase 4 — Settings Card
- [x] Disk threshold form (warn %, error %)
- [x] Notification rule checkboxes (on_corruption, on_error, on_warn, on_completion)
- [x] Test Email button
- [x] Schedule table with inline editing (label, cron_expr, enabled toggle)
- [x] Config editor: Scrub %, Scrub Frequency, Max Workers, Log Level, Log Retention
- [x] SMTP settings (host, port, sender, recipient — non-secret only)
- [x] Save Config → `POST /api/config` → persisted to `config.yaml`

### 🔧 Phase 5 — Polish (remaining)
- [ ] Responsive grid: collapse to 2 → 1 columns on small screens (`@media`)
- [ ] Add new schedule entry UI (currently only editing existing entries)
- [ ] Delete schedule entry UI
- [ ] SMTP credentials: show env-var note (`SMTP_USERNAME`, `SMTP_PASSWORD`)
- [ ] Disk usage bar colours respect the currently configured threshold values
- [ ] Per-drive corruption history drill-down (click corrupted count in history table)
- [ ] Update `docs/api.md` with `/api/config` endpoint details
- [ ] Update `docs/architecture.md`

---

## Open questions

| #  | Question                                                          | Current assumption                    |
| -- | ----------------------------------------------------------------- | ------------------------------------- |
| 1  | Should `target_paths` be editable (requires restart)?             | No — shown read-only with explanation |
| 2  | Light theme toggle?                                               | Dark only for now                     |
| 3  | Per-drive history vs. combined history?                           | Combined (all drives in one table)    |
| 4  | Add/delete schedule entries from UI?                              | Add/delete planned for Phase 5        |
