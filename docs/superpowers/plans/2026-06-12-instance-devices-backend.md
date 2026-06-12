# Instance Devices Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add cached backend device awareness through `GET /api/instances/:id/devices` and compact `devices` summaries in `GET /api/instances/:id/status`.

**Architecture:** Add `DeviceClient` under the instances module to own auth extraction, internal GOWA fetches, response normalization, timeout, and cache lifecycle. Extend instance service/status/routes/models/types to consume it while keeping existing status fields compatible.

**Tech Stack:** Bun, TypeScript, Elysia schemas, bun:test, existing SQLite-backed `InstanceService`.

---

## File Map

- Create `src/modules/instances/utils/device-client.ts`: device response types, cache, auth header creation, fetch timeout, response normalization, summary helper, cache clear.
- Create `src/modules/instances/utils/device-client.test.ts`: unit tests for normalization, cache, auth, stale fallback, and not-running behavior.
- Modify `src/types/index.ts`: add `Instance.DeviceSummary`, `Instance.DevicesResponse`, and `StatusResponse.devices`.
- Modify `src/modules/instances/model.ts`: add device schemas and status summary schema.
- Modify `src/modules/instances/service.ts`: include device summary in status and clear cache on stop/kill/delete.
- Modify `src/modules/instances/index.ts`: add `GET /:id/devices` route.
- Modify `src/modules/instances/routes.test.ts` and `src/modules/instances/service.test.ts`: coverage for endpoint and status summary.
- Modify `client/src/types/index.ts` and `client/src/lib/api.ts`: frontend types and API client method only.

---

### Task 1: Add DeviceClient Utility

**Files:**
- Create: `src/modules/instances/utils/device-client.ts`
- Create: `src/modules/instances/utils/device-client.test.ts`

- [ ] **Step 1: Write failing tests**

Create `src/modules/instances/utils/device-client.test.ts` with tests for: not-running skips fetch; array response normalizes and sends basic auth; `{ devices: [...] }` and `{ data: [...] }` normalize; fresh cache is used within TTL; stale cache is returned on refresh failure; unexpected response shape returns safe error; summary omits full list.

- [ ] **Step 2: Run tests to verify failure**

Run: `bun test src/modules/instances/utils/device-client.test.ts`
Expected: FAIL because `./device-client` does not exist.

- [ ] **Step 3: Implement `DeviceClient`**

Create `src/modules/instances/utils/device-client.ts` with exported `DeviceClient`, `DevicesResponse`, and `DeviceSummary`. Implement `getDevices(instance)`, `getDevicesSummary(instance)`, `clearCache(instanceId)`, and `clearAllCache()`. Use default TTL `15_000`, default timeout `3_000`, env overrides `INSTANCE_DEVICES_CACHE_TTL_MS` and `INSTANCE_DEVICES_FETCH_TIMEOUT_MS`, `fetch(http://localhost:${port}/app/${key}/devices)`, `Authorization: Basic ...` from `config.flags.basicAuth[0]`, and defensive normalization for array, `devices`, and `data` response shapes.

- [ ] **Step 4: Run utility tests**

