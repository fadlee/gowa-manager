# GOWA Manager Go Backend Big-Bang Rewrite — Design

> Date: 2026-07-18
> Status: Approved

## 1. Purpose

Rewrite the GOWA Manager backend from Bun, Elysia, and TypeScript to Go to improve:

- single-binary distribution and deployment;
- operational stability, especially process supervision and proxying;
- CPU and memory efficiency.

The rewrite is a big-bang implementation and cutover. It is not a progressive module migration and will not introduce a hybrid Bun/Go production runtime.

## 2. Decisions and Constraints

- The Go backend must be a complete drop-in replacement.
- The React frontend must work without API contract changes.
- Existing `data/` directories and `gowa.db` databases must remain usable.
- The first stable release must support Linux amd64, Linux arm64, and Windows amd64.
- Docker Linux remains a supported deployment path.
- The Go implementation must reach full feature parity before production canary rollout.
- Canary rollout occurs per host. A host runs either Bun or Go, never both against the same data directory.
- The Bun backend remains unchanged and buildable during the rewrite as the behavioral baseline and rollback implementation.
- The source removal of the Bun backend, if desired, is a separate change after the Go release stabilizes.

## 3. Scope

### 3.1 Compatibility surface

The Go backend must preserve the following externally observable contracts.

#### HTTP API

- Existing paths and methods under `/api/**`.
- Request bodies, query parameters, and validation behavior.
- HTTP status codes.
- JSON field names and response shapes.
- Error response formats and user-visible messages.
- Basic Auth and login/logout behavior.
- CORS behavior.

#### Proxy

- Instance-key-based URLs.
- HTTP reverse proxy behavior.
- WebSocket upgrade and bidirectional forwarding.
- Request and response streaming.
- Header, cookie, redirect, and path rewriting.
- Magic admin links and related cookies.

#### Storage

- Existing `${DATA_DIR}/gowa.db` files.
- Existing SQLite schema and stored values.
- Existing `data/instances` and `data/bin/versions` layouts.
- Existing GOWA version aliases and legacy paths.
- Rollback compatibility: Bun must still be able to open data used by Go.

#### Process management

- Instance start, stop, kill, and restart.
- Process-tree termination on Linux and Windows.
- Status and error persistence.
- Recovery and configured auto-restart after manager restart.
- Graceful manager shutdown.

#### Configuration and distribution

- Existing CLI flags and environment variables.
- Existing precedence rules between CLI, environment, and defaults.
- Existing port fallback behavior.
- Embedded React production build.
- A single executable for each supported target.

### 3.2 Non-goals

The rewrite does not include:

- endpoint or frontend contract redesign;
- destructive SQLite schema changes;
- data-directory restructuring;
- a new user or authentication system;
- a new instance configuration format;
- UI redesign;
- unrelated product features;
- intentional changes to legacy behavior unless required for security or data integrity and explicitly approved.

Legacy bugs discovered during the rewrite are recorded separately. Compatibility takes priority unless retaining a behavior would create a security or integrity defect.

## 4. Development and Cutover Model

The rewrite is developed on an isolated branch and worktree. The Bun backend remains intact throughout development.

The Go backend may run in tests or staging only with:

- a separate temporary or copied data directory;
- separate manager and instance ports;
- fixture or noncritical GOWA processes.

Bun and Go must never simultaneously manage the same production data directory or GOWA processes.

The cutover is atomic at the host level: stop Bun, perform backup and preflight checks, start Go, and validate. There is no request-level or module-level traffic split.

## 5. Go Architecture

Use a modular monolith: one executable and one manager process with strongly separated internal packages.

```text
cmd/gowa-manager/
    main.go

internal/
    app/                 # bootstrap and dependency wiring
    config/              # CLI, environment, defaults, validation
    database/            # SQLite connection and migrations
    instances/           # instance CRUD and orchestration
    supervisor/          # child-process lifecycle
    proxy/               # HTTP and WebSocket reverse proxy
    versions/            # GOWA version download and management
    monitoring/          # CPU, memory, disk, and history
    system/              # ports and system status
    auth/                # Basic Auth and login compatibility
    scheduler/           # cleanup and auto-update jobs
    httpapi/             # router, middleware, and response mapping
    static/              # embedded React build
    filesystem/          # instance directory operations
    platform/            # Linux and Windows adapters

migrations/              # embedded SQLite migrations
web/                     # embedded frontend artifact
test/
    contract/
    integration/
    fixtures/
```

Names may be adjusted during planning, but package boundaries and dependency direction must remain clear.

### 5.1 Dependency direction

```text
HTTP handlers / schedulers
           ↓
Application services
           ↓
Domain interfaces
           ↓
SQLite / OS / network / filesystem adapters
```

HTTP handlers must not directly access SQLite or operating-system process APIs. Only the supervisor package may start, signal, or terminate GOWA processes.

