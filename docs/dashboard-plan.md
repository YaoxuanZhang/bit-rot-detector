# Bit Rot Detector — Dashboard Plan

> **Status**: In progress on branch `copilot/redesign-config-architecture`  
> This document tracks all planned and in-progress work for the card-based dashboard redesign.

---

## Goals

1. **Fully-featured dashboard** for the nominal use-case: monitor N drives, trigger scans, review history, tweak config — without leaving the browser.
2. **No modals / popups** — cards physically expand in-place with animated transitions.
3. **Live progress** — scan progress is visible in real-time via SSE.
4. **Drives visible on page load** — disk list is eagerly loaded before any scan runs.
5. **Editable config** — all non-secret settings are editable from the Settings card and persisted to `config.yaml`.

---

## Architecture overview

```
Browser (React / Vite)
  ├── App.tsx             — grid shell, SSE orchestration, global state
  ├── cards/RunCard.tsx   — drive grid, scan controls, live progress, failures
  ├── cards/HistoryCard.tsx — run history table, corruption events, compare modal
  └── cards/SettingsCard.tsx — thresholds, notifications, schedule, config editor

Go backend
  ├── GET  /api/status      — aggregated drive status (last run + running flag)
  ├── GET  /api/drives      — drive paths + health (available before first run)
  ├── GET  /api/progress    — SSE stream of ProgressEvent during scan/scrub
  ├── GET  /api/history     — RunRecord[] from each drive's SQLite DB
  ├── GET  /api/corruption  — runs with files_corrupted > 0
  ├── GET  /api/compare     — delta between two run IDs
  ├── GET  /api/config      — full config (secrets redacted)
  ├── POST /api/config      — patch runtime-mutable config keys → saved to YAML
  ├── GET  /api/settings    — disk thresholds + notification rules
  ├── POST /api/settings    — update thresholds + notifications
  ├── GET  /api/schedule    — schedule entries
  ├── POST /api/schedule    — upsert a schedule entry
  ├── POST /api/sync        — trigger sync (all drives)
  ├── POST /api/scrub       — trigger scrub (all drives)
  ├── POST /api/drives/{idx}/sync  — per-drive sync
  └── POST /api/drives/{idx}/scrub — per-drive scrub
```

---

## Bug fixes (blocking)

### 1. `ProgressEvent` field mismatch
**Symptom**: progress bar never renders during scans.

Backend (`domain.ProgressEvent`) emits:
```json
{ "phase": "walk|hash|scrub|done", "drive": "Movies", "count": 250, "total": 0 }
```
Frontend `ProgressEvent` interface incorrectly expects `type`, `path`, `pct`, `done`.

**Fix**: update `api.ts`, `sse.ts`, and `RunCard.tsx` to use `phase`/`drive`/`count`/`total`.

Progress UX per phase:
| Phase   | Left label         | Right label          | Bar?                  |
| ------- | ------------------ | -------------------- | --------------------- |
| `walk`  | Walk — discovering | `count` files        | Indeterminate (pulse) |
| `hash`  | Hash — scanning    | `count` / walk total | % of discovered files |
| `scrub` | Scrub — verifying  | `count` / `total`    | % of files to scrub   |
| `done`  | — clear progress — | –                    | –                     |

Walk total is not known upfront; store the last `walk` count as `discoveredTotal` in state to use as the denominator during the `hash` phase.

### 2. `RunRecord` field mismatch
**Symptom**: History card shows empty/NaN values; sorting is broken.

Backend JSON:
```json
{ "id": 42, "drive_name": "Movies", "files_scanned": 1000, "files_added": 5, ... }
```
Frontend interface used: `id: string`, `drive`, `scanned`, `added`, etc.

**Fix**: update `RunRecord` in `api.ts` and all usages in `HistoryCard.tsx`.

### 3. `DriveHealth` object vs string
**Symptom**: `drive.health === 'error'` is always false; health dot is always "ok".

