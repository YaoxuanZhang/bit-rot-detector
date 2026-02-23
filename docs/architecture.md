# Architecture

This document describes the internal structure of `bit-rot-detector`, the data flows
for each operation, and the crash-safety algorithm that keeps the production database
consistent.

---

## High-Level Overview

```
┌────────────────────────────────────────────────────────────────────────┐
│  Entry point  cmd/bit-rot-detector/main.go                             │
│  ─────────────────────────────────────────────────────────────────────│
│  • Parses CLI flags (-sync, -scrub, -watch, -web, -test-email, -addr) │
│  • Loads .env via godotenv, then reads config from environment         │
│  • Installs signal.NotifyContext for graceful shutdown (SIGINT/TERM)   │
│  • Selects operating mode:                                             │
│      CLI mode   → coordinator.Run → mailer.SendUnifiedReport → exit   │
│      Watch mode → watcher.Run (loops on file changes)                 │
│      Web mode   → api.Server.ListenAndServe (blocks until ctx done)   │
└───────────────────────┬────────────────────────────────────────────────┘
                        │ coordinator.Run(ctx, paths, opts)
                        ▼
┌───────────────────────────────────────────────────────────┐
│  coordinator  (one goroutine per drive, fanned out)        │
│  • Opens storage.Repository (shadow DB)                   │
│  • Calls scanner.Syncer.SyncDirectory                     │
│  • Calls scanner.Syncer.ScrubFiles                        │
│  • Calls storage.Repository.InsertRunHistory              │
│  • Calls storage.Repository.Commit (atomic rename)        │
│  • Collects domain.DriveResult for each drive             │
└───────────────────────────────────────────────────────────┘
```

---

## Module Responsibilities

### `cmd/bit-rot-detector`

Entry point only. Responsible for:
- Flag parsing and mode selection
- Constructing all top-level dependencies (`config.Config`, `mailer.Mailer`,
  `api.Server`, `settings.Store`, `scheduler.Store`, `retry.Queue`)
- Wiring the Copilot agent assignment workflow

### `internal/domain`

Pure data types shared across all packages. Contains no behaviour and no imports
beyond the standard library. This deliberate constraint prevents import cycles.

Key types:
- `FileRecord` — one row in the `files` table (path, hash, timestamps, size, mtime)
- `SyncResult` — per-drive sync statistics (scanned, added, modified, moved, removed)
- `ScrubResult` — per-drive scrub statistics (validated, corrupted paths, errors)
- `DriveHealth` — disk-usage statistics and SMART summary
- `WorkItem` / `WorkResult` — producer-consumer channel payloads for the hash worker pool
- `ProgressEvent` — SSE-friendly event emitted during a running operation
- `RunRecord` — one row in the `run_history` table
- `RunDelta` — field-by-field difference between two `RunRecord`s

### `internal/config`

Reads and validates all configuration from environment variables. Fails fast on the
first validation error. Provides `envOrDefault` for safe fallback handling.

### `internal/hasher`

Wraps `github.com/zeebo/blake3` in a context-aware function:
```
Hash(ctx context.Context, path string) (string, error)
```
Returns the hex-encoded BLAKE3 digest. Returns the context error immediately if the
context is cancelled during file I/O, ensuring prompt cancellation on shutdown or
interrupt.

### `internal/monitor`

Detects whether the block device backing a given path is rotational (HDD) or
non-rotational (SSD/NVMe) by reading `/sys/block/<dev>/queue/rotational` on Linux.
Returns `(isRotational bool, err error)`. The coordinator uses this to set the worker
count: 1 for HDD, `runtime.NumCPU()` for SSD, `runtime.NumCPU()` if detection fails.

### `internal/storage`

Provides a SQLite repository with a crash-safe shadow-DB swap strategy:

```
Open(dir) → opens bitrot.db.shadow (copy of bitrot.db or blank)
UpsertFile, DeleteFile, GetFilesForScrub, UpdateScrubStatus
InsertRunHistory
Commit()  → os.Rename(shadow → prod) + write new canary digest
Rollback() → delete shadow file
```

