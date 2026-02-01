# Bit Rot Detector

Production-grade utility for detecting file corruption (bit rot) using BLAKE3 hashing with intelligent move detection and SMTP2GO notifications.

## Features

- **BLAKE3 Hashing**: Fast, cryptographically secure file integrity verification
- **Atomic Transactions**: All database updates happen atomically at the end of successful runs
- **Intelligent Move Detection**: Detects relocated files by matching size/mtime/hash, preserving scrub history
- **Configurable Scrubbing**: Verify a configurable percentage of files at daily/weekly/monthly intervals
- **SMTP2GO Notifications**: Email alerts for new files, successful scrubs, and critical failures
- **Canary Protection**: Prevents mass-deletion logic on unmounted drives
- **Comprehensive Logging**: Structured logging to both console and file

## Installation

This project uses [uv](https://github.com/astral-sh/uv) for dependency management.

```bash
# Clone the repository
cd /path/to/bit-rot-detector

# Install dependencies with uv
uv sync

# Install development dependencies (for testing)
uv sync --extra dev
```

## Testing

The project includes a comprehensive test suite using pytest.

```bash
# Run all tests
uv run pytest

# Run with coverage report
uv run pytest --cov=src/bit_rot

# Run specific test file
uv run pytest tests/test_config.py -v

# Run with verbose output
uv run pytest -v
```

Test coverage reports are generated in `htmlcov/` directory.

## Configuration

Configuration is managed through environment variables. A template is provided:

1. Copy the example environment file:

```bash
cp .env.example .env
```

2. Edit `.env` with your settings. See [`.env.example`](.env.example) for all available options and detailed documentation.

```bash
# Target directory to monitor
TARGET_DIRECTORY=/path/to/monitor

### Email Notifications

Configure SMTP settings and notification preferences:

# SMTP Configuration
SMTP_HOST=mail.smtp2go.com
SMTP_PORT=587
SMTP_USERNAME=your_username
SMTP_PASSWORD=your_password
SMTP_SENDER=sender@example.com
SMTP_RECIPIENT=recipient@example.com

# Notification Preferences
NOTIFY_SYNC_SUCCESS=true         # Email after sync operations (shows total files synced)
NOTIFY_SCRUB_SUCCESS=false       # Email after successful scrub operations
NOTIFY_CRITICAL_FAILURES=true   # Email on critical failures (bit rot, canary)

# Scrub configuration
SCRUB_PERCENTAGE=1.0          # 0.1 to 100.0
SCRUB_FREQUENCY=daily         # daily, weekly, or monthly

# Log retention
LOG_RETENTION_DAYS=7          # Days to keep log files
```

3. Create the canary file in your target directory:

```bash
touch /path/to/monitor/.bitrot-canary
```

**Important**: The canary file prevents the tool from running if the target directory is unmounted (e.g., external drive). If the canary is missing, the tool will abort to prevent marking all files as deleted.

## Usage

### Test Email Configuration

Before running scans, verify your SMTP configuration:

```bash
uv run bit-rot-detector --test-email
```

### Run Sync Only

Scan the directory, detect new/moved/deleted files:

```bash
uv run bit-rot-detector --sync
```

### Run Scrub Only

Re-verify file hashes to detect bit rot:

```bash
uv run bit-rot-detector --scrub
```

### Run Both (Default)

Run sync followed by scrub:

```bash
uv run bit-rot-detector
```

## How It Works

### The "Sync & Scrub" Pattern

1. **Canary Check**: Verifies `.bitrot-canary` exists before any operations
2. **Phase 1 - Sync**: Walks directory tree, records file metadata (size, mtime, hash)
3. **Phase 2 - Modification Detection**: Compares size/mtime with database, re-hashes only changed files
4. **Phase 3 - Move Detection**: Identifies relocated files by matching size/mtime, then verifying hash
5. **Phase 4 - Deletion Detection**: Removes database entries for files not seen in current session
6. **Phase 5 - Scrubbing**: Re-verifies a configurable percentage of files (oldest first) to detect bit rot

### Atomic Transactions

All database updates are staged during the scan and committed atomically at the end. If the scan fails, no changes are written to the database.

### Scrub Frequency

- **daily**: Scrub any eligible files (no age filter)
- **weekly**: Only scrub files not verified in the last 7 days
- **monthly**: Only scrub files not verified in the last 30 days

This allows you to scrub a small percentage daily while ensuring all files are eventually verified.

## Cron Job Setup

For daily automated scans, add to your crontab:

```bash
# Edit crontab
crontab -e

# Add this line to run daily at 2 AM
0 2 * * * cd /path/to/bit-rot-detector && /path/to/uv run bit-rot-detector >> /var/log/bitrot-cron.log 2>&1
```

Or use a more specific schedule:

```bash
# Run sync daily at 2 AM
0 2 * * * cd /path/to/bit-rot-detector && /path/to/uv run bit-rot-detector --sync

# Run scrub weekly on Sundays at 3 AM
0 3 * * 0 cd /path/to/bit-rot-detector && /path/to/uv run bit-rot-detector --scrub
```

## Logging

All operations are logged with rotation and automatic cleanup:
- **Console**: INFO level and above
- **Log Files**: DEBUG level and above, stored in `logs/` directory
  - Format: `bitrot_YYYYMMDD_HHMMSS.log` (timestamped per run)
  - Retention: Configurable via `LOG_RETENTION_DAYS` (default: 7 days)
  - Old logs are automatically deleted on each run

Log format: `[TIMESTAMP] [LEVEL] [MODULE] - Message`

Example log files:
```
logs/bitrot_20260129_023045.log
logs/bitrot_20260129_140522.log
logs/bitrot_20260130_023001.log
```

## Database Schema

SQLite database (`bitrot.db`) with a single `files` table:

| Column | Type | Description |
|--------|------|-------------|
| abs_path | TEXT (PK) | Absolute file path |
| hash | TEXT | BLAKE3 hash |
| added_at | TEXT | ISO timestamp when first added |
| last_seen | TEXT | ISO timestamp of last sync |
| last_scrubbed | TEXT | ISO timestamp of last scrub |
| scrub_count | INTEGER | Number of times scrubbed |
| file_size | INTEGER | File size in bytes |
| mtime | REAL | File modification time (Unix timestamp) |

## Error Handling

- **Permission Errors**: Files that cannot be read are logged and skipped
- **I/O Errors**: Filesystem errors are caught and logged
- **Database Corruption**: Detected on startup with clear recovery instructions
- **Canary Missing**: Operations aborted before any database changes
- **SMTP Failures**: Email errors logged but don't stop operations

## Known Limitations

- **Symlinks**: Not followed (prevents infinite loops and tracking files outside target directory)
- **Move + Modify**: If a file is both moved and modified between scans, scrub history is lost (treated as new file)
- **Hardlinks**: Multiple hardlinks to same inode are tracked separately (inefficient but correct)
- **FAT32 Filesystems**: 2-second timestamp precision may miss some rapid modifications (size comparison provides additional protection)
- **Case-Insensitive Filesystems**: On macOS/Windows, files differing only in case may cause issues

## Notifications

Email notifications are sent based on your configuration:

- **New Files** (`NOTIFY_NEW_FILES=true`): When new files are added during sync
- **Scrub Success** (`NOTIFY_SCRUB_SUCCESS=true`): When scrub completes successfully
- **Critical Failures** (`NOTIFY_CRITICAL_FAILURES=true`): When bit rot is detected or critical errors occur

All emails include a summary statistics report.

## License

MIT License - See LICENSE file for details.
