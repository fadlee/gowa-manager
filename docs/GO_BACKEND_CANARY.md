# Go Backend Canary Cutover Runbook

Step-by-step procedure for cutting over from the **Bun** backend to the **Go**
backend in production. This runbook is written for **operators** performing a
controlled canary cutover during a maintenance window.

> **Related:** [GO_BACKEND_OPERATIONS.md](GO_BACKEND_OPERATIONS.md) (normal
> operation) · [GO_BACKEND_ROLLBACK.md](GO_BACKEND_ROLLBACK.md) (rollback)

---

## ⚠️ Critical Safety Rules

1. **Bun and Go NEVER share a live data directory.** Only one manager process
   may own a `DATA_DIR` at any time. The cutover stops Bun, lets child
   processes stabilize, then starts Go against the same directory.
2. **Never start Go while Bun is still running** against the same data
   directory. The lock file (`flock`) will prevent this, but do not rely on it
   alone — verify Bun is stopped.
3. **Always take a backup before starting Go.** If Go writes an incompatible
   database, you need the pre-cutover backup to roll back.
4. **Always run preflight before starting Go.** The preflight script checks
   the binary, data directory, port, and disk space.
5. **Always run smoke tests after starting Go.** Do not promote until smoke
   tests pass.
6. **Default to hold, not promote.** If anything is ambiguous, hold and
   observe longer. Rollback if a trigger is met.

---

## 1. Prerequisites

Before beginning the cutover, verify all of the following:

### 1.1 Artifacts Verified

- [ ] Go binary downloaded and SHA-256 verified against `checksums.txt`
- [ ] Go binary is executable (`chmod +x` on Linux)
- [ ] Go binary version recorded (`./gowa-manager-go --version`)
- [ ] Pinned Bun rollback artifact available (`gowa-manager-bun-rollback`)
  with its SHA-256 recorded
- [ ] `sqlite3` CLI available on the host (for integrity checks)

```bash
# Verify Go binary checksum
sha256sum gowa-manager-linux-amd64
# Compare against the line in checksums.txt from the release

# Record versions
./gowa-manager-go --version
# Record Bun rollback artifact path and checksum
sha256sum gowa-manager-bun-rollback
```

### 1.2 Backup Destination Ready

- [ ] Backup directory exists and is writable
- [ ] Sufficient free space (at least 2× the data directory size)
- [ ] Backup script tested in staging

### 1.3 Maintenance Window

- [ ] Maintenance window scheduled and announced
- [ ] Low-traffic period selected
- [ ] Stakeholders notified of potential downtime
- [ ] Rollback runbook open and ready

### 1.4 Operator Readiness

- [ ] Operator has access to the host (SSH / RDP / console)
- [ ] Operator knows the current Bun binary path, version, and config
- [ ] Operator knows the `DATA_DIR` path
- [ ] Operator knows the admin credentials
- [ ] Operator has the Go binary path ready

---

## 2. Exact Canary Sequence

Follow these steps **in order**. Do not skip steps. Record the result of each
step before proceeding.

### Step 1: Record Current Bun Binary Path, Version, and Config

Before changing anything, record the current state so you can roll back.

```bash
# Record the Bun binary path
which gowa-manager   # or: readlink -f $(which gowa-manager)
# or the path you used to start it, e.g.:
# /opt/gowa-manager/gowa-manager-bun

# Record the Bun version
gowa-manager --version
# or check the process command line:
ps aux | grep gowa-manager

# Record the config: port, data-dir, admin credentials
# (from your service file, environment, or process args)
```

Write down:
- **Bun binary path:** ____________________
- **Bun version:** ____________________
- **DATA_DIR:** ____________________
- **Port:** ____________________
- **Admin username:** ____________________
- **Admin password:** ____________________ *(store securely)*
- **Bun PID:** ____________________

### Step 2: Stop Bun Manager Gracefully

Send `SIGTERM` to the Bun manager. The manager initiates graceful shutdown:
it stops accepting new requests, drains in-flight requests, and stops
managing child processes. **Child processes (running GOWA instances) continue
running independently** — they are not killed.

```bash
# Linux — via systemd
sudo systemctl stop gowa-manager

# Linux — manual
kill -TERM <bun-pid>

# Windows — via NSSM
nssm stop GOWAManager

# Windows — manual
Stop-Process -Id <bun-pid>
```

Wait for the process to exit. Verify it is gone:

```bash
# Linux
ps aux | grep gowa-manager   # should show no Bun manager process

# Windows
Get-Process gowa-manager -ErrorAction SilentlyContinue
```

### Step 3: Wait for Child Processes to Stabilize

