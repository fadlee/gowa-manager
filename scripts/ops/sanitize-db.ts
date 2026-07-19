/**
 * sanitize-db.ts — Operator tool for creating sanitized copies of production
 * GOWA Manager SQLite databases.
 *
 * Usage:
 *   bun run scripts/ops/sanitize-db.ts <source-db> <output-db>
 *   bun run scripts/ops/sanitize-db.ts --help
 *
 * What it does:
 *   1. Copies the source DB to the output path.
 *   2. Replaces all `config` values with a feature-flag-only placeholder
 *      (boolean flags only — no real URLs, tokens, or secrets).
 *   3. Replaces all `key` values with generated safe keys (sanitized-1, ...).
 *   4. Replaces all `name` values with safe names (Instance 1, ...).
 *   5. Leaves status, port, gowa_version, created_at, updated_at, and
 *      error_message untouched (not sensitive).
 *   6. Runs PRAGMA integrity_check on the sanitized DB.
 *   7. Prints a report of what was sanitized (counts, never values).
 *
 * Security: this script NEVER outputs real tokens, URLs, phone numbers, or
 * credentials. It only reports aggregate counts.
 */

import { Database } from "bun:sqlite";
import { copyFileSync, existsSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";

const REQUIRED_COLUMNS = [
  "id",
  "key",
  "name",
  "port",
  "status",
  "config",
  "gowa_version",
  "created_at",
  "updated_at",
  "error_message",
] as const;

// Feature flags that may appear in sanitized config JSON. Values are always
// booleans — never real URLs, tokens, or secrets.
const SANITIZED_CONFIG_FLAGS = ["webhook", "auth", "proxy", "autoUpdate", "metrics"] as const;

function printHelp(): void {
  const lines = [
    "sanitize-db — Create a sanitized copy of a GOWA Manager SQLite database",
    "",
    "USAGE:",
    "  bun run scripts/ops/sanitize-db.ts <source-db> <output-db>",
    "  bun run scripts/ops/sanitize-db.ts --help",
    "",
    "ARGS:",
    "  source-db   Path to the production SQLite database to sanitize.",
    "  output-db   Path where the sanitized copy will be written.",
    "",
    "WHAT IT DOES:",
    "  - Copies the source DB to the output path.",
    "  - Replaces `config` with feature-flag-only placeholder JSON (booleans).",
    "  - Replaces `key` with generated safe keys (sanitized-1, sanitized-2, ...).",
    "  - Replaces `name` with safe names (Instance 1, Instance 2, ...).",
    "  - Leaves status, port, gowa_version, timestamps, error_message untouched.",
    "  - Runs PRAGMA integrity_check on the sanitized DB.",
    "  - Prints an aggregate report (counts only, never values).",
    "",
    "SECURITY:",
    "  Never outputs real tokens, URLs, phone numbers, or credentials.",
    "  Only aggregate counts are reported.",
  ];
  console.log(lines.join("\n"));
}

function fail(msg: string): never {
  console.error(`error: ${msg}`);
  process.exit(1);
}

function buildSanitizedConfig(): string {
  // Build a config with all known feature flags set to true. This preserves
  // the "shape" of a configured instance without exposing any real values.
  const cfg: Record<string, boolean> = {};
  for (const flag of SANITIZED_CONFIG_FLAGS) {
    cfg[flag] = true;
  }
  return JSON.stringify(cfg);
}

function verifyColumns(db: Database): void {
  const cols = db.query("PRAGMA table_info(instances)").all() as Array<{
    name: string;
  }>;
  const present = new Set(cols.map((c) => c.name));
  const missing = REQUIRED_COLUMNS.filter((c) => !present.has(c));
  if (missing.length > 0) {
    fail(`instances table missing required columns: ${missing.join(", ")}`);
  }
}

function integrityCheck(db: Database): string {
  const row = db.query("PRAGMA integrity_check").get() as { integrity_check?: string } | Record<string, unknown> | null;
  // PRAGMA integrity_check returns a single row with column "integrity_check".
  if (row && typeof row === "object" && "integrity_check" in row) {
    return String((row as Record<string, unknown>).integrity_check);
  }
  // Some drivers return the value under a different key; fall back to first value.
  if (row && typeof row === "object") {
    const values = Object.values(row);
    if (values.length > 0) return String(values[0]);
  }
  return "error";
}

interface SanitizeReport {
  sourceDb: string;
  outputDb: string;
  rowsSanitized: number;
  configsReplaced: number;
  keysReplaced: number;
  namesReplaced: number;
  integrityCheck: string;
}

function main(): void {
  const args = process.argv.slice(2);

  if (args.length === 0 || args.includes("--help") || args.includes("-h")) {
    printHelp();
    process.exit(0);
  }

  if (args.length !== 2) {
    printHelp();
    fail("expected exactly two arguments: <source-db> <output-db>");
  }

  const sourceDb = resolve(args[0]);
  const outputDb = resolve(args[1]);

  if (!existsSync(sourceDb)) {
    fail(`source database not found: ${sourceDb}`);
  }

  // Ensure the output directory exists.
  const outDir = dirname(outputDb);
  if (!existsSync(outDir)) {
    mkdirSync(outDir, { recursive: true });
  }

  // 1. Copy the source DB to the output path.
  copyFileSync(sourceDb, outputDb);

  // 2. Open the copy and sanitize.
  // The file already exists from the copy above; open it read-write.
  const db = new Database(outputDb);

  try {
    verifyColumns(db);

    // Run integrity check on the copy before mutating.
    const preIntegrity = integrityCheck(db);
    if (preIntegrity !== "ok") {
      fail(`source DB failed integrity_check before sanitization: ${preIntegrity}`);
    }

    // Build the sanitized config placeholder once.
    const sanitizedConfig = buildSanitizedConfig();

    // Fetch all rows (id only) so we can iterate deterministically.
    const rows = db.query("SELECT id FROM instances ORDER BY id ASC").all() as { id: number }[];

    let configsReplaced = 0;
    let keysReplaced = 0;
    let namesReplaced = 0;

    // Use a transaction for atomicity.
    const tx = db.transaction(() => {
      let idx = 1;
      for (const row of rows) {
        const safeKey = `sanitized-${idx}`;
        const safeName = `Instance ${idx}`;
        db.query(
          "UPDATE instances SET config = ?, key = ?, name = ? WHERE id = ?",
        ).run(sanitizedConfig, safeKey, safeName, row.id);
        configsReplaced++;
        keysReplaced++;
        namesReplaced++;
        idx++;
      }
    });
    tx();

    // 3. Run integrity_check on the sanitized DB.
    const postIntegrity = integrityCheck(db);

    const report: SanitizeReport = {
      sourceDb,
      outputDb,
      rowsSanitized: rows.length,
      configsReplaced,
      keysReplaced,
      namesReplaced,
      integrityCheck: postIntegrity,
    };

    // 4. Print the report (counts only, never values).
    console.log("sanitization report");
    console.log("-------------------");
    console.log(`source DB:        ${report.sourceDb}`);
    console.log(`output DB:        ${report.outputDb}`);
    console.log(`rows sanitized:   ${report.rowsSanitized}`);
    console.log(`configs replaced: ${report.configsReplaced}`);
    console.log(`keys replaced:    ${report.keysReplaced}`);
    console.log(`names replaced:   ${report.namesReplaced}`);
    console.log(`integrity_check:  ${report.integrityCheck}`);

    if (postIntegrity !== "ok") {
      console.error(`error: sanitized DB failed integrity_check: ${postIntegrity}`);
      process.exit(1);
    }

    console.log("\nsanitized DB ready for compatibility testing.");
    console.log("supply it via GOWA_COMPAT_SAMPLES=<path> to the Go test harness.");
  } finally {
    db.close();
  }
}

main();
