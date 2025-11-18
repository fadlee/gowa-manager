# GOWA Manager – Root AGENTS Guide

## 1. Project Snapshot

- **Repo type**: Single full-stack project (Bun + Elysia backend, React + Vite frontend).
- **Primary stack**: Bun, Elysia, TypeScript, SQLite, React, Vite, TailwindCSS, TanStack Query.
- **Structure**: Backend/CLI in `src/`, SPA frontend in `client/`. Each has its own AGENTS file.
- **Rule**: Always check the **nearest `AGENTS.md`** to the file you’re editing.

---

## 2. Root Setup Commands

Run all commands from the **repo root** unless noted.

```bash
# Install backend + CLI deps
bun install

# Install frontend deps
cd client && bun install

# Dev – integrated mode (build client on change, serve via Bun)
bun run dev

# Dev – backend only (API + embedded static)
bun run dev:server

# Dev – frontend only (Vite dev server on :5173)
cd client && bun run dev

# Build all for production (client + embed + server)
bun run build:production

# Typecheck all TypeScript (no emit)
bun run build:tsc

# Tests (Bun test runner; currently no tests checked in)
bun test
```

---

## 3. Universal Conventions

- **Language & style**
  - Backend & frontend are **TypeScript-first**. Prefer proper types over `any`.
  - No explicit ESLint/Prettier config in this repo; follow existing patterns in `src/**` and `client/src/**`.
- **Architecture**
  - Backend:
    - Use **module structure**: `src/modules/<domain>/{index.ts, model.ts, service.ts, utils/**}`.
    - All DB access via `src/db.ts` prepared queries.
  - Frontend:
    - Use `client/src/lib/api.ts` as the single REST client.
    - Use **React Query** for data fetching, not ad-hoc `fetch`.
- **Git & branches**
  - Default: branch off `main` with descriptive names:
    - `feat/<area>-<short-desc>`, `fix/<area>-<short-desc>`, `chore/<area>-<short-desc>`.
  - Keep commits **small and focused**; imperative subject lines.
- **Commits**
  - No commit linting enforced; Conventional Commits style is **encouraged but not required**:
    - `feat: add instance CPU usage chart`
    - `fix: handle missing gowa_version`
- **PR expectations**
  - PRs should:
    - Include **tests** when adding behavior (once tests exist).
    - Update docs in `README.md` or `docs/**` if APIs/CLI flags change.
    - Pass `bun run build:tsc` and `bun run build:production`.

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

- **Backend service & CLI**: `src/`
  - Start with: `src/index.ts`, `src/modules/instances/index.ts`, `src/modules/system/versions.ts`.
  - See [src/AGENTS.md](src/AGENTS.md) for backend patterns and commands.
- **Web UI (SPA)**: `client/`
  - Start with: `client/src/App.tsx`, `client/src/components/InstanceList.tsx`, `client/src/lib/api.ts`.
  - See [client/AGENTS.md](client/AGENTS.md) for frontend patterns and commands.
- **Static & embedding**
  - `scripts/embed-static.ts`, `scripts/dev-watch.ts`
  - `public/` and `src/embedded-static.ts` (generated).
- **Docs**
  - Integration and versioning: `docs/INTEGRATED_SETUP.md`, `docs/VERSION_MANAGEMENT.md`.
  - CLI flags reference: `docs/gowa-cli-flags.md`.

### Quick Find Commands (from repo root)

```bash
# Search for a backend route definition
rg -n "new Elysia" src/modules

# Find instance-related API handlers
rg -n "instancesModule" src/modules

# Find system/version-related logic
rg -n "VersionManager" src/modules src/types

# Find all Bun.spawn usages (process management)
rg -n "Bun\.spawn" src

# Find React components
rg -n "export function .*" client/src

# Find React Query usages
rg -n "useQuery" client/src
rg -n "useMutation" client/src

# Find API client usage
rg -n "apiClient\." client/src

# Find types
rg -n "export interface Instance" client/src/types src/types
```

---

## 6. Definition of Done (Repo-wide)

Before you consider a change **ready for PR**:

- **Build & types**
  - `bun run build:tsc`
  - `bun run build:production` (ensures client build + embed + server build).
- **Tests**
  - `bun test` (once tests exist; keep it passing at all times).
- **Self-check**
  - No obvious TypeScript `any` leaks, unused imports, or dead code in touched files.
  - Docs updated if you changed:
    - Public API routes (`/api/**`)
    - CLI flags
    - Auth/env behavior
