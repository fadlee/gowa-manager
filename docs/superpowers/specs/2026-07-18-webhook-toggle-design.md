# Webhook Toggle Design

## Goal

Allow users to temporarily disable an instance webhook URL without deleting it. Disabled webhook URLs remain visible in the settings UI, but are not passed to the GOWA process as `--webhook` arguments when the instance config is used.

## Chosen Approach

Keep the existing `webhooks: string[]` field as the canonical list of configured webhook URLs and add a new optional `disabledWebhooks: string[]` field to the CLI flags shape.

This preserves compatibility with existing configs. Older configs without `disabledWebhooks` continue to treat every URL in `webhooks` as active.

## Data Model

Add to `CliFlags` on both frontend and backend config types:

```ts
disabledWebhooks?: string[];
```

Semantics:

- `webhooks` contains all configured webhook URLs.
- `disabledWebhooks` contains URLs from `webhooks` that are temporarily disabled.
- URLs present in `disabledWebhooks` but no longer present in `webhooks` should be cleaned up by the UI when a webhook row is removed.

## Frontend Behavior

In `client/src/components/CliFlags/WebhooksSection.tsx`:

- Each existing webhook row gets an on/off switch.
- Enabled rows show as active and remain eligible for runtime CLI arguments.
- Disabled rows stay visible, use muted styling, and show a small `Disabled` state label.
- Toggling off appends the URL to `disabledWebhooks` if it is not already present.
- Toggling on removes the URL from `disabledWebhooks`.
- Removing a webhook removes it from both `webhooks` and `disabledWebhooks`.
- Adding a new webhook adds it as enabled by default.

The legacy `client/src/components/CliFlags.tsx` duplicate component, if still compiled, should receive matching behavior or be left untouched only if confirmed unused.

## Backend Behavior

In `src/modules/instances/utils/config-parser.ts`:

- When converting flags to process arguments, compute active webhooks as:
  - every URL in `flags.webhooks`
  - excluding any URL included in `flags.disabledWebhooks`
- Emit `--webhook=<url>` only for active webhook URLs.
- If `disabledWebhooks` is missing or empty, behavior remains unchanged.

## Instance Detail Display

In `client/src/components/instance-detail/OverviewSection.tsx`, webhook display may show disabled state for clarity:

- Active webhook URLs display normally.
- Disabled webhook URLs display muted with a `Disabled` label.

This is optional for the first implementation but preferred because it makes saved disabled state visible outside the edit dialog.

## Testing

Update or add tests for `ConfigParser.processArgs`:

1. Existing `webhooks` without `disabledWebhooks` still emits all webhook arguments.
2. `disabledWebhooks` excludes matching URLs from emitted `--webhook` args.
3. Disabled-only edge case emits no webhook arguments when all configured URLs are disabled.

## Non-Goals

- Do not delete webhook URLs when disabling them.
- Do not migrate existing saved configs in the database.
- Do not change the public GOWA CLI flag names.
- Do not validate webhook URL reachability.
