# Components Layer – AGENTS (`client/src/components`)

## 1. Identity

- **Responsibility**: All React UI components for the GOWA Manager frontend.
  - Instance list, cards, dialogs, tables.
  - Version selection, login, API info modal.
  - Reusable UI primitives under `ui/`.
- **Entrypoints**:
  - Top-level features: `InstanceList`, `InstanceCard`, `CreateInstanceDialog`, `EditInstanceDialog`, `InstanceTable`, `VersionSelector`, `LoginPage`, `ApiInfoModal`.
  - Design system primitives: `ui/*.tsx`.

---

## 2. Layout & Composition Patterns

- **Feature-first components**:
  - `InstanceList.tsx` – fetches instances, handles filters and view mode (cards/table), composes `InstanceCard` / `InstanceTable`.
  - `InstanceCard.tsx` – per-instance status, actions, resource display.
  - `CreateInstanceDialog.tsx` / `EditInstanceDialog.tsx` – dialogs for managing instances.
  - `VersionSelector.tsx` – version dropdown + install button + status badges.
- **UI primitives** (`ui/`):
  - `button.tsx`, `card.tsx`, `input.tsx`, `dialog.tsx`, `badge.tsx`, `switch.tsx`, `tooltip.tsx`, `accordion.tsx`, `alert.tsx`, etc.
  - Tailwind classes + `cn()` from `src/lib/utils.ts`.

---

## 3. Patterns & Conventions

### 3.1 Data fetching in components

- Always go through `apiClient` and React Query:

  ```ts
  const { data: instances } = useQuery({
    queryKey: ['instances'],
    queryFn: () => apiClient.getInstances(),
  })
  ```

- For mutations, use `useMutation` and invalidate queries (`['instances']`, `['instance-status', id]`).
  - See `InstanceCard.tsx` and `InstanceList.tsx` for patterns.

### 3.2 UI structure

- **Cards**:
  - Use `Card`, `CardHeader`, `CardContent`, `CardFooter` from `ui/card`.
  - Icon + text combos (Lucide) with Tailwind classes for colours and spacing.
  - Example: `InstanceCard.tsx` is the canonical pattern for complex cards.

- **Dialogs**:
  - Use `Dialog` primitives from `ui/dialog`.
  - Keep open state in parent and pass `open`/`onOpenChange` props.

### 3.3 Styling

- Tailwind utility classes with consistent patterns:
  - Status colors, grid layouts, spacing.
- Use `cn()` from `src/lib/utils.ts` when conditionally composing classes.
- Prefer `ui` primitives over direct Antd controls, except where Antd is intentionally used (`Segmented`, `Input`, `Select` in `InstanceList`).

---

## 4. Touch Points / Key Files

- `InstanceList.tsx` – filters, layout, view toggle, integration with dialogs & table.
- `InstanceCard.tsx` – full instance lifecycle UI pattern.
- `InstanceTable.tsx` – tabular view for instances.
- `CreateInstanceDialog.tsx` / `EditInstanceDialog.tsx` – form patterns, use of `VersionSelector` and `CliFlagsComponent`.
- `VersionSelector.tsx` – version management UI and React Query usage for versions.
- `LoginPage.tsx` – authentication UI.
- `ApiInfoModal.tsx` – displays API/CLI details per instance.
- `CliFlags/` – CLI flags UX (documented in its own AGENTS file).

---

## 5. JIT Index Hints

From `client/`:

```bash
# Find top-level feature components
rg -n "export function Instance" src/components

# Find use of VersionSelector
rg -n "VersionSelector" src/components

# Find dialog components
rg -n "CreateInstanceDialog" src/components
rg -n "EditInstanceDialog" src/components

# Find where Antd is used
rg -n "from 'antd'" src/components
```

---

## 6. Common Gotchas

- **Mixed UI libraries**: Antd + custom `ui` components coexist; follow existing patterns and avoid introducing a third style system.
- **Query invalidation**: When adding new mutations, ensure you invalidate `['instances']` and related keys; otherwise the UI will look stale.
- **Keys**: When mapping instances, prefer `instance.id` over `index` keys.

---

## 7. Pre-PR Checklist (Components)

- Run a build:

```bash
cd client && bun run build
```

- Manual checks:
  - Instances list loads, filters and view mode toggle work.
  - Cards and table stay in sync when performing actions.
  - Dialogs open/close correctly and reflect backend changes after actions.
