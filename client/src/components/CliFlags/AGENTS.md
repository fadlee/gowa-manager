# CLI Flags UI – AGENTS (`client/src/components/CliFlags`)

## 1. Identity

- **Responsibility**: UI for configuring instance CLI flags before start/restart.
  - Basic auth users.
  - Webhook URLs.
  - Advanced options (validation, OS name, auto-mark-read, auto-reply, webhook secret).
- **Entrypoints**:
  - `index.tsx` – `CliFlagsComponent` that composes all sections.
  - `BasicAuthSection.tsx`, `WebhooksSection.tsx`, `AdvancedOptionsSection.tsx`.

---

## 2. How It Works

- `CliFlagsComponent`:
  - Receives `flags: CliFlags` and `onChange(flags)`.
  - Internally defines `updateFlag(key, value)` which copies current flags and overrides a single key.
  - Renders sections in order:
    - `BasicAuthSection` → `WebhooksSection` → `AdvancedOptionsSection`.

- Each section is responsible for one part of the `CliFlags` type from `client/src/types/index.ts`.

---

## 3. Patterns & Conventions

### 3.1 List editing (auth/webhooks)

- **BasicAuthSection**:
  - Uses local `newAuth` state and `flags.basicAuth` array.
  - Adds entry on:
    - Button click.
    - Password blur when username/password are filled.
    - Enter press on password field.
  - Includes small UX helpers:
    - Show/hide password per-row.
    - Copy password to clipboard with a temporary "Copied" state.
    - Remove row via icon button.

- **WebhooksSection**:
  - Uses `newWebhook` input and `flags.webhooks` array.
  - Adds entry on:
    - Button click.
    - Blur when input has content.
    - Enter key.
  - Each row has a remove icon button.

### 3.2 Toggle & simple inputs (advanced)

- **AdvancedOptionsSection**:
  - Collapsible panel using `showAdvanced` state and a `Button` with Chevron icons.
  - Uses `Switch` + `Input` from `ui/` for flags:
    - `accountValidation`, `os`, `autoMarkRead`, `autoReply`, `webhookSecret`.
  - Some options (like `debug`, `basePath`) are commented out but show intended future fields.

---

## 4. Touch Points / Key Files

- `index.tsx` – parent component used inside dialogs/forms.
- `BasicAuthSection.tsx` – reference for password list UX.
- `WebhooksSection.tsx` – reference for editable list of strings.
- `AdvancedOptionsSection.tsx` – reference for collapsible "advanced" sections.

These are typically used from:
- `CreateInstanceDialog.tsx` / `EditInstanceDialog.tsx` via `CliFlagsComponent`.

---

## 5. JIT Index Hints

From `client/`:

```bash
# Find all CLI flags-related components
rg -n "CliFlagsComponent" client/src
rg -n "BasicAuthSection" client/src
rg -n "WebhooksSection" client/src
rg -n "AdvancedOptionsSection" client/src

# Find flags type
rg -n "interface CliFlags" client/src/types
```

---

## 6. Common Gotchas

- **Flags shape**: When adding new flags, update:
  - `CliFlags` type in `client/src/types/index.ts`.
  - `CliFlagsComponent.updateFlag` usage or section components accordingly.
  - Backend `ConfigParser` and any corresponding CLI mappings.
- **Auto-add on blur**: Both Basic Auth and Webhooks sections auto-add entries on blur; keep this UX consistent if you extend behaviour.
- **Clipboard API**: `BasicAuthSection` uses `navigator.clipboard`; ensure it’s only called in the browser (it already is).

---

## 7. Pre-PR Checklist (CLI Flags UI)

- Create/edit instance via dialogs and verify:
  - Auth pairs are added/removed correctly.
  - Webhooks are added/removed and persisted.
  - Advanced flags appear with current values and can be toggled/edited.
- Confirm that edited flags result in the expected `config` payload going to the backend.
