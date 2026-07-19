# Go Backend Rollback Runbook

Procedure for rolling back from the **Go** backend to the **Bun** backend.
This runbook is written for **operators** performing an emergency or planned
rollback after a Go cutover.

> **Related:** [GO_BACKEND_OPERATIONS.md](GO_BACKEND_OPERATIONS.md) (normal
> operation) · [GO_BACKEND_CANARY.md](GO_BACKEND_CANARY.md) (cutover)

---

## ⚠️ Critical Safety Rules

1. **Stop Go before starting Bun.** Bun and Go never share a live data
   directory. The rollback script enforces this, but verify manually.
2. **Always dry-run first.** The rollback script defaults to dry-run mode.
   Review the dry-run output before adding `--execute`.
3. **Know your DB strategy before executing.** The rollback script chooses
   between "use current DB" and "restore backup" based on SQLite integrity
   and schema checks. Understand both paths (§3) before executing.
4. **Never delete the data directory.** The rollback script handles DB
   restoration. Do not manually delete `gowa.db` or the lock file.
5. **Have the pre-cutover backup path ready.** You will need it if the
   current DB is not compatible with Bun.

---

## 1. Rollback Triggers

Roll back immediately if **any** of the following are observed after the Go
cutover:

### 1.1 SQLite Integrity Failure

**Symptom:** `PRAGMA integrity_check` returns errors, or the Go logs report
database corruption.

**Check:**
```bash
sqlite3 /var/lib/gowa-manager/data/gowa.db "PRAGMA integrity_check;"
```

**Action:** Roll back via the **restore-backup path** (§3.2). The current DB
is not trustworthy.

### 1.2 Recovery Failure (Instances Don't Start After Restart)

**Symptom:** After the Go manager restarts (or auto-restarts), running
instances do not come back up. `/api/system/status` shows recovery errors.

**Check:**
```bash
curl -u admin:<password> http://localhost:3000/api/system/status
curl -u admin:<password> http://localhost:3000/api/instances
```

**Action:** Roll back. If the DB is intact (integrity_check passes), use the
**current-compatible-DB path** (§3.1). If the DB is corrupt, use the
**restore-backup path** (§3.2).

### 1.3 Duplicate or Orphan Process

**Symptom:** Multiple manager processes are running, or child GOWA processes
are in a zombie/orphaned state with no clear parent.

**Check:**
```bash
# Linux
ps aux | grep gowa-manager
ps aux | grep gowa

# Windows
Get-Process | Where-Object { $_.ProcessName -like "*gowa*" }
```

**Action:** Stop all manager processes. Roll back. If process state is
ambiguous (unclear which PID is the real manager), use
`--override-ambiguous-state` on the rollback script **only** after manually
verifying no manager is running.

### 1.4 Proxy Failures (HTTP or WebSocket)

**Symptom:** The HTTP or WebSocket proxy is not forwarding requests to
instances. `/app/{instanceKey}/...` returns errors.

**Check:**
```bash
# Test HTTP proxy for a known instance
curl -u <instance-auth> http://localhost:3000/app/<instanceKey>/app/devices

# Check instance is running
curl -u admin:<password> http://localhost:3000/api/instances
```

**Action:** Roll back. Proxy regression is a hard blocker — external clients
and webhooks depend on the proxy.

### 1.5 Crash Loop (Go Manager Crashes Repeatedly)

**Symptom:** The Go manager process exits and restarts repeatedly (via
systemd/docker restart policy). Logs show panics or fatal errors.

**Check:**
```bash
# Linux (systemd)
journalctl -u gowa-manager --since "10 minutes ago" | grep -i "panic\|fatal\|error"

# Docker
docker logs --since 10m gowa-manager 2>&1 | grep -i "panic\|fatal\|error"
```

**Action:** Stop the restart loop (disable restart policy or stop the
service). Roll back. Do not let the crash loop continue — it may corrupt
state.

### 1.6 Memory Leak (RSS Grows Unbounded)

**Symptom:** The Go manager's RSS grows continuously without plateauing over
the observation window.

