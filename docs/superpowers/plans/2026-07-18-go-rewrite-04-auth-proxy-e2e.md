# Go Rewrite 04: Authentication, Proxy, and Frontend E2E Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete all externally visible backend behavior by implementing compatible manager authentication, magic admin links, streaming HTTP reverse proxy, WebSocket bridging, proxy status/health, safe observability, and production-frontend end-to-end tests.

**Architecture:** Management APIs are wrapped by constant-time Basic Auth middleware while public health/auth/proxy routing preserves current route order. HTTP proxying builds on `httputil.ReverseProxy`; WebSocket proxying uses one upstream connection per client and explicit bidirectional cancellation. Authentication and URL rewriting remain isolated pure utilities with exhaustive parity tests.

**Tech Stack:** Go 1.24, `net/http`, `httputil.ReverseProxy`, `github.com/coder/websocket` (or another reviewed minimal library chosen at implementation), `crypto/hmac`, Playwright/browser E2E, Go test fixtures.

---

## Dependencies and outputs

- Requires Plans 01–03.
- Uses current proxy behavior in `src/modules/proxy/**`, auth behavior in `src/modules/auth/**` and `src/middlewares/auth.ts`, and the production React app as contract references.
- Completes feature parity but does not switch production release defaults; packaging and cutover are Plan 05.

## File structure

- Create `internal/auth/basic.go`, `magic.go`, tests.
- Create `internal/httpapi/auth.go`, auth middleware/tests.
- Create `internal/proxy/target.go`, `rewrite.go`, `http.go`, `websocket.go`, `registry.go` and tests.
- Create `internal/httpapi/proxy.go`, tests.
- Create `internal/observability/metrics.go`, tests.
- Create `internal/testutil/upstream/main.go`: HTTP/WebSocket fixture.
- Create `test/contract/proxy_test.go`, `security_test.go`.
- Create `test/e2e/go-backend.spec.ts`, `playwright.config.ts`.
- Modify `internal/httpapi/server.go`, `internal/app/app.go`, `package.json`, and CI test workflow.

### Task 1: Implement manager Basic Auth and login/logout compatibility

**Files:**
- Create: `internal/auth/basic.go`
- Create: `internal/auth/basic_test.go`
- Create: `internal/httpapi/auth.go`
- Create: `internal/httpapi/auth_test.go`
- Modify: `internal/httpapi/server.go`

- [ ] **Step 1: Port authentication cases**

Test missing/malformed scheme, invalid base64, usernames/passwords containing colon according to current behavior, valid credentials, wrong username/password, constant-time comparison path, challenge header, `/api/auth/login`, `/api/auth/logout`, and unprotected `/api/health`.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/auth ./internal/httpapi -run 'TestBasic|TestAuthRoutes' -v`

Expected: FAIL.

- [ ] **Step 3: Implement credential parsing and middleware**

Use `subtle.ConstantTimeCompare` on hashes of fixed length. Never log credentials or `Authorization`. Protect instance and system APIs exactly where Bun currently applies the guard. Preserve auth and proxy route order.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/auth ./internal/httpapi -run 'TestBasic|TestAuthRoutes' -v`

```bash
git add internal/auth/basic* internal/httpapi/auth* internal/httpapi/server.go
git commit -m "feat: add compatible Go manager authentication"
```

### Task 2: Implement magic admin token and cookie utilities

**Files:**
- Create: `internal/auth/magic.go`
- Create: `internal/auth/magic_test.go`

- [ ] **Step 1: Port token/cookie tests**

