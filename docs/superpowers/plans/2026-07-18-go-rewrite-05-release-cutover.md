# Go Rewrite 05: Release, Canary, and Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the completed Go backend is releasable on Linux amd64/arm64 and Windows amd64, package it as the production single binary and Docker image, rehearse backup/rollback, and execute a controlled per-host canary cutover.

**Architecture:** Release engineering remains separate from feature implementation. Reproducible CI builds signed/checksummed artifacts, runs native platform and architecture tests, compares measured Bun/Go performance, validates real database copies, and exposes scripted preflight/smoke/backup/rollback operations. Production defaults change only after every mandatory gate passes.

**Tech Stack:** GitHub Actions, Go 1.24, Bun/Vite frontend build, Docker Buildx, PowerShell and POSIX shell scripts, SQLite integrity checks, benchmark tooling, artifact checksums/SBOM.

---

## Dependencies and outputs

- Requires Plans 01–04 and all parity/E2E tests passing.
- This is the only plan that changes production build/start/release defaults from Bun to Go.
- The final Bun binary, build command, source, release tag, and rollback procedure remain available throughout stabilization.

## File structure

- Create `test/benchmark/backend_bench_test.go`, `scripts/benchmark-backends.ts`, baseline JSON.
- Create `.github/workflows/go-test.yml`, `go-release.yml` or update existing workflows with explicit Go jobs.
- Create `scripts/release/build-go.ts`, `verify-artifact.ts`, `generate-sbom.ts`.
- Create `scripts/ops/preflight.{sh,ps1}`, `backup.{sh,ps1}`, `smoke.{sh,ps1}`, `rollback.{sh,ps1}`.
- Create `docs/GO_BACKEND_OPERATIONS.md`, `docs/GO_BACKEND_CANARY.md`, `docs/GO_BACKEND_ROLLBACK.md`.
- Modify `Dockerfile`, `Dockerfile.prebuilt`, `package.json`, release workflow, README and integrated setup docs.
- Create `test/compat/production-data_test.go` and a manifest for sanitized database samples.

### Task 1: Capture Bun performance and resource baseline

**Files:**
- Create: `test/benchmark/backend_bench_test.go`
- Create: `scripts/benchmark-backends.ts`
- Create: `test/benchmark/baseline.schema.json`
- Create: `test/benchmark/bun-baseline.json`

- [ ] **Step 1: Define repeatable benchmark scenarios**

Measure cold startup to health, idle RSS after stabilization, CRUD latency distribution, HTTP proxy throughput/latency for small and large bodies, WebSocket connection/message throughput, monitoring cost with a fixed number of fake instances, graceful shutdown duration, executable size, and Docker image size. Record OS, architecture, CPU, runtime versions, sample count, and fixture commit.

- [ ] **Step 2: Implement harness with warmups and multiple samples**

Start each backend on independent temporary data/ports. Use the same fake GOWA/upstream fixtures. Store raw samples and median/p95. Fail incomplete runs but do not compare across unlike machines.

- [ ] **Step 3: Capture and review Bun baseline**

Run on designated Linux amd64 benchmark host: `bun run scripts/benchmark-backends.ts --backend bun --output test/benchmark/bun-baseline.json`.

Expected: valid JSON matching the schema and no surviving fixture processes.

- [ ] **Step 4: Commit**

```bash
git add test/benchmark scripts/benchmark-backends.ts
git commit -m "perf: capture Bun backend baseline"
```

### Task 2: Add Go benchmark comparison and acceptance thresholds

**Files:**
- Modify: `scripts/benchmark-backends.ts`
- Create: `test/benchmark/compare.ts`
- Create: `test/benchmark/thresholds.json`

- [ ] **Step 1: Run the same scenarios against Go**

Generate Go samples on the same host and build mode as Bun baseline. The comparison must reject machine metadata mismatches.

- [ ] **Step 2: Encode approved thresholds**

