# Go Rewrite 01: Compatibility Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish a buildable Go backend shell with compatible CLI configuration, SQLite access, exclusive data-directory ownership, health/static HTTP serving, and reusable Bun-versus-Go contract fixtures.

**Architecture:** Add Go beside the unchanged Bun backend. Use `cmd/gowa-manager-go` as the executable entry point and focused packages under `internal/`. This plan deliberately stops before domain write APIs and process supervision; it produces a safe shell and the test infrastructure required by later plans.

**Tech Stack:** Go 1.24, `net/http`, `log/slog`, `embed`, `github.com/gofrs/flock`, Bun test runner for the baseline, Go `testing`/`httptest`. The SQLite driver (default `modernc.org/sqlite`) and WebSocket library (default `github.com/coder/websocket`) are validated and pinned by the Task 2 spike before any code depends on them.

---

## Dependencies and outputs

- Depends on approved design: `docs/superpowers/specs/2026-07-18-go-backend-big-bang-rewrite-design.md`.
- Must not delete or rewrite `src/**`.
- Produces a Go executable that supports compatible `--help`, `--version`, configuration precedence, database initialization, manager locking, `/api/health`, SPA static serving, and graceful HTTP shutdown.
- Later plans extend this executable; this plan is not a production cutover.

## File structure

- Create `go.mod`, `go.sum`: Go module and pinned dependencies.
- Create `docs/superpowers/spikes/2026-07-18-go-rewrite-tech-spikes.md`: recorded driver/library/journal-mode decisions from the Task 2 spike.
- Create `cmd/gowa-manager-go/main.go`: thin process entry point.
- Create `internal/buildinfo/version.go`: manager version injected by build flags.
- Create `internal/config/config.go`, `internal/config/config_test.go`: CLI/env parsing and validation.
- Create `internal/ownership/lock.go`, platform tests: exclusive data-directory lock.
- Create `internal/database/database.go`, `schema.go`, tests and fixtures: compatible SQLite initialization.
- Create `internal/httpapi/server.go`, `health.go`, `static.go` and tests: HTTP shell.
- Create `internal/app/app.go`, `app_test.go`: dependency wiring and shutdown.
- Create `internal/testutil/process.go`, `ports.go`, `database.go`: shared integration helpers.
- Create `test/contract/README.md`, `test/contract/normalize.go`, `normalize_test.go`: differential-test conventions.
- Create `scripts/build-go.ts`: frontend build, version injection, and Go build orchestration.
- Modify `.gitignore`: ignore Go binaries and temporary contract artifacts.
- Modify `package.json`: add non-production Go development commands without changing current Bun production commands.

### Task 1: Initialize the isolated Go module

**Files:**
- Create: `go.mod`
- Create: `cmd/gowa-manager-go/main.go`
- Create: `internal/buildinfo/version.go`
- Modify: `.gitignore`

- [ ] **Step 1: Write the minimal entry-point compile test**

Create `internal/buildinfo/version_test.go`:

```go
package buildinfo

import "testing"

func TestDisplayVersion(t *testing.T) {
	old := Version
	Version = "1.8.1-test"
	t.Cleanup(func() { Version = old })
	if got := DisplayVersion(); got != "GOWA Manager v1.8.1-test" {
		t.Fatalf("DisplayVersion() = %q", got)
	}
}
```

- [ ] **Step 2: Run the test and verify the package is missing**

Run: `go test ./internal/buildinfo`

Expected: FAIL because `go.mod` and the package do not exist.

- [ ] **Step 3: Add the module and implementation**

Use module path `github.com/fadlee/gowa-manager`. Set Go version to `1.24`. Implement:

```go
package buildinfo

var Version = "dev"

func DisplayVersion() string { return "GOWA Manager v" + Version }
```

Create `main.go` that calls a temporary `run()` returning an exit code; do not add business logic yet. Add `/gowa-manager-go`, `/gowa-manager-go.exe`, `/dist-go/`, and `/.contract-tmp/` to `.gitignore`.

- [ ] **Step 4: Verify formatting and compilation**