All reads and writes during a run target the shadow database. The production
`bitrot.db` is never modified in place.

Uses `modernc.org/sqlite` (CGO-free), which runs SQLite as pure Go.

### `internal/scanner`

The `Syncer` struct implements the producer-consumer hash pipeline:

1. **Walk goroutine** — `filepath.WalkDir` emits `WorkItem`s into a buffered channel.
   Files whose stored `size` and `mtime` match the on-disk values receive a `KnownHash`
   and skip re-hashing.
2. **Worker pool** — N goroutines (count from `monitor`) read `WorkItem`s, call
   `hasher.Hash`, emit `WorkResult`s.
3. **Collector goroutine** — reads `WorkResult`s and updates the `storage.Repository`.
   Classifies each result as new, modified, moved, or unchanged.
4. **Deletion sweep** — after the walk completes, removes records for paths no longer
   on disk.
5. **Scrub** — `ScrubFiles` reads eligible files from the DB, re-hashes them, and
   reports mismatches as corrupted.

Progress events (`ProgressEvent`) are emitted to `opts.ProgressCh` at regular
intervals during walk, hash, and scrub phases.

### `internal/coordinator`

Orchestrates sync and scrub across multiple drives:
- Spawns one goroutine per drive path.
- Opens a `storage.Repository` for each drive.
- Calls `scanner.Syncer.SyncDirectory` and/or `ScrubFiles`.
- Inserts a `RunRecord` into `run_history` on success.
- Commits or rolls back the shadow DB.
- Aggregates results into `[]DriveResult`.

Provides helper functions (`ToMailerSyncEntries`, `ToMailerScrubEntries`,
`ToHealthSlice`, `CollectErrors`) consumed by the mailer and the web server.

### `internal/mailer`

Sends SMTP email notifications using STARTTLS:
- `SendUnifiedReport` — builds a plain-text multi-section report (drive health, sync
  stats, scrub stats, errors, bit-rot alert) and sends it.
- `SendTestEmail` — sends a minimal connectivity test message; used by `-test-email`
  flag and `POST /api/test-email`.

Refuses to send credentials over a plaintext connection: if STARTTLS is not offered
by the server, the call returns an error rather than fall back to plain auth.

### `internal/api`

HTTP server (`net/http` only, no external router) that serves:
- A single-page web UI (embedded via `go:embed`)
- A JSON REST API (17 endpoints)
- A Server-Sent Events stream (`GET /api/progress`)

The `progressHub` fan-out struct broadcasts `ProgressEvent` values to all active SSE
subscribers. When an operation is triggered via the API, a `progCh` channel is created
and a forwarder goroutine bridges it to the hub.

Background operations run under the server's lifetime context (not the HTTP request
context) so they are not cancelled by client disconnection.

### `internal/watcher`

Wraps `fsnotify` to watch a set of directories recursively. Applies a configurable
debounce (3 seconds) so rapid bursts of file events are coalesced into a single
re-run. Newly created subdirectories are watched automatically.

### `internal/settings`

An in-memory `Store` with optional JSON file persistence:
- `DiskThresholds` — warning and critical disk-usage percentages (defaults: 75%, 90%)
- `NotificationRules` — per-event email toggles (success, warning, corruption, error)

### `internal/scheduler`

Stores `ScheduleEntry` records in memory (optionally persisted to JSON). A background
`Runner` checks entries on a 1-minute tick and calls a trigger function when an entry
comes due. Supports `@hourly`, `@daily`, `@weekly`, and `HH:MM` expressions.

### `internal/retry`

A bounded, concurrency-safe queue of failed operations with exponential-backoff
rescheduling (capped at 24 hours). Items are added when transient errors occur and
exposed via `GET /api/retries`.

---

## Data Flow: Sync Operation

