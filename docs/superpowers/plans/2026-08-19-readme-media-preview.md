# README Media Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture safe local Dashboard + instance media and embed a screenshot plus 10–15 second GIF preview in `README.md`.

**Architecture:** Use the existing Go production build and Playwright browser tooling to run and inspect the application with an isolated temporary data directory. Store only two generated media assets in `docs/assets/`, then add a small repository-relative Preview section to the README; no application code or runtime behavior changes are required.

**Tech Stack:** Go backend, Bun scripts, Vite embedded SPA build, Playwright, PNG, GIF, Markdown.

## Global Constraints

- Use isolated temporary/demo data; do not modify production data under `data/`.
- Do not capture or commit real credentials, tokens, personal data, or production URLs.
- Store media at `docs/assets/dashboard-preview.png` and `docs/assets/dashboard-preview.gif`.
- Keep the GIF between 10 and 15 seconds and optimized for a repository README.
- Use repository-relative README links and accessible alt text.
- Do not add runtime dependencies or application features.

---

### Task 1: Prepare and build the local application

**Files:**
- Read: `package.json`, `playwright.config.ts`, `cmd/gowa-manager-go/main.go`
- Create at runtime only: temporary directory under `/tmp` for demo data and server logs

**Interfaces:**
- Consumes: existing `bun run build:production` and Go server CLI.
- Produces: a locally reachable Go-backed application at a temporary port, with its process ID and isolated data path recorded for capture.

- [ ] **Step 1: Build the production application**

Run from the repository root:

```bash
bun run build:production
```

Expected: client build and Go binary build pass. Ignore the known non-blocking Vite bundle-size warning if it is the only warning.

- [ ] **Step 2: Start an isolated Go server**

Create a temporary data directory and launch the built binary on an unused local port, for example:

```bash
tmpdir="$(mktemp -d)"
port=39090
./dist-go/gowa-manager-go --data-dir "$tmpdir" --port "$port" >"$tmpdir/server.log" 2>&1 &
echo $! >"$tmpdir/server.pid"
```

Use the actual binary path and flags from the build output/CLI help if they differ. Confirm the server responds before browser capture:

```bash
curl --fail --silent "http://127.0.0.1:${port}/api/health"
```

Do not use the repository `data/` directory.

- [ ] **Step 3: Confirm the login surface**

Open the local URL with the existing Playwright/CloakBrowser tooling and confirm the login form and default/demo credentials behavior. Use only local demo credentials; do not place credentials in README assets or logs that will be committed.

- [ ] **Step 4: Stop and clean up on failure**

If build or startup fails, stop the server using the recorded PID and inspect the temporary `server.log`. Do not modify application code as part of media capture.

---

### Task 2: Create dashboard demo state and capture PNG

**Files:**
- Create: `docs/assets/dashboard-preview.png`
- Read: `client/src/components/InstanceList.tsx`, `client/src/components/InstanceCard.tsx`, `test/e2e/go-backend.spec.ts`

**Interfaces:**
- Consumes: running local Go application from Task 1.
- Produces: `docs/assets/dashboard-preview.png` showing the dashboard and one safe demo instance.

- [ ] **Step 1: Create one safe demo instance through the UI**

Use the UI flow rather than direct database edits. Set a neutral name such as `Demo Instance`; use local-only or mock-safe values. Avoid entering real WhatsApp credentials, webhook URLs, tokens, or external URLs. If the UI requires a GOWA version/binary that cannot be safely started offline, leave the instance stopped while ensuring the dashboard shows its configuration/status card.

- [ ] **Step 2: Arrange the dashboard for capture**

Use a stable desktop viewport wide enough to show the header, instance card, status badge, and resource/management controls without horizontal clipping. Wait for the UI to settle and ensure no error overlay, browser devtools, local filesystem path, credential, token, or personal information is visible.

- [ ] **Step 3: Capture the PNG**

Save the dashboard screenshot exactly as:

```text
docs/assets/dashboard-preview.png
```

Use Playwright's screenshot capability or the browser capture tool. Prefer the application viewport rather than a full desktop screenshot so browser chrome and unrelated local details are excluded.

- [ ] **Step 4: Validate the image**

Check the file type, dimensions, and size:

