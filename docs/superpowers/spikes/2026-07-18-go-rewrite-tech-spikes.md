# Go Rewrite Technology Spikes

> Date: 2026-07-18
> Status: Accepted for foundation implementation

## Summary

Task 2 of `docs/superpowers/plans/2026-07-18-go-rewrite-01-foundation.md` validated the early technology choices for the Go rewrite foundation.

- SQLite driver: `modernc.org/sqlite` pinned at `v1.38.2` when first used by production Go code.
- WebSocket library: `github.com/coder/websocket` pinned at `v1.8.14` when first used by production Go code.
- Existing Bun database journal mode: `delete`.
- First Go release must not switch existing databases to WAL.
- SQLite `RETURNING` statements used by the Bun baseline are supported by the selected driver.

The throwaway spike code lived under `/spike/` and is intentionally gitignored.

## Bun SQLite Baseline

The current Bun backend opens SQLite in `src/db.ts` with:

```ts
const db = new Database(join(absoluteDataDir, 'gowa.db'))
```

No explicit journal mode, busy timeout, or foreign key pragma is configured by the Bun backend.

Runtime pragmas from a Bun-created `gowa.db` on the development machine:

| PRAGMA | Value |
|---|---|
| `journal_mode` | `delete` |
| `busy_timeout` | `undefined` when read via the Bun spike query result; no explicit timeout configured by backend |
| `foreign_keys` | `0` |
| `encoding` | `UTF-8` |
| `user_version` | `0` |

The Go driver opened the same Bun-created database and reported:

| PRAGMA | Value |
|---|---|
| `journal_mode` | `delete` |
| `busy_timeout` | `5000` after opening with Go timeout configuration |
| `foreign_keys` | `0` |
| `encoding` | `UTF-8` |
| `user_version` | `0` |

## Required SQL Compatibility

The Bun backend relies on these statement shapes in `src/db.ts`:

```sql
INSERT INTO instances (key, name, port, config, gowa_version)
VALUES (?, ?, ?, ?, ?)
RETURNING *
```

```sql
UPDATE instances
SET key = ?, name = ?, port = ?, config = ?, gowa_version = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *
```

```sql
UPDATE instances
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *
```

```sql
UPDATE instances
SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *
```

```sql
UPDATE instances
SET error_message = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *
```

The spike verified `INSERT ... RETURNING` and `UPDATE ... RETURNING` against `modernc.org/sqlite v1.38.2`.

## SQLite Driver Decision

Use `modernc.org/sqlite v1.38.2` for the Go backend foundation unless a later plan finds a specific blocker not covered here.

The spike proved that `modernc.org/sqlite v1.38.2`:

- opens a database created by `bun:sqlite` without migration;
- supports `INSERT ... RETURNING` and `UPDATE ... RETURNING`;
- honors a `busy_timeout=5000` configuration for concurrent writers;
- keeps concurrent writers safe under the existing `delete` journal mode when writer concurrency is constrained;
- returns `ok` from `PRAGMA integrity_check` after Go writes;
- writes data that `bun:sqlite` can reopen and read afterward.

Observed spike output:

```text
bun PRAGMA journal_mode=delete
bun PRAGMA busy_timeout=undefined
bun PRAGMA foreign_keys=0
bun PRAGMA encoding=UTF-8
bun PRAGMA user_version=0
PRAGMA journal_mode=delete
PRAGMA busy_timeout=5000
PRAGMA foreign_keys=0
PRAGMA encoding=UTF-8
PRAGMA user_version=0
insert returning: id=2 key=GO123456 name=go row status=stopped config={"from":"go"} version=latest
update returning: id=2 name=go row updated
concurrent writer waited 338ms and succeeded
integrity_check=ok
bun reopen row={"key":"GO123456","name":"go row updated","config":"{\"from\":\"go\"}"}
```

Implementation caveats for Task 5 and later plans:

- Open with `busy_timeout=5000`.
- Set `SetMaxOpenConns(1)` for initial write safety unless later benchmark data justifies changing it.
- Do not enable WAL in the first Go release.
- Preserve `RETURNING` behavior for repository methods that replace current Bun prepared statements.
- Keep additive migrations only; do not use destructive schema changes.

## WebSocket Library Decision

Use `github.com/coder/websocket v1.8.14` for the Go WebSocket proxy plan.

The spike proved that `github.com/coder/websocket v1.8.14` supports:

- server upgrade through `websocket.Accept`;
- client dial through `websocket.Dial`;
- text frames;
- binary frames;
- close with status code and reason;
- context-aware operations;
- pure-Go builds with no cgo dependency.

Observed spike output:

```text
text echo kind=MessageText bytes=5 match=true
binary echo kind=MessageBinary bytes=4 match=true
ping/pong control frames handled while peer is reading
close propagated with normal closure
```

Implementation caveats for the WebSocket proxy plan:

- `Ping` waits for a pong observed by an active read loop. The bridge must keep reads active on both sides if it introduces heartbeats.
- Control frames are handled while the peer is reading; do not design heartbeat code that blocks all reads while waiting for ping completion.
- Message size limits were not changed in this spike. Later proxy work must choose explicit limits or document why the library defaults are safe for GOWA traffic.
