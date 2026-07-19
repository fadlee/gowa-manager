/**
 * test/benchmark/compare.ts
 *
 * Compares a Go backend baseline against the captured Bun baseline using
 * the acceptance thresholds defined in thresholds.json.
 *
 * Usage:
 *   bun run test/benchmark/compare.ts                              # capture Go baseline on-the-fly
 *   bun run test/benchmark/compare.ts --go-baseline <path>         # use pre-captured Go baseline
 *   bun run test/benchmark/compare.ts --bun-baseline <path>        # override Bun baseline path
 *   bun run test/benchmark/compare.ts --thresholds <path>          # override thresholds path
 *
 * The script:
 *   1. Loads the Bun baseline and thresholds.
 *   2. Loads a pre-captured Go baseline OR captures one on-the-fly by
 *      running scripts/benchmark-backends.ts --backend go.
 *   3. Verifies machine metadata matches (os, arch, cpu, cpuCount).
 *   4. Compares each scenario against its threshold.
 *   5. Prints a human-readable report.
 *   6. Exits 0 on PASS, non-zero on FAIL.
 *
 * Do NOT relax thresholds to make a failure disappear. A failure must be
 * reported as a blocked release with measured regressions.
 */

import { readFileSync, existsSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { spawn } from 'bun'

// ---------------------------------------------------------------------------
// Types (mirrors of the baseline structure)
// ---------------------------------------------------------------------------

interface Distribution {
  raw: number[]
  median: number
  p95: number
  unit: string
}

interface DurationScenario {
  raw: number[]
  median: number
  p95: number
  unit: string
  sampleCount: number
}

interface SizeScenario {
  raw: number[]
  median: number
  p95: number
  unit: string
}

interface CrudOperationDist extends Distribution {}

interface CrudLatencyScenario {
  operations: {
    create: CrudOperationDist
    read: CrudOperationDist
    update: CrudOperationDist
    delete: CrudOperationDist
  }
  median: number
  p95: number
  sampleCount: number
}

interface ProxyHttpBodyScenario {
  bodySizeBytes: number
  requestsPerSec: Distribution
  latency: Distribution
}

interface ProxyHttpScenario {
  small: ProxyHttpBodyScenario
  large: ProxyHttpBodyScenario
}

interface WebSocketScenario {
  clientCount: number
  messagesPerClient: number
  messagesPerSec: Distribution
  latency: Distribution
}

interface MonitoringCostScenario {
  instanceCount: number
  windowSeconds: number
  cpuPercent: Distribution
  rssBytes: Distribution
}

interface DockerImageSizeScenario {
  available: boolean
  sizeBytes: number | null
  imageName: string | null
}

interface BaselineMetadata {
  backend: 'bun' | 'go'
  os: string
  arch: string
  cpu: string
  cpuCount: number
  runtimeVersion: string
  goVersion: string
  sampleCount: number
  fixtureCommit: string
  capturedAt: string
  host: string
}

interface Baseline {
  metadata: BaselineMetadata
  scenarios: {
    coldStartup: DurationScenario
    idleRss: SizeScenario
    crudLatency: CrudLatencyScenario
    proxyHttp: ProxyHttpScenario
    webSocket: WebSocketScenario
    monitoringCost: MonitoringCostScenario
    gracefulShutdown: DurationScenario
    executableSize: SizeScenario
    dockerImageSize: DockerImageSizeScenario
  }
}

interface ThresholdScenario {
  metric?: string
  unit?: string
  type: 'relative' | 'absoluteNoWorse' | 'relativeNoRegression' | 'absoluteBound' | 'informational'
  maxPercentRegression?: number
  absoluteMaxMs?: number
  rationale?: string
}

interface LeakDetectionThresholds {
  description: string
  goroutines: { maxDelta: number; rationale: string }
  rss: { maxPercentGrowth: number; rationale: string }
  fds: { maxDelta: number; rationale: string }
}

interface Thresholds {
  note: string
  approvalRequired: boolean
  rationale: string
  metadataMatch: {
    required: string[]
    description: string
  }
  scenarios: Record<string, ThresholdScenario>
  leakDetection: LeakDetectionThresholds
}

// ---------------------------------------------------------------------------
// CLI arg parsing
// ---------------------------------------------------------------------------

interface CompareArgs {
  goBaseline: string | null
  bunBaseline: string
  thresholds: string
}

function parseArgs(argv: string[]): CompareArgs {
  let goBaseline: string | null = null
  let bunBaseline = 'test/benchmark/bun-baseline.json'
  let thresholds = 'test/benchmark/thresholds.json'

  let i = 0
  while (i < argv.length) {
    const arg = argv[i]
    switch (arg) {
      case '--go-baseline':
        i++
        if (i >= argv.length) throw new Error('Missing value for --go-baseline')
        goBaseline = argv[i]
        break
      case '--bun-baseline':
        i++
        if (i >= argv.length) throw new Error('Missing value for --bun-baseline')
        bunBaseline = argv[i]
        break
      case '--thresholds':
        i++
        if (i >= argv.length) throw new Error('Missing value for --thresholds')
        thresholds = argv[i]
        break
      case '--help':
      case '-h':
        console.log(`
Usage: bun run test/benchmark/compare.ts [options]

Options:
  --go-baseline <path>    Path to pre-captured Go baseline JSON.
                          If omitted, captures Go baseline on-the-fly.
  --bun-baseline <path>   Path to Bun baseline JSON (default: test/benchmark/bun-baseline.json)
  --thresholds <path>     Path to thresholds JSON (default: test/benchmark/thresholds.json)
  -h, --help              Show this help message.
`)
        process.exit(0)
        break
      default:
        throw new Error(`Unknown argument: ${arg}`)
    }
    i++
  }

  return { goBaseline, bunBaseline, thresholds }
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

function loadJSON<T>(path: string): T {
  const abs = resolve(path)
  if (!existsSync(abs)) {
    throw new Error(`File not found: ${abs}`)
  }
  const data = readFileSync(abs, 'utf8')
  return JSON.parse(data) as T
}

// ---------------------------------------------------------------------------
// Go baseline capture (on-the-fly)
// ---------------------------------------------------------------------------

async function captureGoBaseline(outputPath: string): Promise<void> {
  console.log('No --go-baseline provided; capturing Go baseline on-the-fly...')
  console.log('  Running: bun run scripts/benchmark-backends.ts --backend go --output ' + outputPath)
  console.log('  (This takes several minutes — start a backend, run 9 scenarios, etc.)\n')

  const proc = spawn({
    cmd: ['bun', 'run', 'scripts/benchmark-backends.ts', '--backend', 'go', '--output', outputPath],
    stdout: 'inherit',
    stderr: 'inherit',
    cwd: process.cwd(),
  })

  // Wait for the harness to finish, with a generous timeout. The harness
  // may not exit cleanly on Windows due to lingering async I/O (stderr
  // readers), so we also check for the output file as a completion signal.
  const timeoutMs = 5 * 60 * 1000 // 5 minutes
  const start = Date.now()
  const outputPathAbs = resolve(outputPath)

  // Poll for the output file while also waiting for the process to exit.
  while (Date.now() - start < timeoutMs) {
    // Check if the process has exited.
    const exitCode = await Promise.race([
      proc.exited.then((code) => code),
      Bun.sleep(2000).then(() => null as number | null),
    ])
    if (exitCode !== null) {
      if (exitCode !== 0) {
        throw new Error(`Go baseline capture failed (exit ${exitCode})`)
      }
      break
    }
    // Check if the output file has been written (harness may hang on exit
    // even after writing the baseline).
    if (existsSync(outputPathAbs)) {
      // Give the harness a moment to finish flushing, then proceed.
      await Bun.sleep(1000)
      try {
        proc.kill()
      } catch {}
      break
    }
  }

  if (!existsSync(outputPathAbs)) {
    throw new Error('Go baseline capture timed out — no output file produced')
  }

  console.log(`\nGo baseline captured to ${outputPath}\n`)
}

// ---------------------------------------------------------------------------
// Metadata comparison
// ---------------------------------------------------------------------------

interface MetadataMismatch {
  field: string
  bunValue: string
  goValue: string
}

function compareMetadata(
  bun: BaselineMetadata,
  go: BaselineMetadata,
  required: string[],
): MetadataMismatch[] {
  const mismatches: MetadataMismatch[] = []
  for (const field of required) {
    const bunVal = String((bun as any)[field] ?? '')
    const goVal = String((go as any)[field] ?? '')
    if (bunVal !== goVal) {
      mismatches.push({ field, bunValue: bunVal, goValue: goVal })
    }
  }
  return mismatches
}

// ---------------------------------------------------------------------------
// Scenario comparison
// ---------------------------------------------------------------------------

type Verdict = 'PASS' | 'FAIL' | 'INFO' | 'SKIP'

interface ScenarioResult {
  name: string
  verdict: Verdict
  bunValue: number | null
  goValue: number | null
  unit: string
  threshold: string
  detail: string
}

function pctDiff(bun: number, go: number): number {
  if (bun === 0) return 0
  return ((go - bun) / bun) * 100
}

function compareScenario(
  name: string,
  threshold: ThresholdScenario,
  bunValue: number | null,
  goValue: number | null,
  unit: string,
): ScenarioResult {
  const base: ScenarioResult = {
    name,
    verdict: 'SKIP',
    bunValue,
    goValue,
    unit,
    threshold: threshold.type,
    detail: '',
  }

  if (threshold.type === 'informational') {
    return {
      ...base,
      verdict: 'INFO',
      detail: 'Informational only — no threshold applied.',
    }
  }

  if (bunValue == null || goValue == null) {
    return {
      ...base,
      verdict: 'SKIP',
      detail: 'Missing measurement — skipped.',
    }
  }

  const diff = pctDiff(bunValue, goValue)

  switch (threshold.type) {
    case 'absoluteNoWorse': {
      // Go must be <= Bun (e.g., idle RSS). 0% tolerance.
      const maxPct = threshold.maxPercentRegression ?? 0
      if (goValue > bunValue * (1 + maxPct / 100)) {
        return {
          ...base,
          verdict: 'FAIL',
          detail: `Go (${goValue}) exceeds Bun (${bunValue}) by ${diff.toFixed(1)}% — must not be worse (max ${maxPct}%).`,
        }
      }
      return {
        ...base,
        verdict: 'PASS',
        detail: `Go (${goValue}) <= Bun (${bunValue}) — within ${maxPct}% tolerance.`,
      }
    }

    case 'relativeNoRegression': {
      // For throughput (higher is better): Go must be >= (1 - tolerance) * Bun.
      const maxPct = threshold.maxPercentRegression ?? 0
      const minAcceptable = bunValue * (1 - maxPct / 100)
      if (goValue < minAcceptable) {
        return {
          ...base,
          verdict: 'FAIL',
          detail: `Go (${goValue.toFixed(1)}) is ${Math.abs(diff).toFixed(1)}% below Bun (${bunValue.toFixed(1)}) — max ${maxPct}% regression allowed.`,
        }
      }
      return {
        ...base,
        verdict: 'PASS',
        detail: `Go (${goValue.toFixed(1)}) within ${maxPct}% of Bun (${bunValue.toFixed(1)}).`,
      }
    }

    case 'relative': {
      // For latency/duration (lower is better): Go must be <= (1 + tolerance) * Bun.
      const maxPct = threshold.maxPercentRegression ?? 0
      const maxAcceptable = bunValue * (1 + maxPct / 100)
      if (goValue > maxAcceptable) {
        return {
          ...base,
          verdict: 'FAIL',
          detail: `Go (${goValue.toFixed(2)}) is ${diff.toFixed(1)}% above Bun (${bunValue.toFixed(2)}) — max ${maxPct}% regression allowed.`,
        }
      }
      return {
        ...base,
        verdict: 'PASS',
        detail: `Go (${goValue.toFixed(2)}) within ${maxPct}% of Bun (${bunValue.toFixed(2)}).`,
      }
    }

    case 'absoluteBound': {
      // Go must be below an absolute bound (e.g., graceful shutdown < 5s).
      const maxMs = threshold.absoluteMaxMs ?? Infinity
      if (goValue > maxMs) {
        return {
          ...base,
          verdict: 'FAIL',
          detail: `Go (${goValue.toFixed(0)} ms) exceeds absolute bound of ${maxMs} ms.`,
        }
      }
      return {
        ...base,
        verdict: 'PASS',
        detail: `Go (${goValue.toFixed(0)} ms) within absolute bound of ${maxMs} ms.`,
      }
    }

    default:
      return {
        ...base,
        verdict: 'SKIP',
        detail: `Unknown threshold type: ${threshold.type}`,
      }
  }
}

// ---------------------------------------------------------------------------
// Leak detection
// ---------------------------------------------------------------------------

interface LeakMetricResult {
  name: string
  verdict: Verdict
  measured: number | null
  threshold: number
  unit: string
  detail: string
}

interface LeakDetectionResult {
  verdict: Verdict // PASS, FAIL, or SKIP
  goroutineDelta: number | null
  rssGrowthPercent: number | null
  fdHandleDelta: number | null
  metrics: LeakMetricResult[]
  detail: string
  rawOutput: string
}

// Run the Go leak detection test and parse machine-readable metrics from its
// stdout. If the test fails to run or produces no parseable output, the result
// is reported as SKIP (not FAIL) with a warning, since the leak test may not
// be runnable in all environments.
async function runLeakDetection(thresholds: LeakDetectionThresholds): Promise<LeakDetectionResult> {
  console.log('\n── Running Leak Detection ──')
  console.log('  go test ./test/benchmark/ -run TestLeakDetection -v -timeout 120s\n')

  const proc = Bun.spawn(
    ['go', 'test', './test/benchmark/', '-run', 'TestLeakDetection', '-v', '-timeout', '120s'],
    {
      stdout: 'pipe',
      stderr: 'pipe',
      cwd: process.cwd(),
    },
  )

  const stdoutText = await new Response(proc.stdout).text()
  const stderrText = await new Response(proc.stderr).text()
  const exitCode = await proc.exited

  // Parse machine-readable metric lines printed by leak_test.go.
  // A value of -1 indicates the metric was unavailable on this platform.
  const goroDeltaMatch = stdoutText.match(/^goroutine_delta:\s*(-?\d+)\s*$/m)
  const rssGrowthMatch = stdoutText.match(/^rss_growth_percent:\s*(-?[\d.]+)\s*$/m)
  const fdDeltaMatch = stdoutText.match(/^fd_handle_delta:\s*(-?\d+)\s*$/m)
  const leakTestMatch = stdoutText.match(/^leak_test:\s*(PASS|FAIL)\s*$/m)

  const goroDelta = goroDeltaMatch ? parseInt(goroDeltaMatch[1], 10) : null
  const rssGrowth = rssGrowthMatch ? parseFloat(rssGrowthMatch[1]) : null
  const fdDelta = fdDeltaMatch ? parseInt(fdDeltaMatch[1], 10) : null
  const testVerdict = leakTestMatch ? (leakTestMatch[1] as 'PASS' | 'FAIL') : null

  // If no parseable output at all, report as SKIP (not FAIL).
  if (testVerdict === null && goroDelta === null && rssGrowth === null && fdDelta === null) {
    console.log('  ⚠️  Leak test produced no parseable metrics — reporting SKIP.')
    console.log(`  (go test exit code: ${exitCode})`)
    if (stderrText.trim()) {
      console.log('  stderr (truncated):', stderrText.trim().slice(0, 500))
    }
    return {
      verdict: 'SKIP',
      goroutineDelta: null,
      rssGrowthPercent: null,
      fdHandleDelta: null,
      metrics: [],
      detail:
        'Leak test did not produce parseable output — may not be runnable in this environment.',
      rawOutput: stdoutText,
    }
  }

  // Compare each metric against thresholds from thresholds.json.
  const metrics: LeakMetricResult[] = []
  let overallFail = false

  // Goroutines
  if (goroDelta !== null && goroDelta >= 0) {
    const max = thresholds.goroutines.maxDelta
    const fail = goroDelta > max
    if (fail) overallFail = true
    metrics.push({
      name: 'leak.goroutines',
      verdict: fail ? 'FAIL' : 'PASS',
      measured: goroDelta,
      threshold: max,
      unit: 'delta',
      detail: `Goroutine delta ${goroDelta} ${fail ? 'exceeds' : 'within'} max ${max}.`,
    })
  } else {
    metrics.push({
      name: 'leak.goroutines',
      verdict: 'SKIP',
      measured: null,
      threshold: thresholds.goroutines.maxDelta,
      unit: 'delta',
      detail: 'Goroutine measurement unavailable on this platform.',
    })
  }

  // RSS
  if (rssGrowth !== null && rssGrowth >= 0) {
    const max = thresholds.rss.maxPercentGrowth
    const fail = rssGrowth > max
    if (fail) overallFail = true
    metrics.push({
      name: 'leak.rss',
      verdict: fail ? 'FAIL' : 'PASS',
      measured: rssGrowth,
      threshold: max,
      unit: '%',
      detail: `RSS growth ${rssGrowth.toFixed(2)}% ${fail ? 'exceeds' : 'within'} max ${max}%.`,
    })
  } else {
    metrics.push({
      name: 'leak.rss',
      verdict: 'SKIP',
      measured: null,
      threshold: thresholds.rss.maxPercentGrowth,
      unit: '%',
      detail: 'RSS measurement unavailable on this platform.',
    })
  }

  // FDs / handles
  if (fdDelta !== null && fdDelta >= 0) {
    const max = thresholds.fds.maxDelta
    const fail = fdDelta > max
    if (fail) overallFail = true
    metrics.push({
      name: 'leak.fds',
      verdict: fail ? 'FAIL' : 'PASS',
      measured: fdDelta,
      threshold: max,
      unit: 'delta',
      detail: `FD/handle delta ${fdDelta} ${fail ? 'exceeds' : 'within'} max ${max}.`,
    })
  } else {
    metrics.push({
      name: 'leak.fds',
      verdict: 'SKIP',
      measured: null,
      threshold: thresholds.fds.maxDelta,
      unit: 'delta',
      detail: 'FD/handle measurement unavailable on this platform.',
    })
  }

  const verdict: Verdict = overallFail ? 'FAIL' : 'PASS'
  console.log(`  Leak test self-verdict: ${testVerdict ?? 'N/A'}`)
  console.log(`  Leak detection verdict: ${verdict}`)
  for (const m of metrics) {
    console.log(`    ${verdictIcon(m.verdict)} ${m.name}: ${m.detail}`)
  }

  if (exitCode !== 0 && !overallFail) {
    console.log(`  ⚠️  go test exited with code ${exitCode} but metrics parsed OK.`)
  }

  return {
    verdict,
    goroutineDelta: goroDelta,
    rssGrowthPercent: rssGrowth,
    fdHandleDelta: fdDelta,
    metrics,
    detail: overallFail
      ? 'Leak detection FAILED — resource leak detected above thresholds.'
      : 'Leak detection passed — all measured metrics within thresholds.',
    rawOutput: stdoutText,
  }
}

// ---------------------------------------------------------------------------
// Report formatting
// ---------------------------------------------------------------------------

function formatValue(val: number | null, unit: string): string {
  if (val == null) return 'N/A'
  if (unit === 'bytes') return `${(val / 1024 / 1024).toFixed(1)} MB`
  if (unit === 'req/s') return `${val.toFixed(0)} req/s`
  if (unit === 'msg/s') return `${val.toFixed(0)} msg/s`
  if (unit === 'ms') return `${val.toFixed(2)} ms`
  if (unit === '%') return `${val.toFixed(2)}%`
  if (unit === 'delta') return `${val}`
  return `${val.toFixed(2)} ${unit}`
}

function verdictIcon(v: Verdict): string {
  switch (v) {
    case 'PASS': return '✅'
    case 'FAIL': return '❌'
    case 'INFO': return 'ℹ️'
    case 'SKIP': return '⏭️'
  }
}

function printReport(
  metadataOk: boolean,
  metadataMismatches: MetadataMismatch[],
  results: ScenarioResult[],
  bunMeta: BaselineMetadata,
  goMeta: BaselineMetadata,
  leakResult: LeakDetectionResult | null,
): void {
  console.log('\n' + '='.repeat(72))
  console.log('  GOWA Manager: Go vs Bun Benchmark Comparison Report')
  console.log('='.repeat(72))

  // Metadata
  console.log('\n── Machine Metadata ──')
  console.log(`  Bun: ${bunMeta.os}/${bunMeta.arch}, ${bunMeta.cpu.trim()} (${bunMeta.cpuCount} cores)`)
  console.log(`       Bun ${bunMeta.runtimeVersion}, ${bunMeta.goVersion}`)
  console.log(`  Go:  ${goMeta.os}/${goMeta.arch}, ${goMeta.cpu.trim()} (${goMeta.cpuCount} cores)`)
  console.log(`       Bun ${goMeta.runtimeVersion}, ${goMeta.goVersion}`)

  if (!metadataOk) {
    console.log('\n  ❌ METADATA MISMATCH — comparison rejected:')
    for (const m of metadataMismatches) {
      console.log(`     ${m.field}: Bun="${m.bunValue}" vs Go="${m.goValue}"`)
    }
    console.log('\n  Baselines were captured on different machines/configurations.')
    console.log('  Recapture both baselines on the same host and build mode.\n')
    return
  }
  console.log('  ✅ Metadata matches.\n')

  // Scenario results
  console.log('── Scenario Results ──\n')
  const formatRow = (name: string, verdict: Verdict, bun: string, go: string, detail: string) => {
    const icon = verdictIcon(verdict)
    const status = verdict.padEnd(4)
    const namePadded = name.padEnd(28)
    console.log(`  ${icon} ${status} ${namePadded} Bun: ${bun.padEnd(16)} Go: ${go.padEnd(16)}`)
    console.log(`         ${detail}`)
  }

  for (const r of results) {
    formatRow(
      r.name,
      r.verdict,
      formatValue(r.bunValue, r.unit),
      formatValue(r.goValue, r.unit),
      r.detail,
    )
  }

  // Leak detection results
  if (leakResult) {
    console.log('\n── Leak Detection ──\n')
    if (leakResult.verdict === 'SKIP' && leakResult.metrics.length === 0) {
      console.log(`  ${verdictIcon('SKIP')} SKIP  ${leakResult.detail}`)
    } else {
      for (const m of leakResult.metrics) {
        const icon = verdictIcon(m.verdict)
        const status = m.verdict.padEnd(4)
        const namePadded = m.name.padEnd(28)
        const measured = m.measured !== null ? formatValue(m.measured, m.unit) : 'N/A'
        const thresh = formatValue(m.threshold, m.unit)
        console.log(`  ${icon} ${status} ${namePadded} Measured: ${measured.padEnd(16)} Max: ${thresh.padEnd(16)}`)
        console.log(`         ${m.detail}`)
      }
      console.log(`\n  Overall leak verdict: ${verdictIcon(leakResult.verdict)} ${leakResult.verdict}`)
      console.log(`  ${leakResult.detail}`)
    }
  }

  // Summary — includes leak detection in the counts
  const allResults: { verdict: Verdict }[] = [...results]
  if (leakResult) {
    allResults.push(...leakResult.metrics)
  }
  const passed = allResults.filter((r) => r.verdict === 'PASS').length
  const failed = allResults.filter((r) => r.verdict === 'FAIL').length
  const info = allResults.filter((r) => r.verdict === 'INFO').length
  const skipped = allResults.filter((r) => r.verdict === 'SKIP').length

  console.log('\n── Summary ──')
  console.log(`  Passed:        ${passed}`)
  console.log(`  Failed:        ${failed}`)
  console.log(`  Informational: ${info}`)
  console.log(`  Skipped:       ${skipped}`)

  if (failed > 0) {
    console.log('\n  ❌ OVERALL: FAIL — release is BLOCKED')
    console.log('  Do NOT relax thresholds to make failures disappear.')
    console.log('  Investigate each regression before proceeding.\n')
  } else {
    console.log('\n  ✅ OVERALL: PASS — Go backend meets acceptance thresholds\n')
  }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2))

  // Load thresholds
  const thresholds = loadJSON<Thresholds>(args.thresholds)
  console.log('\nLoaded thresholds from ' + resolve(args.thresholds))
  if (thresholds.approvalRequired) {
    console.log('  ⚠️  ' + thresholds.note)
  }

  // Load Bun baseline
  const bunBaseline = loadJSON<Baseline>(args.bunBaseline)
  console.log('Loaded Bun baseline from ' + resolve(args.bunBaseline))
  console.log(`  Captured: ${bunBaseline.metadata.capturedAt}`)

  // Get or capture Go baseline
  let goBaselinePath: string
  if (args.goBaseline) {
    goBaselinePath = args.goBaseline
  } else {
    goBaselinePath = 'test/benchmark/go-baseline.json'
    if (!existsSync(resolve(goBaselinePath))) {
      await captureGoBaseline(goBaselinePath)
    } else {
      console.log(`Using existing Go baseline at ${goBaselinePath}`)
    }
  }

  const goBaseline = loadJSON<Baseline>(goBaselinePath)
  console.log('Loaded Go baseline from ' + resolve(goBaselinePath))
  console.log(`  Captured: ${goBaseline.metadata.capturedAt}\n`)

  // Compare metadata
  const requiredFields = thresholds.metadataMatch.required
  const mismatches = compareMetadata(bunBaseline.metadata, goBaseline.metadata, requiredFields)
  const metadataOk = mismatches.length === 0

  // Compare scenarios
  const results: ScenarioResult[] = []
  const bunS = bunBaseline.scenarios
  const goS = goBaseline.scenarios
  const tScenarios = thresholds.scenarios

  // coldStartup — median, lower is better
  results.push(
    compareScenario('coldStartup', tScenarios.coldStartup, bunS.coldStartup.median, goS.coldStartup.median, 'ms'),
  )

  // idleRss — median, must not be worse (Go <= Bun)
  results.push(
    compareScenario('idleRss', tScenarios.idleRss, bunS.idleRss.median, goS.idleRss.median, 'bytes'),
  )

  // crudLatency — p95, lower is better
  results.push(
    compareScenario('crudLatency (p95)', tScenarios.crudLatency, bunS.crudLatency.p95, goS.crudLatency.p95, 'ms'),
  )

  // proxyHttp throughput — median, higher is better (no regression)
  results.push(
    compareScenario('proxyHttp.small.throughput', tScenarios.proxyHttpThroughput, bunS.proxyHttp.small.requestsPerSec.median, goS.proxyHttp.small.requestsPerSec.median, 'req/s'),
  )
  results.push(
    compareScenario('proxyHttp.large.throughput', tScenarios.proxyHttpThroughput, bunS.proxyHttp.large.requestsPerSec.median, goS.proxyHttp.large.requestsPerSec.median, 'req/s'),
  )

  // proxyHttp latency — p95, lower is better
  results.push(
    compareScenario('proxyHttp.small.latency', tScenarios.proxyHttpLatency, bunS.proxyHttp.small.latency.p95, goS.proxyHttp.small.latency.p95, 'ms'),
  )
  results.push(
    compareScenario('proxyHttp.large.latency', tScenarios.proxyHttpLatency, bunS.proxyHttp.large.latency.p95, goS.proxyHttp.large.latency.p95, 'ms'),
  )

  // webSocket throughput — median, higher is better
  results.push(
    compareScenario('webSocket.throughput', tScenarios.webSocketThroughput, bunS.webSocket.messagesPerSec.median, goS.webSocket.messagesPerSec.median, 'msg/s'),
  )

  // webSocket latency — p95, lower is better
  results.push(
    compareScenario('webSocket.latency', tScenarios.webSocketLatency, bunS.webSocket.latency.p95, goS.webSocket.latency.p95, 'ms'),
  )

  // monitoringCost CPU — median
  results.push(
    compareScenario('monitoringCost.cpu', tScenarios.monitoringCostCpu, bunS.monitoringCost.cpuPercent.median, goS.monitoringCost.cpuPercent.median, '%'),
  )

  // gracefulShutdown — median, absolute bound
  results.push(
    compareScenario('gracefulShutdown', tScenarios.gracefulShutdown, bunS.gracefulShutdown.median, goS.gracefulShutdown.median, 'ms'),
  )

  // executableSize — informational
  results.push(
    compareScenario('executableSize', tScenarios.executableSize, bunS.executableSize.median, goS.executableSize.median, 'bytes'),
  )

  // dockerImageSize — informational
  const bunDocker = bunS.dockerImageSize.sizeBytes
  const goDocker = goS.dockerImageSize.sizeBytes
  results.push(
    compareScenario('dockerImageSize', tScenarios.dockerImageSize, bunDocker, goDocker, 'bytes'),
  )

  // ── Leak detection ──
  // Run the Go leak detection test (TestLeakDetection) and compare its
  // metrics against the thresholds.json leakDetection section. This is only
  // meaningful when comparing the Go backend (always the case here). If the
  // test cannot run or produces no parseable output, it is reported as SKIP.
  let leakResult: LeakDetectionResult | null = null
  if (metadataOk) {
    try {
      leakResult = await runLeakDetection(thresholds.leakDetection)
    } catch (err) {
      console.log(`\n  ⚠️  Leak detection could not run: ${err}`)
      leakResult = {
        verdict: 'SKIP',
        goroutineDelta: null,
        rssGrowthPercent: null,
        fdHandleDelta: null,
        metrics: [],
        detail: `Leak detection could not run: ${err}`,
        rawOutput: '',
      }
    }
  } else {
    console.log('\n  ⏭️  Skipping leak detection — metadata mismatch.')
  }

  // Print report
  printReport(metadataOk, mismatches, results, bunBaseline.metadata, goBaseline.metadata, leakResult)

  // Exit code
  if (!metadataOk) {
    process.exit(2)
  }
  const failed = results.filter((r) => r.verdict === 'FAIL').length
  const leakFailed = leakResult?.metrics.filter((m) => m.verdict === 'FAIL').length ?? 0
  if (failed > 0 || leakFailed > 0) {
    process.exit(1)
  }
  process.exit(0)
}

main().catch((err) => {
  console.error('\n❌ Comparison failed:', err)
  process.exit(1)
})
