# GOWA Manager — Backend Test Coverage Backlog

> Convention: `[ ]` not started · `[~]` in active sprint · `[x]` shipped · `[>]` deferred · `[-]` dropped

> Active sprint: 2026-W29 — see TASKS.md for technical breakdown.

> Last updated: 2026-07-20

---

## Baseline (2026-07-18)

- Aggregate: 84.48% funcs / 88.26% lines
- 171 tests pass, 0 fail, 387 expect() calls across 21 files
- Domain logic & routes teruji baik; side-effect heavy code jadi titik buta

## Sprint 2026-W29 result (2026-07-18 → 2026-07-20)

- Aggregate: 96.11% funcs / 98.97% lines (+11.63% funcs / +10.71% lines vs baseline)
- 384 tests pass, 0 fail, 863 expect() calls across 29 files (+213 tests)
- `process-manager.ts`: 21.43% -> 100.00% lines
- `service.websocket.ts`: 21.82% -> 100.00% lines
- `resource-monitor.ts`: 11.64% -> 86.73% lines
- `auto-updater.ts`: 13.19% -> 98.18% lines
- `versions.ts`: 52.98% -> 100.00% lines
- `cli.ts`: 19.73% -> 100.00% lines
- `proxy/service.ts`: 66.86% -> 85.06% lines
- `instances/index.ts`: 76.49% -> 100.00% lines
- `proxy/index.ts`: 49.04% -> 97.87% lines
- `version-manager.ts`: 75.69% -> 98.26% lines
- `system/index.ts`: 83.54% -> 100.00% lines
- `instances/service.ts`: 91.39% -> 95.17% lines

---

# 0. High-Priority Coverage (P0)

## 0.1 Process Manager — P0
- [x] Test `ProcessManager` lifecycle (add/remove/get/isReallyRunning) — shipped:2026-07-18
- [x] Test `stopProcess` & `killProcess` (success + missing instance) — shipped:2026-07-18
- [x] Test `cleanupAllInstances` (idempotency via `isShuttingDown` guard) — shipped:2026-07-18
- [x] Test `setupExitHandlers` registration (mock process events) — shipped:2026-07-19

## 0.2 WebSocket Proxy Service — P0
- [x] Test `createWebSocketConnection` returns null for missing/stopped/no-port instance — shipped:2026-07-18
- [x] Test `createWebSocketConnection` builds correct target URL & forwards headers — shipped:2026-07-18
- [x] Test connection registry set/get/close/closeAll behavior — shipped:2026-07-18
- [x] Test error/close handlers remove connection from registry — shipped:2026-07-18

# 1. Medium-Priority Coverage (P1)

## 1.1 Resource Monitor — P1
- [x] Test `getResourceUsage` happy path with mocked pidusage — shipped:2026-07-18
- [x] Test `getResourceUsage` returns null on ESRCH (process gone) — shipped:2026-07-18
- [x] Test history tracking & rolling average (max 10 entries) — shipped:2026-07-18
- [x] Test disk size cache TTL behavior — shipped:2026-07-18
- [x] Test `clearHistory` & `clearAllHistory` — shipped:2026-07-18
- [x] Test `getMultipleResourceUsage` with mixed success/failure — shipped:2026-07-18
- [x] Test `calculateDirectorySize` recursive sum — shipped:2026-07-18

## 1.2 Auto-Updater — P1
- [x] Test update check returns null when no update available — shipped:2026-07-18
- [x] Test update check returns version info when newer version exists — shipped:2026-07-18
- [x] Test performUpdate flow with mocked download/extract — shipped:2026-07-18
- [x] Test auto-update disabled when config flag off — shipped:2026-07-18

## 1.3 System Versions API — P1
- [x] Test `versions.ts` route handlers (list, install, remove, available) — shipped:2026-07-18
- [x] Test install error paths (network failure, bad archive) — shipped:2026-07-18
- [x] Test version list sorting & latest marker — shipped:2026-07-18

# 2. Lower-Priority Coverage (P2)

## 2.1 CLI — P2
- [x] Test `getConfig` parses data dir, port, admin creds from env + argv — shipped:2026-07-18
- [x] Test default values when env/argv absent — shipped:2026-07-18
- [x] Test CLI help output format — shipped:2026-07-18
- [x] Test invalid flag handling — shipped:2026-07-18

## 2.2 Proxy Service Edge Cases — P2
- [x] Test `forwardRequest` timeout handling — shipped:2026-07-18
- [x] Test `forwardRequest` with various binary content types — shipped:2026-07-18
- [x] Test `isInstanceAvailable` for all failure modes — shipped:2026-07-18
- [x] Test `getProxyStatus` shape for running/stopped instances — shipped:2026-07-18

## 2.3 Instances Module Index — P2
- [x] Test admin link generation edge cases (covered: 56-60, 75-76, 91-92) — shipped:2026-07-18
- [x] Test lifecycle route error paths (covered: 205-210, 232, 253-299) — shipped:2026-07-18

# 3. Maintenance (P2)

## 3.1 Coverage Gates — P2
- [x] Add coverage threshold to CI (target: 90% lines) — shipped:2026-07-19
- [x] Document test running & coverage commands in docs/test-coverage/README.md — shipped:2026-07-19