Cover signing, instance binding, expiry boundary, tampering, malformed token, cookie name, path `/app/{instanceKey}/`, HttpOnly, SameSite, Secure based on request scheme, max age, clear cookie, cookie parsing, and key rotation behavior derived from current implementation.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/auth -run TestMagic -v`

Expected: FAIL.

- [ ] **Step 3: Implement HMAC token utilities**

Use HMAC-SHA256 and constant-time verification. Token payload contains version, instance key, and expiry. Preserve the current secret derivation/configuration so existing links behave as required during cutover; if old tokens cannot intentionally survive manager restart, encode that fact in tests and docs.

- [ ] **Step 4: Verify fuzz tests and commit**

Fuzz malformed tokens and cookie headers; verification must never panic.

Run: `go test -race ./internal/auth -run 'TestMagic|FuzzMagic' -v`

```bash
git add internal/auth/magic*
git commit -m "feat: add Go magic admin authentication"
```

### Task 3: Build deterministic HTTP and WebSocket upstream fixture

**Files:**
- Create: `internal/testutil/upstream/main.go`
- Create: `internal/testutil/upstream/upstream_test.go`

- [ ] **Step 1: Define fixture endpoints**

Provide echo for method/path/query/headers/body, streaming chunks with flushes, redirects, cookies, JSON URLs, binary PNG/PDF, large body, delayed response, connection close, WebSocket text/binary echo, ping/pong, close codes, and forced upstream disconnect.

- [ ] **Step 2: Implement and test the fixture**

The command prints its selected port in machine-readable JSON and shuts down on signals. It is test-only and must not appear in production binaries.

- [ ] **Step 3: Run tests and commit**

Run: `go test -race ./internal/testutil/upstream -v`

```bash
git add internal/testutil/upstream
git commit -m "test: add proxy upstream fixture"
```

### Task 4: Implement safe proxy target resolution and pure rewriting

**Files:**
- Create: `internal/proxy/target.go`
- Create: `internal/proxy/target_test.go`
- Create: `internal/proxy/rewrite.go`
- Create: `internal/proxy/rewrite_test.go`

- [ ] **Step 1: Port proxy utility cases**

Cover instance unavailable/not found, localhost target only, root and wildcard paths, query preservation, `/app/{key}` prefix handling, `X-Forwarded-For/Host/Proto`, Host removal/replacement, hop-by-hop headers, redirect `Location`, cookie path/domain rewriting, binary content detection, and current JSON URL rewrite behavior where active.

- [ ] **Step 2: Add SSRF rejection tests**

Prove no request input can select scheme, hostname, or port. Reject invalid DB ports and keys before constructing target URLs.

- [ ] **Step 3: Implement pure functions**

Keep target resolution dependent only on a repository lookup. Rewriting functions receive explicit request URL/instance key and return copied headers; do not mutate shared header maps.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/proxy -run 'TestTarget|TestRewrite|TestSSRF' -v`

```bash
git add internal/proxy/target* internal/proxy/rewrite*
git commit -m "feat: add safe Go proxy rewriting"
```

### Task 5: Implement streaming HTTP reverse proxy

**Files:**
- Create: `internal/proxy/http.go`
- Create: `internal/proxy/http_test.go`

- [ ] **Step 1: Write integration tests against upstream fixture**

Cover all HTTP methods, query/body forwarding, selected headers, explicit authorization preservation, instance Basic Auth injection only with valid magic cookie, binary responses, streaming first-byte timing, large bodies without full buffering, redirects/cookies, upstream status, timeout distinctions, cancellation, and unavailable upstream errors.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/proxy -run TestHTTPProxy -v`

Expected: FAIL.

- [ ] **Step 3: Implement `httputil.ReverseProxy` wrapper**

Customize `Rewrite`/Director according to the Go version, `ModifyResponse`, transport timeouts, flush interval, and `ErrorHandler`. Do not set a global response timeout that breaks long streams. Explicit client authorization always wins over magic-cookie injection.

- [ ] **Step 4: Run race/leak tests**

Run: `go test -race ./internal/proxy -run TestHTTPProxy -count=20 -v`

Expected: PASS with stable goroutines and closed bodies.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/http*
git commit -m "feat: stream HTTP traffic through Go proxy"
```

### Task 6: Implement WebSocket registry and bridge

**Files:**
- Create: `internal/proxy/registry.go`
- Create: `internal/proxy/registry_test.go`
- Create: `internal/proxy/websocket.go`
- Create: `internal/proxy/websocket_test.go`

- [ ] **Step 1: Write registry ownership tests**

