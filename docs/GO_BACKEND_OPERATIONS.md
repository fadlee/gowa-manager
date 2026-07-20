# Go Backend Operations Runbook

Normal-operation guide for running the **Go** GOWA Manager backend in
production. This runbook is written for **operators** (ops engineers, SREs,
on-call), not developers.

> **Before you start:** Read [GO_BACKEND_CANARY.md](GO_BACKEND_CANARY.md) for
> the cutover procedure from Bun to Go, and [GO_BACKEND_ROLLBACK.md](GO_BACKEND_ROLLBACK.md)
> for the rollback procedure. The Go backend must only be promoted to
> production via the canary sequence — never start Go against a live data
> directory that Bun is still managing.

---

## 1. CLI and Environment Compatibility

The Go backend is CLI- and env-compatible with the Bun backend for the
operational flags below. Bun-only flags (e.g. `--watch`, dev-mode flags) are
**not** supported by the Go binary.

### CLI Flags

| Flag | Go | Bun | Description |
|------|----|----|-------------|
| `-p, --port <port>` | ✅ | ✅ | Server port (default: `3000`) |
| `--host <addr>` | ✅ | ❌ | Bind address (default: `127.0.0.1`; use `0.0.0.0` for Docker) |
| `-u, --admin-username <user>` | ✅ | ✅ | Admin username (default: `admin`) |
| `-P, --admin-password <pass>` | ✅ | ✅ | Admin password (default: `password`) |
| `-d, --data-dir <path>` | ✅ | ✅ | Data directory (default: `./data`) |
| `-h, --help` | ✅ | ✅ | Show help |
| `-v, --version` | ✅ | ✅ | Show version |
| `--watch` | ❌ | ✅ | Dev-only, not in Go binary |

> **Note:** The Go binary accepts `--host` as a first-class flag. Bun does not
> expose `--host` (it binds via the Elysia listen address). For Docker, the Go
> image sets `HOST=0.0.0.0` automatically.

### Environment Variables

| Variable | Go | Bun | Description |
|----------|----|----|-------------|
| `PORT` | ✅ | ✅ | Server port |
| `HOST` | ✅ | ❌ | Bind address (default: `127.0.0.1`) |
| `ADMIN_USERNAME` | ✅ | ✅ | Admin username |
| `ADMIN_PASSWORD` | ✅ | ✅ | Admin password |
| `DATA_DIR` | ✅ | ✅ | Data directory |
| `GOWA_METRICS_ENABLED` | ✅ | ❌ | Enable `/metrics` endpoint (`0`/`1`, default: `0`) |
| `CORS_ALLOWED_ORIGINS` | ❌ | ✅ | Bun-only CORS config |
| `PROXY_WS_INJECT_INSTANCE_AUTH` | ❌ | ✅ | Bun-only WebSocket auth injection |

**Precedence:** CLI flags override environment variables. Environment variables
override defaults.

### Go-only: Metrics Endpoint

The Go backend exposes an opt-in Prometheus metrics endpoint:

```bash
GOWA_METRICS_ENABLED=1 ./gowa-manager-go
```

- Endpoint: `GET /metrics`
- **Loopback-only** — bound to `127.0.0.1` even when `HOST=0.0.0.0`.
- Not available on the Bun backend.

---

## 2. Data Directory Ownership and Permissions

The data directory (`DATA_DIR`) holds the SQLite database, the manager lock
file, installed GOWA binaries, and instance-specific data.

```
{DATA_DIR}/
├── gowa.db                  # SQLite database
├── .gowa-manager.lock       # Manager lock file (flock)
├── bin/                     # Installed GOWA binaries
│   └── versions/            # Version-specific binaries
└── instances/               # Instance-specific data
```

### Critical Rules

1. **One manager at a time.** Only one manager process (Bun **or** Go) may own
   a given `DATA_DIR` at any time. The lock file (`.gowa-manager.lock`)
   enforces this via `flock`. Starting a second manager against the same
   directory will fail.
2. **Bun and Go never share a live data directory.** During cutover, Bun is
   stopped and its child processes are allowed to stabilize before Go starts
   against the same directory. See [GO_BACKEND_CANARY.md](GO_BACKEND_CANARY.md).
3. **Ownership.** The process user must own (or have read/write/execute
   permissions on) the entire `DATA_DIR` tree.
4. **Permissions.** Restrict to the service user. The database and instance
   configs may contain sensitive data.

