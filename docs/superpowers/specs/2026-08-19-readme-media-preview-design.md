# README Media Preview Design

## Goal

Add a visual preview for the Dashboard + instance flow to the repository README using media captured from the local application with safe demo data.

## Scope

- Capture one representative dashboard screenshot showing the instance list and status/resource information.
- Capture a short 10–15 second animated preview of the same dashboard/instance flow.
- Store both assets under `docs/assets/` with stable, descriptive filenames.
- Add a concise `Preview` section near the top of `README.md`, after the project introduction and before the feature list.
- Use repository-relative Markdown paths so the README works from the repository and on GitHub.
- Include accessible alt text and a short caption; avoid real credentials, tokens, personal data, or production URLs.

## Capture

Run the local Go-backed application with isolated temporary/demo data. Use the existing UI and browser tooling to capture the dashboard and instance state. Do not alter production data or commit generated runtime data.

The screenshot should prioritize the main dashboard and an instance row/card. The animated preview should demonstrate a small amount of navigation or status interaction without requiring viewers to understand setup details.

## Files

- `docs/assets/dashboard-preview.png`
- `docs/assets/dashboard-preview.gif`

The GIF should be optimized enough for a repository README and remain within the agreed 10–15 second range. No new runtime dependency or application feature is needed.

## README placement

Add:

```md
## Preview

Dashboard and instance monitoring:

![GOWA Manager dashboard with an instance](docs/assets/dashboard-preview.png)

<details>
<summary>Watch a short dashboard preview</summary>

![Animated GOWA Manager dashboard preview](docs/assets/dashboard-preview.gif)

</details>
```

Keep the media section short and do not duplicate the feature documentation already present later in the README.

## Validation

- Confirm both files exist, render as valid media, and are not accidentally tracked runtime data.
- Confirm README relative links point to the committed files.
- Confirm the screenshot/GIF contain no secrets or personal data.
- Run the repository's relevant build/check command if capture requires a local app build; no application code changes are expected.
