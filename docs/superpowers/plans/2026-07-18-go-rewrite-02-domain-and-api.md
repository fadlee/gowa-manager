# Go Rewrite 02: Domain, SQLite, and Management API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the complete non-runtime management domain in Go: instance persistence and filesystem workflows, system APIs, device summaries, GOWA version discovery/install/cleanup, and API response parity.

**Architecture:** Build repositories and application services behind narrow interfaces, then mount legacy-compatible HTTP handlers. Lifecycle actions are represented by an injected interface and remain fake in this plan; the real process supervisor is added in Plan 03. Filesystem changes use staging/compensation around SQLite transactions.

**Tech Stack:** Go 1.24, `database/sql`, `modernc.org/sqlite`, `net/http`, `encoding/json`, `archive/zip`, `crypto/sha256`, Go tests and Bun/Go contract harness.

---

## Dependencies and outputs

- Requires Plan 01 completion.
- Uses the unchanged Bun implementation and tests in `src/modules/instances/**` and `src/modules/system/**` as behavior references.
- Produces complete instance CRUD/read APIs, system status/config/port APIs, device/test-connection/admin-link application hooks, and all version APIs.
- Start/stop/kill/restart endpoints are mounted against a fake lifecycle adapter only in tests until Plan 03.

## File structure

- Create `internal/instances/model.go`, `errors.go`, `repository.go`, `repository_sqlite.go`, `service.go` and tests.
- Create `internal/instances/config.go`, `name.go`, `filesystem.go`, `devices.go`, `connection.go` and tests.
- Create `internal/system/service.go`, `ports.go` and tests.
- Create `internal/versions/model.go`, `github.go`, `installer.go`, `service.go` and tests.
- Create `internal/httpapi/instances.go`, `system.go`, `versions.go`, schemas and tests.
- Create `test/contract/management_test.go` and deterministic fixtures.
- Modify `internal/httpapi/server.go` and `internal/app/app.go` to wire repositories and routes.

### Task 1: Define instance domain types and SQLite repository

**Files:**
- Create: `internal/instances/model.go`
- Create: `internal/instances/errors.go`
- Create: `internal/instances/repository.go`
- Create: `internal/instances/repository_sqlite.go`
- Create: `internal/instances/repository_sqlite_test.go`

- [ ] **Step 1: Write repository tests against temporary SQLite**

Define `Instance` with `ID int64`, `Key string`, `Name string`, `Port *int`, `Status string`, `Config string`, `GOWAVersion string`, `ErrorMessage *string`, `CreatedAt string`, and `UpdatedAt string`. Test list ordering, lookups by ID/key, create with `RETURNING`, update while preserving key/port, status/error updates, port update, delete, and unique conflicts for key/name.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/instances -run TestSQLiteRepository -v`

Expected: FAIL because repository types are absent.

- [ ] **Step 3: Implement repository interfaces and adapter**

Use explicit scan helpers and typed sentinel errors `ErrNotFound` and `ErrConflict`. Never expose `sql.ErrNoRows` above the adapter. Preserve SQLite timestamp strings exactly; do not convert them to localized time strings.

- [ ] **Step 4: Run repository tests with race detector**

Run: `go test -race ./internal/instances -run TestSQLiteRepository -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/instances
git commit -m "feat: add Go instance repository"
```

### Task 2: Port instance configuration and name generation

**Files:**
- Create: `internal/instances/config.go`
- Create: `internal/instances/config_test.go`
- Create: `internal/instances/name.go`
- Create: `internal/instances/name_test.go`

- [ ] **Step 1: Translate existing TypeScript cases into table tests**

Port the behavioral cases from `config-parser.test.ts`, `update-config.test.ts`, and `name-generator.test.ts`. Cover default `rest` arguments, `PORT` substitution, environment parsing, flags, webhook enable toggles, basePath normalization to `/app/{key}`, invalid JSON fallback on create, and preserving existing config on update.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/instances -run 'TestConfig|TestName' -v`

Expected: FAIL.

- [ ] **Step 3: Implement typed config with raw compatibility boundary**