### Linux

```bash
# Create the data directory owned by the service user
sudo mkdir -p /var/lib/gowa-manager/data
sudo chown -R gowa:gowa /var/lib/gowa-manager/data
sudo chmod 750 /var/lib/gowa-manager/data
```

### Windows

The service account (typically `LocalSystem` or a dedicated service user)
must have full control over the data directory. Grant explicit permissions if
using a dedicated account:

```powershell
icacls "C:\ProgramData\gowa-manager\data" /grant "GOWAService:(OI)(CI)F" /T
```

---

## 3. Linux: systemd Service Example

Create `/etc/systemd/system/gowa-manager.service`:

```ini
[Unit]
Description=GOWA Manager (Go backend)
After=network.target
Wants=network.target

[Service]
Type=simple
User=gowa
Group=gowa

# Binary path — adjust to your install location
ExecStart=/usr/local/bin/gowa-manager-go \
  --data-dir /var/lib/gowa-manager/data \
  --port 3000 \
  --host 127.0.0.1 \
  --admin-username admin \
  --admin-password ${ADMIN_PASSWORD}

# Read credentials from an environment file
EnvironmentFile=/etc/gowa-manager/gowa-manager.env

# Graceful shutdown: SIGTERM, wait up to 30s, then SIGKILL
KillSignal=SIGTERM
TimeoutStopSec=30
Restart=on-failure
RestartSec=5

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/gowa-manager/data
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Create the environment file `/etc/gowa-manager/gowa-manager.env` (mode `600`,
owned by root):

```env
ADMIN_PASSWORD=change-me-to-a-strong-password
GOWA_METRICS_ENABLED=1
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable gowa-manager
sudo systemctl start gowa-manager
sudo systemctl status gowa-manager
```

View logs:

```bash
journalctl -u gowa-manager -f
```

---

## 4. Docker Container Example

The Go backend ships two Dockerfiles:

- `Dockerfile` — multi-stage build (Bun frontend → Go builder → Alpine runtime).
- `Dockerfile.prebuilt` — uses prebuilt Go binaries from `./binaries/`.

Both produce an Alpine-based image with `ffmpeg`, `ca-certificates`, and
`tzdata`, running as a non-root `app` user with a `/data` volume.

### Build

```bash
# From source
docker build -t gowa-manager-go:candidate -f Dockerfile .

# From prebuilt binaries (place binaries in ./binaries/ first)
docker build -t gowa-manager-go:candidate -f Dockerfile.prebuilt .
```

### Run

```bash
docker run -d \
  --name gowa-manager \
  -p 3000:3000 \
  -v gowa-data:/data \
  -e ADMIN_PASSWORD=change-me \
  -e GOWA_METRICS_ENABLED=1 \
  --restart unless-stopped \
  gowa-manager-go:candidate
```

> **`HOST=0.0.0.0` is required** for Docker port mapping. The image sets this
> by default. If you override `HOST`, use `0.0.0.0` so the container accepts
> connections forwarded by Docker.

### docker-compose.yml

```yaml
version: "3.8"
services:
  gowa-manager:
    image: gowa-manager-go:candidate
    ports:
      - "3000:3000"
    volumes:
      - gowa-data:/data
    environment:
      ADMIN_PASSWORD: ${ADMIN_PASSWORD}
      GOWA_METRICS_ENABLED: "1"
    restart: unless-stopped

volumes:
  gowa-data:
```

### Inspecting a Running Container

```bash
docker logs -f gowa-manager
docker exec gowa-manager wget -qO- http://localhost:3000/api/health
docker exec gowa-manager wget -qO- http://localhost:3000/api/ready
```

---

## 5. Windows: Service / Scheduled Task Guidance

The Go binary is a plain console executable (`gowa-manager-go-windows-amd64.exe`).
It does not register itself as a Windows Service natively. Use a service
wrapper.

### Option A: NSSM (Non-Sucking Service Manager)

```powershell
# Download nssm from https://nssm.cc/
nssm install GOWAManager "C:\Program Files\gowa-manager\gowa-manager-go-windows-amd64.exe"
nssm set GOWAManager AppParameters "--data-dir C:\ProgramData\gowa-manager\data --port 3000 --admin-username admin --admin-password change-me"
nssm set GOWAManager AppDirectory "C:\Program Files\gowa-manager"
nssm set GOWAManager AppStdout "C:\ProgramData\gowa-manager\logs\stdout.log"
nssm set GOWAManager AppStderr "C:\ProgramData\gowa-manager\logs\stderr.log"
nssm set GOWAManager AppRotateFiles 1
nssm set GOWAManager AppRotateBytes 10485760
nssm start GOWAManager
```

### Option B: sc.exe with a service wrapper

If using `sc.exe` directly, you need a wrapper (like `winsw`) because the Go
binary does not implement the Windows Service Control interface. Follow the
wrapper's documentation to wrap `gowa-manager-go-windows-amd64.exe`.

### Option C: Scheduled Task (for non-service deployments)

```powershell
$action = New-ScheduledTaskAction `
  -Execute "C:\Program Files\gowa-manager\gowa-manager-go-windows-amd64.exe" `
  -Argument "--data-dir C:\ProgramData\gowa-manager\data --port 3000"

