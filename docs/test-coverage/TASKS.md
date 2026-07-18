# Active Tasks — Backend Test Coverage

## Sprint: 2026-W29
Updated: 2026-07-19

## 0.1 ProcessManager tests — DONE

Target file: `src/modules/instances/utils/process-manager.test.ts` (new)
Source under test: `src/modules/instances/utils/process-manager.ts` (21.43% -> 100.00% lines)

### Tasks
- [x] Create test file with imports & shared setup
- [x] Test `getRunningProcesses` returns the internal map
- [x] Test `addProcess` / `getProcessInfo` / `removeProcess` round-trip
- [x] Test `isReallyRunning` returns true only after `addProcess`
- [x] Test `stopProcess` returns false for unknown instance
- [x] Test `stopProcess` calls treeKill with SIGTERM and removes entry
- [x] Test `killProcess` returns false for unknown instance
- [x] Test `killProcess` calls treeKill with SIGKILL and removes entry
- [x] Test `killProcess` swallows ESRCH error code silently
- [x] Test `cleanupAllInstances` no-op when already shutting down
- [x] Test `cleanupAllInstances` kills all tracked processes and clears map
- [x] Test `setupExitHandlers` registers handlers for all 5 process events
- [x] Test SIGTERM/SIGINT handlers call cleanupAllInstances + exit(0)
- [x] Test beforeExit handler calls cleanupAllInstances without exit
- [x] Test uncaughtException handler logs error, cleans up, exits(1)
- [x] Test unhandledRejection handler logs reason+promise, cleans up, exits(1)
- [x] Verify `bun test --coverage` shows process-manager.ts = 100% lines

## 0.2 WebSocketProxyService tests — DONE

Target file: `src/modules/proxy/service.websocket.test.ts` (new)
Source under test: `src/modules/proxy/service.websocket.ts` (21.82% -> 100.00% lines)

### Tasks
- [x] Create test file with imports & DB spy setup
- [x] Test `createWebSocketConnection` returns null when instance missing
- [x] Test returns null when instance status !== 'running'
- [x] Test returns null when instance has no port
- [x] Test builds `ws://localhost:{port}{path}` URL for running instance
- [x] Test forwards headers via `applyInstanceWebSocketAuthHeader`
- [x] Test `getWebSocketConnection` returns registered connection
- [x] Test `closeWebSocketConnection` removes connection
- [x] Test `getConnectionCount` reflects registry size
- [x] Test `closeAllWebSocketConnections` clears registry
- [x] Test error/close handlers delete connection from registry
- [x] Verify coverage of service.websocket.ts > 75% lines (100.00%)

## 1.1 ResourceMonitor tests — DONE

Target file: `src/modules/instances/utils/resource-monitor.test.ts` (new)
Source under test: `src/modules/instances/utils/resource-monitor.ts` (11.64% -> 86.73% lines)

### Tasks
- [x] Create test file with mocked pidusage & real DATA_DIR filesystem
- [x] Test `getResourceUsage` happy path returns cpu/memory stats
- [x] Test `getResourceUsage` returns null on ESRCH without warning
- [x] Test `getResourceUsage` returns null and warns on non-ESRCH errors
- [x] Test history tracking & rolling average across multiple calls
- [x] Test history trims to last 10 measurements
- [x] Test disk size cache computes from instance directory on first call
- [x] Test disk size cache serves cached value within TTL, recalculates after clear
- [x] Test `clearHistory` removes single instance history only
- [x] Test `clearAllHistory` removes all instance history
- [x] Test `getMultipleResourceUsage` tolerates mixed success/failure
- [x] Test `calculateDirectorySize` recursively sums nested files
- [x] Test `calculateDirectorySize` returns 0 for missing directory
- [x] Verify coverage of resource-monitor.ts > 80% lines (86.73%)

### Notes
- `testPidUsage` (lines 152-166) remains uncovered; low-value wrapper around
  pidusage self-test, deferred until explicitly needed.
- Used `mock.module('pidusage', ...)` safely — only `resource-monitor.ts`
  imports pidusage, so no cross-test leakage.
- Used `beforeEach` with `mockClear()` on console spies to prevent call count
  accumulation across tests from breaking `not.toHaveBeenCalled()` assertions.

## 1.2 AutoUpdater tests — DONE

Target file: `src/modules/system/auto-updater.test.ts` (new, by subagent)
Source under test: `src/modules/system/auto-updater.ts` (13.19% -> 98.18% lines)

### Tasks
- [x] Test getStatus returns default status copy
- [x] Test start schedules delayed first check + periodic interval
- [x] Test start clears previous interval before rescheduling
- [x] Test stop clears interval and resets nextCheck
- [x] Test stop is a no-op when no interval active
- [x] Test checkAndUpdate skips when already checking
- [x] Test checkAndUpdate no versions available / cannot determine latest
- [x] Test checkAndUpdate latest version already installed
- [x] Test checkAndUpdate successful update with no running instances
- [x] Test checkAndUpdate restarts running latest instances
- [x] Test checkAndUpdate partial restart failures
- [x] Test checkAndUpdate installVersion throws (error path + finally resets isChecking)
- [x] Test getLatestInstances filters by latest/missing gowa_version
- [x] Verify coverage of auto-updater.ts > 75% lines (98.18%)

### Notes
- Line 35 (setTimeout callback in start) uncovered — mocked timer prevents
  callback execution; covered indirectly by direct checkAndUpdate tests.