Decode only fields the manager modifies; preserve unknown JSON fields using `map[string]json.RawMessage`. Generate eight-character keys from `A-Z0-9` using `crypto/rand`, retry repository conflicts, and use the same vocabulary/pattern as the Bun random-name generator.

- [ ] **Step 4: Add property tests**

Use Go fuzz tests to prove generated keys always match `^[A-Z0-9]{8}$` and update normalization always restores the correct basePath without deleting unknown config fields.

- [ ] **Step 5: Verify and commit**

Run: `go test -race ./internal/instances -v`

```bash
git add internal/instances/config* internal/instances/name*
git commit -m "feat: port instance configuration behavior"
```

### Task 3: Implement staged instance filesystem operations

**Files:**
- Create: `internal/instances/filesystem.go`
- Create: `internal/instances/filesystem_test.go`

- [ ] **Step 1: Write filesystem tests**

Specify directories under `${DATA_DIR}/instances/{id}`. Test create idempotency, staged deletion via rename, restoration, reset to an empty recreated directory, missing directory behavior, path traversal rejection, and cleanup after injected failures.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/instances -run TestFilesystem -v`

Expected: FAIL.

- [ ] **Step 3: Implement `Filesystem`**

Expose `Ensure`, `StageDelete`, `Restore`, and `Purge`. Temporary trash paths stay under `${DATA_DIR}/.trash/` and include instance ID plus random suffix. Validate that every resolved path remains under the configured data directory.

- [ ] **Step 4: Test Windows-compatible path handling**

Use `filepath` APIs only. Add tests with separators and invalid absolute paths; do not hardcode `/` outside URL construction.

- [ ] **Step 5: Verify and commit**

Run: `go test -race ./internal/instances -run TestFilesystem -v`

```bash
git add internal/instances/filesystem*
git commit -m "feat: add safe instance filesystem workflows"
```

### Task 4: Implement instance CRUD service with compensation

**Files:**
- Create: `internal/instances/service.go`
- Create: `internal/instances/service_test.go`

- [ ] **Step 1: Write service tests with repository/filesystem fakes**

Cover generated name/key, next port allocation, default config, version default `latest`, directory creation, update preserving key/port, deletion/reset while stopped, conflict mapping, and compensation when DB or filesystem operations fail. For a running instance, verify delete/reset calls the injected lifecycle `Stop` before staging files.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/instances -run TestService -v`

Expected: FAIL.

- [ ] **Step 3: Implement the service**

Define narrow dependencies:

```go
type PortAllocator interface { Next(context.Context) (int, error) }
type Lifecycle interface {
	Stop(context.Context, int64) (Status, error)
	Status(context.Context, int64) (Status, error)
}
```

Create uses a DB transaction coordinated with directory staging. Delete/reset use reversible trash rename before commit and purge only after commit. Return typed errors suitable for HTTP mapping.

- [ ] **Step 4: Verify failure injection and race tests**

Run: `go test -race ./internal/instances -run TestService -v`

Expected: PASS with no leaked temporary directories.

- [ ] **Step 5: Commit**

```bash
git add internal/instances/service*
git commit -m "feat: add instance management service"
```

### Task 5: Implement system status and port allocation

**Files:**
- Create: `internal/system/service.go`
- Create: `internal/system/ports.go`
- Create: `internal/system/service_test.go`
- Create: `internal/system/ports_test.go`

- [ ] **Step 1: Port system service cases**

Test counts for total/running/stopped, uptime in milliseconds, manager version, allocated-port count, reported next port, config paths, ports below 1024 rejected, instance port 3000 reserved, manager HTTP port 3000 allowed, and allocation starts at 8000 while skipping DB and OS usage.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/system -v`

Expected: FAIL.

- [ ] **Step 3: Implement bind-based availability checks**

Use `net.Listen("tcp", "127.0.0.1:<port>")` and close immediately. Keep manager-port and instance-port rules separate. Add a bounded maximum (`65535`) and return `ErrNoAvailablePort` instead of looping forever.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/system -v`

```bash
git add internal/system
git commit -m "feat: add Go system and port services"
```

### Task 6: Implement device summaries and connection tests

