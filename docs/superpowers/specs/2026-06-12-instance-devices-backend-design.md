# Instance Devices Backend Design

## Scope

Implement backend support for device awareness per GOWA instance. This is a backend-first slice: it adds an explicit devices endpoint and a compact devices summary on the existing instance status response. Frontend card/table changes are out of scope for this spec, but the API is shaped so the current dashboard status polling can consume it later without extra per-card queries.

## API Contract

### `GET /api/instances/:id/devices`

Returns normalized device information for one instance.

- If the instance does not exist, return `404` with the existing `Instance not found` response shape.
- If the instance is not running or has no port, return `200`:

```ts
{
  count: 0,
  connected: false,
  stale: false,
  devices: [],
  source: 'not-running'
}
```

- If the instance is running, fetch internally from `http://localhost:{port}/app/{key}/devices`.
- Return normalized data:

```ts
{
  count: number,
  connected: boolean,
  stale: boolean,
  devices: Array<Record<string, unknown>>,
  fetchedAt?: string,
  source: 'live' | 'cache' | 'not-running',
  error?: string
}
```

### `GET /api/instances/:id/status`

Keep all existing fields and add an optional summary:

```ts
devices?: {
  count: number
  connected: boolean
  stale: boolean
  fetchedAt?: string
  error?: string
}
```

The status response does not include the full device list so the existing 5-second polling payload stays small.

## Backend Design

Create a focused device helper, for example `src/modules/instances/utils/device-client.ts`, owned by the instances module. It should handle:

- Instance state checks (`not-running` when status is not `running` or port is missing).
- Basic auth extraction from `instance.config` using the same convention as `test-connection`: `config.flags.basicAuth[0]`.
- Internal fetch to the instance-local GOWA devices endpoint.
- A short timeout, defaulting to 3 seconds, so status polling does not hang on a slow GOWA instance.
- Defensive response parsing and normalization.
- In-memory per-instance caching.

`InstanceService.getInstanceStatus` should call the helper for a summary only. The new route should call the helper for the full normalized response. Stop, kill, and delete flows should clear the device cache for that instance.

## Cache Behavior

Use an in-memory `Map<number, CachedDevices>` keyed by instance id.

- Default TTL: 15 seconds.
- Optional env override: `INSTANCE_DEVICES_CACHE_TTL_MS`.
- Fresh cache can satisfy both `/devices` and `/status` requests with `source: 'cache'`.
- If live fetch fails and an older cache entry exists, return it with `stale: true`, `source: 'cache'`, and a short `error` message.
- If live fetch fails and no cache exists, return `count: 0`, `connected: false`, `stale: false`, `source: 'live'`, and `error`.
- Clear cache on stop, kill, and delete.

## Device Normalization

GOWA response shape may vary, so normalization should be tolerant:

- If the response JSON is an array, treat it as the device list.
- If it is an object with `devices` array, use `devices`.
- If it is an object with `data` array, use `data`.
- Otherwise return an empty list and `error: 'Unexpected devices response shape'`.

`count` is the normalized list length. `connected` is `count > 0`.

## Error Handling And Security

Errors should be visible but safe:

- Do not log credentials or full instance config.
- Return short operational errors such as timeout, non-2xx GOWA response, invalid JSON, or unexpected response shape.
- Do not expose stack traces in API responses.
- Do not make non-running instances look like hard failures; use `source: 'not-running'` and no `error`.

## Type And Schema Changes

Update both backend and frontend-shared types:

- `src/modules/instances/model.ts`: add devices response schemas and status summary schema.
- `src/types/index.ts`: extend `Instance.StatusResponse` and add device response/summary types.
- `client/src/types/index.ts`: mirror `InstanceStatus.devices` and add frontend device response types if the API client is updated in this slice.
- `client/src/lib/api.ts`: add `getInstanceDevices(id)` only if touched during backend-first implementation; no UI usage yet.

## Tests

Add backend tests for:

- `/api/instances/:id/devices` returns 404 for missing instance.
- Non-running instance returns `count: 0`, `connected: false`, and `source: 'not-running'`.
- Running instance with mocked GOWA array response returns the correct count and full list.
- Running instance with mocked `{ devices: [...] }` or `{ data: [...] }` response normalizes correctly.
- Second request within TTL uses cache.
- Fetch failure after a successful fetch returns stale cached data.
- `/status` includes a compact `devices` summary.
- Basic auth header is sent when `flags.basicAuth[0]` exists.

## Out Of Scope

- Dashboard card redesign.
- Summary bar and grid/table toggle.
- QR pairing link behavior.
- Batch endpoint for all instances. If listing performance becomes an issue with many instances, add `GET /api/instances/devices-summary` as a later iteration.