### 5.2 Bootstrap

`cmd/gowa-manager/main.go` remains thin. It will:

1. Parse CLI and environment configuration.
2. Initialize safe structured logging.
3. Resolve and validate the data directory.
4. Acquire an exclusive manager lock.
5. Open and validate SQLite.
6. Apply only compatible, idempotent migrations.
7. Build repositories and services.
8. Reconcile persisted and actual process state.
9. Start the HTTP server and schedulers.
10. Coordinate graceful shutdown.

## 6. HTTP and Static Application

Prefer the Go standard library:

- `net/http` for the server;
- `http.ServeMux` or a small router if route ergonomics require it;
- `httputil.ReverseProxy` as the HTTP proxy foundation;
- a small, maintained WebSocket library for bridging;
- `embed.FS` for the React production build.

A large web framework is unnecessary. Standard-library-first code provides direct control over streaming, flushing, WebSocket upgrades, cancellation, and shutdown, while reducing binary dependencies.

Handlers are responsible only for:

- decoding and validating requests;
- invoking application services;
- mapping typed domain errors to legacy status codes and JSON responses.

## 7. Database Design

SQLite access is hidden behind repository interfaces. A representative interface is:

```go
type InstanceRepository interface {
    List(ctx context.Context) ([]Instance, error)
    FindByID(ctx context.Context, id int64) (Instance, error)
    FindByKey(ctx context.Context, key string) (Instance, error)
    Create(ctx context.Context, input CreateInstance) (Instance, error)
    Update(ctx context.Context, input UpdateInstance) (Instance, error)
    UpdateStatus(ctx context.Context, id int64, status Status, message *string) error
    Delete(ctx context.Context, id int64) error
}
```

A pure-Go SQLite driver is preferred for reliable Linux/Windows cross-compilation, subject to an early compatibility spike. The selected driver must prove:

- compatibility with existing SQLite files;
- support for required SQL, including `RETURNING` where retained;
- correct locking and busy-timeout behavior;
- safe transaction semantics;
- acceptable resource usage and performance;
- continued ability for `bun:sqlite` to reopen the database after Go writes to it.

Database requirements:

- explicit transaction boundaries;
- configured `busy_timeout`;
- tested journal mode on existing databases;
- idempotent migration runner;
- backup before any migration;
- no destructive migration in the first Go release.

## 8. Exclusive Manager Ownership

A data directory must have exactly one active manager.

The Go backend acquires an operating-system-appropriate exclusive lock before opening the database for migration or touching child processes. If the lock cannot be acquired, startup fails with a clear error. The server and schedulers do not start.

This lock prevents accidental simultaneous Bun/Go operation during cutover or rollback. Deployment procedures must also stop and verify the previous manager before starting the next one because the Bun implementation does not necessarily participate in the new lock protocol.

## 9. Process Supervisor

The supervisor is the only package allowed to invoke OS process APIs. Its runtime registry includes:

- instance ID;
- PID and process handle;
- start time;
- requested stop reason;
- exit state and generation.

The registry is runtime state, not permanent truth. SQLite contains desired and last-known state; the operating system represents actual state. Startup reconciliation resolves discrepancies before normal operation.

### 9.1 Lifecycle rules

- Operations for one instance are serialized.
- Different instances may operate concurrently.
- Start must never create duplicate child processes.
- Restart is one coordinated operation.
- A failed start cleans up any partially started process and persists a safe error.
- Request cancellation must not orphan an unmanaged process.
- Exit callbacks carry a generation identifier so an old callback cannot overwrite a newer operation's state.

### 9.2 Platform adapters

- Linux uses process groups, graceful signaling, timeout, then forced tree termination.
- Windows uses tested process handles and Job Objects or another proven tree-termination mechanism.
- Stop and kill semantics must match existing observable behavior.
- Cross-compilation is insufficient validation; process lifecycle tests run natively on both Linux and Windows.

## 10. Data and Operation Flows

### 10.1 Start instance

```text
HTTP handler
  → authenticate and validate
  → InstanceService.Start
  → load instance
  → validate installed version, config, and port
  → acquire per-instance operation lock
  → Supervisor.Start
  → wait for readiness or early-exit window
  → persist running status or typed error
  → return legacy-compatible response
```

### 10.2 Database and filesystem consistency

SQLite and filesystem changes cannot share one atomic transaction. Workflows use reversible staging.

Create example:

1. Validate name, key, port, and config.
2. Create a temporary directory.
3. Begin the SQLite transaction.
4. Insert the instance.
5. Rename the temporary directory to its final path.
6. Commit.
7. On failure, roll back and remove temporary artifacts.

Delete/reset example:

1. Confirm the instance is stopped.
2. Rename its directory to a temporary trash path.
3. Apply the database change in a transaction.
4. Commit.
5. Remove trash asynchronously.
6. Restore the directory if the transaction fails.