Backend sends `health` as `DriveHealth` object `{ total_space, used_space, free_space, smart_status }`.
Frontend treats it as a string.

**Fix**: add `DriveHealth` interface to `api.ts`, update `DriveStatus`. Compute health string from percentage:
```ts
usedPct = health.used_space / health.total_space * 100
errPct  → from settings (default 90)
warnPct → from settings (default 75)
```

### 4. `SyncResult` / `ScrubResult` field prefixes
**Symptom**: all sync/scrub stat boxes show 0.

Backend: `files_scanned`, `files_added`, etc. (prefixed).  
Frontend `SyncScrubResult` used: `scanned`, `added`, etc.

**Fix**: update both interfaces and all consumers in `RunCard.tsx`.

### 5. `CorruptionEntry` shape mismatch
**Symptom**: history card corruption panel either crashes or shows blanks.

Backend returns a `CorruptionEvent` (run summary): `drive_name`, `started_at`, `files_corrupted`, `run_id`.  
Frontend expected file-level detail: `path`, `detected_at`, `hash_expected`, `hash_actual` — which the backend never provides.

**Fix**: update `CorruptionEntry` interface to match actual backend shape; update corruption table columns.

### 6. `CompareResponse` field names
**Symptom**: compare modal crashes/shows blanks.

Backend response: `{ "run_a": {...}, "run_b": {...}, "delta": { "files_added": N, ... } }`.  
Frontend expects `a`, `b`, and `delta.added`, `delta.scanned`, etc.

**Fix**: update `CompareResponse` interface; update modal rendering.

### 7. `RunRecord.id` type: string → number
**Symptom**: compare selection / API calls silently broken.

Backend: `id` is `int64`. Frontend signature uses `string`.

**Fix**: change to `number`.

### 8. `make dev` target missing
**Symptom**: `make dev` exits with code 2.

**Fix**: add `run` and `dev` targets to `Makefile`.

---

## Feature: In-place card expansion

### Current behaviour
Cards use `position: fixed; inset: 1rem` + semi-transparent overlay + blur on other cards.
This is a *popup/modal*, not a physical card expansion.

### Target behaviour
- Cards **stay in the CSS grid flow**.
- Expanding a card applies `grid-column: 1 / -1`, causing it to span the full row width.
- Other cards reflow naturally below (no blur, no overlay).
- The expansion is animated with `@keyframes cardExpand` (scale + opacity from-top).
- Collapsing is the reverse (`@keyframes cardCollapse`), implemented by briefly adding a `.collapsing` class before removing `.expanded` in React.

```
┌──────────────────────────────────────────────────────┐
│  Run Card (expanded, spans full width)               │
│  Live progress bars, drive rows, toolbar             │
└──────────────────────────────────────────────────────┘
┌────────────────────┐  ┌─────────────────────────────┐
│  History Card      │  │  Settings Card              │
│  (collapsed)       │  │  (collapsed)                │
└────────────────────┘  └─────────────────────────────┘
```

**CSS changes:**
- Remove `.card-overlay`, `.card-grid.has-expanded`, and the blur filter rules.
- `.card.expanded` becomes `grid-column: 1 / -1` + `animation: cardExpand`.  
- `.card.collapsing` has `animation: cardCollapse` and pointer-events none.

**React changes:**
- Remove the overlay `<div>` from `App.tsx`.
- Remove `has-expanded` class toggle.
- Add a `collapsing` CSS class transition before state removal.

---

## Feature: Drives visible on page load

### Current behaviour
The drive list in RunCard is empty until a scan completes, because `GET /api/status` returns `drives: []` on first start.

### Target behaviour
`GET /api/drives` is called immediately on page load. It always returns `paths: []string` (the configured paths from `config.yaml`) even before any run completes. Drive rows are rendered using this path list, with stats showing `—` until a run provides them.