**Files:**
- Create: `internal/instances/devices.go`
- Create: `internal/instances/devices_test.go`
- Create: `internal/instances/connection.go`
- Create: `internal/instances/connection_test.go`

- [ ] **Step 1: Port device-client behavior into HTTP fixture tests**

Cover live response, cache use, stale cache, non-running source, malformed JSON, timeout, HTTP error, auth config, count/connected/fetchedAt/error fields, and cache clearing on reset/delete/stop.

- [ ] **Step 2: Add test-connection behavior cases**

Verify `ok`, optional status/body, message text, bounded body size, timeout, unavailable instance, and no secret leakage.

- [ ] **Step 3: Implement clients with injected `http.Client` and clock**

Use context deadlines and per-instance synchronized cache entries. Never log response bodies by default. Ensure response bodies are always closed.

- [ ] **Step 4: Run leak/race tests and commit**

Run: `go test -race ./internal/instances -run 'TestDevice|TestConnection' -v`

```bash
git add internal/instances/devices* internal/instances/connection*
git commit -m "feat: add instance device and connection clients"
```

### Task 7: Implement version discovery and installed-version resolution

**Files:**
- Create: `internal/versions/model.go`
- Create: `internal/versions/github.go`
- Create: `internal/versions/github_test.go`
- Create: `internal/versions/service.go`
- Create: `internal/versions/service_test.go`

- [ ] **Step 1: Write installed/available version tests**

Cover `${DATA_DIR}/bin/versions/{version}/gowa[.exe]`, installed metadata, `latest` resolution, available GitHub release merge, request `limit`, API failure returning the legacy-compatible fallback, disk usage, active version protection, and rejecting removal of `latest`.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/versions -v`

Expected: FAIL.

- [ ] **Step 3: Implement GitHub client and service**

Use injected base URL and `http.Client`; set `Accept`, user-agent, and bounded timeouts. Parse release tags and assets without assuming response order. Use `runtime.GOOS/GOARCH` to choose Linux amd64/arm64 and Windows amd64 assets.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/versions -v`

```bash
git add internal/versions/model.go internal/versions/github* internal/versions/service*
git commit -m "feat: add GOWA version discovery"
```

### Task 8: Implement atomic version installation and cleanup

**Files:**
- Create: `internal/versions/installer.go`
- Create: `internal/versions/installer_test.go`

- [ ] **Step 1: Write archive and failure tests**

Use local fixture servers and generated ZIP archives. Test download, platform asset selection, extraction to staging, binary-name validation, executable permission on Linux, atomic final rename, interrupted download cleanup, zip-slip rejection, existing-version idempotency, removal, and keep-count cleanup.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/versions -run TestInstaller -v`

Expected: FAIL.

- [ ] **Step 3: Implement secure installer**

Stream to a temporary file while calculating SHA-256 for logs/diagnostics. Bound response and extracted sizes. Reject archive paths escaping staging. Never expose GitHub token headers in errors. Replace a version only through same-filesystem rename.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/versions -run 'TestInstaller|TestCleanup' -v`

```bash
git add internal/versions/installer*
git commit -m "feat: install and clean GOWA versions safely"
```

### Task 9: Add instance management HTTP routes

**Files:**
- Create: `internal/httpapi/instances.go`
- Create: `internal/httpapi/instances_test.go`
- Modify: `internal/httpapi/server.go`

- [ ] **Step 1: Port route contract cases**

Cover all existing paths: list, devices, detail, create, update, delete, reset-data, start, stop, kill, restart, status, admin-link, and test-connection. Assert legacy status codes and JSON shapes from `src/modules/instances/index.ts` tests. Lifecycle endpoints use an injected fake in this plan.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/httpapi -run TestInstanceRoutes -v`

Expected: FAIL.

- [ ] **Step 3: Implement strict handlers**

Use `json.Decoder`, reject invalid IDs, enforce name lengths, preserve permissive create-config fallback, and map typed errors centrally. Do not return Go field names; define explicit JSON response structs with `gowa_version`, `error_message`, `created_at`, and `updated_at`.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/httpapi -run TestInstanceRoutes -v`

