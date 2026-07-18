# Magic Admin Link Design

## Goal

Let users open the proxied GOWA admin page without manually entering the instance Basic Auth username and password.

## Chosen Approach

Use a short-lived magic admin link. The frontend opens `/app/{key}/?autologin=<token>`. The proxy validates the signed token, sets a short-lived HTTP-only cookie for that instance, redirects to `/app/{key}/`, and injects the instance's first Basic Auth credential into proxied upstream requests while the cookie is valid.

This avoids putting `username:password` in the browser URL.

## Data and Auth Source

- Use the first configured Basic Auth pair from `instance.config.flags.basicAuth[0]`.
- If no Basic Auth is configured, opening admin keeps the current behavior and opens `/app/{key}/`.
- The magic token must be scoped to one instance key.
- The magic token must expire quickly, e.g. 60 seconds.
- The browser cookie should also expire quickly, e.g. 15 minutes.

## Backend API

Add an authenticated manager API endpoint:

```http
POST /api/instances/:id/admin-link
```

Response:

```ts
{
  url: string;
  expiresAt: string;
}
```

Behavior:

- Requires existing manager authentication like other `/api/instances` endpoints.
- Looks up the instance by ID.
- If instance does not exist, returns 404.
- If instance has no Basic Auth pair, returns the normal proxy URL without `autologin`.
- If Basic Auth exists, returns `/app/{instance.key}/?autologin=<signed-token>`.

## Token Format

Use server-side signing with a secret derived from existing manager credentials or a generated runtime secret.

Token payload:

```ts
{
  instanceKey: string;
  exp: number;
  nonce: string;
}
```

Token validation checks:

- Signature is valid.
- `instanceKey` matches the requested `/app/{key}` route.
- `exp` is in the future.

Nonce one-time storage is preferred but not required for the first implementation because the token TTL is short and the endpoint requires manager auth to mint.

## Proxy Behavior

When a request to `/app/{key}/...` includes `autologin` query param:

1. Validate the token for `{key}`.
2. If invalid or expired, continue without setting auth cookie or return a user-friendly unauthorized response.
3. If valid, set HTTP-only cookie scoped to `/app/{key}`.
4. Redirect to the same proxied path without the `autologin` query param.

For subsequent proxied HTTP requests:

- If the request lacks an `Authorization` header and has a valid magic auth cookie for the instance, inject `Authorization: Basic <token>` using `flags.basicAuth[0]`.
- Do not override an explicit incoming `Authorization` header.
- If no Basic Auth is configured, do not inject anything.

WebSocket behavior can remain unchanged for the first implementation because the existing websocket proxy already supports instance auth injection.

## Frontend Behavior

Replace direct `window.open(apiClient.getProxyUrl(instance.key), '_blank')` admin actions with:

1. If instance is running, call `apiClient.createAdminLink(instance.id)`.
2. Open the returned URL in a new tab.
3. If the API call fails, show a visible error/toast or fall back to the normal proxy URL with a clear message.

Touch points:

- `client/src/lib/api.ts`: add `createAdminLink(id)`.
- `client/src/components/InstanceCard.tsx`: use magic admin link for the Open button.
- `client/src/pages/InstanceDetailPage.tsx`: use magic admin link for Open Admin.
- `client/src/pages/DashboardPage.tsx`: use magic admin link for Admin button in dashboard card if it opens the admin page.

## Security Notes

- Do not include raw username or password in URLs.
- Set auth cookies as `HttpOnly`, `SameSite=Lax`, and scoped to `/app/{key}`.
- Use `Secure` when the request is HTTPS.
- Keep token TTL short.
- Keep cookie TTL modest, e.g. 15 minutes, to avoid repeated prompts during normal admin use.

## Testing

Backend tests:

- Admin link route returns normal proxy URL when no Basic Auth exists.
- Admin link route returns URL with `autologin` when Basic Auth exists.
- Proxy autologin validates token, sets cookie, and redirects without `autologin`.
- Proxy injects Basic Auth from valid cookie.
- Proxy does not override explicit `Authorization` header.
- Expired/invalid token does not create an auth cookie.

Frontend verification:

- Typecheck passes.
- Admin buttons use `createAdminLink` instead of direct proxy URL.
- Existing direct copy-base-url behavior remains unchanged.

## Non-Goals

- Do not expose credentials in URL format `user:pass@host`.
- Do not implement user-selectable Basic Auth pairs in this first version.
- Do not add persistent login sessions to the upstream GOWA app.