$trigger = New-ScheduledTaskTrigger -AtStartup

$settings = New-ScheduledTaskSettingsSet `
  -AllowStartIfOnBatteries `
  -DontStopIfGoingOnBatteries `
  -RestartCount 3 `
  -RestartInterval (New-TimeSpan -Minutes 1)

Register-ScheduledTask -TaskName "GOWAManager" `
  -Action $action -Trigger $trigger -Settings $settings `
  -RunLevel Highest -User "SYSTEM"
```

> A scheduled task does not provide automatic restart on crash the way a
> service does. Prefer NSSM or a service wrapper for production.

---

## 6. Logs, Metrics, and Readiness Monitoring

### HTTP Health and Readiness Endpoints

| Endpoint | Auth | Description |
|----------|------|-------------|
| `GET /api/health` | None | `200` when the process is alive |
| `GET /api/ready` | None | `200` when ready; `503` during startup/shutdown |
| `GET /metrics` | None (loopback-only) | Prometheus metrics (requires `GOWA_METRICS_ENABLED=1`) |
| `GET /api/instances` | Basic Auth | List instances |
| `GET /api/system/status` | Basic Auth | System status |
| `GET /api/system/versions/installed` | Basic Auth | Installed GOWA versions |
| `GET /api/system/auto-update/status` | Basic Auth | Auto-update status |

### Checking Health

```bash
# Liveness — is the process up?
curl -fsS http://localhost:3000/api/health && echo "alive"

# Readiness — is it ready to serve traffic?
curl -fsS http://localhost:3000/api/ready && echo "ready"
```

Use `/api/ready` (not `/api/health`) for load-balancer health checks. During
graceful shutdown, `/api/ready` returns `503` so traffic drains before the
process exits.

### Metrics (Prometheus)

Enable with `GOWA_METRICS_ENABLED=1`. Scrape from localhost:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: gowa-manager
    static_configs:
      - targets: ['localhost:3000']
    metrics_path: /metrics
```

> `/metrics` is **loopback-only**. Even when `HOST=0.0.0.0`, the metrics
> endpoint only answers on `127.0.0.1`. Use a local Prometheus agent or a
> sidecar to scrape it.

### Logs

The Go backend writes structured logs (`slog` text format) to **stderr**. There
is no log file — capture stderr via your process manager:

- **systemd:** `journalctl -u gowa-manager -f`
- **Docker:** `docker logs -f gowa-manager`
- **NSSM:** configured log files (see above)
- **Scheduled Task:** redirect output in the task action

### Load Balancer Health Check

Configure your load balancer to hit `GET /api/ready`:

- Interval: 5–10s
- Timeout: 2s
- Healthy: HTTP 200
- Unhealthy: HTTP 503 or connection refused

---

## 7. Backup Cadence

The ops backup script (`scripts/ops/backup.sh` / `backup.ps1`) creates a
consistent snapshot of the SQLite database and data-directory metadata with a
SHA-256 manifest.

### Recommended Cadence

| Environment | Cadence | Retention |
|-------------|---------|-----------|
| Production | Daily + before any cutover | 30 days or 10 backups, whichever is more |
| Staging | Before each cutover dry run | 7 days |
| Development | On demand | As needed |

### Scheduled Backup (Linux cron)

```cron
# Daily at 03:00
0 3 * * *  gowa  /opt/gowa-manager/scripts/ops/backup.sh \
  --data-dir /var/lib/gowa-manager/data \
  --backup-dir /var/backups/gowa-manager \
  >> /var/log/gowa-backup.log 2>&1
```

