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

### 🔄 In Progress
- [ ] No active task selected

### 📋 Up Next
- [ ] Build Overview connection/integration card
  - Show base URL with copy button
  - Show basic auth credentials masked by default
  - Add reveal/copy actions for username and password
  - Show active webhook URL
  - Show WhatsApp connection status if available
- [ ] Fix API request snippets
  - Add working `curl` example for quick testing
  - Add JavaScript `fetch` example with correct URL construction
  - Add Python example if useful
  - Include `Authorization: Basic ...` only when basic auth is configured
  - Avoid confusing double paths such as `/app/{id}/app/devices`
- [ ] Add API documentation tab or section
  - Link to GOWA Swagger/OpenAPI docs, e.g. instance `/docs` when available
  - Link to upstream `aldinokemal/go-whatsapp-web-multidevice` documentation
  - Document common endpoints: devices, send message, login/QR, webhook
  - Include copyable quickstart examples
- [ ] Add Test Connection action
  - Call a safe endpoint such as health/devices
  - Show success/error response inline
  - Validate base URL and basic auth credentials from the UI
- [ ] Clean up Admin Panel navigation
  - Avoid duplicate unclear entries between sidebar and header
  - If kept in sidebar, separate with divider and external-link icon
  - Prefer a prominent `Open Admin Panel` button in instance header
- [ ] Improve header information density
  - Add status badge, port, uptime, lifecycle actions, and admin panel link
  - Keep Overview focused on integration details
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