Require correctness first, idle RSS no worse than Bun, no material HTTP proxy throughput regression, stable WebSocket behavior, bounded startup/shutdown, and zero leak trend. Convert “material” into numeric tolerances based on observed variance, document the chosen percentages in `thresholds.json`, and require owner approval in the commit message/body.

- [ ] **Step 3: Add repeated leak runs**

Run 1,000 HTTP requests, 500 WebSocket connect/send/close cycles, and 100 process start/stop cycles; compare beginning/end goroutines, RSS, file descriptors, and Windows handles.

- [ ] **Step 4: Verify and commit**

Run: `bun run test:benchmark:compare`

Expected: PASS or an explicit blocked release with measured regressions; do not relax thresholds to make a failure disappear.

```bash
git add scripts/benchmark-backends.ts test/benchmark
git commit -m "perf: gate Go backend against Bun baseline"
```

### Task 3: Build native CI test matrix

**Files:**
- Create: `.github/workflows/go-test.yml`
- Modify: `.github/workflows/test.yml`

- [ ] **Step 1: Add fast Go checks**

Run format check, `go vet`, unit tests, race tests where supported, Bun baseline tests, and frontend typecheck/build. Cache Go modules and Bun dependencies by lockfile hashes.

- [ ] **Step 2: Add native platform jobs**

Required jobs:

- Linux amd64: full unit/integration/contract/E2E/race.
- Linux arm64: native self-hosted runner or real arm64 runner for lifecycle and integration tests.
- Windows amd64: native process supervisor/runtime/proxy/contract tests.
- Linux Docker amd64: image smoke test and volume persistence.

No required platform may be represented only by cross-compilation.

- [ ] **Step 3: Add fixture cleanup diagnostics**

Always upload logs on failure and fail jobs if fake GOWA processes survive. Artifacts must redact credentials and magic tokens.

- [ ] **Step 4: Verify workflow syntax and commit**

Run the repository's workflow linter if present; otherwise parse YAML and inspect action versions manually.

```bash
git add .github/workflows/go-test.yml .github/workflows/test.yml
git commit -m "ci: test Go backend on Linux and Windows"
```

### Task 4: Create reproducible release artifacts

**Files:**
- Create: `scripts/release/build-go.ts`
- Create: `scripts/release/verify-artifact.ts`
- Create: `scripts/release/generate-sbom.ts`
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Implement release build orchestration**

Build the production React app once from locked dependencies, embed it, then build:

- `gowa-manager-linux-amd64`;
- `gowa-manager-linux-arm64`;
- `gowa-manager-windows-amd64.exe`.

Inject exact manager version, commit SHA, and build timestamp policy. Prefer reproducible timestamp omission or `SOURCE_DATE_EPOCH`. Strip symbols only after stack/debug trade-offs are approved.

- [ ] **Step 2: Generate checksums and SBOM**

Create SHA-256 manifest and per-release SBOM containing Go modules and frontend dependencies. Never embed build-host paths or credentials.

- [ ] **Step 3: Verify artifacts**

For each executable, verify filename/platform, `--version`, `--help`, embedded SPA marker, health startup, and checksum. Run native smoke tests on the corresponding runner before upload.

- [ ] **Step 4: Preserve Bun rollback artifact**

Release workflow also stores the pinned final Bun binary/artifact under a clearly labeled rollback asset during stabilization.

- [ ] **Step 5: Commit**

```bash
git add scripts/release .github/workflows/release.yml
git commit -m "build: package Go backend release artifacts"
```

### Task 5: Update Docker images for Go with rollback tag

**Files:**
- Modify: `Dockerfile`
- Modify: `Dockerfile.prebuilt`
- Create: `test/compat/docker_test.go`

- [ ] **Step 1: Add Docker persistence and signal tests**

Test `/data` volume, existing SQLite startup, embedded SPA, health/readiness, non-root permissions where practical, `SIGTERM` shutdown, child-process cleanup, restart recovery, and rollback image reading the same volume copy.

