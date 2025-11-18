# System & Versions Module – AGENTS (`src/modules/system`)

## 1. Identity

- **Responsibility**:
  - System-level status and configuration.
  - Port allocation & availability checks.
  - GOWA binary version discovery, installation, cleanup.
  - Automatic instance media cleanup (JPEG & media file deletion).
- **Entrypoints**:
  - HTTP: `versionsModule` (e.g. `/api/system/versions/**`) in `versions.ts`.
  - Service: `SystemService` in `service.ts`.
  - Version orchestration: `VersionManager` in `version-manager.ts`.
  - Scheduled cleanup: `CleanupScheduler` in `cleanup-scheduler.ts` (runs daily at midnight).

---

## 2. How This Module Works

- `SystemService`:
  - Uses DB (`queries.getAllInstances`) to compute counts and allocated ports.
  - Picks next free port via `getNextAvailablePort()` + `isPortAvailable()` using `net.createServer`.
  - Exposes data directory & paths using `DATA_DIR` or CLI config.
- `VersionManager`:
  - Computes `dataDir/bin/versions/{version}/gowa` paths.
  - Talks to GitHub Releases for `go-whatsapp-web-multidevice` to discover available versions.
  - Uses `binary-download.ts` for installing specific versions.
  - Manages cleanup of old versions based on `installedAt` and `keepCount`.
- `versions.ts` Elysia module:
  - Defines all version-related endpoints, request/response models, and maps errors to `{ error, success: false }`.

---

## 3. Patterns & Conventions

### 3.1 System status & ports (`service.ts`)

- Use DB as the **source of truth** for allocated ports; confirm with OS availability:

  ```ts
  static async getNextAvailablePort(): Promise<number> {
    const instances = queries.getAllInstances.all() as any[]
    const usedPorts = instances.filter(i => i.port !== null).map(i => i.port)
    // start at 8000, skip used, confirm with isPortAvailable
  }
  ```

- `isPortAvailable(port)` checks:
  - Special-case `3000` as reserved.
  - Disallow ports `<1024`.
  - Uses `net.createServer().listen(port, '127.0.0.1')` to ensure the OS agrees.

### 3.2 Version discovery & storage (`version-manager.ts`)

- Data location:
  - `dataDir/bin/versions/{version}/gowa[.exe]`.
  - `dataDir` is from `getConfig().dataDir` via `cli.ts` and resolved to an absolute path.
- Installed versions:
  - `getInstalledVersions()` enumerates directories under `bin/versions` and inspects the binary file for `size` and `birthtime`.
  - `resolveLatestVersion()` picks a max tag by simple string/semver ordering.
- Available versions:
  - `getAvailableVersions(limit)` calls GitHub API and merges with installed version info.
  - Special `version: 'latest'` entry uses `isLatest` and `getVersionBinaryPath('latest')`.
- Installation & cleanup:
  - `installVersion(version)` delegates to `downloadSpecificVersion` from `binary-download.ts`.
  - `cleanup(keepCount)` keeps the newest `N` versions by `installedAt` and deletes older ones.

### 3.3 HTTP surface (`versions.ts`)

- Expose operations via `versionsModule`:
  - `GET /installed` → `VersionManager.getInstalledVersions()`.
  - `GET /available?limit=10` → `VersionManager.getAvailableVersions(limit)`.
  - `POST /install` → `VersionManager.installVersion(version)`.
  - `DELETE /:version` → `VersionManager.removeVersion(version)`.
  - `GET /:version/available` → `VersionManager.isVersionAvailable(version)` with binary path.
  - `GET /usage` → `VersionManager.getVersionsSize()`.
  - `POST /cleanup` → `VersionManager.cleanup(keepCount)`.

- **DO**:
  - Capture errors and map to `ErrorModel` (`{ error, success: false }`).
  - Use `t.Object(...)` models for all responses.

### 3.4 Automatic cleanup (`cleanup-scheduler.ts`)

- **CleanupScheduler** runs daily at midnight (UTC `0 0 * * *`):
  - Automatically deletes `*.jpeg` and `*.jpg` files from each instance's `storages/` directory.
  - Automatically deletes all files from each instance's `statics/media/` directory.
  - Iterates through all instances in the database.
  - Logs results: count of deleted files, errors, and total duration.
  - Runs independently of instance lifecycle (doesn't require instances to be running).

- **Starting the scheduler**:
  - Automatically started in `src/index.ts` during app initialization via `CleanupScheduler.start()`.
  - Gracefully stopped on process shutdown via `SIGINT` handler.

- **Implementation details**:
  - Uses `node-cron` for cron scheduling.
  - Handles missing directories gracefully (returns 0 deletions if path doesn't exist).
  - Logs each instance's cleanup with instance name and file counts.
  - Catches and logs errors per-instance to avoid stopping cleanup for other instances.

---

## 4. Touch Points / Key Files

- `service.ts` – system status, port allocation, data directory info.
- `version-manager.ts` – core version management and disk usage.
- `versions.ts` – Elysia routes and request/response schemas.
- `cleanup-scheduler.ts` – automatic daily cleanup of instance media and storage files.
- `../../binary-download.ts` – used internally by `VersionManager.installVersion`.

---

## 5. JIT Index Hints

```bash
# Find where system status is computed
rg -n "getSystemStatus" src/modules/system

# Find port allocation logic
rg -n "getNextAvailablePort" src/modules/system
rg -n "isPortAvailable" src/modules/system

# Find version-related endpoints
rg -n "versionsModule" src/modules/system

# Find GitHub-related logic
rg -n "GitHubRelease" src/modules/system/version-manager.ts
rg -n "api.github.com" src/modules/system/version-manager.ts

# Find cleanup scheduler logic
rg -n "CleanupScheduler" src/modules/system/cleanup-scheduler.ts
rg -n "runCleanup\|cleanupInstanceStorageJpegs\|cleanupInstanceMediaFiles" src/modules/system/cleanup-scheduler.ts
```

---

## 6. Common Gotchas

- **Port conflicts**: Always use `SystemService.getNextAvailablePort()`; do not allocate ports manually.
- **DATA_DIR vs CLI**: Data paths are driven by CLI config/env; don’t hardcode `./data`.
- **`latest` handling**:
  - `isVersionAvailable('latest')` checks the actual latest installed version.
  - You cannot remove `latest` via `DELETE /:version`.
- **GitHub API failures**:
  - `getAvailableVersions()` returns `[]` on errors; handle gracefully on the frontend (show fallback or warning).
- **Cleanup scheduler**:
  - Runs automatically at midnight UTC; no manual API endpoint exists yet.
  - Missing directories are handled gracefully and don't cause errors.
  - If instances are being used during cleanup, in-flight operations should not be affected (files are only deleted, not accessed).

---

## 7. Pre-PR Checklist (System/Versions)

- Verify basic flows via API:
  - List installed versions, fetch available versions, install/remove version.
  - Call `GET /api/system/ports/next` (if available) or `getSystemStatus()` equivalent from the UI.
- Ensure:
  - `bun run build:tsc && bun run build:production` succeed.