**Check:**
```bash
# Linux
ps -o rss -p <go-pid>
# Check every 60s for 10 minutes — if RSS grows monotonically, it's a leak.

# Windows
Get-Process gowa-manager | Select WS
```

**Action:** Roll back. A memory leak will eventually cause an OOM kill,
which is worse than a controlled rollback.

### 1.7 Auth Regression (Login/Logout Broken)

**Symptom:** Basic auth credentials that worked under Bun fail under Go.
Login or logout endpoints return unexpected status codes.

**Check:**
```bash
curl -u admin:<password> http://localhost:3000/api/instances
# Expected: 200 with instance list
# If 401/403/500: auth regression
```

**Action:** Roll back. Auth regression is a security and availability
blocker.

### 1.8 Error Threshold Exceeded

**Symptom:** The error rate exceeds the acceptable threshold defined for
your deployment (e.g., >1% of requests return 5xx).

**Check:**
```bash
# Monitor logs for 5xx
journalctl -u gowa-manager --since "5 minutes ago" | grep "5[0-9][0-9]"
# Or check /metrics if enabled
curl http://localhost:3000/metrics | grep error
```

**Action:** Roll back if the error rate exceeds your threshold and is not
decreasing.

### 1.9 Ambiguous Lifecycle State

**Symptom:** It is unclear whether the Go manager is running, whether child
processes are managed, or whether the lock file is valid. Process state
cannot be determined with confidence.

**Check:**
```bash
# Linux
ps aux | grep gowa-manager
ls -la /var/lib/gowa-manager/data/.gowa-manager.lock
fuser /var/lib/gowa-manager/data/.gowa-manager.lock 2>/dev/null

# Windows
Get-Process gowa-manager -ErrorAction SilentlyContinue
```

**Action:** Roll back. Use `--override-ambiguous-state` on the rollback
script **only** after manually confirming no Go manager is running. If you
cannot confirm, stop all candidate processes first, then roll back.

---

## 2. Before You Roll Back

### 2.1 Gather Information

Record the following before executing the rollback:

- **Go PID:** `ps aux | grep gowa-manager-go` → ____
- **Go version:** `./gowa-manager-go --version` → ____
- **DATA_DIR:** → ____
- **Pre-cutover backup directory:** → ____
- **Pinned Bun rollback binary path:** → ____
- **Pinned Bun rollback binary SHA-256:** → ____

### 2.2 Verify the Bun Rollback Artifact

```bash
# Verify the pinned Bun binary exists and its checksum matches
sha256sum gowa-manager-bun-rollback
# Compare against the recorded checksum from the release
```

If the checksum does not match, **do not roll back** with this binary.
Obtain a verified copy before proceeding.

### 2.3 Notify Stakeholders

- Announce that a rollback is in progress
- Estimate downtime (typically 5–15 minutes)
- Confirm the maintenance window is still active or re-open it

---

## 3. Rollback Paths

The rollback script (`scripts/ops/rollback.sh` / `rollback.ps1`) chooses
between two DB strategies automatically based on SQLite integrity and schema
checks. Understand both before executing.

### 3.1 Path A: Current-Compatible-DB

**When:** The Go-written database passes `PRAGMA integrity_check` and its
schema is compatible with the Bun backend.

**What happens:**
1. Stop the Go manager (SIGTERM, wait for graceful shutdown)
2. Verify DB integrity (`PRAGMA integrity_check`)
3. Verify schema compatibility with Bun
4. Start the pinned Bun manager with the **current** database
5. Run Bun smoke tests

**Data loss:** None. The current DB (including any writes Go made) is used.

### 3.2 Path B: Restore-Backup

**When:** The Go-written database fails integrity check, or its schema is
not compatible with the Bun backend.

**What happens:**
1. Stop the Go manager (SIGTERM, wait for graceful shutdown)
2. Verify DB integrity — fails
3. Restore the pre-cutover backup (from Step 4 of the canary)
4. Start the pinned Bun manager with the **restored** database
5. Run Bun smoke tests

**Data loss:** Any writes the Go manager made after the cutover are lost.
The database reverts to the pre-cutover backup state.

