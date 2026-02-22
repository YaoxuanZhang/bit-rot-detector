# Bit Rot Detector

A production-grade Go utility that detects silent file corruption (bit rot) using
[BLAKE3](https://github.com/BLAKE3-team/BLAKE3) hashing, an atomic shadow-database
swap for crash safety, and an IO-aware worker pool that adapts to both HDDs and SSDs.

---

## Features

| Feature | Detail |
|---|---|
| **BLAKE3 hashing** | Fast, cryptographically secure file integrity verification |
| **Atomic Shadow-DB swap** | Crash-safe SQLite: production DB stays untouched until 100 % success |
| **IO-aware concurrency** | 1 worker on HDD (no head thrashing); `NumCPU` workers on SSD/NVMe |
| **Multi-drive support** | Concurrent processing of multiple mount points |
| **Intelligent move detection** | Relocates files by size+mtime match, preserves scrub history |
| **Canary protection** | Aborts if `.bitrot-canary` is missing (unmounted drive guard) |
| **DB checksum verification** | BLAKE3 digest of `bitrot.db` stored in canary; mismatch aborts |
| **Scrub scheduling** | Configurable percentage + daily / weekly / monthly frequency |
| **Structured JSON logging** | `slog`-based, level-filtered, writes to stderr |
| **SMTP notifications** | Unified end-of-run report with drive health, sync + scrub stats |
| **Static binary** | `CGO_ENABLED=0`; no runtime dependencies |

---

## Quick Start

### Build from source

```bash
# Requires Go 1.21+
make build                   # → ./bin/bit-rot-detector
```

### Docker

```bash
# Run with Docker Compose (edit docker-compose.yml first)
docker compose up
```

### Pull from GHCR

```bash
docker pull ghcr.io/yaoxuanzhang/bit-rot-detector:latest
```

---

## Usage

```text
bit-rot-detector [flags]

Flags:
  -sync         run sync operation only
  -scrub        run scrub operation only
  -test-email   send a test email and exit
```

Without flags, both sync and scrub run in sequence.

### Prerequisites

1. **Canary file** — create a marker file in every monitored directory:

   ```bash
   touch /path/to/drive/.bitrot-canary
   ```

   The tool refuses to run if the canary is absent.  This protects against
   an unmounted drive being treated as "all files deleted".

2. **Configuration** — copy `.env.example` to `.env` and fill in values, or
   export the variables directly.

### Examples

```bash
# Sync only (record new / modified / moved / deleted files)
TARGET_DIRECTORY=/mnt/nas bit-rot-detector -sync

# Full run: sync then scrub 1 % of files
TARGET_DIRECTORY=/mnt/nas bit-rot-detector

# Scrub 100 % of files
TARGET_DIRECTORY=/mnt/nas SCRUB_PERCENTAGE=100 bit-rot-detector -scrub

# Monitor two drives
TARGET_DIRECTORY=/mnt/drive1,/mnt/drive2 bit-rot-detector

# Verify SMTP settings
TARGET_DIRECTORY=/mnt/nas bit-rot-detector -test-email
```

---

## Configuration

All settings are read from environment variables (`.env` is auto-loaded from the
working directory if it exists; environment variables always take precedence).

| Variable | Default | Description |
|---|---|---|
| `TARGET_DIRECTORY` | *(required)* | Comma-separated list of absolute paths to monitor |
| `SCRUB_PERCENTAGE` | `1.0` | Fraction of files to re-verify per run (0.1 – 100.0) |
| `SCRUB_FREQUENCY` | `daily` | Age filter for scrub selection: `daily`, `weekly`, `monthly` |
| `MAX_WORKERS` | `4` | Upper bound on hashing goroutines (IO-aware detection may lower this) |
| `LOG_LEVEL` | `INFO` | Minimum log severity: `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `LOG_RETENTION_DAYS` | `7` | Days to retain old log files |
| `SMTP_HOST` | `mail.smtp2go.com` | SMTP server hostname |
| `SMTP_PORT` | `587` | SMTP server port |
| `SMTP_USERNAME` | | SMTP authentication username |
| `SMTP_PASSWORD` | | SMTP authentication password |
| `SMTP_SENDER` | | Envelope sender address |
| `SMTP_RECIPIENT` | | Envelope recipient address |
| `NOTIFY_ON_SUCCESS` | `true` | Send email on clean runs (failure/corruption emails are always sent) |

See [`.env.example`](.env.example) for a fully annotated template.

---

## How It Works

### Sync phase

1. **Canary pre-check** — confirm `.bitrot-canary` exists; read stored DB checksum.
2. **DB integrity check** — compare BLAKE3 digest of `bitrot.db` with the value
   stored in the canary; abort on mismatch.
3. **Shadow DB open** — copy `bitrot.db` to `bitrot.db.shadow`; all writes target
   the shadow.
4. **Directory walk** — recursive walker sends `WorkItem`s into a buffered channel.
5. **Worker pool** — N goroutines read from the channel, hash each file with BLAKE3,
   and send `WorkResult`s back.
6. **Collector** — compares results against stored records; classifies each file as
   *new*, *modified*, *moved*, or *unchanged*.
7. **Deletion sweep** — records for files no longer on disk are removed.
8. **Canary post-check** — confirm the canary still exists (drive may have
   unmounted during the scan).
9. **Shadow DB commit** — `os.Rename(shadow → prod)` atomically replaces the
   production database.
10. **Canary update** — new BLAKE3 digest of `bitrot.db` is written to the canary.

### Scrub phase

Re-hashes a configurable percentage of previously recorded files (oldest-scrubbed
first).  A hash mismatch is reported as a **BIT ROT DETECTED** event and included
in the notification email.

The `SCRUB_FREQUENCY` setting adds an age filter so that, for example, with
`SCRUB_FREQUENCY=weekly` only files not scrubbed in the last 7 days are eligible,
even if `SCRUB_PERCENTAGE=100`.

### IO-aware worker pool

On Linux, the tool reads `/sys/block/<dev>/queue/rotational` to detect the drive
type:

| Drive type | Workers |
|---|---|
| HDD (rotational) | 1 (prevents seek thrashing) |
| SSD / NVMe | `runtime.NumCPU()` |
| Unknown / non-Linux | `runtime.NumCPU()` |

The `MAX_WORKERS` setting is an upper bound; the IO-aware detection may reduce
the count further.

### Crash safety

If the process is interrupted at any point before the final `os.Rename`, the
shadow file is deleted on the next startup and the production database remains the
"Last Known Good" state.  No partial writes ever reach `bitrot.db`.

---

## Architecture

```
cmd/bit-rot-detector/
  main.go             signal.NotifyContext graceful shutdown, slog setup

internal/
  domain/
    interfaces.go     Hasher + Repository interfaces
    models.go         FileRecord, SyncResult, ScrubResult, DriveHealth, WorkItem/Result

  hasher/
    hasher.go         BLAKE3 via zeebo/blake3; context-cancellation aware

  storage/
    repository.go     modernc.org/sqlite (CGO-free); shadow-DB swap logic

  scanner/
    scanner.go        Syncer: SyncDirectory + ScrubFiles (producer-consumer pool)

  monitor/
    drive.go          IO-aware detection via /sys/block/<dev>/queue/rotational

  mailer/
    mailer.go         SMTP STARTTLS; unified end-of-run report builder

  config/
    config.go         Environment-variable config with validation

  coordinator/
    coordinator.go    Multi-drive orchestration; per-drive goroutines
    statfs_unix.go    disk-usage via syscall.Statfs (Linux/macOS)
    statfs_windows.go disk-usage via GetDiskFreeSpaceEx (Windows)
```

---

## Development

### Prerequisites

- Go 1.21+
- `golangci-lint` (optional, for `make lint`)

### Common tasks

```bash
make build          # compile static binary → ./bin/bit-rot-detector
make test           # go test -v -race ./...
make test-cover     # tests + coverage report → coverage.html
make vet            # go vet ./...
make lint           # golangci-lint run ./...
make fmt            # gofmt in-place
make tidy           # go mod tidy && go mod verify
make clean          # remove ./bin/ and coverage artefacts
```

### Running tests

```bash
go test -v -race ./...
```

Tests are fully self-contained and use `t.TempDir()` for isolation; no external
services are required.  The mailer tests spin up a local in-process fake SMTP
listener.

---

## CI / CD

| Workflow | Trigger | What it does |
|---|---|---|
| `validate.yml` | Every push / PR | `go vet`, `go test -race`, coverage upload, `golangci-lint` |
| `docker.yml` | Push to main/master, version tags | Builds multi-arch image and pushes to GHCR |

### GHCR image tags

| Event | Tags produced |
|---|---|
| `v1.2.3` release | `1.2.3` · `1.2` · `1` · `latest` · `sha-<hash>` |
| `v1.2.3-beta.1` pre-release | `1.2.3-beta.1` · `1.2` · `sha-<hash>` |
| Push to `main` | `main` · `sha-<hash>` |
| Pull request | Build-only validation (no push) |

`latest` and rolling major tags are suppressed for pre-releases and `v0.x`.

---

## Database Schema

A `bitrot.db` SQLite file is stored in the root of each monitored directory.

```sql
CREATE TABLE files (
    abs_path      TEXT PRIMARY KEY,
    hash          TEXT    NOT NULL,
    added_at      TEXT    NOT NULL,
    last_seen     TEXT    NOT NULL,
    last_scrubbed TEXT,
    scrub_count   INTEGER NOT NULL DEFAULT 0,
    file_size     INTEGER NOT NULL,
    mtime         REAL    NOT NULL
);
```

---

## Cron example

```cron
# Daily sync + scrub at 02:00
0 2 * * * TARGET_DIRECTORY=/mnt/nas /usr/local/bin/bit-rot-detector
```

---

## License

MIT — see [LICENSE](LICENSE).