Run: `bun test src/modules/instances/utils/device-client.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit utility**

Run: `git add src/modules/instances/utils/device-client.ts src/modules/instances/utils/device-client.test.ts && git commit -m "feat: add cached instance device client"`

---

### Task 2: Extend Backend Types And Schemas

**Files:**
- Modify: `src/types/index.ts`
- Modify: `src/modules/instances/model.ts`

- [ ] **Step 1: Update shared backend types**

In `src/types/index.ts`, add `Instance.DeviceSummary` and `Instance.DevicesResponse`, then add `devices?: DeviceSummary` to `Instance.StatusResponse`.

- [ ] **Step 2: Update Elysia models**

In `src/modules/instances/model.ts`, add `deviceSummary` and `devicesResponse` schemas. Add optional `devices: deviceSummary` to `statusResponse`.

- [ ] **Step 3: Run typecheck**

Run: `bun run build:tsc`
Expected: PASS.

- [ ] **Step 4: Commit type/schema changes**

Run: `git add src/types/index.ts src/modules/instances/model.ts && git commit -m "feat: add instance device response schemas"`

---

### Task 3: Wire Service And Routes

**Files:**
- Modify: `src/modules/instances/service.ts`
- Modify: `src/modules/instances/index.ts`
- Modify: `src/modules/instances/service.test.ts`
- Modify: `src/modules/instances/routes.test.ts`

- [ ] **Step 1: Add failing service test**

In `src/modules/instances/service.test.ts`, import `DeviceClient` and add a test in `InstanceService.getInstanceStatus` that spies on `DeviceClient.getDevicesSummary`, returns `{ count: 2, connected: true, stale: false, fetchedAt: '2026-06-12T00:00:00.000Z' }`, calls `InstanceService.getInstanceStatus`, and expects `status.devices` to equal that summary.

- [ ] **Step 2: Add failing route tests**

In `src/modules/instances/routes.test.ts`, import `DeviceClient`, store/restore `originalGetDevices`, add a test that creates an instance, stubs `DeviceClient.getDevices` to return two devices with `source: 'live'`, calls `GET /api/instances/:id/devices`, and expects the full response. Add a second test that `GET /api/instances/999999/devices` returns 404.

- [ ] **Step 3: Run focused tests to verify failure**

Run: `bun test src/modules/instances/service.test.ts src/modules/instances/routes.test.ts`
Expected: FAIL because service and route are not wired.

- [ ] **Step 4: Update service**

In `src/modules/instances/service.ts`, import `DeviceClient`. Clear cache in `deleteInstance`, `stopInstance`, and `killInstance` after resource history is cleared. In `getInstanceStatus`, call `DeviceClient.getDevicesSummary(instance)` inside a try/catch and include `devices` in the returned status.

- [ ] **Step 5: Add route**

In `src/modules/instances/index.ts`, import `DeviceClient`. Add `.get('/:id/devices', ...)` before `/:id/status`; return 404 when missing and `DeviceClient.getDevices(instance)` otherwise, with `InstanceModel.devicesResponse` for 200.

- [ ] **Step 6: Run focused tests**

Run: `bun test src/modules/instances/service.test.ts src/modules/instances/routes.test.ts`
Expected: PASS.

- [ ] **Step 7: Commit service/routes wiring**

Run: `git add src/modules/instances/service.ts src/modules/instances/index.ts src/modules/instances/service.test.ts src/modules/instances/routes.test.ts && git commit -m "feat: expose instance devices backend API"`

---

### Task 4: Update Frontend API Types Only

**Files:**
- Modify: `client/src/types/index.ts`
- Modify: `client/src/lib/api.ts`

- [ ] **Step 1: Extend frontend types**

In `client/src/types/index.ts`, add `DeviceSummary` and `InstanceDevicesResponse`. Add `devices?: DeviceSummary` to `InstanceStatus`.

- [ ] **Step 2: Add API client method**

In `client/src/lib/api.ts`, import `InstanceDevicesResponse` and add `getInstanceDevices(id): Promise<InstanceDevicesResponse>` returning `/instances/${id}/devices`.

- [ ] **Step 3: Run frontend build**

Run: `cd client && bun run build`
Expected: PASS.

- [ ] **Step 4: Commit frontend API types**

Run: `git add client/src/types/index.ts client/src/lib/api.ts && git commit -m "feat: add frontend instance devices API types"`

---

### Task 5: Full Validation

- [ ] **Step 1: Run backend focused tests**

Run: `bun test src/modules/instances/utils/device-client.test.ts src/modules/instances/service.test.ts src/modules/instances/routes.test.ts`
Expected: PASS.

- [ ] **Step 2: Run backend typecheck**

Run: `bun run build:tsc`
Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run: `cd client && bun run build`
Expected: PASS.

- [ ] **Step 4: Check git status**

Run: `git status --short`
Expected: clean working tree after commits, or only unrelated pre-existing user changes.