- [ ] **Step 2: Implement multi-stage Go image**

Build frontend and Go binary in builder stages; runtime contains only required CA certificates/timezone/system tools plus binary. Use an explicit user and writable `/data`. Keep GOWA child binary execution requirements in mind.

- [ ] **Step 3: Implement prebuilt artifact image**

Copy verified architecture-specific binary, set executable permission, and use exec-form entrypoint. Do not add Bun to the Go runtime image.

- [ ] **Step 4: Publish separate stabilization tags**

Publish Go candidate tags and preserve a Bun rollback tag. Do not repoint `latest` until canary promotion is approved.

- [ ] **Step 5: Verify and commit**

Run: `go test ./test/compat -run TestDocker -v`

```bash
git add Dockerfile Dockerfile.prebuilt test/compat/docker_test.go
git commit -m "build: package Go backend Docker images"
```

### Task 6: Validate representative production data copies

**Files:**
- Create: `test/compat/production-data_test.go`
- Create: `test/compat/testdata/manifest.json`
- Create: `scripts/ops/sanitize-db.ts`

- [ ] **Step 1: Define sanitized sample manifest**

Include schema version/columns, row counts, statuses, config feature flags present, version layouts, and expected filesystem categories. Do not commit real tokens, URLs, phone/device data, credentials, or media.

- [ ] **Step 2: Implement compatibility workflow**

For each external sanitized sample path supplied in CI/staging: copy data, run preflight, start Go, exercise major reads/writes, stop normally; repeat with forced stop; run `PRAGMA integrity_check`; then open and operate with pinned Bun.

- [ ] **Step 3: Test migration idempotency**

Run Go startup multiple times and compare schema plus data. Verify no destructive or repeated mutation.

- [ ] **Step 4: Commit harness, not private samples**

```bash
git add test/compat scripts/ops/sanitize-db.ts
git commit -m "test: validate Go against production data shapes"
```

### Task 7: Implement cross-platform preflight and backup tools

**Files:**
- Create: `scripts/ops/preflight.sh`
- Create: `scripts/ops/preflight.ps1`
- Create: `scripts/ops/backup.sh`
- Create: `scripts/ops/backup.ps1`
- Create: `test/ops/preflight_test.go`
- Create: `test/ops/backup_test.go`

- [ ] **Step 1: Specify preflight checks**

Check OS/architecture, binary checksum/version, data path and free space, read/write/execute permissions, absence of active Bun/Go manager, manager lock, HTTP port, SQLite integrity, required columns, installed GOWA binaries and execute permission, backup destination, and child-process/port inventory.

- [ ] **Step 2: Implement machine-readable and human output**

Both shell variants produce equivalent JSON plus concise console output and nonzero exit on blockers. Passwords/config values are never printed.

- [ ] **Step 3: Implement consistent backup**

After stopping Bun and stabilizing child state, use SQLite online backup or safe file copy according to journal mode, then copy instance/version metadata needed for rollback. Generate SHA-256 manifest and verify it immediately. Never claim atomicity across files; record start/end timestamps and manager downtime state.

- [ ] **Step 4: Test failure cases**

Cover insufficient space, corrupt DB, manager still active, lock held, occupied port, missing binary, permission failure, checksum mismatch, and paths containing spaces.

- [ ] **Step 5: Commit**

```bash
git add scripts/ops/preflight.* scripts/ops/backup.* test/ops
git commit -m "ops: add Go cutover preflight and backup"
```

### Task 8: Implement smoke-test and rollback tools

**Files:**
- Create: `scripts/ops/smoke.sh`
- Create: `scripts/ops/smoke.ps1`
- Create: `scripts/ops/rollback.sh`
- Create: `scripts/ops/rollback.ps1`
- Create: `test/ops/smoke_test.go`
- Create: `test/ops/rollback_test.go`

