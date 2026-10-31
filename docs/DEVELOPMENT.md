# Development

GOWA Manager is a Go backend (`cmd/`, `internal/`) with an embedded React SPA (`client/`). Bun is used only for frontend tooling and build scripts.

Requirements: Go 1.24+, Bun.

## Run locally

```bash
git clone https://github.com/fadlee/gowa-manager.git
cd gowa-manager
bun install

bun run dev
```

Open http://localhost:5173 and log in with `admin` / `password123`.

`bun run dev` builds and starts the Go backend (on :3000, or the next free port) and the Vite dev server with hot reload, which proxies `/api` and `/app` to the backend. Ctrl+C stops both.

- UI changes reload instantly. After changing Go code, press Ctrl+C and run `bun run dev` again.
- Data is kept in `.contract-tmp/dev/` (gitignored). Delete it to start fresh.
- Extra arguments go to the backend: `bun run dev -- --verbose`.
- `GOWA_SKIP_STARTUP_INSTALL=1 bun run dev` skips downloading the latest GOWA on start.
- To try the first-run setup page, run the two sides without a seeded user: `bun run dev:go -- --data-dir .contract-tmp/setup` and, in a second terminal, `bun run dev:web`. The setup token is printed by the backend.

To preview the production build instead (one binary with the UI embedded, on :3000):

```bash
bun run build:go
./dist-go/gowa-manager-go --data-dir .contract-tmp/preview -u admin -P password123
```

## Build

```bash
# Build the client, embed it, and build the Go binary
bun run build:go
```

The client is built by Vite into `public/`, then copied to `internal/static/web/` and embedded with `go:embed`. See [INTEGRATED_SETUP.md](INTEGRATED_SETUP.md).

## Tests

```bash
# Go: unit, integration, ops-script, and compat tests (what CI gates on)
go test ./...
go test -race ./cmd/... ./internal/...

# Frontend: type-check, then build
cd client && bun run typecheck && bun run build

# Script tests (CI workflows, Docker runtime, release artifacts)
bun test scripts

# Browser E2E against a freshly built Go backend (Playwright)
bun run test:e2e:go
```

Tests are hermetic: each one uses its own temp data directory and free port, builds the manager from source, and only touches processes it started itself (processes are matched by exact executable path, never by name). A real GOWA Manager running on the same machine does not affect the results.

Opt-in suites:

- `GOWA_DOCKER_TESTS=1 go test ./test/compat/ -run TestDockerSuite` builds the Docker image and checks container behavior. It needs Docker with Linux containers.
- `go test -run TestLeakDetection -count=1 ./test/benchmark/` is a resource-leak stress test with tight thresholds. CI's blocking jobs skip it.

## Project layout

```
cmd/gowa-manager-go/   # Entry point, CLI
internal/
├── app/               # Wiring and startup
├── httpapi/           # HTTP routes and middleware
├── users/             # Users, sessions, API keys, setup flow
├── instances/         # Instance model, config, lifecycle
├── supervisor/        # GOWA process management
├── versions/          # GOWA version download and storage
├── proxy/             # /app/{key} HTTP and WebSocket proxy
├── database/          # SQLite schema and migrations
└── static/            # Embedded Web UI
client/src/            # React SPA (React Query, Tailwind)
scripts/               # Build, release, and ops scripts
test/                  # Ops, compat, benchmark, and E2E tests
```

## Releases

Pushing a `v*` tag runs `.github/workflows/release.yml`. It builds the binaries and Docker images and creates a GitHub release.

1. Set `version` in `package.json`. The binary reports this version.
2. Create an annotated tag. Its message becomes the release notes: `git tag -a v3.1.0 --cleanup=verbatim -F notes.md`.
3. Push the tag.

Tags with `-alpha.`, `-beta.`, or `-rc.` (for example `v3.1.0-rc.1`) are published as prereleases. Prereleases do not move the Docker `latest` tag and are not picked up by `npx` or `install.sh`.

## Contributing

1. Branch off `main`: `feat/<area>-<desc>`, `fix/<area>-<desc>`, or `chore/<area>-<desc>`.
2. Follow the existing patterns. Use Go for backend changes and TypeScript for the frontend.
3. Add tests for new behavior.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) (`feat: …`, `fix: …`).
5. Update the docs when you change API routes, CLI flags, or auth and env behavior.
6. Make sure `go test ./...` and `cd client && bun run build` pass, then open a pull request.
