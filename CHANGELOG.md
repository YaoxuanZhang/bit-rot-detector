# Changelog

All notable changes to `bit-rot-detector` are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased] — `copilot/refactor-bit-rot-detector-go`

### Added

#### Web Dashboard (Phase 1 – 2)
- Single-page web UI served at `/` via `-web` flag; no tabs, all sections scroll inline.
- Server-Sent Events endpoint `GET /api/progress` with live per-file progress events.
- Run history persisted in a `run_history` SQLite table; exposed at `GET /api/history`.
- Chart.js charts: stacked line (Added/Modified/Removed/Corrupted over time) and
  duration bar chart (sync vs scrub, show/hide toggles).
- History table with two-run selection and comparison modal (`GET /api/compare`).
- Corruption drill-down panel (`GET /api/corruption`).
- Status bar with `Bit Rot Detected` state when `files_corrupted > 0`.
- Drive cards with actual numeric stats (fixed by adding `json:""` tags to
  `SyncResult`, `ScrubResult`, and `DriveHealth`).

#### Phase 3 — Next-Phase Features
- `GET /api/export?format=json|csv` — full run-history export.
- `GET /api/compare?a=&b=` — delta between two `RunRecord`s.
- `POST /api/drives/{idx}/sync|scrub` — per-drive sync/scrub triggers.
- `POST /api/test-email` — SMTP connectivity test endpoint.
- `GET/POST /api/settings` — runtime-configurable disk thresholds and notification rules.
- `GET/POST /api/schedule` — cron-like auto-run schedule with `@hourly`, `@daily`,
  `@weekly`, and `HH:MM` expressions.
- `GET /api/retries` — in-memory retry queue with exponential backoff.
- **`internal/settings`** package — `Store` with `Get()`/`Set()` and JSON file persistence.
- **`internal/scheduler`** package — `Store`, `Runner`, and `isDue` scheduler logic.
- **`internal/retry`** package — bounded queue with `Add`, `Due`, `MarkDone`, `MarkFailed`.
- **`internal/watcher`** package — `fsnotify`-based recursive watcher with 3s debounce.

#### Testing
- 14+ new tests in `internal/api/server_test.go` covering all new endpoints.
- Unit tests for `internal/retry`, `internal/settings`, `internal/scheduler`
  (including white-box `isDue` and `Runner.check` tests).
- Storage tests for `InsertRunHistory`, `GetRunsByIDs`, `GetCorruptionHistory`,
  `GetRunHistoryForPath`.
- Integration tests (S27–S29) for CSV export, run comparison, and corruption drill-down.
- Overall coverage: ~79% across all packages; api 87%, retry 97%, settings 95%.

#### Documentation
- Complete `README.md` rewrite with web UI section, new flags, API overview table,
  updated architecture diagram, troubleshooting table.
- `docs/architecture.md` — module responsibilities, data-flow diagrams, shadow-DB
  swap algorithm, concurrency model.
- `docs/api.md` — full REST API reference with request/response examples and SSE
  stream format.
- `docs/ui.md` — UI layout, card descriptions, chart filters, status logic, design tokens.
- `docs/ops.md` — bare-metal + Docker deploy, systemd service/timer, cron examples,
  logging, DB backup/restore, canary explanation, security notes.
- `CHANGELOG.md` (this file).

#### CI
- `.github/workflows/agent-on-issue.yml` — automatically assigns the GitHub Copilot
  coding agent to every newly opened issue.

### Fixed
- Drive card stats showed `—` because Go structs lacked `json:"snake_case"` tags;
  fixed by adding tags to `SyncResult`, `ScrubResult`, and `DriveHealth`.
- Unchecked `fmt.Fprintf` return value in SSE write path (`handleProgress`) now
  returns on write error.
- Shadow DB left on disk after previous crash is now cleaned up on next `Open()` call.
- Data race in watcher package resolved; SQLite WAL mode disabled for shadow-copy
  compatibility.
- Flaky SSE test stabilised with explicit `context.WithCancel` teardown.

### Changed
- `api.New()` signature unchanged; optional dependencies (`mailer`, `settings`,
  `scheduler`, `retryQ`) are injected via `SetMailer` / `WithSettings` /
  `WithScheduler` / `WithRetryQueue` methods.
- `domain.SyncResult`, `domain.ScrubResult`, `domain.DriveHealth` fields are now
  tagged with snake_case JSON keys for consistent API payloads.
- `startOperation` refactored into `startOperationForPaths(paths []string, ...)` to
  support per-drive triggers without code duplication.

---

## [Pre-release] — Phase 1+2 (2024-01)

- Live SSE progress, run history, tabbed Chart.js dashboard.
- Initial implementation of `watcher` package and real-time file-change detection.

---

## [Initial] — Baseline

- BLAKE3 hashing with IO-aware worker pool.
- Atomic shadow-DB swap for crash-safe SQLite writes.
- Canary file protection against unmounted drives.
- SMTP notifications with unified end-of-run report.
- Multi-drive concurrent processing.
- Static binary with `CGO_ENABLED=0`.