```bash
file docs/assets/dashboard-preview.png
identify docs/assets/dashboard-preview.png 2>/dev/null || true
```

If ImageMagick is unavailable, use the browser/image tool or Python standard-library-compatible inspection available in the environment. The image must be a valid PNG and must not contain sensitive data.

---

### Task 3: Record and optimize the GIF preview

**Files:**
- Create: `docs/assets/dashboard-preview.gif`

**Interfaces:**
- Consumes: the same local application state and dashboard layout as Task 2.
- Produces: `docs/assets/dashboard-preview.gif`, a 10–15 second animated dashboard preview suitable for GitHub README rendering.

- [ ] **Step 1: Record a short interaction**

Record 10–15 seconds showing the dashboard and one small, understandable interaction such as opening the instance details/actions and returning to the dashboard. Keep the demo state static enough to avoid flaky loading or transient errors. Do not show authentication secrets or external integrations.

If the installed browser tooling cannot directly record GIF, record a local MP4/WebM or a sequence of PNG frames, then convert with an already-installed tool such as `ffmpeg` or ImageMagick. Do not add a dependency solely for conversion.

- [ ] **Step 2: Convert to GIF**

Write the final artifact to:

```text
docs/assets/dashboard-preview.gif
```

Keep the capture at a README-friendly viewport and frame rate. Remove browser chrome and trim dead time; retain the agreed 10–15 second duration.

- [ ] **Step 3: Validate and optimize**

Run:

```bash
file docs/assets/dashboard-preview.gif
identify docs/assets/dashboard-preview.gif 2>/dev/null || true
```

Confirm it is animated, lasts 10–15 seconds, renders without corruption, and contains no sensitive data. If the file is unnecessarily large, reduce dimensions/frame rate with installed tooling while preserving readability.

---

### Task 4: Add the README Preview section

**Files:**
- Modify: `README.md` after the introductory paragraph and before `## Features`
- Consume: `docs/assets/dashboard-preview.png`, `docs/assets/dashboard-preview.gif`

**Interfaces:**
- Consumes: validated committed media paths from Tasks 2–3.
- Produces: a README section that renders both assets using relative paths and accessible descriptions.

- [ ] **Step 1: Insert the minimal Markdown section**

Add:

```md
## Preview

Dashboard and instance monitoring:

![GOWA Manager dashboard with a demo instance](docs/assets/dashboard-preview.png)

<details>
<summary>Watch a short dashboard preview</summary>

![Animated GOWA Manager dashboard preview](docs/assets/dashboard-preview.gif)

</details>
```

Do not duplicate feature explanations already present in the README.

- [ ] **Step 2: Verify links and diff**

Run:

```bash
grep -n "dashboard-preview" README.md
 git diff --check
git status --short
```

Expected: both relative paths exist, no whitespace errors, and only the README plus two media files are pending after the previously committed design/spec changes.

- [ ] **Step 3: Inspect rendered README locally**

Use a local Markdown preview if available, or inspect the raw links and open both assets directly. Confirm the screenshot is visible before the collapsible GIF preview and that alt text is meaningful.

- [ ] **Step 4: Commit the media update**

```bash
git add README.md docs/assets/dashboard-preview.png docs/assets/dashboard-preview.gif
git commit -m "docs: add README dashboard preview"
```

---

### Task 5: Final verification and cleanup

**Files:**
- Verify: `README.md`, `docs/assets/dashboard-preview.png`, `docs/assets/dashboard-preview.gif`
- Runtime-only: temporary server/data directory from Task 1

**Interfaces:**
- Consumes: final README and media artifacts.
- Produces: clean repository state and a concise verification report.

- [ ] **Step 1: Remove runtime artifacts**

Stop the local Go server and remove only the temporary data directory created for capture. Ensure no generated files under `data/`, `dist/`, or other ignored runtime paths were added to git.

- [ ] **Step 2: Verify repository state**

Run:

```bash
git status -sb
git diff HEAD^ --stat
git diff --check
```

Expected: the media commit is clean, with only the intended README and two media assets.

- [ ] **Step 3: Report capture limitations honestly**

If an instance could not be started because no offline GOWA binary was available, describe the screenshot as a dashboard demo instance rather than claiming a live WhatsApp connection. Do not add fake runtime claims to the README.