Run: `gofmt -w cmd/gowa-manager-go internal/buildinfo && go test ./internal/buildinfo && go build ./cmd/gowa-manager-go`

Expected: PASS and a buildable command.

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/gowa-manager-go internal/buildinfo .gitignore
git commit -m "chore: initialize Go backend module"
```

### Task 2: De-risk the SQLite driver, WebSocket library, and journal-mode baseline

**Rationale:** Two technology choices (the pure-Go SQLite driver and the WebSocket library) and one unknown (the journal mode/pragmas of live Bun databases) are the highest-risk assumptions in the whole rewrite. The design flags the SQLite driver as "subject to an early compatibility spike." Resolve all three here, before Task 5 builds real database code and before Plan 04 builds the proxy, so a late incompatibility cannot force a rewrite. This task is a throwaway investigation plus a committed decision record; it does not add production code or dependencies to the root module.

**Files:**
- Create: `docs/superpowers/spikes/2026-07-18-go-rewrite-tech-spikes.md` (committed findings and decisions)
- Create: `spike/` (throwaway experiments with their own `go.mod`; gitignored, never committed)
- Modify: `.gitignore`

- [ ] **Step 1: Record the Bun database journal-mode and pragma baseline**

The current Bun backend opens SQLite with `new Database(path)` and no options in `src/db.ts`, so it neither enables WAL nor sets `busy_timeout`, and it relies heavily on `INSERT/UPDATE ... RETURNING *`. Confirm the actual runtime state rather than assuming it.

Against a database created by the running Bun backend (and, if available, a sanitized copy of a real `gowa.db`), record `PRAGMA journal_mode`, `PRAGMA busy_timeout`, `PRAGMA foreign_keys`, `PRAGMA encoding`, and `PRAGMA user_version`. Capture the exact statement shapes from `src/db.ts` that the Go driver must support, especially every `RETURNING *` query. Write the results into the spike document.

- [ ] **Step 2: Prove SQLite driver compatibility (default `modernc.org/sqlite`)**

In `spike/sqlite/` (its own throwaway module), prove against the driver that:

- it opens a database created by `bun:sqlite` without migration;
- `INSERT ... RETURNING *` and `UPDATE ... RETURNING *` return the expected rows;
- `busy_timeout=5000` is honored;
- two concurrent writers behave correctly under the journal mode recorded in Step 1 (no corruption, no unexpected `SQLITE_BUSY` beyond the timeout);
- `PRAGMA integrity_check` returns `ok` after Go writes;
- `bun:sqlite` can reopen the same file afterward and read back Go-written values.

If `modernc.org/sqlite` fails any hard requirement, record the failure and evaluate the documented fallback (a cgo driver such as `github.com/mattn/go-sqlite3`) together with its cross-compilation cost for Linux amd64/arm64 and Windows amd64. **Do not start Task 5 until a driver is chosen and recorded.** Pin a reviewed version published at least 7 days ago.

- [ ] **Step 3: Evaluate the WebSocket bridging library**

In `spike/websocket/` (throwaway module), evaluate the candidate library (default `github.com/coder/websocket`) for the behavior Plan 04 needs: client dial and server upgrade, text and binary frames, ping/pong, close code/reason propagation, `context` cancellation, and pure-Go build with no cgo. Confirm the currently maintained version and API surface. Record the chosen library and version so Plan 04 does not re-litigate the decision.

- [ ] **Step 4: Write the decision record and gate later plans**

`docs/superpowers/spikes/2026-07-18-go-rewrite-tech-spikes.md` must state: the chosen SQLite driver and pinned version; the chosen WebSocket library and pinned version; the recorded journal mode, `busy_timeout`, and encoding; and every caveat later plans must honor (for example "do not enable WAL in the first release", "preserve `RETURNING`", and any WebSocket message-size limit). Add `/spike/` to `.gitignore`. Do not commit the throwaway `spike/` code, and do not add these dependencies to the root `go.mod` yet — Task 5 and Plan 04 add them where they are actually used.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/spikes/2026-07-18-go-rewrite-tech-spikes.md .gitignore
git commit -m "docs: record Go rewrite technology spikes"
```