Use a unique connection ID per client, not only instance key. Test add/get/delete/close, multiple clients for one instance, close-all on shutdown, and concurrent operations.

- [ ] **Step 2: Write WebSocket bridge tests**

Cover text and binary messages, large payloads, query and safe headers, subprotocol, ping/pong, normal close code/reason, abnormal client disconnect, upstream restart/disconnect, cancellation, multiple concurrent clients, and no cross-client message mixing.

- [ ] **Step 3: Implement with a reviewed WebSocket library**

At execution time, verify the current maintained version and API before pinning. Run one copy loop in each direction under shared cancellation; close both sides idempotently. Bound message size consistently with the Bun behavior or document/test the agreed safe compatibility limit.

- [ ] **Step 4: Stress and leak test**

Run: `go test -race ./internal/proxy -run 'TestWebSocket|TestRegistry' -count=50`

Expected: PASS and registry returns to zero.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/proxy/registry* internal/proxy/websocket*
git commit -m "feat: bridge WebSockets through Go proxy"
```

### Task 7: Add proxy routes, status, health, and autologin

**Files:**
- Create: `internal/httpapi/proxy.go`
- Create: `internal/httpapi/proxy_test.go`
- Modify: `internal/httpapi/server.go`

- [ ] **Step 1: Port the complete proxy route suite**

Cover `/app/:key`, `/app/:key/*`, `/app/:key/status`, `/app/:key/health`, `/app/:key/ws`, unavailable responses, content types, redirects, magic `autologin` query handling, query removal, cookie set/clear, and route precedence.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/httpapi -run TestProxyRoutes -v`

Expected: FAIL.

- [ ] **Step 3: Implement handlers**

Autologin validation occurs before forwarding and redirects to the same URL without the token. Proxy routes remain outside manager Basic Auth, matching current behavior; their target availability and instance-level auth rules still apply.

- [ ] **Step 4: Verify and commit**

Run: `go test -race ./internal/httpapi ./internal/proxy -run 'TestProxyRoutes|TestHTTPProxy|TestWebSocket' -v`

```bash
git add internal/httpapi/proxy* internal/httpapi/server.go
git commit -m "feat: expose Go HTTP and WebSocket proxy routes"
```

### Task 8: Complete admin-link route compatibility

**Files:**
- Modify: `internal/httpapi/instances.go`
- Modify: `internal/httpapi/instances_test.go`

- [ ] **Step 1: Add exact admin-link tests**

For an instance without Basic Auth return `/app/{key}/` without token. With configured Basic Auth return `/app/{key}/?autologin=<token>` and compatible `expiresAt`. Test missing instance and ensure manager auth protects the route.

- [ ] **Step 2: Connect handler to magic-token service**

Do not parse or expose credentials in the response. Keep token lifetime centralized in auth configuration.

- [ ] **Step 3: Verify and commit**

Run: `go test -race ./internal/httpapi -run TestAdminLink -v`

```bash
git add internal/httpapi/instances*
git commit -m "feat: complete Go admin-link flow"
```

### Task 9: Add safe metrics and readiness instrumentation

**Files:**
- Create: `internal/observability/metrics.go`
- Create: `internal/observability/metrics_test.go`
- Modify: `internal/httpapi/server.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Write metric behavior/security tests**

Track request count/latency, active processes, start failures/restarts, active HTTP proxy requests/WebSockets, SQLite busy/errors, scheduler failures, goroutines, and memory. Verify no route labels contain raw IDs/keys and no credentials/tokens/config appear.

- [ ] **Step 2: Implement opt-in localhost-only endpoint**

Add config disabled by default. When enabled, reject non-loopback remote addresses and expose a stable text format. Instrument with bounded-cardinality labels.

- [ ] **Step 3: Verify and commit**

Run: `go test -race ./internal/observability ./internal/httpapi ./internal/app -run 'TestMetrics|TestReady' -v`

```bash
git add internal/observability internal/httpapi/server.go internal/app/app.go
git commit -m "feat: add safe Go canary observability"
```

### Task 10: Add Bun-versus-Go proxy and security contract suite

**Files:**
- Create: `test/contract/proxy_test.go`
- Create: `test/contract/security_test.go`

- [ ] **Step 1: Compare HTTP proxy behavior**

Run both managers separately against equivalent upstream fixtures and normalized instance DB rows. Compare methods, paths, query, headers, body, status, binary payload, stream chunks, redirects, cookies, auth injection, status, and health.

- [ ] **Step 2: Compare WebSocket behavior**

Compare text/binary echo, query forwarding, close code/reason, upstream disconnect, and concurrent clients. Timing is compared by bounded acceptance windows rather than exact values.

- [ ] **Step 3: Add security regression cases**

Test SSRF payloads, CRLF headers, path traversal, malformed Basic Auth, token tampering/replay across keys, oversized payload behavior, logs without secrets, and management endpoints requiring auth.

- [ ] **Step 4: Run suite and commit**

Run: `go test -race ./test/contract -run 'TestProxyParity|TestSecurityContracts' -count=1 -v`

```bash
git add test/contract/proxy_test.go test/contract/security_test.go
git commit -m "test: verify Go proxy and security parity"
```

### Task 11: Add production frontend end-to-end suite

**Files:**
- Create: `playwright.config.ts`
- Create: `test/e2e/go-backend.spec.ts`
- Create: `test/e2e/helpers.ts`
- Modify: `package.json`

- [ ] **Step 1: Add deterministic E2E environment**

Build frontend, Go manager, fake GOWA, and upstream fixtures. Seed a temporary DB and installed fake version. Start Go with known credentials and random ports. Ensure cleanup terminates all processes.

- [ ] **Step 2: Automate core user flows**

Test login/logout, dashboard/list/detail, create/update/delete, start/status/stop/restart/kill, reset data, devices, test connection, admin link, install/switch version using fixture GitHub endpoint, HTTP proxy navigation, and a WebSocket-backed fixture action.

- [ ] **Step 3: Assert browser console/network health**

Fail on uncaught page errors, unexpected console errors, API 5xx, failed static assets, and authentication loops. Capture trace/screenshots only on failure.

- [ ] **Step 4: Run E2E**

Run: `bun run test:e2e:go`

Expected: PASS against the production frontend build and Go backend.

- [ ] **Step 5: Commit**

```bash
git add playwright.config.ts test/e2e package.json bun.lock
git commit -m "test: add frontend E2E for Go backend"
```

### Task 12: Integrate application wiring and shutdown of proxy connections

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Add wiring/shutdown tests**

Prove repository target resolver, HTTP proxy, WebSocket bridge, auth, and metrics receive the same service graph. Shutdown marks unready, drains HTTP, closes WebSockets, then cleans child processes before DB/lock release.

- [ ] **Step 2: Complete production wiring**

Remove every temporary adapter from Plans 01–03. Application startup must fail if a required dependency cannot initialize; optional metrics may remain disabled.

- [ ] **Step 3: Verify and commit**

Run: `go test -race ./internal/app ./internal/httpapi ./internal/proxy ./internal/auth ./internal/observability -v`

```bash
git add internal/app
git commit -m "feat: complete Go backend service wiring"
```

### Task 13: Verify full feature parity

- [ ] Run:

```bash
go fmt ./...
go vet ./...
go test -race ./...
go test -race ./test/contract -count=1
bun run build:tsc
bun test
bun run build:production
bun run build:go
bun run test:e2e:go
git diff --check
```

Expected: all PASS.

- [ ] Inspect coverage by package and add tests for any newly introduced untested error branch that can affect auth, data integrity, process ownership, or proxy connection cleanup.

## Plan completion gate

Before Plan 05:

- Every current HTTP/API/proxy/WebSocket feature is implemented in Go.
- No temporary production adapter remains.
- Bun/Go management, runtime, proxy, and security parity suites pass.
- Production React E2E passes against Go.
- Auth, logs, metrics, and proxy targets pass security tests.
- Go still has not replaced Bun as the production/release default.
