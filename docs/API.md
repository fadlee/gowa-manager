# API Reference

All endpoints are under `http://<manager>/api`. Requests and responses use JSON. Except for the public endpoints listed below, every request must be authenticated with a session cookie, an API key, or HTTP Basic. See [AUTHENTICATION.md](AUTHENTICATION.md).

```bash
curl -H "Authorization: Bearer gowa_xxx" http://localhost:3000/api/instances
```

## Public

| Method | Path | Description |
|---|---|---|
| GET | `/api/health` | Liveness check. |
| GET | `/api/ready` | Readiness check. |
| GET | `/api/auth/status` | `{ needsSetup, user }` for the current caller. |
| POST | `/api/auth/setup` | Register the first superadmin: `{ "setupToken", "username", "password" }`. |
| POST | `/api/auth/login` | Start a session: `{ "username", "password" }`. |
| POST | `/api/auth/logout` | End the current session. |

## Instances

| Method | Path | Description |
|---|---|---|
| GET | `/api/instances` | List instances. |
| POST | `/api/instances` | Create an instance: `{ "name", "gowa_version", "config" }`. All fields are optional. |
| GET | `/api/instances/{id}` | Get one instance. |
| PUT | `/api/instances/{id}` | Update `name`, `gowa_version`, or `config`. Restart the instance to apply. |
| DELETE | `/api/instances/{id}` | Stop and delete an instance and its data. |
| POST | `/api/instances/{id}/start` | Start. |
| POST | `/api/instances/{id}/stop` | Stop. |
| POST | `/api/instances/{id}/restart` | Restart. |
| POST | `/api/instances/{id}/kill` | Force-stop. |
| GET | `/api/instances/{id}/status` | Status, PID, port, uptime, resources, devices. |
| GET | `/api/instances/{id}/devices` | Connected WhatsApp devices. |
| GET | `/api/instances/{id}/logs?tail=200` | Recent stdout/stderr lines. |
| GET | `/api/instances/{id}/files?path=<dir>` | List files in the instance folder (read-only). |
| GET | `/api/instances/{id}/files/preview?path=<file>` | Preview a file. |
| GET | `/api/instances/{id}/files/download?path=<file>` | Download a file. |
| POST | `/api/instances/{id}/reset-data` | Stop the instance and wipe its data folder (WhatsApp sessions included). |
| POST | `/api/instances/{id}/admin-link` | Create a signed one-click link to the instance UI. |
| POST | `/api/instances/{id}/test-connection` | Check that the instance answers. |

`config` is a JSON string with the instance's GOWA flags and environment, for example `basicAuth`, webhooks, and OS name.

```bash
curl -X POST http://localhost:3000/api/instances \
  -H "Authorization: Bearer gowa_xxx" -H "Content-Type: application/json" \
  -d '{"name": "sales", "gowa_version": "latest"}'
```

## GOWA versions

| Method | Path | Description |
|---|---|---|
| GET | `/api/system/versions/installed` | Installed versions. |
| GET | `/api/system/versions/available?limit=10` | Versions available on GitHub. |
| POST | `/api/system/versions/install` | Install a version: `{ "version": "v7.5.1" }`. |
| DELETE | `/api/system/versions/{version}` | Remove an installed version. |
| GET | `/api/system/versions/{version}/available` | Check whether a version is installed. |
| GET | `/api/system/versions/usage` | Disk usage per version. |
| POST | `/api/system/versions/cleanup` | Keep only the newest versions: `{ "keepCount": 3 }`. |

## System

| Method | Path | Description |
|---|---|---|
| GET | `/api/system/status` | Manager status and instance counts. |
| GET | `/api/system/config` | Manager configuration. |
| GET | `/api/system/ports/next` | Next free port for an instance. |
| GET | `/api/system/ports/{port}/available` | Check whether a port is free. |
| GET | `/api/system/auto-update/status` | GOWA auto-update status. |
| POST | `/api/system/auto-update/check` | Check for GOWA updates now. |
| GET | `/api/system/auto-update/instances` | Auto-update state per instance. |

## Account (session or Basic only, not API keys)

| Method | Path | Description |
|---|---|---|
| GET | `/api/account` | Current user. |
| PUT | `/api/account/password` | Change password: `{ "currentPassword", "newPassword" }`. |
| GET | `/api/account/api-keys` | List your API keys. |
| POST | `/api/account/api-keys` | Create a key: `{ "name" }`. The response has the full key once. |
| DELETE | `/api/account/api-keys/{id}` | Revoke a key. |

## Users (superadmin only, not API keys)

| Method | Path | Description |
|---|---|---|
| GET | `/api/users` | List users. |
| POST | `/api/users` | Create a user: `{ "username", "password", "role" }`. `role` is `superadmin` or `user`. |
| PATCH | `/api/users/{id}` | Change `role` or `isActive`. |
| PUT | `/api/users/{id}/password` | Reset a user's password: `{ "password" }`. |
| DELETE | `/api/users/{id}` | Delete a user. |

## Instance proxy

| Path | Description |
|---|---|
| `/app/{key}/...` | The instance's GOWA web UI and API. Protected by the instance's own Basic Auth, not by the manager login. |
| `/app/{key}/ws` | GOWA WebSocket. |
| `GET /app/{key}/status` | Proxy status for the instance. |
| `GET /app/{key}/health` | Pings the instance. |

## Metrics

`/metrics` serves Prometheus metrics when `GOWA_METRICS_ENABLED=1`. It only answers requests from loopback addresses.
