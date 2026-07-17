# Webhook Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-webhook enable/disable toggles so users can pause webhook URLs without deleting them.

**Architecture:** Keep `webhooks: string[]` as the list of configured URLs and add `disabledWebhooks?: string[]` for paused URLs. The frontend manages the toggle state and cleanup; the backend filters disabled URLs when generating `--webhook` CLI arguments.

**Tech Stack:** TypeScript, React, Bun test runner, existing UI primitives (`Button`, `Input`, `Switch`, `Badge` if available).

---

## File Structure

- Modify `client/src/types/index.ts`: add `disabledWebhooks?: string[]` to `CliFlags`.
- Modify `src/modules/instances/utils/config-parser.ts`: add backend flag type field and filter disabled webhook URLs in `flagsToArgs`.
- Modify `src/modules/instances/utils/config-parser.test.ts`: test default compatibility and disabled filtering.
- Modify `client/src/components/CliFlags/WebhooksSection.tsx`: add row toggle, state label, muted disabled styling, and cleanup on remove.
- Modify `client/src/components/instance-detail/OverviewSection.tsx`: parse disabled state and show disabled webhook rows clearly.
- Check `client/src/components/CliFlags.tsx`: legacy duplicate component; update only if compiled or imported.

---

### Task 1: Backend filtering and tests

**Files:**
- Modify: `src/modules/instances/utils/config-parser.ts`
- Modify: `src/modules/instances/utils/config-parser.test.ts`

- [ ] **Step 1: Add failing backend tests**

Add these tests to `src/modules/instances/utils/config-parser.test.ts` after `converts flags to CLI arguments`:

```ts
test('excludes disabled webhooks from CLI arguments', () => {
  const args = ConfigParser.flagsToArgs({
    webhooks: ['https://example.com/a', 'https://example.com/b', 'https://example.com/c'],
    disabledWebhooks: ['https://example.com/b'],
  })

  expect(args).toEqual([
    '--webhook=https://example.com/a',
    '--webhook=https://example.com/c',
  ])
})

test('emits no webhook args when every webhook is disabled', () => {
  const args = ConfigParser.flagsToArgs({
    webhooks: ['https://example.com/a', 'https://example.com/b'],
    disabledWebhooks: ['https://example.com/a', 'https://example.com/b'],
  })

  expect(args).toEqual([])
})
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
bun test src/modules/instances/utils/config-parser.test.ts
```

Expected: TypeScript/test failure because `disabledWebhooks` is not in the backend `CliFlags` interface or disabled URLs are still emitted.

- [ ] **Step 3: Implement backend field and filtering**

In `src/modules/instances/utils/config-parser.ts`, add to `interface CliFlags`:

```ts
disabledWebhooks?: string[];
```

Replace the webhook block in `flagsToArgs` with:

```ts
if (flags.webhooks && flags.webhooks.length > 0) {
  const disabledWebhooks = new Set(flags.disabledWebhooks || [])
  flags.webhooks
    .filter(webhook => !disabledWebhooks.has(webhook))
    .forEach(webhook => {
      args.push(`--webhook=${webhook}`)
    })
}
```

- [ ] **Step 4: Run backend tests**

Run:

```bash
bun test src/modules/instances/utils/config-parser.test.ts
```

Expected: all tests in the file pass.

---

### Task 2: Frontend type and webhook settings UI

**Files:**
- Modify: `client/src/types/index.ts`
- Modify: `client/src/components/CliFlags/WebhooksSection.tsx`

- [ ] **Step 1: Add frontend type field**

In `client/src/types/index.ts`, add to `CliFlags`:

```ts
disabledWebhooks?: string[];
```

- [ ] **Step 2: Import Switch**

In `client/src/components/CliFlags/WebhooksSection.tsx`, add:

```ts
import { Switch } from '../ui/switch'
```

- [ ] **Step 3: Add toggle helper functions**

Inside `WebhooksSection`, after `handleWebhookBlur`, add:

```ts
const isWebhookDisabled = (webhook: string) => (flags.disabledWebhooks || []).includes(webhook)

const toggleWebhook = (webhook: string, enabled: boolean) => {
  const disabledWebhooks = flags.disabledWebhooks || []
  if (enabled) {
    updateFlag('disabledWebhooks', disabledWebhooks.filter((disabledWebhook) => disabledWebhook !== webhook))
    return
  }

  if (!disabledWebhooks.includes(webhook)) {
    updateFlag('disabledWebhooks', [...disabledWebhooks, webhook])
  }
}
```