## 11. Startup Reconciliation

Startup proceeds as follows:

1. Acquire exclusive ownership.
2. Open and validate SQLite.
3. Identify records previously marked running.
4. Inspect actual process and port state where reliably possible.
5. Apply the same configured restart behavior as Bun.
6. Restart eligible instances with bounded concurrency.
7. Persist each recovery result.
8. Mark the manager ready.

The existing `/api/health` response remains compatible. A separate internal readiness endpoint may be added for Docker or orchestration, provided it does not alter frontend behavior and is safely exposed.

## 12. HTTP Proxy

```text
request
  → resolve and validate instance key
  → determine localhost instance target
  → normalize and rewrite path
  → proxy to the instance port
  → rewrite response headers, cookies, and redirects
  → stream response to the client
```

Requirements:

- avoid unnecessary request or response buffering;
- remove hop-by-hop headers correctly;
- preserve upstream status codes;
- preserve legacy cookie and redirect behavior;
- separate connection timeouts from stream lifetime;
- propagate client cancellation upstream;
- never allow proxying to arbitrary user-supplied hosts.

## 13. WebSocket Proxy

WebSocket bridging performs:

1. Instance-key and state validation.
2. Upstream WebSocket connection.
3. Client connection upgrade.
4. Bidirectional copy loops.
5. Close-frame and close-code propagation.
6. Shared cancellation when either direction exits.
7. Idempotent connection closure.
8. Registry cleanup.

Tests must cover text and binary messages, large payloads, ping/pong, abnormal disconnects, upstream restart, cancellation, multiple clients, and goroutine/handle leakage.

## 14. Error Handling

Domain failures are typed, for example:

- `ErrInstanceNotFound`;
- `ErrInstanceAlreadyRunning`;
- `ErrPortUnavailable`;
- `ErrVersionUnavailable`;
- `ErrInvalidConfiguration`;
- `ErrProcessStartFailed`;
- `ErrUnauthorized`;
- `ErrConflict`.

The HTTP layer maps these errors to the existing status codes and JSON shapes. Internal errors include operational context but responses and logs must not disclose:

- admin credentials or authorization headers;
- magic-link tokens;
- complete instance configurations;
- webhook secrets;
- sensitive command-line arguments;
- stack traces or sensitive filesystem paths.

## 15. Logging and Observability

Structured logging should include only safe fields such as:

- operation;
- instance ID/key;
- PID;
- port;
- GOWA version;
- duration;
- status;
- error category.

Canary observability must include:

- HTTP request counts and latency;
- active child processes;
- process start failures and restarts;
- active HTTP and WebSocket proxy connections;
- SQLite busy and error counts;
- scheduler failures;
- goroutine and memory usage.

A metrics endpoint may be added as opt-in and localhost-only. It must not expand the default public attack surface.

## 16. Shutdown

On `SIGINT` or `SIGTERM`, the application:

1. Becomes unready.
2. Stops accepting new requests.
3. Cancels schedulers.
4. Drains ordinary requests within a timeout.
5. Closes WebSockets in a controlled manner.
6. Applies the legacy-compatible child-process shutdown policy.
7. Flushes logs.
8. Closes SQLite.
9. Releases the manager lock.

A second signal or shutdown timeout forces termination while making a best effort to avoid database corruption and unmanaged child processes.

## 17. Verification Strategy

The implementation is big-bang, but verification is layered and continuous.

### 17.1 Unit tests

Cover:

- CLI and environment parsing;
- configuration validation;
- key generation;
- error mapping;
- port selection;
- path, cookie, redirect, and header rewriting;
- version resolution;
- scheduler policy;
- supervisor state transitions;
- Linux and Windows command construction.

### 17.2 Repository tests

Use temporary databases to test:

- empty database initialization;
- opening old schemas and data fixtures;
- CRUD and unique constraints;
- timestamps and nullable fields;
- transaction rollback;
- concurrent access and busy timeout;
- idempotent migrations;
- reopening the database with Bun after Go has written to it.

### 17.3 Integration tests

Run the actual Go binary with:

- a temporary data directory;
- random ports;
- a fake GOWA executable;
- HTTP and WebSocket fixture servers;
- the real React production build where appropriate.

The fake GOWA binary must simulate successful startup, delayed readiness, immediate crash, shutdown hangs, process trees, HTTP responses, WebSocket echo, CPU/memory load, and selected exit codes.

### 17.4 Contract tests

Run equivalent requests against Bun and Go with identical fixtures. Compare:

- status codes;
- JSON responses;
- significant headers;
- cookies and redirects;
- database side effects;
- filesystem side effects;
- child-process state.

Normalize nondeterministic values such as timestamps, random ports, PIDs, and absolute paths.