- [ ] **Step 1: Implement non-destructive smoke tests by default**

Check health/readiness, auth, list/detail/status, proxy status/health, system/version reads, scheduler status, and metrics when enabled. Destructive mode must require an explicit test instance ID/key and may start/stop/create/delete only that fixture.

- [ ] **Step 2: Implement rollback guardrails**

Rollback stops traffic/Go, waits for lifecycle operations, records child state, captures logs/current DB, runs integrity/schema checks, chooses current DB only if compatible or restores the named verified backup, starts the pinned Bun command, and runs Bun smoke tests.

- [ ] **Step 3: Require explicit confirmations**

Rollback scripts default to dry-run and require `--execute`, exact backup manifest, expected Go PID/version, and expected Bun artifact checksum. They refuse ambiguous child/process state unless an operator supplies the documented override.

- [ ] **Step 4: Test staging rehearsal**

Automate Go cutover, mutation on a test instance, rollback to Bun using current compatible DB, then a second rehearsal restoring backup. Both end with integrity and Bun smoke tests passing.

- [ ] **Step 5: Commit**

```bash
git add scripts/ops/smoke.* scripts/ops/rollback.* test/ops
git commit -m "ops: automate Go smoke tests and Bun rollback"
```

### Task 9: Write operator runbooks

**Files:**
- Create: `docs/GO_BACKEND_OPERATIONS.md`
- Create: `docs/GO_BACKEND_CANARY.md`
- Create: `docs/GO_BACKEND_ROLLBACK.md`
- Modify: `docs/INTEGRATED_SETUP.md`
- Modify: `README.md`

- [ ] **Step 1: Document normal operation**

Include CLI/env compatibility, data-directory ownership, Linux service/container examples, Windows service/task guidance, logs/metrics/readiness, backup cadence, and troubleshooting.

- [ ] **Step 2: Document exact canary sequence**

Record Bun binary/config → stop Bun → establish child state → backup/checksum → preflight → start Go → smoke → observe → promote/hold. State explicitly that Bun and Go never share a live data directory.

- [ ] **Step 3: Document rollback triggers and commands**

List integrity failure, recovery failure, duplicate/orphan process, proxy failures, crash loop, leak, auth regression, error threshold, and ambiguous lifecycle state. Include both current-compatible-DB and restore-backup paths.

- [ ] **Step 4: Perform a documentation dry run**

A person not involved in implementation follows the staging runbooks without undocumented commands. Record and fix every ambiguity.

- [ ] **Step 5: Commit**

```bash
git add docs README.md
git commit -m "docs: add Go backend operations and rollback runbooks"
```

### Task 10: Rehearse full cutover and rollback in staging

**Files:**
- Create: `docs/release/go-cutover-rehearsal-YYYY-MM-DD.md`

- [ ] **Step 1: Prepare staging from a representative sanitized copy**

Record host, platform, artifact checksums, Bun version, Go version, DB integrity, instance/version counts, and baseline metrics.

- [ ] **Step 2: Execute the canary runbook exactly**

Do not use ad-hoc commands. Save preflight, backup, smoke, and observation outputs with secrets redacted.

- [ ] **Step 3: Exercise critical flows**

Start/stop/restart one noncritical fixture, HTTP proxy, WebSocket, create/delete test instance, manager restart/recovery, scheduler manual check, DB integrity, and resource stability.

- [ ] **Step 4: Roll back twice**

First use the current Go-written DB after compatibility check; second restore the pre-cutover backup. Both must return to a healthy Bun backend.

- [ ] **Step 5: Record evidence and commit**

The rehearsal document includes timestamps, pass/fail results, metric deltas, issues, and corrective commits. No secret/log payload is committed.

```bash
git add docs/release/go-cutover-rehearsal-*.md
git commit -m "docs: record Go backend cutover rehearsal"
```

### Task 11: Switch production commands to Go