- [ ] **Step 4: Clean disabled list when removing webhook**

Replace `removeWebhook` with:

```ts
const removeWebhook = (index: number, e: React.MouseEvent) => {
  e.preventDefault()
  const webhooks = flags.webhooks || []
  const webhookToRemove = webhooks[index]
  updateFlag('webhooks', webhooks.filter((_, i) => i !== index))
  updateFlag('disabledWebhooks', (flags.disabledWebhooks || []).filter((webhook) => webhook !== webhookToRemove))
}
```

- [ ] **Step 5: Render row switch and state label**

Replace each webhook row body with a version that computes disabled state:

```tsx
{flags.webhooks.map((webhook, index) => {
  const disabled = isWebhookDisabled(webhook)
  return (
    <div
      key={`${webhook}-${index}`}
      className={`flex gap-2 items-center p-2 rounded-md ${
        disabled
          ? 'bg-gray-100 text-gray-500 dark:bg-gray-800/60 dark:text-gray-400'
          : 'bg-gray-200 dark:bg-gray-700'
      }`}
    >
      <span className="rounded bg-gray-300 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-gray-700 dark:bg-gray-600 dark:text-gray-200">
        {index + 1}
      </span>
      <span className={`flex-1 font-mono text-sm truncate ${disabled ? 'text-gray-500 line-through dark:text-gray-400' : 'text-gray-900 dark:text-white'}`}>
        {webhook}
      </span>
      <span className={`text-xs font-medium ${disabled ? 'text-gray-500 dark:text-gray-400' : 'text-green-600 dark:text-green-400'}`}>
        {disabled ? 'Disabled' : 'Active'}
      </span>
      <Switch
        checked={!disabled}
        onCheckedChange={(checked) => toggleWebhook(webhook, checked)}
        aria-label={`${disabled ? 'Enable' : 'Disable'} webhook ${webhook}`}
      />
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={(e) => removeWebhook(index, e)}
        className="p-0 w-6 h-6"
      >
        <X className="w-3 h-3" />
      </Button>
    </div>
  )
})}
```

- [ ] **Step 6: Run frontend typecheck**

Run:

```bash
bun run build:tsc
```

Expected: typecheck passes or reveals import/typing issues to fix.

---

### Task 3: Instance overview disabled indicator

**Files:**
- Modify: `client/src/components/instance-detail/OverviewSection.tsx`

- [ ] **Step 1: Track disabled webhooks during config parse**

Near `let webhooks: string[] = []`, add:

```ts
let disabledWebhooks: string[] = []
```

Inside the config parse block, add:

```ts
disabledWebhooks = config.flags?.disabledWebhooks || []
```

After parsing, add:

```ts
const disabledWebhookSet = new Set(disabledWebhooks)
```

- [ ] **Step 2: Render disabled overview rows**

Replace the `webhooks.map` row with:

```tsx
{webhooks.map((webhook, index) => {
  const disabled = disabledWebhookSet.has(webhook)
  return (
    <div key={`${webhook}-${index}`} className="flex items-center gap-2">
      <code className={`min-w-0 flex-1 truncate rounded-md bg-white px-3 py-2 font-mono text-sm dark:bg-gray-950 ${disabled ? 'text-gray-500 line-through dark:text-gray-500' : 'text-gray-900 dark:text-white'}`}>
        {webhook}
      </code>
      {disabled && (
        <span className="rounded-full bg-gray-200 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-400">
          Disabled
        </span>
      )}
      <CopyButton content={webhook} variant="ghost" className="text-gray-600 dark:text-gray-400" />
    </div>
  )
})}
```

- [ ] **Step 3: Run typecheck**

Run:

```bash
bun run build:tsc
```

Expected: typecheck passes.

---

### Task 4: Final verification

**Files:**
- Verify all touched files.

- [ ] **Step 1: Run targeted tests**

Run:

```bash
bun test src/modules/instances/utils/config-parser.test.ts
```

Expected: all config parser tests pass.

- [ ] **Step 2: Run full typecheck**

Run:

```bash
bun run build:tsc
```

Expected: TypeScript passes.

- [ ] **Step 3: Check git diff**

Run:

```bash
git diff -- client/src/types/index.ts src/modules/instances/utils/config-parser.ts src/modules/instances/utils/config-parser.test.ts client/src/components/CliFlags/WebhooksSection.tsx client/src/components/instance-detail/OverviewSection.tsx
```

Expected: diff only contains webhook toggle changes.
