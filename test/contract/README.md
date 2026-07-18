# Backend Contract Test Foundation

Contract tests compare Bun and Go backend behavior with equivalent fixtures.

Snapshots should include status, selected headers, decoded JSON bodies, relevant database rows, and filesystem paths. Normalize nondeterministic values before comparison:

- timestamps: `created_at`, `updated_at`;
- process values: `pid`, `uptime`;
- known manager and instance ports;
- temporary absolute filesystem paths;
- volatile headers such as `Date` and `X-Request-ID`.

Do not normalize by running broad regex replacements over response strings. Decode JSON first and walk typed values so meaningful user-visible strings stay intact.