**Implementation:**
1. In `App.tsx`, call `api.getDrives()` alongside `api.getStatus()` on mount.
2. Pass a `drives` prop to `RunCard` (the raw `DrivesResponse`).
3. In `RunCard`, merge `status.drives` (has stats) with `drives.paths` (always has paths) so that drives are always shown.

---

## Feature: Config editor in Settings card

### Target behaviour
The expanded Settings card shows a **Config** section below Schedule with:
- One row per config key: key name, current value, editable input, source badge (`yaml` / `env` / `default`), mutability badge (`editable` / `read-only`).
- A **Save Config** button that calls `POST /api/config`.
- Read-only fields (e.g. `target_paths`, SMTP secrets) are shown but their inputs are disabled.

**Editable keys (sent via POST /api/config):**
| Key                  | Type    | Notes                       |
| -------------------- | ------- | --------------------------- |
| `scrub_percentage`   | float   | 0.1–100.0                   |
| `scrub_frequency`    | enum    | daily / weekly / monthly    |
| `max_workers`        | integer | 1–32                        |
| `log_level`          | enum    | DEBUG / INFO / WARN / ERROR |
| `log_retention_days` | integer | ≥1                          |
| `smtp.host`          | string  |                             |
| `smtp.port`          | integer |                             |
| `smtp.sender`        | string  |                             |
| `smtp.recipient`     | string  |                             |

**Read-only keys (displayed, not editable):**
| Key             | Reason                                        |
| --------------- | --------------------------------------------- |
| `target_paths`  | Requires restart; file-path validation needed |
| `smtp.username` | Secret — env var only                         |
| `smtp.password` | Secret — env var only                         |

---

## Phased delivery

### ✅ Phase 0 — Config Architecture  
- [x] `gopkg.in/yaml.v3` dependency  
- [x] `config.Load` / `config.Save` / `config.Store`  
- [x] `POST /api/config` applies mutable key patches  

### 🔧 Phase 1 — Foundations + Bug Fixes (current focus)
- [ ] Fix all 7 type mismatches listed above  
- [ ] Fix `make dev` target  
- [ ] In-place card expansion (replace popup with physical expand)  
- [ ] Eager drive discovery on page load  

### 📊 Phase 2 — Run Card (polish)
- [ ] Walk/Hash/Scrub phase labels in progress section  
- [ ] Discovered-files counter shown on right during walk/hash  
- [ ] Per-drive disk-usage bar (used % of total)  
- [ ] Drive detail: total_space / free_space displayed  

### 📋 Phase 3 — History Card (polish)
- [ ] Auto-refresh on SSE `done` event (currently missing)  
- [ ] Corruption table shows correct fields (drive_name, started_at, files_corrupted)  
- [ ] Compare modal uses corrected field names  
- [ ] Sparkline in collapsed view  

### ⚙️  Phase 4 — Settings Card
- [ ] Config editor section (GET + POST /api/config)  
- [ ] Schedule CRUD (add new entry, delete entry)  
- [ ] Thresholds applied to health-dot computation client-side  

### 🎨 Phase 5 — Polish & Docs
- [ ] Responsive grid: 3 → 2 → 1 columns  
- [ ] Keyboard navigation: Escape collapses, Tab moves between cards  
- [ ] Disk-usage bar colours respect configured thresholds  
- [ ] Update `docs/ui.md`, `docs/api.md`, `docs/architecture.md`  
- [ ] Integration tests for `/api/config` endpoints  

---

## Open questions / decisions needed

| #   | Question                                                                                      | Default assumption                             |
| --- | --------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| 1   | Should `target_paths` be editable from the UI (requires restart)?                             | No — show read-only, add a note                |
| 2   | Should collapse animate (reverse scale) or be instant?                                        | Instant for now (CSS only, no JS delay needed) |
| 3   | Should the sparkline in HistoryCard collapsed view show `files_scanned` or `files_corrupted`? | `files_corrupted` (more actionable)            |
| 4   | Dark theme only or add light theme toggle?                                                    | Dark only for now                              |