```
POST /api/sync
    │
    ├─ api.Server.startOperationForPaths
    │      • sets status.Running = true
    │      • returns 202 Accepted
    │      • creates progCh channel
    │      • goroutine: forwards progCh → progressHub → SSE clients
    │
    └─ goroutine: coordinator.Run(ctx, paths, opts{RunSync:true})
           │
           ├─ for each path (concurrent):
           │    ├─ storage.Open(path) → opens shadow DB
           │    ├─ monitor.IsRotational(path) → determine worker count
           │    ├─ scanner.Syncer.SyncDirectory(ctx, repo, progCh)
           │    │      ├─ Walk goroutine  → WorkItem channel
           │    │      ├─ N Worker goroutines → WorkResult channel
           │    │      └─ Collector goroutine → repo.UpsertFile / DeleteFile
           │    ├─ repo.InsertRunHistory(rec)
           │    └─ repo.Commit() → os.Rename(shadow → prod)
           │
           └─ api.Server updates status{Running:false, Drives:[...]}
```

---

## Data Flow: Scrub Operation

```
POST /api/scrub
    │  (same as sync up to coordinator.Run)
    │
    └─ coordinator.Run(ctx, paths, opts{RunScrub:true})
           │
           ├─ for each path (concurrent):
           │    ├─ storage.Open(path)
           │    ├─ repo.GetFilesForScrub(pct, freq) → eligible FileRecords
           │    ├─ scanner.Syncer.ScrubFiles(ctx, records, progCh)
           │    │      ├─ for each record: hasher.Hash(path)
           │    │      └─ mismatch → FilesCorrupted list
           │    ├─ repo.UpdateScrubStatus(path, time)
           │    ├─ repo.InsertRunHistory(rec{FilesCorrupted: N})
           │    └─ repo.Commit()
           │
           └─ if any corruption → mailer.SendUnifiedReport (CRITICAL)
```

---

## Shadow-DB Swap Algorithm

```
Startup
│
├─ Does bitrot.db.shadow exist?
│    YES → crash recovery: delete shadow, proceed with bitrot.db as prod
│    NO  → continue
│
├─ Does bitrot.db exist?
│    YES → copy bitrot.db → bitrot.db.shadow (starting point)
│    NO  → create blank bitrot.db.shadow (first run)
│
│  All reads and writes from this point target bitrot.db.shadow
│
├─ Canary post-check (drive still mounted?)
│
├─ Commit path:
│    os.Rename("bitrot.db.shadow", "bitrot.db")  ← atomic on POSIX
│    write BLAKE3(bitrot.db) to .bitrot-canary
│
└─ Rollback path (any error before Commit):
     os.Remove("bitrot.db.shadow")
     bitrot.db is untouched
```

**Why this is safe:**  
`os.Rename` is atomic on POSIX file systems (single call to `rename(2)`). Either the
new database is fully in place or the old one remains. There is no window where
`bitrot.db` is in a partial state.

**WAL/journal mode:**  
The repository opens SQLite in DELETE journal mode (not WAL mode). This ensures there
are no `-wal` or `-shm` sidecar files that would be missed by the shadow-copy
strategy.

---

## Concurrency Model

```
┌─────────────────────────────────────────────────────┐
│ api.Server goroutines                               │
│  • HTTP handler goroutines (per request, from net/http pool)
│  • 1 forwarder goroutine per active operation       │
│  • 1 background operation goroutine per trigger     │
│  • 1 SSE handler goroutine per connected client     │
│                                                     │
│ All share: s.mu (RWMutex) for status reads/writes   │
│            progressHub.mu (Mutex) for fan-out       │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ coordinator goroutines (per operation)              │
│  • 1 orchestrator goroutine per active Run()        │
│  • 1 drive goroutine per path (concurrent)          │
│    Each drive goroutine:                            │
│      1 walker goroutine                             │
│      N worker goroutines (IO-aware count)           │
│      1 collector goroutine                          │
└─────────────────────────────────────────────────────┘
```

The API server enforces at most one active operation at a time using a
`status.Running` guard (protected by `s.mu`). Concurrent trigger attempts receive
`409 Conflict`.