### Scheduled Backup (Windows Task Scheduler)

```powershell
$action = New-ScheduledTaskAction `
  -Execute "powershell.exe" `
  -Argument "-NoProfile -File C:\opt\gowa-manager\scripts\ops\backup.ps1 -DataDir C:\ProgramData\gowa-manager\data -BackupDir C:\backups\gowa-manager"

$trigger = New-ScheduledTaskTrigger -Daily -At 3am

Register-ScheduledTask -TaskName "GOWABackup" `
  -Action $action -Trigger $trigger -User "SYSTEM" -RunLevel Highest
```

### Verifying a Backup

```bash
# Linux
sh scripts/ops/backup.sh --verify --backup-dir /var/backups/gowa-manager/<timestamp>
```

```powershell
# Windows
powershell -File scripts\ops\backup.ps1 -Verify -BackupDir C:\backups\gowa-manager\<timestamp>
```

The `--verify` flag re-reads the manifest and re-hashes every file. Exit
non-zero on any mismatch.

> **Important:** The backup script does **not** stop the manager. For
> pre-cutover backups, stop Bun first and let child processes stabilize (see
> the canary runbook). For routine backups while Go is running, the script
> uses SQLite's online backup API for WAL-mode databases, so it is safe to
> run against a live Go manager.

---

## 8. Troubleshooting

### Manager won't start

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| `address already in use` | Another process on the port | `lsof -i :3000` / `netstat -ano \| findstr 3000`; stop the conflicting process or change `--port` |
| `lock file held` / `flock` error | Another manager owns the data dir | Stop the other manager; do **not** delete the lock file while a manager is running |
| `permission denied` on data dir | Wrong user/permissions | `chown` / `chmod` / `icacls` the data directory (see §2) |
| `unknown option` | Bun-only flag passed to Go | Remove `--watch` or other Bun-only flags; check §1 |
| Binary not executable | Missing `+x` on Linux | `chmod +x gowa-manager-go` |

### `/api/ready` returns 503

- The manager is still starting up (instance recovery in progress). Wait.
- The manager is shutting down (SIGTERM received). Let it drain.
- If it stays 503 indefinitely, check logs for startup errors.

### Instances not starting after restart

1. Check `/api/system/status` for recovery state.
2. Check logs for per-instance errors.
3. Verify GOWA binaries exist in `{DATA_DIR}/bin/versions/`.
4. If recovery failed, this is a **rollback trigger** — see
   [GO_BACKEND_ROLLBACK.md](GO_BACKEND_ROLLBACK.md).

### Proxy not working (HTTP or WebSocket)

1. Verify the instance is running: `GET /api/instances`.
2. Check the proxy path: `/app/{instanceKey}/...`.
3. Check instance port allocation in the database.
4. If proxy is broken after a Go cutover, this is a **rollback trigger**.

### Auth not working (login/logout broken)

1. Verify `ADMIN_USERNAME` / `ADMIN_PASSWORD` are set correctly.
2. Test with curl:
   ```bash
   curl -u admin:password http://localhost:3000/api/instances
   ```
3. If auth is broken after a Go cutover, this is a **rollback trigger**.

### Database issues

```bash
# Check SQLite integrity (requires sqlite3 CLI)
sqlite3 /var/lib/gowa-manager/data/gowa.db "PRAGMA integrity_check;"

# If integrity fails, this is a rollback trigger — do not continue running Go.
```

### Disk full

1. Check free space: `df -h` / `Get-PSDrive`.
2. Clean old GOWA versions: `POST /api/system/versions/cleanup {"keepCount": 3}`.
3. Clean old backups (outside the data directory).

### Memory growing unbounded

- Monitor RSS: `ps -o rss -p <pid>` / `Get-Process gowa-manager | Select WS`.
- If RSS grows without bound, this is a **rollback trigger** (memory leak).

---

## 9. Related Documents

- [GO_BACKEND_CANARY.md](GO_BACKEND_CANARY.md) — Cutover from Bun to Go
- [GO_BACKEND_ROLLBACK.md](GO_BACKEND_ROLLBACK.md) — Rollback from Go to Bun
- [INTEGRATED_SETUP.md](INTEGRATED_SETUP.md) — Development setup
- [VERSION_MANAGEMENT.md](VERSION_MANAGEMENT.md) — GOWA version management