### Task 3: Implement compatible CLI and environment parsing

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `cmd/gowa-manager-go/main.go`

- [ ] **Step 1: Add table-driven compatibility tests**

Define:

```go
type Config struct {
	Port          int
	AdminUsername string
	AdminPassword string
	DataDir       string
}

type Action int
const (
	ActionRun Action = iota
	ActionHelp
	ActionVersion
)

func Parse(args []string, getenv func(string) string) (Config, Action, error)
```

Tests must cover defaults (`3000`, `admin`, `password`, `./data`), environment values, CLI precedence, short and long flags, empty/missing values, port range `1..65535`, username max 50, password max 100, unknown options, unexpected positional arguments, and removal of a duplicated executable argument matching the legacy rules in `src/cli.ts`.

- [ ] **Step 2: Run tests and confirm failure**

Run: `go test ./internal/config -run TestParse -v`

Expected: FAIL because `Parse` does not exist.

- [ ] **Step 3: Implement parsing without a global `flag.CommandLine`**

Use a dedicated `flag.FlagSet` or explicit parser so tests are deterministic. Define typed errors carrying the exact legacy user-facing message. Export `HelpText()` with the same options and environment names as `src/cli.ts`; `VersionText()` must use `buildinfo.DisplayVersion()` and identify the Go build.

- [ ] **Step 4: Wire actions into `main.go`**

`main()` calls `os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))`. Help/version return 0; parse errors print once to stderr and return 1. Never print the admin password.

- [ ] **Step 5: Verify tests and command behavior**

Run:

```bash
go test ./internal/config -v
go run ./cmd/gowa-manager-go -- --help
go run ./cmd/gowa-manager-go -- --version
```

Expected: tests PASS; both commands exit 0 and display compatible information.

- [ ] **Step 6: Commit**

```bash
git add internal/config cmd/gowa-manager-go
git commit -m "feat: add compatible Go CLI configuration"
```

### Task 4: Add exclusive data-directory ownership

**Files:**
- Create: `internal/ownership/lock.go`
- Create: `internal/ownership/lock_test.go`

- [ ] **Step 1: Write lock behavior tests**

Specify:

```go
type Lock struct { /* private */ }
func Acquire(dataDir string) (*Lock, error)
func (l *Lock) Release() error
```

Tests use `t.TempDir()` and prove: directory creation, first acquisition succeeds, second acquisition fails with `ErrAlreadyLocked`, release allows reacquisition, and repeated release is safe.

- [ ] **Step 2: Run the tests to verify failure**

Run: `go test ./internal/ownership -v`

Expected: FAIL because the package is absent.

- [ ] **Step 3: Implement with `github.com/gofrs/flock`**

Lock `${dataDir}/.gowa-manager.lock`. Wrap errors with the path, but do not delete an active lock file. Add the dependency with `go get github.com/gofrs/flock@<reviewed-current-version>` and commit the resulting checksums. The executor must record the selected version in the commit body.

- [ ] **Step 4: Verify process-level contention**

Add a helper-process test using `os/exec` so contention is proven across processes, not only two objects in one test process.

Run: `go test -race ./internal/ownership -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/ownership
git commit -m "feat: lock Go manager data directory"
```

### Task 5: Implement compatible SQLite initialization

**Files:**
- Create: `internal/database/database.go`
- Create: `internal/database/schema.go`
- Create: `internal/database/database_test.go`
- Create: `internal/database/testdata/legacy.sql`

- [ ] **Step 1: Add database contract tests**

Tests must prove that `Open(ctx, dataDir)` creates `gowa.db` and the legacy `instances` table; existing databases missing `gowa_version` or `error_message` are upgraded idempotently; all existing rows survive; `PRAGMA integrity_check` returns `ok`; a second open observes `busy_timeout`; and timestamps/config remain text-compatible.

Use this schema as the required final shape:

```sql
CREATE TABLE IF NOT EXISTS instances (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL UNIQUE,
  port INTEGER,
  status TEXT DEFAULT 'stopped',
  config TEXT DEFAULT '{}',
  gowa_version TEXT DEFAULT 'latest',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  error_message TEXT DEFAULT NULL
);
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/database -v`

