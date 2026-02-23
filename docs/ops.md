# Operations Guide

This document covers deployment, scheduling, monitoring, log management, and
backup/restore procedures for `bit-rot-detector`.

---

## Deployment

### Bare Metal (Recommended for NAS/Server)

1. **Build a static binary**

   ```bash
   make build
   sudo cp bin/bit-rot-detector /usr/local/bin/
   ```

2. **Create the canary file** in every monitored directory:

   ```bash
   sudo touch /mnt/nas/.bitrot-canary
   sudo touch /mnt/backup/.bitrot-canary
   ```

3. **Create a configuration file** at `/etc/bit-rot-detector.env`:

   ```bash
   TARGET_DIRECTORY=/mnt/nas,/mnt/backup
   SCRUB_PERCENTAGE=1.0
   SCRUB_FREQUENCY=daily
   MAX_WORKERS=4
   LOG_LEVEL=INFO
   LOG_RETENTION_DAYS=14
   SMTP_HOST=mail.smtp2go.com
   SMTP_PORT=587
   SMTP_USERNAME=user@example.com
   SMTP_PASSWORD=secret
   SMTP_SENDER=nas@example.com
   SMTP_RECIPIENT=admin@example.com
   NOTIFY_ON_SUCCESS=false
   ```

   Restrict permissions:
   ```bash
   sudo chmod 600 /etc/bit-rot-detector.env
   ```

4. **First run** to seed the database:

   ```bash
   sudo env $(cat /etc/bit-rot-detector.env | xargs) bit-rot-detector -sync
   ```

---

### Web UI Mode (Daemon)

To run the web UI as a background service, use a systemd unit (see below) with the
`-web` flag.

---

### Docker

```bash
# 1. Copy environment file
cp .env.example .env
# edit .env with your settings

# 2. Create canary
touch /path/to/data/.bitrot-canary

# 3. Run once (sync + scrub)
docker compose run --rm bit-rot-detector

# 4. Run sync only
docker compose run --rm bit-rot-detector -sync

# 5. Run scrub only
docker compose run --rm bit-rot-detector -scrub

# 6. Start web UI (bind port 8080)
docker run -d \
  --env-file .env \
  -v /path/to/data:/path/to/data \
  -p 8080:8080 \
  ghcr.io/yaoxuanzhang/bit-rot-detector:latest \
  -web -addr :8080
```

The official multi-arch image (`linux/amd64`, `linux/arm64`) is available at:

```
ghcr.io/yaoxuanzhang/bit-rot-detector:latest
```

---

## Scheduling

### cron

```cron
# Run sync + scrub daily at 02:00
0 2 * * * root env $(cat /etc/bit-rot-detector.env | xargs) /usr/local/bin/bit-rot-detector

# Run sync only every hour
0 * * * * root env $(cat /etc/bit-rot-detector.env | xargs) /usr/local/bin/bit-rot-detector -sync

# Run a full 100% scrub every Sunday at 03:00
0 3 * * 0  root env $(cat /etc/bit-rot-detector.env | xargs) SCRUB_PERCENTAGE=100 /usr/local/bin/bit-rot-detector -scrub
```

### systemd Service (CLI mode)

Create `/etc/systemd/system/bit-rot-detector.service`:

```ini
[Unit]
Description=Bit Rot Detector (daily run)
After=network.target local-fs.target

[Service]
Type=oneshot
EnvironmentFile=/etc/bit-rot-detector.env
ExecStart=/usr/local/bin/bit-rot-detector
StandardOutput=journal
StandardError=journal
```

Create `/etc/systemd/system/bit-rot-detector.timer`:

```ini
[Unit]
Description=Run Bit Rot Detector daily at 02:00

[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

Enable:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now bit-rot-detector.timer
sudo systemctl list-timers bit-rot-detector.timer
```

### systemd Service (Web UI / Daemon mode)

Create `/etc/systemd/system/bit-rot-detector-web.service`:

```ini
[Unit]
Description=Bit Rot Detector Web UI
After=network.target local-fs.target

[Service]
Type=simple
EnvironmentFile=/etc/bit-rot-detector.env
ExecStart=/usr/local/bin/bit-rot-detector -web -addr :8080
Restart=on-failure
RestartSec=10s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now bit-rot-detector-web.service
```

### In-App Schedule (Web UI)

The Settings → Schedule card allows setting `@daily`, `@weekly`, `@hourly`, or
`HH:MM` auto-run entries directly in the UI (backed by `POST /api/schedule`).
The scheduler runner fires within 60 seconds of an entry becoming due.

> **Note:** The in-app scheduler runs only while the web process is alive. For
> guaranteed execution use cron or systemd timers instead.

---

## Logging

The application logs structured JSON to **stderr** via `log/slog`.

| Log level | When to use |
|---|---|
| `DEBUG` | Verbose per-file hashing events; useful for diagnosing slow runs |
| `INFO` | Normal operation (run start/end, counts, canary check) — default |
| `WARN` | Non-fatal issues (SMTP send failed, permission denied on individual file) |
| `ERROR` | Fatal configuration errors, DB commit failures |

Set `LOG_LEVEL=DEBUG` for detailed output. Filter with `journalctl`:

