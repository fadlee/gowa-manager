# Web UI – AGENTS Guide (`client/`)

## 1. Package Identity

- **What it is**: React SPA for managing GOWA instances:
  - Instance list, details, actions (start/stop/restart/delete).
  - Version selection & installation.
  - Resource usage, status, proxy links, login.
- **Primary tech**: React 18, TypeScript, Vite, TailwindCSS, Radix/shadcn-style UI, TanStack Query.

---

## 2. Setup & Run

From `client/`:

```bash
# Install frontend dependencies
bun install

# Dev – Vite on :5173
bun run dev

# Build for production (outputs to client/dist)
bun run build

# Preview production build
bun run preview
```

In integrated mode (recommended for full stack), **from repo root**:

```bash
bun run dev        # backend + static-build watch
# OR run separately:
bun run dev:server # backend on :3000
cd client && bun run dev  # frontend on :5173
```

---

## 3. Patterns & Conventions (Frontend)

### 3.1 File organization

- **Entry**
  - `src/main.tsx` – React root, QueryClientProvider, etc.
  - `src/App.tsx` – High-level layout and routing logic (if any).
- **Components**
  - `src/components/ui/*.tsx` – reusable primitives (button, card, dialog, input, etc.).
  - `src/components/InstanceList.tsx` – root view for instances.
  - `src/components/InstanceCard.tsx` – per-instance card with actions + resource display.
  - `src/components/CreateInstanceDialog.tsx`, `EditInstanceDialog.tsx`, `VersionSelector.tsx`.
  - `src/components/CliFlags/**` – modular CLI flags sections (basic auth, webhooks, advanced options).
  - `src/components/LoginPage.tsx` – login UI.
- **Lib & types**
  - `src/lib/api.ts` – `apiClient` wrapper; **all network calls go through here**.
  - `src/lib/auth.tsx` – auth context/provider.
  - `src/lib/utils.ts` – `cn()` helper using `clsx` + `tailwind-merge`.
  - `src/types/index.ts` – shared TS types (`Instance`, `InstanceStatus`, `VersionInfo`, etc.).

### 3.2 Data fetching & mutations

- **DO**:
  - Use **TanStack Query** (`useQuery`, `useMutation`) as in `InstanceCard.tsx`:
    - Query instance status with key `['instance-status', id]`.
    - Invalidate `['instances']` and status queries on mutations.
  - Use `apiClient` methods instead of raw `fetch`:

    ```ts
    const { data: instances } = useQuery({
      queryKey: ['instances'],
      queryFn: () => apiClient.getInstances(),
    })
    ```

- **DON'T**:
  - ❌ Call `fetch('/api/...')` directly in components; centralize in `apiClient`.
  - ❌ Share server response shapes ad-hoc; always use `src/types/index.ts`.

### 3.3 UI components & styling

- **Base primitives**
  - Use components from `src/components/ui`:
    - `Button`, `Card`, `Dialog`, `Input`, `Badge`, `Switch`, `Tooltip`, etc.
- **Composition examples**
  - ✅ **DO**: Follow `InstanceCard.tsx` as a reference for:
    - Combining Lucide icons with Tailwind styling.
    - Using `Card`, `CardHeader`, `CardContent`, `CardFooter`.
    - Rendering dynamic state (running/stopped/error) with badges and progress bars.
  - ✅ **DO**: Use `cn()` from `src/lib/utils.ts` to merge Tailwind classes.

- **Avoid**
  - ❌ Avoid duplicating UI styles when an existing `ui` component can be composed.
  - ❌ Avoid pulling in raw Radix primitives directly if a corresponding `ui` wrapper exists.

### 3.4 Error & status handling

- Instances:
  - `InstanceCard.tsx`:
    - Keeps `lastError` in local state; clears on successful actions.
    - Shows error banner if `status.error_message` or `instance.error_message` present.
    - Distinguishes `running`, `stopped`, and `error` for button sets.
- **DO**:
  - Propagate backend `error_message` fields to the UI where appropriate.
  - Provide visible feedback (`Loader2` spinner, disabled buttons) during mutations.
- **DON'T**:
  - ❌ Swallow errors silently; at minimum log and surface a banner or toast.

---

## 4. Touch Points / Key Files

- **Entry & layout**
  - `src/main.tsx` – React root, provider setup.
  - `src/App.tsx` – overall structure.
- **Core features**
  - Instances:
    - `src/components/InstanceList.tsx` – list + fetch of instances.
    - `src/components/InstanceCard.tsx` – all instance lifecycle actions + resource UI.
  - Dialogs:
    - `src/components/CreateInstanceDialog.tsx`
    - `src/components/EditInstanceDialog.tsx`
  - Versions:
    - `src/components/VersionSelector.tsx`
  - Auth:
    - `src/components/LoginPage.tsx`
    - `src/lib/auth.tsx`
- **Infrastructure**
  - `src/lib/api.ts` – **single API client** for `/api` endpoints.
  - `src/lib/utils.ts` – `cn()` helper for classes.
  - `src/types/index.ts` – types shared across components.

---

## 5. JIT Index Hints (Frontend)

From `client/`:

```bash
# Find React components
rg -n "export function .*" src/components

# Find instance-related UI
rg -n "InstanceCard" src/components
rg -n "InstanceList" src/components

# Find where API client is used
rg -n "apiClient\." src

# Find React Query usage
rg -n "useQuery" src
rg -n "useMutation" src

# Find types for instances and versions
rg -n "interface Instance" src/types
rg -n "VersionInfo" src/types
```

---

## 6. Common Gotchas

- **API base path**
  - `API_BASE` is `/api` (see `src/lib/api.ts`); the backend is assumed to be on same origin.
- **Proxy URLs**
  - `apiClient.getProxyUrl(instance.key)` builds URLs like `window.location.origin + '/app/{key}/'`.
  - Keep this helper in sync with backend `Proxy.PREFIX`.
- **State invalidation**
  - When adding new mutations, always invalidate relevant queries:
    - Typically `['instances']` and any `['instance-status', id]` keys.

---

## 7. Pre-PR Checks (Frontend)

From `client/`:

```bash
# Build should pass
bun run build
```

From repo root (full-stack sanity):

```bash
bun run build:tsc && bun run build:production
```

(Once you add frontend tests with Vitest/Testing Library, extend this with `bun test` in `client/`.)
