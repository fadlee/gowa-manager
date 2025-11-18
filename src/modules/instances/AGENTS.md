# Instances Module – AGENTS (`src/modules/instances`)

## 1. Identity

- **Responsibility**: Full lifecycle of GOWA instances.
  - Create/update/delete instances in SQLite.
  - Start/stop/restart/kill processes via Bun.
  - Track status, uptime, error messages, resource usage.
- **Entrypoints**:
  - HTTP: `src/modules/instances/index.ts` (`/api/instances/**`).
  - Service: `src/modules/instances/service.ts`.
  - Utils: `src/modules/instances/utils/*.ts`.

---

## 2. How This Module Works

- **Data model** (backed by `src/db.ts` `instances` table):
  - `id`, `key`, `name`, `port`, `status`, `config` (JSON string), `gowa_version`, `error_message`.
- **Process management**:
  - `ProcessManager` keeps a map of running processes and sets up exit handlers.
  - `Bun.spawn` is called **only** in `InstanceService.startInstance`.
- **Resource tracking**:
  - `ResourceMonitor` periodically records CPU/memory/disk and exposes history per instance.
- **Filesystem layout**:
  - Instance data lives under a per-instance directory created by `DirectoryManager` (see utils).

---

## 3. Patterns & Conventions

### 3.1 Route handlers (`index.ts`)

- Always delegate work to `InstanceService` and use `InstanceModel` schemas:

  ```ts
  .post('/', async ({ body, set }) => {
    try {
      const requestBody = body || {}
      const instance = await InstanceService.createInstance(requestBody)
      set.status = 201
      return instance
    } catch (error) {
      set.status = 400
      return {
        error: error instanceof Error ? error.message : 'Failed to create instance',
        success: false,
      }
    }
  }, {
    body: InstanceModel.createBody,
    response: { 201: InstanceModel.instanceResponse, 400: InstanceModel.validationError },
  })
  ```

- **DO**:
  - Use `set.status` and return `{ error, success: false }` on failures.
  - Reference `InstanceModel.*` for `body` and `response` typing.
- **DON'T**:
  - ❌ Access `db` or spawn processes directly from route handlers.
  - ❌ Throw raw errors without mapping them to structured responses.

### 3.2 Service layer (`service.ts`)

- Central place for:
  - DB access via `queries.*` from `src/db.ts`.
  - Directory creation/cleanup.
  - Process lifecycle via `ProcessManager`.
  - Config parsing + environment building via `ConfigParser`.
  - Version existence checks via `VersionManager.isVersionAvailable`.
- Example flow in `startInstance(id)`:
  - Load instance from DB → ensure version installed → create directory → parse config → spawn → register process → update status + clear errors → compute status with resources.

### 3.3 Config handling (`utils/config-parser.ts`)

- `ConfigParser` is the **single source** for:
  - Default config (`getDefaultConfig`).
  - Merging user JSON into defaults.
  - Translating config into CLI args and env vars.
- **DO**:
  - Extend config flags via `ConfigParser` and type changes, not ad-hoc JSON munging.
- **DON'T**:
  - ❌ Manually build `cmd`/`env` for GOWA processes in new code.

### 3.4 Directories & resources (`utils/directory-manager.ts`, `utils/resource-monitor.ts`)

- **DirectoryManager**:
  - Responsible for creating and cleaning up per-instance dirs.
  - Used from `InstanceService` before starting/killing instances.
- **ResourceMonitor**:
  - Encapsulates `pidusage` integration.
  - Provides `getResourceUsage(pid, id)` and `clearHistory(id)`.

---

## 4. Touch Points / Key Files

- `index.ts` – all routes for `/api/instances` including actions.
- `service.ts` – instance lifecycle & orchestration.
- `utils/process-manager.ts` – process registry, exit handling.
- `utils/directory-manager.ts` – per-instance folders.
- `utils/config-parser.ts` – config/flags/env shaping.
- `utils/resource-monitor.ts` – CPU/memory/disk history.

**Good starting read**: `service.ts` → `utils/*` → `index.ts`.

---

## 5. JIT Index Hints

```bash
# Find all instance routes
rg -n "instancesModule" src/modules/instances

# Find lifecycle methods
rg -n "startInstance\(" src/modules/instances
rg -n "restartInstance\(" src/modules/instances

# Find where resources are read
rg -n "ResourceMonitor" src/modules/instances

# Find config-related logic
rg -n "ConfigParser" src/modules/instances src/types

# Find instance-related DB queries
rg -n "getAllInstances" src/db.ts
```

---

## 6. Common Gotchas

- **Port allocation**: Never assign ports manually; always call `SystemService.getNextAvailablePort()`.
- **Version availability**: `startInstance` will fail if `VersionManager.isVersionAvailable` is false. Ensure versions are installed first.
- **Error messages**: Store user-facing messages in `error_message`; avoid leaking stack traces.
- **Status correctness**: Use `ProcessManager.isReallyRunning` before assuming a `running` status is valid.

---

## 7. Pre-PR Checklist (Instances)

From repo root:

```bash
bun run build:tsc && bun run build:production
```

Then manually verify:

- Create instance → start → see proxy link and resource usage.
- Stop/restart/kill flows work and DB status matches UI.