```bash
git add internal/httpapi/instances* internal/httpapi/server.go
git commit -m "feat: add Go instance management routes"
```

### Task 10: Add system and version HTTP routes

**Files:**
- Create: `internal/httpapi/system.go`
- Create: `internal/httpapi/system_test.go`
- Create: `internal/httpapi/versions.go`
- Create: `internal/httpapi/versions_test.go`
- Modify: `internal/httpapi/server.go`

- [ ] **Step 1: Add route tests from existing Bun suites**

Cover `/api/system/status`, `/ports/next`, `/config`, `/ports/:port/available`, auto-update placeholder adapter routes, and all `/api/system/versions/**` methods. Assert response fields, errors, query defaults, invalid limits/keepCount, and deletion conflicts.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/httpapi -run 'TestSystemRoutes|TestVersionRoutes' -v`

Expected: FAIL.

- [ ] **Step 3: Implement handlers against interfaces**

Keep auto-update behind an injected interface so Plan 03 can provide the scheduler implementation. Version install/delete/cleanup calls are context-aware and return existing success/error envelopes.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/httpapi -run 'TestSystemRoutes|TestVersionRoutes' -v`

```bash
git add internal/httpapi/system* internal/httpapi/versions* internal/httpapi/server.go
git commit -m "feat: add Go system and version routes"
```

### Task 11: Wire domain services into the application

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Add dependency-wiring tests**

Prove one DB instance is shared by repositories, configured data paths reach filesystem/version services, and partial initialization closes already-created dependencies in reverse order.

- [ ] **Step 2: Wire production adapters**

Construct repository, port allocator, filesystem, device client, connection tester, version service, system service, and HTTP handlers. Provide a lifecycle adapter returning `ErrRuntimeNotReady` only if a lifecycle route is manually called before Plan 03; tests must assert this is never mistaken for success.

- [ ] **Step 3: Verify application API smoke tests**

Start Go on a temporary directory and use `curl` to create, list, update, reset, and delete an instance. Confirm SQLite with `PRAGMA integrity_check`.

- [ ] **Step 4: Commit**

```bash
git add internal/app
git commit -m "feat: wire Go management domain"
```

### Task 12: Add Bun-versus-Go management contract suite

**Files:**
- Create: `test/contract/management_test.go`
- Create: `test/contract/testdata/instance-config.json`
- Modify: `test/contract/README.md`

- [ ] **Step 1: Implement dual-backend test setup**

Start Bun and Go separately with independent temp directories, identical environment/credentials, and fixture versions. Never run them on the same DB. Execute the same ordered request scenarios and normalize nondeterministic fields.

- [ ] **Step 2: Compare read/write scenarios**

Cover health, auth-required management endpoints, create/list/detail/update/reset/delete, system status/config/ports, installed/available/usage versions, install failure, cleanup, devices while stopped, and test-connection failure.

- [ ] **Step 3: Compare side effects**

After each scenario compare normalized SQLite rows and relative filesystem trees. Verify each resulting DB can be read by Bun.

- [ ] **Step 4: Run the focused suite**

Run: `go test -race ./test/contract -run TestManagementParity -v -count=1`

Expected: PASS with zero unexplained snapshots.

- [ ] **Step 5: Commit**

```bash
git add test/contract
git commit -m "test: verify Go management API parity"
```

### Task 13: Verify Plan 02

- [ ] Run:

```bash
go fmt ./...
go vet ./...
go test -race ./internal/instances ./internal/system ./internal/versions ./internal/httpapi ./internal/app
go test ./test/contract -run TestManagementParity -v -count=1
bun run build:tsc
bun test
git diff --check
```

Expected: all PASS.

- [ ] Commit any test-only corrections with `test: complete Go management parity`; do not squash away the task-level commits unless the repository owner requests it.

## Plan completion gate

Before Plan 03:

- All non-runtime management APIs have Bun/Go parity.
- Existing SQLite data remains readable by both implementations.
- Filesystem failure injection leaves no partial state.
- Version downloads are secure and atomic.
- Lifecycle routes exist but have not been connected to real process execution.
