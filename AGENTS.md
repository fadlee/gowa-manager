# GOWA Manager – Root AGENTS Guide

## 1. Project Snapshot

- **Repo type**: Full-stack project (Go backend, React + Vite frontend).
- **Primary stack**: Go 1.24, SQLite, React 18, Vite, TailwindCSS, TanStack Query, Bun (frontend tooling only).
- **Structure**: Go backend in `cmd/` + `internal/`, SPA frontend in `client/`. Each has its own AGENTS file.
- **Rule**: Always check the **nearest `AGENTS.md`** to the file you’re editing.

---

## 2. Root Setup Commands

Run all commands from the **repo root** unless noted.

```bash
# Install frontend deps (Bun is used only for client tooling)
bun install

# Dev – frontend only (Vite dev server on :5173)
bun run dev

# Dev – Go backend
bun run dev:go
# OR: go run ./cmd/gowa-manager-go

# Build for production (client + Go binary)
bun run build:production

# Go tests
go test ./...

# Go benchmark comparison
bun run test:benchmark:compare
```

---

## 3. Universal Conventions

- **Language & style**
  - Go backend: follow existing patterns in `internal/**`. Use `go vet` and `go test`.
  - Frontend: **TypeScript-first**. Prefer proper types over `any`.
  - No explicit ESLint/Prettier config; follow existing patterns in `client/src/**`.
- **Architecture**
  - Backend (Go):
    - Module structure: `internal/<domain>/` with service, handler, and model files.
    - HTTP API routes in `internal/httpapi/`.
    - DB access via `internal/database/`.
  - Frontend:
    - Use `client/src/lib/api.ts` as the single REST client.
    - Use **React Query** for data fetching, not ad-hoc `fetch`.
- **Git & branches**
  - Default: branch off `main` with descriptive names:
    - `feat/<area>-<short-desc>`, `fix/<area>-<short-desc>`, `chore/<area>-<short-desc>`.
  - Keep commits **small and focused**; imperative subject lines.
- **Commits**
  - Always use Conventional Commits for commit messages.
  - No commit linting enforced, but commit subjects must follow the convention:
    - `feat: add instance CPU usage chart`
    - `fix: handle missing gowa_version`
- **PR expectations**
  - PRs should:
    - Include **tests** when adding behavior.
    - Update docs in `README.md` or `docs/**` if APIs/CLI flags change.
    - Pass `go test ./...` and `cd client && bun run build`.

---

## 4. Security & Secrets

- **Never commit real secrets**:
  - `.env` and `.env.example` exist; only `.env.example` should be committed.
  - Use `.gitignore`-d `.env` for local credentials.
- **Auth**
  - Backend uses basic auth variables:
    - `ADMIN_USERNAME`, `ADMIN_PASSWORD` (see `README.md` and `docs/INTEGRATED_SETUP.md`).
  - Do not log sensitive values; log IDs/keys instead.
- **Data paths**
  - Data directory controlled via env/CLI (`DATA_DIR`, CLI flags).
  - SQLite DB stored under `data/` (e.g. `data/gowa.db`); treat as **local data**, not to be checked in.
- **PII**
  - Instance configs may contain URLs, tokens, or webhook targets; do **not** log full configs in production.

---

## 5. JIT Index (what to open, not what to paste)

### Package Structure

- **Go backend**: `cmd/` + `internal/`
  - Start with: `cmd/gowa-manager-go/main.go`, `internal/app/app.go`, `internal/httpapi/`.
  - See `internal/AGENTS.md` (if present) or package-level comments for Go patterns.
- **Web UI (SPA)**: `client/`
  - Start with: `client/src/App.tsx`, `client/src/components/InstanceList.tsx`, `client/src/lib/api.ts`.
  - See [client/AGENTS.md](client/AGENTS.md) for frontend patterns and commands.
- **Static & embedding**
  - `public/` (Vite build output, gitignored) → `internal/static/web/` (Go embed directory).
  - Build: `bun run build:client` then `cp -r public/* internal/static/web/`.
  - Or use `bun run scripts/build-go.ts` which does both.
- **Docs**
  - Integration and versioning: `docs/INTEGRATED_SETUP.md`, `docs/VERSION_MANAGEMENT.md`.
  - CLI flags reference: `docs/gowa-cli-flags.md`.

### Quick Find Commands (from repo root)

```bash
# Search for Go HTTP route definitions
rg -n "func.*Handler\|HandleFunc\|mux\." internal/httpapi

# Find instance-related Go code
rg -n "instances" internal/instances

# Find system/version-related Go logic
rg -n "VersionManager\|versions" internal/versions

# Find process management (Go)
rg -n "exec\.\|os/exec" internal/supervisor

# Find React components
rg -n "export function .*" client/src

# Find React Query usages
rg -n "useQuery" client/src
rg -n "useMutation" client/src

# Find API client usage
rg -n "apiClient\." client/src

# Find types
rg -n "export interface Instance" client/src/types
```

---

## 6. Definition of Done (Repo-wide)

Before you consider a change **ready for PR**:

- **Build & types**
  - `go build ./...`
  - `cd client && bun run build`
- **Tests**
  - `go test ./...`
- **Self-check**
  - No obvious unused imports or dead code in touched files.
  - Docs updated if you changed:
    - Public API routes (`/api/**`)
    - CLI flags
    - Auth/env behavior

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