> **Important:** The rollback script captures the current (Go-written) DB
> into a capture directory before restoring, so no data is permanently lost.
> You can investigate the Go-written DB later.

---

## 4. Rollback Procedure

### 4.1 Dry-Run First (Required)

**Always run a dry-run first.** The default mode prints what would happen
and exits 0 without making any changes.

```bash
# Linux — dry run
sh scripts/ops/rollback.sh \
  --backup-dir /var/backups/gowa-manager/<pre-cutover-timestamp> \
  --go-pid <go-pid> \
  --go-version <go-version> \
  --bun-binary /opt/gowa-manager/gowa-manager-bun-rollback \
  --bun-checksum <sha256-of-bun-binary> \
  --data-dir /var/lib/gowa-manager/data \
  --bun-url http://localhost:3000
```

```powershell
# Windows — dry run
powershell -File scripts\ops\rollback.ps1 `
  -BackupDir C:\backups\gowa-manager\<pre-cutover-timestamp> `
  -GoPid <go-pid> `
  -GoVersion <go-version> `
  -BunBinary C:\opt\gowa-manager\gowa-manager-bun-rollback `
  -BunChecksum <sha256-of-bun-binary> `
  -DataDir C:\ProgramData\gowa-manager\data `
  -BunUrl http://localhost:3000
```

Review the dry-run output:
- Which DB strategy was chosen (current or restore)?
- Were there any warnings?
- Does the plan match your expectations?

**If the dry-run shows errors or an unexpected plan, do not proceed to
execute.** Investigate first.

### 4.2 Execute the Rollback

Once the dry-run output is reviewed and confirmed, add `--execute` (Linux)
or `-Execute` (Windows):

```bash
# Linux — execute
sh scripts/ops/rollback.sh \
  --execute \
  --backup-dir /var/backups/gowa-manager/<pre-cutover-timestamp> \
  --go-pid <go-pid> \
  --go-version <go-version> \
  --bun-binary /opt/gowa-manager/gowa-manager-bun-rollback \
  --bun-checksum <sha256-of-bun-binary> \
  --data-dir /var/lib/gowa-manager/data \
  --bun-url http://localhost:3000
```

```powershell
# Windows — execute
powershell -File scripts\ops\rollback.ps1 `
  -Execute `
  -BackupDir C:\backups\gowa-manager\<pre-cutover-timestamp> `
  -GoPid <go-pid> `
  -GoVersion <go-version> `
  -BunBinary C:\opt\gowa-manager\gowa-manager-bun-rollback `
  -BunChecksum <sha256-of-bun-binary> `
  -DataDir C:\ProgramData\gowa-manager\data `
  -BunUrl http://localhost:3000
```

The script will:
1. Stop the Go manager (SIGTERM, wait up to 10s for quiescence)
2. Record child process state
3. Capture current DB and logs
4. Run SQLite integrity and schema checks
5. Choose DB strategy (current-compatible or restore-backup)
6. If restoring: restore the pre-cutover backup
7. Verify the pinned Bun binary checksum
8. Start the Bun manager
9. Run Bun smoke tests

### 4.3 Override Ambiguous State (If Needed)

If the rollback script reports ambiguous child/process state and you have
**manually verified** that no Go manager is running, use:

```bash
--override-ambiguous-state    # Linux
-OverrideAmbiguousState       # Windows
```

> **Warning:** Only use this flag after manually confirming no manager
> process is running. Using it incorrectly may result in data corruption.

---

## 5. Post-Rollback Verification

After the rollback script completes, verify the Bun manager is healthy.

### 5.1 Process Check

```bash
# Linux
ps aux | grep gowa-manager   # should show the Bun manager, not Go

