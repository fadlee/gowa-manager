# Proxy Module – AGENTS (`src/modules/proxy`)

## 1. Identity

- **Responsibility**:
  - HTTP and WebSocket proxying from GOWA Manager to individual GOWA instances.
  - Health checks and proxy status for instances.
- **Entrypoints**:
  - Elysia module in `index.ts` (prefix `/${ProxyModel.prefix}`; effectively `/app/…`).
  - HTTP proxy logic in `service.ts` (`ProxyService`).
  - WebSocket proxy logic in `service.websocket.ts` (`WebSocketProxyService`).

---

## 2. How This Module Works

- **Routing** (`index.ts`):
  - `.all('/:instanceKey/*')` – forwards all HTTP methods and paths under an instance key.
  - `.all('/:instanceKey')` – fallback route for instance root.
  - `.get('/:instanceKey/status')` – proxy status (port, path, running state).
  - `.get('/:instanceKey/health')` – health check that pings the instance.
  - `.ws('/:instanceKey/ws')` – WebSocket proxy endpoint.

- **Availability checks**:
  - `ProxyService.isInstanceAvailable(instanceKey)` verifies DB record, `status === 'running'`, and non-null port.
  - If not available, routes return `404` or `503` with `{ error, success: false }`.

---

## 3. Patterns & Conventions

### 3.1 HTTP proxying (`service.ts`)

- **Forwarding**:
  - Look up instance via `queries.getInstanceByKey`.
  - Build target URL: `http://localhost:${instance.port}${path}`.
  - Copy headers, adding `X-Forwarded-*` and removing `host`.
  - Detect binary content types and forward as `arrayBuffer`.

- **DO**:
  - Let `ProxyService.forwardRequest(...)` handle HTTP proxying.
  - If you need URL rewriting in JSON, consider `modifyJsonUrls` (currently unused but ready).

- **DON'T**:
  - ❌ Re-implement proxying directly in route handlers.
  - ❌ Bypass `ProxyService.isInstanceAvailable`/`getProxyStatus` when building new endpoints.

### 3.2 WebSocket proxying (`service.websocket.ts` + `index.ts`)

- `WebSocketProxyService.createWebSocketConnection`:
  - Looks up instance by key, ensures running + port.
  - Creates a single `ws://localhost:{port}{path}` connection per instanceKey and caches it.
  - Forwards selected headers (auth, cookies, origin, etc.).
- `.ws('/:instanceKey/ws')` in `index.ts`:
  - Builds a WS path under the same proxy prefix: `/${ProxyModel.prefix}/${instanceKey}/ws?…`.
  - Forwards messages between client and target WS, and tears down connections on close/error.

---

## 4. Touch Points / Key Files

- `index.ts` – Elysia routes and WS integration.
- `service.ts` – HTTP proxy core logic and instance availability.
- `service.websocket.ts` – WebSocket proxy core logic and connection cache.
- `model.ts` – `ProxyModel` (prefix, status schemas, health response).

---

## 5. JIT Index Hints

```bash
# Find all proxy routes
rg -n "proxyModule" src/modules/proxy

# Find HTTP forward logic
rg -n "forwardRequest\(" src/modules/proxy

# Find WebSocket proxy handling
rg -n "WebSocketProxyService" src/modules/proxy

# Find proxy status structures
rg -n "proxyStatus" src/modules/proxy
```

---

## 6. Common Gotchas

- **Instance status**: Proxies rely on DB `status === 'running'` and `port` set; ensure instance lifecycle logic stays in sync.
- **Binary responses**: `ProxyService.forwardRequest` treats binary content specially – if you add content-type conditions, update `isBinaryContent` carefully.
- **Headers**: Host header is stripped and `X-Forwarded-*` headers are added; if you need more headers, add them at the `forwardHeaders` construction site.
- **WS subprotocols & headers**: Only a safe subset is forwarded; be explicit when adding more to avoid leaking sensitive data.

---

## 7. Pre-PR Checklist (Proxy)

- From a running system, verify:
  - Opening an instance via UI navigates correctly to proxied URL (`/app/{key}/…`).
  - Health/status endpoints behave as expected when instances are stopped vs running.
  - Any new routes still respect the `ProxyModel.prefix` and instance availability rules.