Expected: FAIL because `Open` is undefined.

- [ ] **Step 3: Implement using the driver chosen in the Task 2 spike (default `modernc.org/sqlite`)**

Use the driver, version, journal mode, and pragmas recorded in `docs/superpowers/spikes/2026-07-18-go-rewrite-tech-spikes.md`. Expose:

```go
type DB struct { SQL *sql.DB }
func Open(ctx context.Context, dataDir string) (*DB, error)
func (d *DB) IntegrityCheck(ctx context.Context) error
func (d *DB) Close() error
```

Set `busy_timeout=5000`, constrain writer concurrency to one connection unless benchmarks justify another setting, and apply additive migrations in a transaction. Do not change journal mode in this plan.

- [ ] **Step 4: Add Bun reopen compatibility test**

Create a temporary DB with Go, insert a row, then invoke a small Bun subprocess importing `bun:sqlite` to read it. Skip only when Bun is unavailable, with an explicit skip reason. Verify values rather than only open success.

- [ ] **Step 5: Run race and compatibility tests**

Run: `go test -race ./internal/database -v`

Expected: PASS, including Bun reopen test on the project development environment.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/database
git commit -m "feat: initialize compatible SQLite database"
```

### Task 6: Build the HTTP shell and health contract

**Files:**
- Create: `internal/httpapi/server.go`
- Create: `internal/httpapi/health.go`
- Create: `internal/httpapi/server_test.go`

- [ ] **Step 1: Write health, recovery, and middleware tests**

Define `New(Dependencies) http.Handler`. Test `GET /api/health` returns status 200, content type JSON, and exactly:

```json
{"message":"GOWA Manager API is running","success":true}
```

Also test unsupported methods, panic recovery without stack disclosure, request IDs, CORS values derived from configuration, and that API 404 responses are JSON rather than SPA HTML.

- [ ] **Step 2: Run the tests to verify failure**

Run: `go test ./internal/httpapi -run 'TestHealth|TestRecovery|TestCORS' -v`

Expected: FAIL because the server does not exist.

- [ ] **Step 3: Implement the handler shell**

Use `http.ServeMux`; centralize JSON encoding and typed error mapping. Add safe `slog` request logging with method, route, status, and duration. Do not log authorization headers or bodies.

- [ ] **Step 4: Verify tests**

Run: `go test -race ./internal/httpapi -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi
git commit -m "feat: add Go HTTP server shell"
```

### Task 7: Embed and serve the React production build

**Files:**
- Create: `internal/static/assets.go`
- Create: `internal/httpapi/static.go`
- Create: `internal/httpapi/static_test.go`
- Create: `web/.gitkeep`
- Modify: `scripts/build-go.ts`

- [ ] **Step 1: Add static-serving tests with an in-memory filesystem**

Test `/`, `/instances/1`, and `/instances/anything` serve `index.html` with `Cache-Control: no-cache`; `/assets/app.js` serves its content and immutable cache headers; `/favicon.ico` uses a one-year cache; missing assets return 404 rather than SPA HTML.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/httpapi -run TestStatic -v`

Expected: FAIL.

- [ ] **Step 3: Implement filesystem-injected static serving**

Production uses an `embed.FS`; tests inject `fstest.MapFS`. Keep embedding in `internal/static` so `httpapi` does not own build artifacts. `scripts/build-go.ts` runs the existing client build, copies the generated production files into `web/`, generates the manager version, and invokes `go build -ldflags "-X github.com/fadlee/gowa-manager/internal/buildinfo.Version=<version>"`.

- [ ] **Step 4: Build the frontend and Go binary**

Run: `bun run build:client && bun run scripts/build-go.ts`

Expected: React build succeeds and `dist-go/gowa-manager-go` (or `.exe`) is created.

- [ ] **Step 5: Verify static tests and commit**

Run: `go test ./internal/httpapi ./internal/static -v`

