# Backend Test Coverage

## Running tests

```bash
# Run all tests (no coverage)
bun test

# Run tests with coverage report
bun test --coverage

# Run a single test file
bun test src/modules/instances/utils/process-manager.test.ts
```

## Coverage gate

Aggregate coverage is enforced by `scripts/check-coverage.ts` and the
`.github/workflows/test.yml` CI workflow.

```bash
# Check aggregate coverage against thresholds (default: 90% lines, 90% funcs)
bun run scripts/check-coverage.ts

# Custom thresholds
bun run scripts/check-coverage.ts --lines 0.95 --funcs 0.9
```

The script runs `bun test --coverage`, parses the "All files" row from the
text report, and exits non-zero if aggregate coverage is below the threshold.

### Why not `coverageThreshold` in bunfig.toml?

Bun's built-in `coverageThreshold` is **per-file** as of v1.3.x, not
aggregate ([oven-sh/bun#17028](https://github.com/oven-sh/bun/issues/17028)).
Several modules (e.g. `proxy/index.ts`, `version-manager.ts`) are
intentionally below 90% due to network/spawn dependencies that are
impractical to test in CI. A per-file gate would always fail on those files,
so the aggregate check script is used instead.

## Current coverage

| Metric | Baseline (2026-07-18) | Current |
|--------|----------------------|---------|
| Functions | 84.48% | 96.11% |
| Lines | 88.26% | 98.97% |
| Tests | 171 | 384 |
| Test files | 21 | 29 |

## Test structure

Tests live alongside source files using the `.test.ts` suffix:

```
src/
  cli.test.ts                    # CLI argument parsing
  cors-config.test.ts            # CORS configuration
  db.test.ts                     # Database helpers
  error-handler.test.ts          # Global error handler
  middlewares/auth.test.ts       # Basic auth middleware
  modules/
    auth/routes.test.ts          # Auth routes
    instances/
      index.test.ts              # Instance route edge cases
      routes.test.ts             # Instance route integration
      service.test.ts            # Instance service lifecycle
      utils/
        config-parser.test.ts    # Config parsing
        device-client.test.ts    # Device API client
        directory-manager.test.ts # Directory management
        name-generator.test.ts   # Name generation
        process-manager.test.ts  # Process tracking & exit handlers
        resource-monitor.test.ts # CPU/memory/disk monitoring
        update-config.test.ts    # Config normalization
    proxy/
      auth-utils.test.ts         # Proxy auth utilities
      routes.test.ts             # Proxy route auth behavior
      service.test.ts            # HTTP proxy service
      service.websocket.test.ts  # WebSocket proxy service
      utils.test.ts              # Proxy path utilities
      websocket-registry.test.ts # WebSocket connection registry
      websocket-utils.test.ts    # WebSocket message utilities
    system/
      auto-updater.test.ts       # Auto-update scheduler
      routes.test.ts             # System routes
      service.test.ts            # System status & ports
      version-manager.test.ts    # Version management
      versions.test.ts           # Version API routes
```

## Test setup

`test/setup.ts` is preloaded via `bunfig.toml` and:

- Creates an isolated `DATA_DIR` under `.test-data/` for each test run.
- Suppresses noisy console output (DB init, port allocation, instance lifecycle).
- Cleans up the test data directory on process exit.

## Tracking documents

- [BACKLOG.md](./BACKLOG.md) — coverage improvement backlog with status.
- [TASKS.md](./TASKS.md) — technical breakdown of completed sprint work.