Running GOWA instances continue independently after the manager stops. Wait
for them to stabilize — they should **not** crash or restart.

```bash
# List child GOWA processes
# Linux
ps aux | grep gowa

# Windows
Get-Process | Where-Object { $_.ProcessName -like "*gowa*" }
```

Wait **60 seconds** and re-check. The child process count should be stable
(no unexpected restarts or exits). If children are crashing, **do not
proceed** — investigate before continuing.

Record:
- [ ] Child process count before stop: ____
- [ ] Child process count after 60s: ____
- [ ] Children stable? ____

### Step 4: Run Backup

Create a pre-cutover backup. This is your restore point if rollback is needed.

```bash
# Linux
sh scripts/ops/backup.sh \
  --data-dir /var/lib/gowa-manager/data \
  --backup-dir /var/backups/gowa-manager/pre-cutover-$(date -u +%Y%m%dT%H%M%SZ)
```

```powershell
# Windows
powershell -File scripts\ops\backup.ps1 `
  -DataDir C:\ProgramData\gowa-manager\data `
  -BackupDir C:\backups\gowa-manager\pre-cutover-$(Get-Date -Format yyyyMMddTHHmmssZ)
```

Record the backup directory path:
- **Backup dir:** ____________________

### Step 5: Verify Backup

Re-read the manifest and re-hash every file to confirm the backup is intact.

```bash
# Linux
sh scripts/ops/backup.sh \
  --verify \
  --backup-dir /var/backups/gowa-manager/<timestamp>
```

```powershell
# Windows
powershell -File scripts\ops\backup.ps1 `
  -Verify `
  -BackupDir C:\backups\gowa-manager\<timestamp>
```

**Exit code must be 0.** If verification fails, **do not proceed** — re-run
the backup or investigate.

- [ ] Backup verified (exit 0)

### Step 6: Run Preflight

Run the preflight checks to verify the environment is safe for the Go binary.

```bash
# Linux
sh scripts/ops/preflight.sh \
  --binary /opt/gowa-manager/gowa-manager-go \
  --data-dir /var/lib/gowa-manager/data \
  --port 3000 \
  --backup-dir /var/backups/gowa-manager
```

```powershell
# Windows
powershell -File scripts\ops\preflight.ps1 `
  -Binary C:\opt\gowa-manager\gowa-manager-windows-amd64.exe `
  -DataDir C:\ProgramData\gowa-manager\data `
  -Port 3000 `
  -BackupDir C:\backups\gowa-manager
```

Preflight outputs:
- **JSON to stdout** (machine-readable)
- **Human summary to stderr**
- **Exits non-zero** if any blocker is found

If preflight fails, **do not proceed**. Read the stderr summary, fix the
blocker, and re-run preflight.

- [ ] Preflight passed (exit 0)

### Step 7: Start Go Manager

Start the Go manager against the same data directory. Use the same port and
credentials as Bun.

```bash
# Linux
/opt/gowa-manager/gowa-manager-go \
  --data-dir /var/lib/gowa-manager/data \
  --port 3000 \
  --host 127.0.0.1 \
  --admin-username admin \
  --admin-password <password>
```

```powershell
# Windows
C:\opt\gowa-manager\gowa-manager-windows-amd64.exe `
  --data-dir C:\ProgramData\gowa-manager\data `
  --port 3000 `
  --host 127.0.0.1 `
  --admin-username admin `
  --admin-password <password>
```

If running via systemd, update the service file to point to the Go binary and
restart:

```bash
sudo systemctl daemon-reload
sudo systemctl start gowa-manager
```

Verify the process started:

```bash
# Linux
ps aux | grep gowa-manager-go

# Windows
Get-Process gowa-manager
```

Record:
- **Go PID:** ____________________

### Step 8: Run Smoke Tests

Run the post-cutover smoke tests. By default these are **non-destructive**
(GET requests only, plus a credential check).

```bash
# Linux
sh scripts/ops/smoke.sh \
  --url http://localhost:3000 \
  --admin-username admin \
  --admin-password <password>
```

```powershell
# Windows
powershell -File scripts\ops\smoke.ps1 `
  -Url http://localhost:3000 `
  -AdminUsername admin `
  -AdminPassword <password>
```

To also check the `/metrics` endpoint (if `GOWA_METRICS_ENABLED=1`):

```bash
sh scripts/ops/smoke.sh \
  --url http://localhost:3000 \
  --admin-username admin \
  --admin-password <password> \
  --metrics
```

For **destructive** smoke tests (exercises instance lifecycle —
start/stop/create/delete on a test instance):

```bash
sh scripts/ops/smoke.sh \
  --url http://localhost:3000 \
  --admin-username admin \
  --admin-password <password> \
  --destructive \
  --test-key <test-instance-key>
```