```bash
journalctl -u bit-rot-detector-web.service -f
journalctl -u bit-rot-detector-web.service --since "2024-01-15" | grep ERROR
```

### Log retention

`LOG_RETENTION_DAYS` (default 7) controls how many days of log files are kept.
The application itself does **not** write to disk log files; this setting is passed
through to any external log-management layer. When using systemd, configure
`journald` retention separately:

```ini
# /etc/systemd/journald.conf
[Journal]
MaxRetentionSec=2week
```

---

## Database Management

### Location

`bitrot.db` is stored in the **root** of each monitored directory, alongside
`.bitrot-canary`.

```
/mnt/nas/
├── .bitrot-canary
├── bitrot.db
└── ... (your files)
```

### Backup

Back up both `bitrot.db` and `.bitrot-canary` as a pair:

```bash
# Simple copy
cp /mnt/nas/bitrot.db     /backup/nas-bitrot-$(date +%Y%m%d).db
cp /mnt/nas/.bitrot-canary /backup/nas-canary-$(date +%Y%m%d)

# Via rsync (include hidden files)
rsync -a --include=".bitrot-canary" --include="bitrot.db" --exclude="*" \
  /mnt/nas/ /backup/nas-metadata/
```

SQLite's DELETE journal mode means there are no `-wal` or `-shm` sidecar files to
worry about; a simple `cp` is safe when no operation is running.

If an operation is running, use SQLite's online backup:

```bash
sqlite3 /mnt/nas/bitrot.db ".backup /backup/nas-bitrot.db"
```

### Restore

1. Stop the service or ensure no operation is in progress.
2. Copy the backup files back:

   ```bash
   cp /backup/nas-bitrot-20240115.db /mnt/nas/bitrot.db
   cp /backup/nas-canary-20240115    /mnt/nas/.bitrot-canary
   ```

3. Restart the service. The next sync run will detect changes since the backup date.

### Resetting the database

To start fresh (e.g., after a major directory reorganisation):

```bash
# 1. Stop the service.
# 2. Remove the DB and canary.
rm /mnt/nas/bitrot.db /mnt/nas/.bitrot-canary
# 3. Recreate the canary.
touch /mnt/nas/.bitrot-canary
# 4. Run a sync to rebuild from scratch.
bit-rot-detector -sync
```

All files will be treated as new additions on the next run.

---

## Shadow DB Behaviour

A `bitrot.db.shadow` file appears in the monitored directory during an active
operation. If you see this file when no operation should be running, it indicates a
previous run was interrupted before committing.

**Action:** The shadow file is automatically cleaned up on the next startup. You can
also delete it manually — `bitrot.db` is unaffected:

```bash
rm /mnt/nas/bitrot.db.shadow
```

---

## Canary File

The `.bitrot-canary` file serves two purposes:

1. **Presence check** — confirms the target directory is mounted and accessible.
   A missing canary aborts the run entirely, preventing false "all files deleted"
   records when a drive is unmounted.

2. **DB checksum** — stores the BLAKE3 digest of `bitrot.db`. On each run,
   the digest is compared before opening the shadow DB. A mismatch logs a warning
   (it does not abort the run) and the canary is updated after a successful commit.

The canary is a plain text file. Its content is a single hex-encoded BLAKE3 digest:

```
a3f8c2...   ← BLAKE3(bitrot.db)
```

---

## Security Considerations

- The binary runs as **UID 65534** (`nobody`) in the Docker image. Ensure the target
  directory is readable and writable by that UID, or override with `user: "0:0"` in
  `docker-compose.yml`.
- SMTP credentials are stored in the `.env` file. Restrict permissions:
  `chmod 600 .env`.
- The web UI has **no authentication**. Bind to `127.0.0.1` and use a reverse proxy
  (nginx, Caddy) with authentication if the UI must be exposed externally.
- The REST API accepts unauthenticated `POST` requests on any interface. For
  production use, restrict access at the network or proxy layer.

---

## Troubleshooting

### `canary missing: /mnt/nas/.bitrot-canary`

The canary was not found. Either the drive is not mounted or the file was accidentally
deleted.

```bash
# Check if mounted
mount | grep /mnt/nas

# Re-create canary (only if drive is mounted and DB is intact)
touch /mnt/nas/.bitrot-canary
```

### Shadow DB left on disk after restart

Normal recovery path. Delete the shadow manually if it persists:

```bash
rm /mnt/nas/bitrot.db.shadow
```

### High CPU usage (HDD)

Verify the IO-aware detection is working. On Linux:

```bash
cat /sys/block/sda/queue/rotational  # 1 = HDD, 0 = SSD
```

If the device path is resolved incorrectly, override by setting `MAX_WORKERS=1`
manually.

### SMTP `server does not support STARTTLS`

The mailer requires STARTTLS. Use a different SMTP relay or port. Port 587 is the
standard STARTTLS submission port.

### Web UI shows stale data

The UI refreshes on SSE `done` events and on manual Refresh button click. If SSE
connectivity is broken (proxy buffering, firewall), use the Refresh button or reload
the page.

Ensure the proxy sets:
```
X-Accel-Buffering: no
proxy_buffering: off   # nginx
```