Contract tests are a verification tool, not a progressive production migration mechanism.

### 17.5 End-to-end tests

The production frontend must pass these flows against Go:

1. Login and logout.
2. List and view instances.
3. Create, update, and delete an instance.
4. Start, stop, restart, and kill.
5. Reset instance data.
6. Test connection.
7. Generate and use a magic admin link.
8. Install and switch GOWA versions.
9. Access HTTP proxy routes.
10. Exercise WebSocket-backed functionality.

## 18. Platform Matrix

The release suite must pass on:

| Platform | Architecture | Deployment |
|---|---|---|
| Linux | amd64 | Native binary |
| Linux | arm64 | Native runner or real device |
| Linux | amd64 | Docker |
| Windows | amd64 | Native executable |

Windows process lifecycle and cleanup must be tested on a real Windows runner or machine.

## 19. Performance Acceptance

Capture a Bun baseline before implementation and compare identical fixtures for:

- idle resident memory;
- startup time;
- CRUD API latency;
- HTTP proxy throughput;
- concurrent WebSocket connections;
- monitoring CPU cost with many instances;
- shutdown duration;
- executable and container image size.

Acceptance requires:

- no correctness regression;
- idle memory no worse than the measured Bun baseline;
- no material proxy throughput regression;
- no goroutine, file-descriptor, process-handle, or connection leaks;
- startup and shutdown within documented operational timeouts.

Concrete numeric thresholds are derived from the recorded baseline rather than guessed in advance.

## 20. Database Safety Gate

Before production canary:

1. Copy representative production databases and data directories.
2. Run Go against each copy.
3. Exercise all major read/write workflows.
4. Stop Go normally and forcibly in separate runs.
5. Execute `PRAGMA integrity_check`.
6. Reopen the same data with Bun.
7. Verify instances and configurations remain readable and operable.
8. Repeat with data samples from multiple hosts.

## 21. Cutover Gates

Go cannot replace Bun until:

- every endpoint and critical side effect meets parity;
- HTTP and WebSocket integration suites pass;
- old databases open without destructive changes;
- Linux and Windows process-lifecycle suites pass;
- frontend end-to-end tests pass;
- backup and rollback rehearsal succeeds;
- packaging succeeds for all supported targets;
- the deployment procedure proves only one manager can operate per host/data directory;
- performance and leak acceptance criteria pass.

## 22. Canary Procedure

For each host:

1. Record the active Bun binary, version, arguments, environment, and configuration.
2. Stop Bun and verify it has exited.
3. Establish the state of GOWA child processes and ports.
4. Back up `gowa.db`, relevant instance data/configuration, version metadata, and deployment configuration.
5. Verify backup checksums and restorability.
6. Install the Go binary at a staged path.
7. Run preflight checks for platform, permissions, manager ownership, database integrity, ports, and installed versions.
8. Start Go.
9. Run automated smoke tests.
10. Observe the host through the defined canary window.
11. Promote the next host only after exit criteria pass.

Canary means some hosts use Go while other hosts still use Bun. It never means Bun and Go run concurrently on one host's data directory.

### 22.1 Canary smoke tests

- health and readiness;
- authentication;
- instance list and detail;
- process status;
- start and stop one noncritical instance;
- HTTP proxy;
- WebSocket connection;
- create and delete a test instance;
- manager restart and recovery;
- scheduler health;
- SQLite integrity;
- stable memory, file descriptors, handles, and goroutines.

## 23. Rollback

Rollback triggers include:

- SQLite integrity failure;
- failed instance recovery;
- duplicate or orphan child processes;
- persistent HTTP or WebSocket proxy failure;
- manager crash loop;
- resource or handle leak;
- authentication regression;
- error rate over the agreed threshold;
- ambiguous lifecycle state.

Rollback procedure:

1. Stop or drain incoming traffic.
2. Stop Go.
3. Ensure no lifecycle operation is in progress.
4. Establish and record child-process state.
5. Preserve Go logs and a database snapshot for diagnosis.
6. If integrity and compatibility checks pass, restart Bun on the current database.
7. If integrity or schema compatibility is uncertain, restore the verified pre-cutover backup.
8. Start the pinned Bun release with its recorded configuration.
9. Run Bun smoke tests.
10. Reopen traffic.
11. Freeze rollout to other hosts until the failure is understood.

The full rollback procedure must be rehearsed in staging before the first production canary.

## 24. Retirement of the Bun Backend

The Bun source is not removed after the first successful canary. Retirement is considered only after:

- every production host runs Go;
- the stabilization window completes;
- no unresolved rollback condition remains;
- Go backup and recovery procedures are proven;
- deployment and operational documentation is current;
- the Go release is explicitly marked stable.

Source removal is a separate reviewed change. Git tags and the final Bun release artifacts remain available for historical recovery.
