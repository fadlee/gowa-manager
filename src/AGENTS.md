# Backend Service – AGENTS Guide (`src/`)

## 1. Package Identity

- **What it is**: Bun + Elysia backend and CLI for managing GOWA instances:
  - Instance lifecycle, process management, resource monitoring.
  - GOWA version management, ports, proxy, basic auth.
- **Primary tech**: Bun, Elysia, TypeScript, SQLite (`bun:sqlite`).

---

## 2. Setup & Run

From repo root:

```bash
# Typecheck backend only (includes src)
bun run build:tsc

# Dev – backend only (hot reload via Bun)
bun run dev:server

# Dev – integrated (recommended for backend + static client)
bun run dev

# Build backend bundle (after prebuild:production embeds static)
bun run build:production

# Run compiled backend
bun run start

# Produce standalone Bun binaries (server + embedded assets)
bun run compile
bun run compile:optimized
```

---

## 3. Patterns & Conventions (Backend)

### 3.1 Module structure

- **Domain modules** live in `src/modules/<domain>/` with:
  - `index.ts` – Elysia routes + HTTP surface.
  - `model.ts` – schemas, response types (used in `response: { ... }`).
  - `service.ts` – business logic, DB/process orchestration.
  - `utils/**` – helpers (process, config, dirs, monitoring).
- **Examples**
  - `src/modules/instances/`
    - `index.ts` – REST API for `/api/instances`.
    - `service.ts` – spawn/stop/restart, status, resource monitoring.
    - `utils/process-manager.ts`, `utils/directory-manager.ts`, `utils/config-parser.ts`, `utils/resource-monitor.ts`.
  - `src/modules/system/`
    - `service.ts` – port allocation & system info.
    - `versions.ts` + `version-manager.ts` – version management API + logic.
  - `src/modules/proxy/`
    - `index.ts` – proxy routes.
    - `service.ts`, `service.websocket.ts` – HTTP & WebSocket proxying.

### 3.2 Routing patterns (Elysia)

- **DO** follow the pattern in `src/modules/instances/index.ts`:
  - Create a module:

    ```ts
    export const instancesModule = new Elysia({ prefix: '/api/instances' })
      .get('/', /* ... */, { response: { 200: InstanceModel.instanceListResponse } })
      .post('/', /* ... */, { body: InstanceModel.createBody, response: {/* ... */} })
    ```

  - Use `response: { statusCode: Schema }` from `model.ts` to keep responses type-safe and documented.
- **DON'T**:
  - ❌ Define routes directly in `src/index.ts` unless it’s cross-cutting bootstrapping.
  - ❌ Throw raw errors without mapping to structured `{ error, success }` shapes defined in `model.ts`.

### 3.3 Service & DB access

- **Single DB instance**:
  - `src/db.ts` initializes `Database` and exports `queries` with prepared statements.
- **DO**:
  - Use `queries.*` for DB access, e.g.:

    ```ts
    const instance = queries.getInstanceById.get(id)
    ```

  - Use `InstanceService` methods from route handlers:
    - Example usage: `InstanceService.createInstance` in `src/modules/instances/index.ts`.
- **DON'T**:
  - ❌ Instantiate `new Database(...)` in modules; **always** go through `src/db.ts`.
  - ❌ Hardcode SQL strings in route handlers.

### 3.4 Process & resource management

- Centralized in `InstanceService` and `utils`:
  - `Bun.spawn` is only called in `InstanceService.startInstance` (`service.ts`).
  - `ProcessManager` manages lifecycles and exit handlers.
  - `ResourceMonitor` tracks CPU/memory/disk and persists history.
- **DO**:
  - Extend `ProcessManager` or `ResourceMonitor` if you need new process-related features.
- **DON'T**:
  - ❌ Call `Bun.spawn` directly from `index.ts` route modules.

### 3.5 Config & paths

- **Data dir**:
  - Managed via `getConfig()` from `src/cli.ts` (used in `src/db.ts`).
  - DB lives in `${dataDir}/gowa.db`.
- **Instance config**:
  - Parsed and normalized via `ConfigParser`:
    - `getDefaultConfig`, `parseConfig`, `processArgs`, `parseEnvironmentVars`.
  - Proxy basePath set using `Proxy.PREFIX` and instance key in `InstanceService.createInstance`.

---

## 4. Touch Points / Key Files

- **Entry & wiring**
  - `src/index.ts` – Elysia app setup, module registration, static file serving.
  - `src/cli.ts` – CLI parsing and config (data dir, port, admin creds).
- **Database**
  - `src/db.ts` – SQLite initialization, migrations, prepared queries, `generateInstanceKey`.
- **Instances**
  - `src/modules/instances/index.ts` – routes for CRUD + actions.
  - `src/modules/instances/service.ts` – instance lifecycle, process & resource logic.
  - `src/modules/instances/utils/process-manager.ts` – process registry & exit handlers.
- **System & versions**
  - `src/modules/system/service.ts` – ports, system status.
  - `src/modules/system/version-manager.ts` – version install/availability/path logic.
  - `src/modules/system/versions.ts` – version API.
- **Proxy & Auth**
  - `src/modules/proxy/index.ts`, `service.ts`, `service.websocket.ts` – proxying.
  - `src/modules/auth/index.ts` – auth module.
  - `src/middlewares/auth.ts` – reusable auth middleware.
- **Types**
  - `src/types/index.ts` – shared enums/types (e.g. `Proxy.PREFIX`).

---

## 5. JIT Index Hints (Backend)

From repo root:

```bash
# Find all API route modules
rg -n "new Elysia" src/modules

# Find instance API + logic
rg -n "InstanceService" src/modules/instances src/types

# Find where processes are spawned
rg -n "Bun\.spawn" src

# Find version management
rg -n "VersionManager" src/modules src/types

# Find auth
rg -n "authModule" src/modules src/middlewares
rg -n "Authorization" src

# Find DB queries
rg -n "queries\." src
```

---

## 6. Common Gotchas

- **Data directory & DB**
  - DB path depends on CLI/env config; don’t hardcode `./data`. Use `getConfig()` and helpers instead.
- **Version availability**
  - `InstanceService.startInstance` **checks `VersionManager.isVersionAvailable`** first.
  - You must ensure versions are installed via the system/version APIs before starting instances.
- **Error handling**
  - `InstanceService.startInstance` stores error messages via `updateInstanceStatusWithError`.
  - Frontend surfaces `error_message`; keep messages user-friendly, not internal stack traces.

---

## 7. Pre-PR Checks (Backend)

Run from repo root:

```bash
# Types + backend + full build (includes client embed)
bun run build:tsc && bun run build:production && bun test
```

(If there are no tests yet, `bun test` should still pass quickly.)
