# Bit Rot Detector

A production-grade Go utility that detects silent file corruption (bit rot) using
[BLAKE3](https://github.com/BLAKE3-team/BLAKE3) hashing, an atomic shadow-database
swap for crash safety, and an IO-aware worker pool that adapts to both HDDs and SSDs.

---

## Features

| Feature | Detail |
|---|---|
| **BLAKE3 hashing** | Fast, cryptographically secure file-integrity verification |
| **Atomic shadow-DB swap** | Crash-safe SQLite: production DB stays untouched until 100% success |
| **IO-aware concurrency** | 1 worker on HDD (no head thrashing); `NumCPU` workers on SSD/NVMe |
| **Multi-drive support** | Concurrent, per-drive goroutines for N mount points |
| **Move detection** | Matches relocated files by size+mtime, preserves scrub history |
| **Canary protection** | Aborts if `.bitrot-canary` is missing (unmounted-drive guard) |
| **DB checksum verification** | BLAKE3 digest of `bitrot.db` stored in canary; mismatch logs a warning |
| **Scrub scheduling** | Configurable percentage + daily/weekly/monthly frequency |
| **Real-time file watcher** | `fsnotify`-based watcher re-runs sync on detected changes (`-watch`) |
| **Web dashboard** | Single-page UI with live SSE progress, charts, history, settings (`-web`) |
| **REST API** | JSON API for status, history, export, comparison, and operations |
| **SMTP notifications** | Unified end-of-run email with drive health, sync, and scrub stats |
| **Run history** | Per-drive `run_history` table persisted in `bitrot.db` |
| **Retry queue** | In-memory bounded queue with exponential-backoff for transient errors |
| **Schedule entries** | Configurable auto-run schedule (hourly/daily/weekly/HH:MM) |
| **Settings API** | Runtime-configurable disk thresholds and notification rules |
| **Static binary** | `CGO_ENABLED=0`; no runtime dependencies |

---

## Quick Start

### Build from source

```bash
# Requires Go 1.22+
make build           # → ./bin/bit-rot-detector

# Run a full sync + scrub (CLI mode)
TARGET_DIRECTORY=/mnt/nas ./bin/bit-rot-detector

# Start the web UI on port 8080
TARGET_DIRECTORY=/mnt/nas ./bin/bit-rot-detector -web -addr :8080
```

### Docker

```bash
# Edit docker-compose.yml (or set TARGET_DIRECTORY), then:
docker compose up

# Run once and exit (CI/cron use case)
docker compose run --rm bit-rot-detector
```

### Pull from GHCR

```bash
docker pull ghcr.io/yaoxuanzhang/bit-rot-detector:latest
```

### Development shortcut

```bash
make dev             # build + launch web UI at :8080 (loads .env)
make run ARGS="-sync -web"
```

---

## Prerequisites

**Canary file** — create a marker in every monitored directory before the first run:

```bash
touch /mnt/nas/.bitrot-canary
```

The tool refuses to run if the canary is absent; this guards against an unmounted
drive being interpreted as "all files deleted".

**Configuration** — copy `.env.example` to `.env` and fill in values, or export the
variables directly.

---

## CLI Flags

```text
bit-rot-detector [flags]

Flags:
  -sync           Run sync phase only (record new/modified/moved/deleted files)
  -scrub          Run scrub phase only (re-hash recorded files)
  -test-email     Send a test email via SMTP and exit
  -watch          Watch directories for changes and re-run sync on each change
  -web            Start the HTTP web dashboard and REST API server
  -addr string    HTTP listen address when -web is active (default ":8080")
```

Without `-sync` or `-scrub`, both phases run in sequence.

### Examples

```bash
# Sync only
TARGET_DIRECTORY=/mnt/nas bit-rot-detector -sync

# Full run: sync then scrub 1% of files
TARGET_DIRECTORY=/mnt/nas bit-rot-detector

# Scrub 100% of files on two drives
TARGET_DIRECTORY=/mnt/drive1,/mnt/drive2 SCRUB_PERCENTAGE=100 bit-rot-detector -scrub

# Real-time watch mode (re-syncs on file change, 3-second debounce)
TARGET_DIRECTORY=/mnt/nas bit-rot-detector -watch

# Web dashboard on a custom port
TARGET_DIRECTORY=/mnt/nas bit-rot-detector -web -addr :9090

# Verify SMTP connectivity
TARGET_DIRECTORY=/mnt/nas bit-rot-detector -test-email
```

---

## Configuration

All settings are read from environment variables. `.env` is auto-loaded from the
working directory when present; environment variables always take precedence.

| Variable | Default | Description |
|---|---|---|
| `TARGET_DIRECTORY` | *(required)* | Comma-separated list of absolute paths to monitor |
| `SCRUB_PERCENTAGE` | `1.0` | Fraction of files to re-verify per run (0.1–100.0) |
| `SCRUB_FREQUENCY` | `daily` | Age filter: `daily` (no filter), `weekly` (≥7 days), `monthly` (≥30 days) |
| `MAX_WORKERS` | `4` | Upper bound on hashing goroutines (IO-aware detection may lower this) |
| `LOG_LEVEL` | `INFO` | Minimum log severity: `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `LOG_RETENTION_DAYS` | `7` | Days to retain old log files |
| `SMTP_HOST` | `mail.smtp2go.com` | SMTP server hostname |
| `SMTP_PORT` | `587` | SMTP server port (1–65535) |
| `SMTP_USERNAME` | | SMTP authentication username |
| `SMTP_PASSWORD` | | SMTP authentication password |
| `SMTP_SENDER` | | Envelope sender address |
| `SMTP_RECIPIENT` | | Envelope recipient address |
| `NOTIFY_ON_SUCCESS` | `true` | Send email on clean runs; failure/corruption emails are always sent |

See [`.env.example`](.env.example) for a fully annotated template.

---

## How It Works

### Sync phase

1. **Canary pre-check** — confirm `.bitrot-canary` exists and read its stored DB checksum.
2. **DB integrity check** — compare the BLAKE3 digest of `bitrot.db` with the canary value.
3. **Shadow DB open** — copy `bitrot.db` → `bitrot.db.shadow`; all writes go to the shadow.
4. **Walk** — recursive walker emits `WorkItem`s; files whose `size` + `mtime` match the
   stored record receive a `KnownHash` and skip re-hashing.
5. **Worker pool** — N goroutines hash each file with BLAKE3 and send `WorkResult`s back.
6. **Collector** — classifies each result as *new*, *modified*, *moved*, or *unchanged*.
7. **Deletion sweep** — removes DB records for files no longer on disk.
8. **Run history** — inserts a `RunRecord` summary into the shadow `run_history` table.
9. **Canary post-check** — confirms the canary still exists (drive may have unmounted).
10. **Atomic commit** — `os.Rename(shadow → prod)` atomically replaces `bitrot.db`.
11. **Canary update** — new BLAKE3 digest is written to `.bitrot-canary`.

### Scrub phase

Re-hashes a configurable percentage of previously recorded files (oldest-scrubbed first).
A hash mismatch is reported as **BIT ROT DETECTED** and included in the email report.

`SCRUB_FREQUENCY` adds an age filter: with `weekly`, only files not scrubbed in the last
7 days are eligible even when `SCRUB_PERCENTAGE=100`.

### IO-aware worker pool

On Linux, `/sys/block/<dev>/queue/rotational` determines the drive type:

| Drive type | Workers |
|---|---|
| HDD (rotational) | 1 (prevents seek thrashing) |
| SSD / NVMe | `runtime.NumCPU()` |
| Unknown / non-Linux | `runtime.NumCPU()` |

`MAX_WORKERS` is an upper bound; IO-aware detection may reduce the count.

### Crash safety

If the process is interrupted before the final `os.Rename`, the shadow file is deleted
on the next startup and `bitrot.db` remains the Last Known Good state. No partial writes
ever reach the production database.

---

## Web Dashboard

Start with:

```bash
./bin/bit-rot-detector -web -addr :8080
```

Open `http://localhost:8080`. See [docs/ui.md](docs/ui.md) for details.

| Section | Description |
|---|---|
| **Toolbar** | Sync All, Scrub All, Refresh, Export CSV, Export JSON |
| **Status bar** | State (Idle/Running/OK/Error/Bit Rot Detected), last-run time, duration, drive count |
| **Live progress** | Animated fill bar with Walk → Hash → Scrub badges (SSE-driven) |
| **Last failures** | Most recent errors and affected paths (hidden when clean) |
| **Drive cards** | Per-drive stats, disk-usage bar, per-drive Sync/Scrub buttons |
| **Charts** | Stacked line (Added/Modified/Removed/Corrupted); Duration bar (sync vs scrub, toggleable) |
| **History table** | Run records with Compare button |
| **Comparison modal** | Side-by-side run A vs B with delta column |
| **Corruption drill-down** | Runs that detected bit rot, by drive |
| **Settings card** | Disk-usage thresholds, notification-rule toggles |
| **Schedule card** | Auto-run entries (enabled, cron expression) |

---

## API Overview

The web server exposes a JSON REST API. See [docs/api.md](docs/api.md) for the full
reference including request/response examples.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/status` | Latest run summary |
| `GET` | `/api/drives` | Configured drives with health |
| `GET` | `/api/progress` | SSE live-progress stream |
| `GET` | `/api/history` | Run history |
| `GET` | `/api/export` | Export history (JSON or CSV) |
| `GET` | `/api/compare` | Delta between two runs |
| `GET` | `/api/corruption` | Runs with detected corruption |
| `GET/POST` | `/api/settings` | Runtime settings |
| `GET/POST` | `/api/schedule` | Auto-run schedule |
| `GET` | `/api/retries` | Retry queue |
| `POST` | `/api/sync` | Trigger all-drive sync |
| `POST` | `/api/scrub` | Trigger all-drive scrub |
| `POST` | `/api/drives/{idx}/sync` | Per-drive sync |
| `POST` | `/api/drives/{idx}/scrub` | Per-drive scrub |
| `POST` | `/api/test-email` | Send test email |

---

## Database Schema

A `bitrot.db` SQLite file lives in the root of each monitored directory.

```sql
CREATE TABLE files (
    abs_path      TEXT    PRIMARY KEY,
    hash          TEXT    NOT NULL,
    added_at      INTEGER NOT NULL,
    last_seen     INTEGER NOT NULL,
    last_scrubbed INTEGER,
    scrub_count   INTEGER NOT NULL DEFAULT 0,
    file_size     INTEGER NOT NULL,
    mtime         REAL    NOT NULL
);

CREATE TABLE run_history (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    drive_id        TEXT    NOT NULL,
    drive_name      TEXT    NOT NULL,
    started_at      INTEGER NOT NULL,
    duration_ms     INTEGER NOT NULL,
    files_scanned   INTEGER NOT NULL DEFAULT 0,
    files_added     INTEGER NOT NULL DEFAULT 0,
    files_modified  INTEGER NOT NULL DEFAULT 0,
    files_removed   INTEGER NOT NULL DEFAULT 0,
    files_moved     INTEGER NOT NULL DEFAULT 0,
    files_validated INTEGER NOT NULL DEFAULT 0,
    files_corrupted INTEGER NOT NULL DEFAULT 0,
    sync_errors     INTEGER NOT NULL DEFAULT 0,
    scrub_errors    INTEGER NOT NULL DEFAULT 0
);
```

---

## Architecture

See [docs/architecture.md](docs/architecture.md) for the full module breakdown.

```
cmd/bit-rot-detector/   entry point, flags, signal handling
internal/
  api/        HTTP server, REST handlers, SSE progress hub, static SPA
  config/     Environment-variable config with validation
  coordinator/ Multi-drive orchestration, disk-usage stats
  domain/     Shared pure data types (no behaviour, avoids import cycles)
  hasher/     BLAKE3 file hashing with context cancellation
  mailer/     SMTP STARTTLS unified email reports
  monitor/    IO-aware worker-count via /sys/block/<dev>/queue/rotational
  retry/      Bounded in-memory retry queue with exponential backoff
  scanner/    Syncer: walk + hash worker pool + scrub
  scheduler/  Cron-like auto-run scheduler
  settings/   Runtime settings store (disk thresholds, notification rules)
  storage/    SQLite repository with shadow-DB swap
  watcher/    fsnotify real-time file-change watcher
```

---

## Development

### Prerequisites

- Go 1.22+
- `golangci-lint` (optional, for `make lint`)

### Make targets

```bash
make build          # static binary → ./bin/bit-rot-detector
make test           # go test -v -race ./...
make test-cover     # tests + HTML coverage report (coverage.html)
make vet            # go vet ./...
make lint           # golangci-lint run ./...
make fmt            # gofmt in-place
make tidy           # go mod tidy && verify
make clean          # remove ./bin/ and coverage artefacts
make dev            # build + start web UI at :8080 (loads .env)
make run ARGS="..." # go run ./cmd/... with extra flags
```

Tests are fully self-contained (`t.TempDir()` isolation). No external services are
needed; the mailer tests spin up a local in-process fake SMTP listener.

---

## CI / CD

| Workflow | Trigger | What it does |
|---|---|---|
| `validate.yml` | Every push / PR | `go vet`, `go test -race`, coverage upload, `golangci-lint` |
| `docker.yml` | Push to main/tags | Multi-arch image (amd64 + arm64) pushed to GHCR |
| `agent-on-issue.yml` | New issue opened | Assigns GitHub Copilot coding agent to the issue |

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `canary missing` | `.bitrot-canary` not found | `touch /path/.bitrot-canary` |
| `TARGET_DIRECTORY not set` | Missing env var | Set `TARGET_DIRECTORY=/path/to/dir` |
| `target directory does not exist` | Path typo or unmounted drive | Check path and mount status |
| `SMTP_PORT must be a valid port number` | Non-numeric or out-of-range | Set `SMTP_PORT=587` |
| `server does not support STARTTLS` | No STARTTLS but credentials present | Use a different SMTP server or clear credentials |
| Drive cards show `—` values | No sync/scrub completed yet | Trigger a sync via UI or CLI |
| Shadow DB left on disk after crash | Normal; cleaned up on next startup | No action needed |
| `an operation is already in progress` (409) | Concurrent trigger | Wait for the current operation to complete |

---

## Operational Notes

See [docs/ops.md](docs/ops.md) for deployment, cron/systemd examples, and backup procedures.

---

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

---

## License

MIT — see [LICENSE](LICENSE).

