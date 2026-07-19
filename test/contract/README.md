# Backend Contract Test Foundation

Contract tests compare Bun and Go backend behavior with equivalent fixtures.

Run the management parity suite from the repository root:

```bash
go test -race ./test/contract -run TestManagementParity -v -count=1
```

Requirements:

- Go toolchain with CGO-free `modernc.org/sqlite` support from `go.mod`.
- Bun installed and available on `PATH`.
- Project dependencies installed with `bun install`.

`TestManagementParity` starts two separate backend processes:

- Bun: `bun run src/index.ts --port <port> --data-dir <dir>`
- Go: `go run ./cmd/gowa-manager-go --port <port> --data-dir <dir>`

Each backend receives the same `ADMIN_USERNAME`, `ADMIN_PASSWORD`, `PORT`, and `DATA_DIR` environment shape, but always uses its own unique port and isolated temporary data directory. The suite waits for `/api/health`, runs the same ordered management scenarios, compares normalized responses, verifies SQLite integrity for both resulting databases, compares normalized instance rows, and compares relative filesystem trees. Data directories are never shared.

Snapshots should include status, selected headers, decoded JSON bodies, relevant database rows, and filesystem paths. Normalize nondeterministic values before comparison:

- timestamps: `created_at`, `updated_at`;
- process values: `pid`, `uptime`;
- known manager and instance ports;
- temporary absolute filesystem paths;
- volatile headers such as `Date` and `X-Request-ID`.

Do not normalize by running broad regex replacements over response strings. Decode JSON first and walk typed values so meaningful user-visible strings stay intact.

Current limitations:

- Runtime lifecycle is still a Plan 03 fake in Go, so this suite covers stopped-instance behavior (`devices`, `test-connection`, and runtime-not-ready failures) rather than successful process lifecycle parity.
- Go auth/proxy is expected in Plan 04. Bun-only unauthenticated management access is asserted as `401`; authenticated Bun requests are compared to current Go management behavior. Add a dual-backend unauthenticated `401` comparison when Go auth lands.
- Version install failure is normalized to failure intent because Bun currently reports a GitHub release lookup failure while Go rejects invalid version tags earlier. Successful installation parity should be added once fixture binaries or a fake release source are available.