> **Warning:** Destructive mode creates and deletes a test instance. Only run
> it if you have a dedicated test instance key and are prepared for that
> instance to be modified.

**All smoke tests must pass (exit 0).** If any fail, **do not promote** —
investigate or roll back.

- [ ] Non-destructive smoke tests passed (exit 0)
- [ ] Metrics smoke test passed (if applicable)
- [ ] Destructive smoke tests passed (if applicable)

### Step 9: Observe

Monitor the Go manager for the duration of the observation window. The
minimum observation window is **30 minutes**; extend to 1–2 hours for
high-traffic deployments.

#### What to Monitor

| Signal | How to Check | Alert Threshold |
|--------|-------------|-----------------|
| Health | `GET /api/health` | Non-200 |
| Readiness | `GET /api/ready` | Non-200 |
| Error rate | Logs (stderr) | Any 5xx errors |
| Memory (RSS) | `ps -o rss -p <pid>` | Steady growth without bound |
| Instance count | `GET /api/instances` | Unexpected changes |
| Proxy | `GET /app/{key}/...` for a live instance | Non-200 |
| Auth | `curl -u admin:pass /api/instances` | Non-200 |
| Metrics | `GET /metrics` (if enabled) | Error count increasing |

#### Monitoring Commands

```bash
# Continuous health check
watch -n 5 'curl -fsS http://localhost:3000/api/ready && echo ready'

# Check instances
curl -u admin:<password> http://localhost:3000/api/instances | jq .

# Check system status
curl -u admin:<password> http://localhost:3000/api/system/status | jq .

# Monitor logs (systemd)
journalctl -u gowa-manager -f

# Monitor logs (Docker)
docker logs -f gowa-manager

# Monitor RSS (Linux)
watch -n 10 'ps -o rss,vsz,pid -p <go-pid>'

# Monitor RSS (Windows)
while ($true) { Get-Process gowa-manager | Select WS; Start-Sleep 10 }
```

Record observations during the window:
- [ ] Health: stable / issues: ____
- [ ] Readiness: stable / issues: ____
- [ ] Error rate: ____ errors in window
- [ ] RSS: start ____ KB → end ____ KB (trend: stable / growing)
- [ ] Instances: count stable / changes: ____
- [ ] Proxy: working / broken
- [ ] Auth: working / broken

### Step 10: Decision — Promote, Hold, or Rollback

Based on the observation window, make a decision:

#### PROMOTE (continue running Go)

All of the following must be true:
- [ ] Smoke tests passed
- [ ] Health and readiness stable for the full observation window
- [ ] No 5xx errors
- [ ] RSS stable or bounded (not growing without limit)
- [ ] Instance count stable
- [ ] Proxy working for at least one live instance
- [ ] Auth working

**Promotion action:** No action needed — Go is already running. Update your
service definition (systemd / NSSM / Docker) to use the Go binary permanently.
Announce cutover complete.

#### HOLD (monitor longer)

Use hold when:
- Smoke tests passed but you want more observation time
- RSS is growing slowly and you want to confirm it plateaus
- Minor non-blocking issues need investigation

**Hold action:** Continue monitoring. Do not repoint `latest` Docker tags or
announce completion. Re-evaluate after the extended window. If a rollback
trigger occurs, roll back.

#### ROLLBACK (revert to Bun)

Roll back immediately if **any** rollback trigger is met. See
[GO_BACKEND_ROLLBACK.md](GO_BACKEND_ROLLBACK.md) for the full trigger list and
rollback procedure. Key triggers:

- SQLite integrity failure
- Recovery failure (instances don't start)
- Duplicate or orphan process
- Proxy failures (HTTP or WebSocket)
- Crash loop (Go crashes repeatedly)
- Memory leak (RSS grows unbounded)
- Auth regression (login/logout broken)
- Error threshold exceeded
- Ambiguous lifecycle state

### Step 11: Bun and Go Never Share a Live Data Directory

This is not a step to perform — it is an invariant that holds throughout the
entire cutover:

- **Before Step 2:** Bun owns the data directory; Go is not running.
- **Steps 2–6:** Neither Bun nor Go is running. Child processes run
  independently. The data directory is quiescent.
- **Steps 7–10:** Go owns the data directory; Bun is not running.
- **Rollback:** Go is stopped first, then Bun starts. They never overlap.

If at any point both Bun and Go are running against the same `DATA_DIR`,
**stop one immediately** and investigate. The `flock` lock file should
prevent this, but operator vigilance is the primary safeguard.

---

## 3. Observation Metrics Reference

| Metric | Source | Healthy | Concern |
|--------|--------|---------|---------|
| `/api/health` | HTTP | 200 | Non-200 or timeout |
| `/api/ready` | HTTP | 200 | 503 (starting/shutting down) |
| `/metrics` | Prometheus | Stable counters | Error counter increasing |
| RSS | OS | Stable or bounded | Unbounded growth |
| Instance count | `/api/instances` | Stable | Unexpected drops/additions |
| Proxy response | `/app/{key}/` | 200 | 502 / timeout |
| Auth | `/api/instances` w/ Basic Auth | 200 | 401/403 |
| Logs | stderr | No errors | Error lines, panics, stack traces |

---

## 4. Dry-Run Checklist (Documentation Review)

> **Note:** The actual staging dry run is performed in **Task 10** by a human
> operator who was not involved in writing this runbook. This checklist
> documents the dry-run procedure so the operator knows what to do.

### Purpose

A documentation dry run verifies that an operator unfamiliar with the
implementation can follow this runbook end-to-end in a **staging**
environment without needing undocumented commands or tribal knowledge.

### Pre-Dry-Run Setup (performed by the implementation team)

- [ ] Staging environment provisioned with a Bun manager running
- [ ] Go binary built and available in staging
- [ ] Backup destination configured in staging
- [ ] Pinned Bun rollback artifact available in staging
- [ ] Test instances running under the Bun manager

### Dry-Run Procedure (performed by the operator)

1. [ ] Read this entire runbook before starting
2. [ ] Read [GO_BACKEND_ROLLBACK.md](GO_BACKEND_ROLLBACK.md)
3. [ ] Complete the Prerequisites checklist (§1)
4. [ ] Execute Steps 1–8 in order, recording results
5. [ ] Observe for at least 15 minutes (shortened for dry run)
6. [ ] Practice a dry-run rollback (without `--execute`) to verify the
      rollback procedure
7. [ ] Record every ambiguity, missing command, or unclear instruction

### Dry-Run Feedback Checklist

For each step, the operator records:

- [ ] Were all commands present and copy-pasteable?
- [ ] Were all expected outputs documented?
- [ ] Were any assumptions made about the environment?
- [ ] Were any flags or options undocumented?
- [ ] Was the rollback procedure clear and executable?
- [ ] Were error cases and their handling documented?

### Fix Criteria

Every ambiguity recorded by the operator **must be fixed** in this runbook
before the cutover is attempted in production. Fixes are tracked in Task 10.

---

## 5. Quick Reference: Ops Script Flags

### preflight.sh / preflight.ps1

```
-b, --binary PATH      Path to the Go manager binary to verify.
-d, --data-dir DIR     Data directory (default: ./data).
-p, --port N           Manager HTTP port (default: 3000).
--backup-dir DIR       Backup destination directory (default: ./backup).
--sqlite-bin PATH      Path to sqlite3 CLI (default: sqlite3 from PATH).
--min-space BYTES      Minimum required free bytes (default: 10485760 = 10 MiB).
```

Output: JSON to stdout, human summary to stderr. Exits non-zero on blockers.

### backup.sh / backup.ps1

```
-d, --data-dir DIR     Data directory to back up (default: ./data).
-o, --backup-dir DIR   Destination directory (default: ./backup/<timestamp>).
--sqlite-bin PATH      Path to sqlite3 CLI (default: sqlite3 from PATH).
--verify               Re-read manifest and re-hash; no backup performed.
```

### smoke.sh / smoke.ps1

```
--url <base>            Base URL (default: http://localhost:3000).
--admin-username <user> Admin username (default: admin).
--admin-password <pass> Admin password (default: password).
--metrics               Also check GET /metrics.
--destructive           Enable destructive checks (requires --test-key).
--test-key <key>        Instance key for destructive checks.
```

### rollback.sh / rollback.ps1

```
--execute                 Actually perform rollback (default: dry-run).
--backup-dir <path>       Backup directory (required when --execute).
--go-pid <pid>            PID of the running Go manager (required when --execute).
--go-version <version>    Go manager version string (required when --execute).
--bun-binary <path>       Path to pinned Bun binary (required when --execute).
--bun-checksum <sha256>   Expected SHA-256 of Bun binary (required when --execute).
--data-dir <path>         Data directory (default: ./data).
--sqlite-bin <path>       Path to sqlite3 CLI (default: sqlite3 from PATH).
--bun-url <url>           Bun manager URL for smoke tests (default: http://localhost:3000).
--override-ambiguous-state  Proceed even if child/process state is ambiguous.
```

> See [GO_BACKEND_ROLLBACK.md](GO_BACKEND_ROLLBACK.md) for rollback procedure.