**Files:**
- Modify: `package.json`
- Modify: `Dockerfile`
- Modify: `Dockerfile.prebuilt`
- Modify: `.github/workflows/release.yml`
- Modify: `README.md`
- Modify: `docs/INTEGRATED_SETUP.md`

- [ ] **Step 1: Verify all mandatory gates before editing defaults**

Require green native matrix, full parity/E2E, benchmark acceptance, production-data compatibility, staging rehearsal, signed/checksummed artifacts, and owner approval. If any gate fails, stop this task.

- [ ] **Step 2: Point production build/start/release to Go**

Retain explicit `start:bun`, `build:bun`, and pinned rollback workflow during stabilization. Development frontend commands may continue using Bun/Vite. Ensure npm package launcher selects the correct downloaded Go artifact per supported platform.

- [ ] **Step 3: Update release notes and compatibility statement**

State drop-in data/API compatibility, supported platforms, backup recommendation, known differences, and rollback asset identifiers.

- [ ] **Step 4: Run release-candidate verification**

```bash
go test -race ./...
bun test
bun run build:tsc
bun run build:production
bun run test:e2e:go
bun run test:benchmark:compare
```

Build and smoke every release artifact and Docker image on its native platform.

- [ ] **Step 5: Commit**

```bash
git add package.json Dockerfile Dockerfile.prebuilt .github/workflows/release.yml README.md docs/INTEGRATED_SETUP.md
git commit -m "feat: switch GOWA Manager backend to Go"
```

### Task 12: Execute per-host production canary

**Files:**
- Create: `docs/release/go-canary-YYYY-MM-DD.md`

- [ ] **Step 1: Select one noncritical representative host**

Record explicit owner, maintenance window, observation duration, rollback threshold, backup location, and communication channel.

- [ ] **Step 2: Execute scripted backup/preflight/cutover/smoke**

Use only verified release artifacts and committed runbook commands. One host runs one manager.

- [ ] **Step 3: Observe defined metrics**

Monitor HTTP errors/latency, process start/restart failures, active/orphan processes, proxy/WebSocket errors, SQLite busy/errors, scheduler failures, RSS/goroutines/handles, and user-visible behavior.

- [ ] **Step 4: Decide promote, hold, or rollback**

Any mandatory rollback trigger overrides schedule pressure. Record evidence and decision. Promote additional hosts one at a time or in explicitly approved batches only after the first window passes.

- [ ] **Step 5: Commit redacted canary record**

```bash
git add docs/release/go-canary-*.md
git commit -m "docs: record Go backend canary"
```

### Task 13: Stabilize before considering Bun source retirement

**Files:**
- Modify: `BACKLOG.md` if present and using project convention
- Create: `docs/release/go-stabilization.md`

- [ ] **Step 1: Define and complete stabilization window**

Track every host, rollback, incident, resource trend, and compatibility issue. Continue shipping the pinned Bun rollback artifact.

- [ ] **Step 2: Verify recovery with Go**

Perform scheduled backup restore, manager restart, host reboot, and version rollback drills using Go.

- [ ] **Step 3: Make retirement a separate decision**

Do not delete `src/**`, Bun tests, or rollback artifacts in this plan. Create a separate reviewed proposal only after all hosts are stable and no rollback condition remains.

- [ ] **Step 4: Commit stabilization evidence**

```bash
git add docs/release/go-stabilization.md BACKLOG.md
git commit -m "docs: record Go backend stabilization"
```

## Final release gate

The rewrite is complete only when:

- native Linux amd64/arm64 and Windows amd64 suites pass;
- Docker persistence/shutdown tests pass;
- full Bun/Go contracts and frontend E2E pass;
- benchmarks meet approved thresholds;
- representative data safety and Bun reopen checks pass;
- backup and both rollback paths are rehearsed;
- canary succeeds without mandatory rollback triggers;
- all production hosts finish stabilization;
- Bun rollback assets remain retrievable.