```bash
git add internal/static internal/httpapi web/.gitkeep scripts/build-go.ts
git commit -m "feat: embed frontend in Go server"
```

### Task 8: Wire application startup and graceful HTTP shutdown

**Files:**
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`
- Modify: `cmd/gowa-manager-go/main.go`

- [ ] **Step 1: Write application lifecycle tests**

Inject listener, lock, and database factories. Prove startup order is lock → database → HTTP; a database failure releases the lock; cancellation calls `http.Server.Shutdown`, closes DB, then releases lock; and a second signal path can force termination without deadlock.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/app -v`

Expected: FAIL.

- [ ] **Step 3: Implement `app.Run`**

Expose:

```go
type Options struct {
	Config config.Config
	Logger *slog.Logger
}
func Run(ctx context.Context, opts Options) error
```

Find the first available manager HTTP port from the configured port through 100 attempts, matching current fallback behavior. Create a signal-aware context in `main.go`. Do not start schedulers or child processes in this plan.

- [ ] **Step 4: Verify lifecycle and smoke test**

Run:

```bash
go test -race ./internal/app ./internal/httpapi ./internal/database ./internal/ownership
go run ./cmd/gowa-manager-go -- --data-dir .contract-tmp/foundation --port 39000
```

In another shell: `curl -i http://127.0.0.1:39000/api/health`.

Expected: compatible JSON, then clean shutdown on Ctrl+C.

- [ ] **Step 5: Commit**

```bash
git add internal/app cmd/gowa-manager-go
git commit -m "feat: wire Go application lifecycle"
```

### Task 9: Establish reusable contract normalization

**Files:**
- Create: `test/contract/README.md`
- Create: `test/contract/normalize.go`
- Create: `test/contract/normalize_test.go`
- Create: `internal/testutil/process.go`
- Create: `internal/testutil/ports.go`
- Create: `internal/testutil/database.go`

- [ ] **Step 1: Write normalizer tests**

Define a response snapshot containing status, selected headers, JSON body, database rows, and filesystem paths. Tests must normalize timestamps, PID, random manager/instance ports, temporary absolute paths, and JSON object key order while preserving arrays and meaningful strings.

- [ ] **Step 2: Implement the typed snapshot normalizer**

Do not use unrestricted regex replacement over arbitrary response strings. Walk decoded JSON values and normalize only named fields (`created_at`, `updated_at`, `pid`, `uptime`, known paths/ports).

- [ ] **Step 3: Add shared process helpers**

Helpers start a command with a temporary data directory, wait for health with a deadline, capture stdout/stderr, and always terminate the process tree in `t.Cleanup`. Port helpers must bind `127.0.0.1:0` rather than guess a port.

- [ ] **Step 4: Run tests**

Run: `go test -race ./test/contract ./internal/testutil -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add test/contract internal/testutil
git commit -m "test: add backend contract test foundation"
```

### Task 10: Add developer commands and foundation verification

**Files:**
- Modify: `package.json`
- Modify: `README.md`

- [ ] **Step 1: Add additive scripts**

Add `dev:go`, `build:go`, and `test:go`. Do not change `start`, `build:production`, `compile`, or release defaults yet. Document that Go is experimental and cannot share a data directory with Bun.

- [ ] **Step 2: Run complete foundation verification**

Run:

```bash
go fmt ./...
go vet ./...
go test -race ./...
bun run build:tsc
bun test
bun run build:go
git diff --check
```

Expected: all commands PASS; existing Bun test behavior remains unchanged.

- [ ] **Step 3: Commit**

```bash
git add package.json README.md
git commit -m "docs: add experimental Go backend workflow"
```

## Plan completion gate

Before starting Plan 02:

- technology spikes are recorded: SQLite driver, WebSocket library, and the Bun journal-mode/pragma baseline are decided and documented, with `modernc.org/sqlite` (or a recorded fallback) proven against a Bun-created database;
- Go shell builds on the developer OS.
- CLI compatibility tests pass.
- SQLite can be reopened by Bun after Go writes.
- exclusive locking works across processes;
- health and static SPA contracts pass;
- all Bun tests still pass;
- no production command points to Go.
