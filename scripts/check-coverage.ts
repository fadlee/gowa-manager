#!/usr/bin/env bun
/**
 * Aggregate coverage gate.
 *
 * Runs `bun test --coverage`, parses the text report, and exits non-zero if
 * the "All files" aggregate does not meet the configured thresholds.
 *
 * This exists because Bun's built-in `coverageThreshold` (bunfig.toml) is
 * per-file as of v1.3.x — see https://github.com/oven-sh/bun/issues/17028.
 * Several modules (proxy/index.ts, version-manager.ts) are intentionally
 * below 90% due to network/spawn dependencies, so a per-file gate would
 * always fail.
 *
 * Usage:
 *   bun run scripts/check-coverage.ts          # uses defaults (90% lines, 90% funcs)
 *   bun run scripts/check-coverage.ts --lines 0.95 --funcs 0.9
 */
import { spawnSync } from 'node:child_process'

const args = process.argv.slice(2)
function getFlag(name: string, fallback: number): number {
  const idx = args.indexOf(`--${name}`)
  if (idx !== -1 && args[idx + 1]) return parseFloat(args[idx + 1])
  return fallback
}

const MIN_LINES = getFlag('lines', 0.9)
const MIN_FUNCS = getFlag('funcs', 0.9)

const result = spawnSync('bun', ['test', '--coverage'], {
  cwd: process.cwd(),
  encoding: 'utf-8',
  stdio: ['ignore', 'pipe', 'pipe'],
})

const output = result.stdout + result.stderr

// Extract the "All files" row from the coverage table.
// Example: "All files  |  94.42 |  97.48 |"
const match = output.match(/All files\s*\|\s*([\d.]+)\s*\|\s*([\d.]+)/)
if (!match) {
  console.error('Could not parse aggregate coverage from test output.')
  console.error('Make sure `bun test --coverage` is available.')
  process.exit(2)
}

const funcsPct = parseFloat(match[1])
const linesPct = parseFloat(match[2])

console.log(`Aggregate coverage: ${funcsPct}% funcs / ${linesPct}% lines`)
console.log(`Threshold:          ${MIN_FUNCS * 100}% funcs / ${MIN_LINES * 100}% lines`)

let failed = false
if (funcsPct < MIN_FUNCS * 100) {
  console.error(`FAIL: function coverage ${funcsPct}% < ${MIN_FUNCS * 100}%`)
  failed = true
}
if (linesPct < MIN_LINES * 100) {
  console.error(`FAIL: line coverage ${linesPct}% < ${MIN_LINES * 100}%`)
  failed = true
}

if (failed) {
  process.exit(1)
}

console.log('Coverage gate passed.')
process.exit(0)