- Fixed subagent issue: describe-scope `spyOn(globalThis, 'setTimeout'/'setInterval'/'clearInterval')`
  and `spyOn(queries.getAllInstances, 'all')` were not restored, leaking into
  other test files and causing 5 pre-existing failures. Moved all spies to
  `beforeEach` with `mockRestore()` in `afterEach`.

## 1.3 System Versions API tests — DONE

Target file: `src/modules/system/versions.test.ts` (new, by subagent)
Source under test: `src/modules/system/versions.ts` (52.98% -> 100.00% lines)

### Tasks
- [x] Test GET /installed (sorted desc + latest marker, empty list, error path)
- [x] Test GET /available (success with metadata, custom limit, GitHub failure, throw)
- [x] Test POST /install (success, Error throw, non-Error throw, body validation 422)
- [x] Test DELETE /:version (success, latest alias rejection, Error throw, non-Error throw)
- [x] Test GET /:version/available (installed, missing, Error throw, non-Error throw)
- [x] Test GET /usage (disk usage with sizes, empty, Error throw, non-Error throw)
- [x] Test POST /cleanup (default keepCount, custom keepCount, Error throw, non-Error throw)
- [x] Verify coverage of versions.ts > 80% lines (100.00%)

### Notes
- 27 tests covering all 7 route handlers with success + error paths.
- Pre-existing issue documented: several error paths return `{ error, success: false }`
  with HTTP 200 instead of setting `set.status = 500`, causing Elysia response
  validation to produce 422. Catch block lines still execute (coverage achieved).
  This is a code issue in versions.ts, not a test issue.

## 2.1 CLI tests — DONE

Target file: `src/cli.test.ts` (new, by subagent)
Source under test: `src/cli.ts` (19.73% -> 100.00% lines)

### Tasks
- [x] Test parseCliArgs defaults & env-var fallbacks (PORT, ADMIN_USERNAME, ADMIN_PASSWORD, DATA_DIR)
- [x] Test CLI args take precedence over env vars; short-form flags
- [x] Test port validation (non-numeric, below 1, above 65535, boundaries)
- [x] Test username/password validation (empty, max length, boundary)
- [x] Test missing-value errors for every flag
- [x] Test unknown options & unexpected positional args
- [x] Test --help/-h output format & exit code 0
- [x] Test --version/-v output & exit code 0
- [x] Test getConfig argv slicing & binary-path stripping
- [x] Verify coverage of cli.ts > 70% lines (100.00%)

### Notes
- Used `mock.module('process', ...)` with a Proxy to override only the `exit`
  export (Bun's named `exit` binding is not a live binding, so
  `spyOn(process, 'exit')` cannot intercept it). Contained to the test file,
  does not leak.

## 2.2 Proxy Service edge case tests — DONE

Target file: `src/modules/proxy/service.test.ts` (extended, by subagent)
Source under test: `src/modules/proxy/service.ts` (66.86% -> 85.06% lines)

### Tasks
- [x] Test forwardRequest error paths (missing instance, stopped, no port, fetch errors)
- [x] Test forwardRequest body handling (ArrayBuffer, JSON parse failure, URL stripping)
- [x] Test forwardRequest binary content types (image/png, text/html)
- [x] Test isInstanceAvailable for all failure modes
- [x] Test getProxyStatus for missing/running/stopped instances
- [x] Test getAvailableProxyTargets filtering
- [x] Verify coverage of service.ts > 85% lines (85.06%)

### Notes
- 22 new tests appended to existing 11 (total 33).
- `modifyJsonUrls` (lines 136-161) skipped — private unused method, no caller.

## 2.3 Instances Module Index tests — DONE

Target file: `src/modules/instances/index.test.ts` (new, by subagent)
Source under test: `src/modules/instances/index.ts` (76.49% -> 100.00% lines)

### Tasks
- [x] Test create catch block (Error + non-Error throws)
- [x] Test update/delete 404 paths
- [x] Test status 404 path
- [x] Test admin-link invalid-JSON catch + empty-credentials + magic-link + 404
- [x] Test test-connection: 404, not-running, no-port, success, failed-status,
      body truncation, empty body, invalid-config, fetch throws
- [x] Verify coverage of index.ts > 90% lines (100.00%)

### Notes
- 20 tests in a separate file from routes.test.ts.
- All spies created in beforeEach with mockRestore in afterEach.

## 3.1 Coverage Gates — DONE

### Tasks
- [x] Create `scripts/check-coverage.ts` aggregate coverage gate script
- [x] Create `.github/workflows/test.yml` CI workflow (typecheck + coverage gate)
- [x] Document test running & coverage commands in `docs/test-coverage/README.md`
- [x] Note in `bunfig.toml` why `coverageThreshold` is not used (Bun per-file bug)

### Notes
- Bun's `coverageThreshold` is per-file, not aggregate
  ([oven-sh/bun#17028](https://github.com/oven-sh/bun/issues/17028)). Several
  modules (proxy/index.ts at 49% lines, version-manager.ts at 75%) are
  intentionally below 90% due to network/spawn dependencies. A per-file gate
  would always fail, so the CI workflow uses a custom script that checks the
  "All files" aggregate from the text coverage report.
- Default threshold: 90% lines / 90% functions. Current: 97.48% lines / 94.42% funcs.

## Sprint complete

All backlog items shipped. Remaining low-coverage files (proxy/index.ts,
version-manager.ts, system/index.ts) are deferred — they require network
calls, process spawning, or cron scheduling that is impractical to unit test
without heavy integration infrastructure.
