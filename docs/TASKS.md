# Active Tasks

## Sprint: Developer Instance UX
Updated: 2026-06-11

### ✅ Done
- [x] Backend lifecycle endpoints exist for instances
  - Available actions: `POST /start`, `POST /stop`, `POST /restart`, `POST /kill`
  - Source: `src/modules/instances/index.ts`
- [x] Overview shows basic instance URL with copy action
- [x] Settings shows GOWA version and installed status
- [x] Replace lifecycle toggle with explicit action buttons
  - Added primary actions: `Start`, `Restart`, `Stop`
  - Added loading/disabled state while lifecycle request is pending
  - Removed ambiguous ON/OFF switch from instance detail header
  - Source: `client/src/pages/InstanceDetailPage.tsx`
- [x] Add instance status badge in header
  - Shows `Running`, `Stopped`, `Error`, plus pending labels such as `Starting`, `Stopping`, `Restarting`
  - Includes port in the header on wider screens
  - Source: `client/src/pages/InstanceDetailPage.tsx`

- [x] Add restart-required banner after settings save
  - Message: `Changes saved. Restart instance to apply.`
  - Includes inline `Restart now` action
  - Triggers after config or version changes
  - Source: `client/src/components/instance-detail/SettingsSection.tsx`

- [x] Build Overview connection/integration card
  - Shows base URL with copy button
  - Shows basic auth credentials masked by default
  - Adds reveal/copy actions for username and password
  - Shows active webhook URL
  - Shows instance running/stopped connection status
  - Source: `client/src/components/instance-detail/OverviewSection.tsx`

### 🔄 In Progress
- [ ] No active task selected

- [x] Fix API request snippets
  - Added working `curl` example for quick testing
  - Added JavaScript `fetch` example with correct URL construction
  - Includes `Authorization: Basic ...` only when basic auth is configured
  - Avoids confusing double paths such as `/app/{id}/app/devices`
  - Source: `client/src/components/instance-detail/OverviewSection.tsx`

- [x] Add API documentation tab or section
  - Added dedicated instance `API` tab
  - Links to GOWA Swagger/OpenAPI docs via instance `/docs`
  - Links to upstream `aldinokemal/go-whatsapp-web-multidevice` documentation
  - Documents common endpoints: devices, send message, login/QR, webhook
  - Includes copyable quickstart examples with curl/JavaScript tabs
  - Source: `client/src/components/instance-detail/ApiSection.tsx`

- [x] Add Test Connection action
  - Calls safe `GET /devices` endpoint from the manager backend
  - Uses stored instance basic auth server-side to avoid browser auth prompts
  - Shows success/error response inline
  - Validates base URL and basic auth credentials from the UI
  - Source: `src/modules/instances/index.ts`
  - Source: `client/src/components/instance-detail/ApiSection.tsx`

- [x] Clean up Admin Panel navigation
  - Removed duplicate external `Admin` entry from sidebar
  - Moved `Open Admin` action into the instance header near lifecycle buttons
  - Removed duplicate Overview `Open Admin Panel` action
  - Source: `client/src/pages/InstanceDetailPage.tsx`
  - Source: `client/src/components/instance-detail/OverviewSection.tsx`

- [x] Improve header information density
  - Added status badge, port, uptime, lifecycle actions, and admin panel link
  - Kept Overview focused on integration details
  - Source: `client/src/pages/InstanceDetailPage.tsx`

### 📋 Up Next
- [ ] Improve webhook settings UX
  - Clarify whether multiple webhook URLs are supported
  - Add helper text with expected format
  - Add `Send test event` action if backend support exists or can be added

### 🚫 Blocked
- [ ] Add Logs tab for instance debugging
  - Requires backend capture of `Bun.spawn` stdout/stderr
  - Store recent logs in a per-instance ring buffer or log file
  - Add endpoint such as `GET /instances/:id/logs?tail=200`
  - UI should support tail view and manual refresh
- [ ] Inline QR pairing in Overview
  - Requires reliable API/source for QR and WhatsApp connection state
  - Show QR only when instance needs pairing
  - Keep Admin Panel as fallback for advanced pairing/debugging
- [ ] Send webhook test event
  - Requires backend endpoint or confirmed GOWA support for synthetic webhook events
  - Should show request payload and delivery result