# Windows
Get-Process | Where-Object { $_.ProcessName -like "*gowa*" }
```

### 5.2 Health and Readiness

```bash
curl -fsS http://localhost:3000/api/health && echo "alive"
curl -fsS http://localhost:3000/api/ready && echo "ready"
```

### 5.3 Instances

```bash
curl -u admin:<password> http://localhost:3000/api/instances | jq .
```

Verify all expected instances are present and their states are correct.

### 5.4 System Status

```bash
curl -u admin:<password> http://localhost:3000/api/system/status | jq .
```

### 5.5 Proxy

```bash
# Test proxy for a known instance
curl -u <instance-auth> http://localhost:3000/app/<instanceKey>/app/devices
```

### 5.6 Auth

```bash
curl -u admin:<password> http://localhost:3000/api/instances
# Expected: 200
```

### 5.7 Rollback to Bun Smoke Test

Run the smoke script against the Bun manager to confirm it is fully
operational:

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

> **Note:** The `/metrics` endpoint is not available on the Bun backend. Do
> not pass `--metrics` when smoke-testing Bun.

**All smoke tests must pass (exit 0).** If any fail, the Bun manager may
have a problem — investigate the logs and the restored database.

### 5.8 Post-Rollback Checklist

- [ ] Bun manager process running (not Go)
- [ ] `/api/health` returns 200
- [ ] `/api/ready` returns 200
- [ ] `/api/instances` returns expected instances
- [ ] `/api/system/status` returns healthy status
- [ ] Proxy working for at least one instance
- [ ] Auth working (Basic Auth returns 200)
- [ ] Bun smoke tests pass (exit 0)
- [ ] Service definition (systemd/NSSM/Docker) updated to point to Bun
- [ ] Stakeholders notified that rollback is complete

---

## 6. After the Rollback

### 6.1 Preserve Evidence

Do not delete the following until the Go backend issue is diagnosed:
- The Go-written DB capture (created by the rollback script)
- Go manager logs from the cutover period
- The pre-cutover backup (in case it is needed again)

### 6.2 Diagnose the Root Cause

Review the captured artifacts to determine why the Go cutover failed:
- Check Go logs for panics, errors, or unexpected behavior
- Check the Go-written DB for schema differences or corruption
- Check the rollback script's JSON output for the chosen DB strategy and
  any warnings

### 6.3 Update Service Definitions

Ensure your service definition (systemd unit, NSSM config, Docker Compose,
Scheduled Task) points to the **Bun** binary, not the Go binary. This
prevents an accidental Go restart on the next host reboot.

### 6.4 Plan the Next Cutover Attempt

Do not re-attempt the Go cutover until:
- The root cause of the rollback is diagnosed and fixed
- The fix is verified in staging
- A new Go release artifact is built and verified
- A new pre-cutover backup is taken

---

## 7. Rollback Script Flag Reference

```
--execute                   Actually perform rollback (default: dry-run).
--backup-dir <path>         Backup directory (required when --execute).
--go-pid <pid>              PID of the running Go manager (required when --execute).
--go-version <version>      Go manager version string (required when --execute).
--bun-binary <path>         Path to pinned Bun binary (required when --execute).
--bun-checksum <sha256>     Expected SHA-256 of Bun binary (required when --execute).
--data-dir <path>           Data directory (default: ./data).
--sqlite-bin <path>         Path to sqlite3 CLI (default: sqlite3 from PATH).
--bun-url <url>             Bun manager URL for smoke tests (default: http://localhost:3000).
--override-ambiguous-state  Proceed even if child/process state is ambiguous.
```

**Required when `--execute`:** `--backup-dir`, `--go-pid`, `--go-version`,
`--bun-binary`, `--bun-checksum`.

**Default mode:** Dry-run (no changes made). Prints the plan and exits 0.

---

## 8. Rollback Decision Flowchart

```
Is a rollback trigger met?
│
├── No → Continue monitoring (see canary runbook §9–10)
│
└── Yes → Gather info (§2.1)
          │
          ├── Verify Bun rollback artifact checksum (§2.2)
          │   └── Mismatch → STOP, obtain verified binary
          │
          └── Run rollback dry-run (§4.1)
              │
              ├── Review: which DB strategy? (current vs restore)
              │
              ├── Errors or unexpected plan? → Investigate, do not execute
              │
              └── Plan confirmed → Execute rollback (§4.2)
                  │
                  └── Post-rollback verification (§5)
                      │
                      ├── All checks pass → Rollback complete (§6)
                      │
                      └── Checks fail → Investigate Bun manager,
                          check restored DB, review logs
```
