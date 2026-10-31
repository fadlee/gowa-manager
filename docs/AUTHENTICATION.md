# Authentication, Users, and API Keys

## First run

When the database has no users, the Web UI shows a registration page. The first account registered becomes the **superadmin**. Registration needs a one-time **setup token** that the manager prints at startup, so nobody else can claim a freshly exposed install:

```
  Setup:    No users yet. Open the Web UI to register the
            first superadmin with this setup token:
            9U-TIbHB9WrM8o__2c1F4pMq
```

A new token is generated on every start while no users exist. With Docker, read it with `docker logs <container>`.

You can also create the first superadmin without the Web UI. These values are only used while no users exist and are ignored afterwards:

```bash
ADMIN_USERNAME=admin ADMIN_PASSWORD=change-me-please gowa-manager
# or
gowa-manager --admin-username admin --admin-password change-me-please
```

There are no default credentials. When you upgrade an install that set `ADMIN_USERNAME` / `ADMIN_PASSWORD`, those credentials become the superadmin account automatically.

## Roles

- **superadmin:** everything a user can do, plus managing users on the **Users** page.
- **user:** manages all instances and GOWA versions.

Registration closes after the first account. After that, a superadmin creates users, changes roles, resets passwords, and disables or deletes users. The last active superadmin cannot be demoted, disabled, or deleted.

Every user can change their own password on **My account**. Changing it signs out the user's other sessions.

## Ways to authenticate

`/api/*` accepts any of these:

| Method | How | Typical use |
|---|---|---|
| Session cookie | Log in through the Web UI | Browser |
| API key | `Authorization: Bearer gowa_…` or `X-API-Key: gowa_…` | Scripts, integrations |
| HTTP Basic | `curl -u <username>:<password>` | Older scripts |

```bash
curl -H "Authorization: Bearer gowa_xxx" http://localhost:3000/api/instances
curl -H "X-API-Key: gowa_xxx" http://localhost:3000/api/instances
```

## API keys

Each user can generate API keys on **My account**.

- The full key is shown **once**. Only a hash is stored.
- Each key has a name, a visible prefix, and a "last used" time.
- Keys do not expire. Revoke a key when you no longer need it.
- A key has its owner's role, but it **cannot** manage users or API keys, and it cannot change passwords.
- A key stops working when its owner is disabled or deleted.

## Sessions and rate limits

- The Web UI uses an `HttpOnly`, `SameSite=Lax` session cookie. It is valid for 30 days and is extended while you use it.
- Failed logins are limited to 10 per 15 minutes for each client IP and username.

Behind a reverse proxy, all requests can appear to come from the proxy's IP. In that case, repeated failed logins for one username can lock that username for 15 minutes.

## Forgot password

```bash
gowa-manager reset-password <username> [--password <new>] [--data-dir <path>]

# Docker
docker exec -it -u app <container> ./gowa-manager reset-password <username>
```

Without `--password`, the command generates a random password and prints it. All of the user's sessions are signed out. API keys keep working.

## Instance admin links

The manager signs one-click admin links to instance UIs with a secret. Set it with `ADMIN_LINK_SECRET`. If it is not set, a random secret is generated once and stored in the database.

## Proxy paths are not protected by the manager login

Instances are served at `/app/<instance-key>/...`. These proxy paths do **not** need a manager login, so external clients and webhooks can call GOWA directly. Protection is the job of each instance's own GOWA Basic Auth (`basicAuth` in the instance settings). If the manager is reachable by untrusted clients, set it on every instance.

Browsers cannot send an `Authorization` header when they open a WebSocket. To keep the GOWA multi-device page working, the WebSocket proxy adds the instance's first Basic Auth pair when the request has no `Authorization` header. This means anyone who can reach an instance's WebSocket URL can use that connection without the GOWA credentials.
