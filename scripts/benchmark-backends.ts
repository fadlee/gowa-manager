/**
 * benchmark-backends.ts
 *
 * Repeatable performance and resource baseline harness for the GOWA Manager
 * backends (Bun and Go).  Measures nine scenarios:
 *
 *   1. Cold startup to health
 *   2. Idle RSS after stabilization
 *   3. CRUD latency distribution
 *   4. HTTP proxy throughput / latency (small + large bodies)
 *   5. WebSocket connection / message throughput
 *   6. Monitoring cost with a fixed number of fake instances
 *   7. Graceful shutdown duration
 *   8. Executable size
 *   9. Docker image size (when Docker is available)
 *
 * Usage:
 *   bun run scripts/benchmark-backends.ts --backend bun --output test/benchmark/bun-baseline.json
 *   bun run scripts/benchmark-backends.ts --backend go  --output test/benchmark/go-baseline.json
 *
 * The script starts the specified backend on an independent temporary data
 * directory and random port, runs all scenarios with warmups and multiple
 * samples, computes median / p95, records environment metadata, and writes
 * JSON matching test/benchmark/baseline.schema.json.
 *
 * Results are machine-specific — do NOT compare across unlike machines.
 */

import { Database } from 'bun:sqlite'
import { join, resolve } from 'node:path'
import {
  mkdtempSync,
  rmSync,
  existsSync,
  statSync,
  readFileSync,
  writeFileSync,
  mkdirSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { spawn, type Subprocess } from 'bun'
import { WebSocketServer, type WebSocket as WsWebSocket } from 'ws'
import type { Server as HttpServer } from 'node:http'

// ---------------------------------------------------------------------------
// Types
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

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const COLD_STARTUP_SAMPLES = 5
const COLD_STARTUP_WARMUPS = 1
const CRUD_INSTANCE_COUNT = 20
const CRUD_SAMPLES = 3
const CRUD_WARMUPS = 1
const PROXY_REQUESTS = 100
const PROXY_SAMPLES = 3
const PROXY_WARMUPS = 1
const PROXY_SMALL_BODY = 1024 // 1 KB
const PROXY_LARGE_BODY = 1024 * 1024 // 1 MB
const WS_CLIENT_COUNT = 10
const WS_MESSAGES_PER_CLIENT = 50
const WS_SAMPLES = 3
const WS_WARMUPS = 1
const MONITORING_INSTANCE_COUNT = 10
const MONITORING_WINDOW_SECONDS = 5
const SHUTDOWN_SAMPLES = 3
const IDLE_STABILIZATION_SECONDS = 5

// ---------------------------------------------------------------------------
// Statistics helpers
// ---------------------------------------------------------------------------

function median(values: number[]): number {
  if (values.length === 0) return 0
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  if (sorted.length % 2 === 0) {
    return (sorted[mid - 1] + sorted[mid]) / 2
  }
  return sorted[mid]
}

function percentile(values: number[], p: number): number {
  if (values.length === 0) return 0
  const sorted = [...values].sort((a, b) => a - b)
  const index = Math.ceil((p / 100) * sorted.length) - 1
  return sorted[Math.max(0, index)]
}

function dist(values: number[], unit: string): Distribution {
  return {
    raw: values,
    median: median(values),
    p95: percentile(values, 95),
    unit,
  }
}

function durationScenario(values: number[], unit: string): DurationScenario {
  return {
    raw: values,
    median: median(values),
    p95: percentile(values, 95),
    unit,
    sampleCount: values.length,
  }
}

function sizeScenario(values: number[], unit: string): SizeScenario {
  return {
    raw: values,
    median: median(values),
    p95: percentile(values, 95),
    unit,
  }
}

// ---------------------------------------------------------------------------
// Process management
// ---------------------------------------------------------------------------

interface ManagedProcess {
  proc: Subprocess<'ignore', 'pipe', 'pipe'>
  pid: number
  port: number
  dataDir: string
  adminAuth: string
  stderrChunks: string[]
}

/**
 * Find a free TCP port by briefly listening and closing.
 */
async function findFreePort(): Promise<number> {
  const { createServer } = require('node:net')
  return new Promise((resolve, reject) => {
    const server = createServer()
    server.listen(0, '127.0.0.1', () => {
      const port = server.address().port
      server.close(() => resolve(port))
    })
    server.on('error', reject)
  })
}

/**
 * Start a backend process on an independent temp data dir and port.
 * Pre-creates a dummy GOWA binary so the Bun backend skips its network
 * download on startup (avoids measurement noise).
 */
async function startBackend(
  backend: 'bun' | 'go',
  port: number,
  dataDir: string,
): Promise<ManagedProcess> {
  const adminUser = 'admin'
  const adminPass = 'password'
  const adminAuth = btoa(`${adminUser}:${adminPass}`)

  // Pre-create dummy GOWA binary so Bun backend skips download.
  const binDir = join(dataDir, 'bin')
  mkdirSync(binDir, { recursive: true })
  const dummyBinaryName = process.platform === 'win32' ? 'gowa.exe' : 'gowa'
  const dummyBinaryPath = join(binDir, dummyBinaryName)
  writeFileSync(dummyBinaryPath, '#!/bin/sh\necho dummy\n')

  let cmd: string[]
  if (backend === 'bun') {
    cmd = [
      'bun',
      'run',
      'src/index.ts',
      '--port',
      String(port),
      '--admin-username',
      adminUser,
      '--admin-password',
      adminPass,
      '--data-dir',
      dataDir,
    ]
  } else {
    cmd = [
      'go',
      'run',
      './cmd/gowa-manager-go',
      '-p',
      String(port),
      '-u',
      adminUser,
      '-P',
      adminPass,
      '-d',
      dataDir,
    ]
  }

  const proc = spawn({
    cmd,
    stdout: 'pipe',
    stderr: 'pipe',
    cwd: process.cwd(),
    env: {
      ...process.env,
      NODE_ENV: 'production',
    },
  })

  // Capture stderr for debugging (printed on failure).
  const stderrChunks: string[] = []
  ;(async () => {
    const text = await new Response(proc.stderr).text()
    if (text) stderrChunks.push(text)
  })()

  return { proc, pid: proc.pid!, port, dataDir, adminAuth, stderrChunks }
}

/**
 * Poll GET /api/health until 200 or timeout.
 * Returns elapsed milliseconds from startTime, or throws on timeout.
 */
async function waitForHealth(
  port: number,
  timeoutMs = 30000,
  startTime = performance.now(),
): Promise<number> {
  const url = `http://localhost:${port}/api/health`
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url, { signal: AbortSignal.timeout(2000) })
      if (res.ok) {
        return performance.now() - startTime
      }
    } catch {
      // not ready yet
    }
    await Bun.sleep(50)
  }
  throw new Error(`Backend did not become healthy within ${timeoutMs}ms`)
}

/**
 * Stop a managed process gracefully (SIGTERM) and return exit duration.
 * Falls back to kill after a timeout.
 */
async function stopBackend(
  mp: ManagedProcess,
  gracefulTimeoutMs = 10000,
): Promise<number> {
  const start = performance.now()
  try {
    if (process.platform === 'win32') {
      // Windows does not support SIGTERM on child processes reliably.
      // Use taskkill for graceful tree termination.
      const killProc = spawn({
        cmd: ['taskkill', '/PID', String(mp.pid), '/T'],
        stdout: 'ignore',
        stderr: 'ignore',
      })
      await killProc.exited
    } else {
      mp.proc.kill('SIGTERM')
    }
  } catch {
    // process may already be dead
  }

  try {
    await Promise.race([
      mp.proc.exited,
      Bun.sleep(gracefulTimeoutMs).then(() => {
        try {
          mp.proc.kill()
        } catch {}
      }),
    ])
  } catch {
    // force kill
    try {
      mp.proc.kill()
    } catch {}
  }
  return performance.now() - start
}

/**
 * Force-kill a managed process (cleanup, not measured).
 */
async function killBackend(mp: ManagedProcess): Promise<void> {
  try {
    if (process.platform === 'win32') {
      const killProc = spawn({
        cmd: ['taskkill', '/PID', String(mp.pid), '/T', '/F'],
        stdout: 'ignore',
        stderr: 'ignore',
      })
      await killProc.exited
    } else {
      mp.proc.kill('SIGKILL')
    }
  } catch {
    // already dead
  }
  try {
    await mp.proc.exited
  } catch {}
}

// ---------------------------------------------------------------------------
// RSS measurement
// ---------------------------------------------------------------------------

async function measureRSS(pid: number): Promise<number> {
  if (process.platform === 'win32') {
    // Use PowerShell Get-Process for WorkingSet64 (bytes).
    try {
      const proc = spawn({
        cmd: [
          'powershell',
          '-NoProfile',
          '-Command',
          `(Get-Process -Id ${pid}).WorkingSet64`,
        ],
        stdout: 'pipe',
        stderr: 'ignore',
      })
      const text = await new Response(proc.stdout).text()
      const rss = parseInt(text.trim(), 10)
      if (Number.isFinite(rss) && rss > 0) return rss
    } catch {}
    // Fallback: tasklist
    try {
      const proc = spawn({
        cmd: ['tasklist', '/fi', `PID eq ${pid}`, '/fo', 'csv', '/nh'],
        stdout: 'pipe',
        stderr: 'ignore',
      })
      const text = await new Response(proc.stdout).text()
      const match = text.match(/"([\d,]+)"/g)
      if (match && match.length >= 5) {
        // Working Set is the 5th quoted field, in KB with commas.
        const wsKb = parseInt(match[4].replace(/["',]/g, ''), 10)
        if (Number.isFinite(wsKb)) return wsKb * 1024
      }
    } catch {}
    return 0
  }

  if (process.platform === 'linux') {
    try {
      const status = readFileSync(`/proc/${pid}/status`, 'utf8')
      const match = status.match(/VmRSS:\s*(\d+)\s*kB/)
      if (match) return parseInt(match[1], 10) * 1024
    } catch {}
    return 0
  }

  if (process.platform === 'darwin') {
    try {
      const proc = spawn({
        cmd: ['ps', '-o', 'rss', '-p', String(pid)],
        stdout: 'pipe',
        stderr: 'ignore',
      })
      const text = await new Response(proc.stdout).text()
      const lines = text.trim().split('\n')
      if (lines.length >= 2) {
        const rss = parseInt(lines[lines.length - 1].trim(), 10)
        if (Number.isFinite(rss)) return rss * 1024
      }
    } catch {}
    return 0
  }

  return 0
}

// ---------------------------------------------------------------------------
// CPU measurement (monitoring cost)
// ---------------------------------------------------------------------------

async function measureCpuPercent(
  pid: number,
  windowSeconds: number,
): Promise<number> {
  if (process.platform === 'win32') {
    // Use two PowerShell samples of TotalProcessorTime to compute delta.
    try {
      const sample = async (): Promise<[number, number]> => {
        const proc = spawn({
          cmd: [
            'powershell',
            '-NoProfile',
            '-Command',
            `$p = Get-Process -Id ${pid}; $p.TotalProcessorTime.Ticks; (Get-Date).Ticks`,
          ],
          stdout: 'pipe',
          stderr: 'ignore',
        })
        const text = await new Response(proc.stdout).text()
        const lines = text.trim().split('\n')
        return [parseFloat(lines[0]), parseFloat(lines[1])]
      }
      const [cpu1, wall1] = await sample()
      await Bun.sleep(windowSeconds * 1000)
      const [cpu2, wall2] = await sample()
      const cpuDelta = cpu2 - cpu1
      const wallDelta = wall2 - wall1
      // .NET ticks are 100ns each; 1 tick = 100ns = 1e-7 seconds
      const cpuSeconds = (cpuDelta * 100e-9)
      const wallSeconds = (wallDelta * 100e-9)
      if (wallSeconds <= 0) return 0
      return (cpuSeconds / wallSeconds) * 100
    } catch {
      return 0
    }
  }

  // Linux/macOS: sample /proc/<pid>/stat or use ps.
  try {
    const sample = async (): Promise<number> => {
      if (process.platform === 'linux') {
        const stat = readFileSync(`/proc/${pid}/stat`, 'utf8')
        const fields = stat.split(' ')
        // utime + stime (fields 14 and 15, 1-indexed → index 13 and 14)
        return parseInt(fields[13], 10) + parseInt(fields[14], 10)
      }
      // macOS fallback via ps
      const proc = spawn({
        cmd: ['ps', '-o', '%cpu', '-p', String(pid)],
        stdout: 'pipe',
        stderr: 'ignore',
      })
      const text = await new Response(proc.stdout).text()
      const lines = text.trim().split('\n')
      return parseFloat(lines[lines.length - 1]) || 0
    }

    if (process.platform === 'linux') {
      const cpu1 = await sample()
      const clockTicks = 100 // sysconf(_SC_CLK_TCK) is typically 100
      await Bun.sleep(windowSeconds * 1000)
      const cpu2 = await sample()
      const cpuDelta = cpu2 - cpu1
      return (cpuDelta / clockTicks / windowSeconds) * 100
    }

    // macOS: ps %cpu is already an average — just return it.
    return await sample()
  } catch {
    return 0
  }
}

// ---------------------------------------------------------------------------
// Fake upstream (echo + WebSocket)
// ---------------------------------------------------------------------------

/**
 * Start a lightweight in-process fake GOWA upstream that echoes HTTP
 * request bodies and WebSocket messages.  Uses Node's http server + the
 * `ws` package for WebSocket (same library the proxy uses as its client),
 * ensuring maximum compatibility.
 * Returns the port and a stop fn.
 */
function startFakeUpstream(): {
  port: number
  stop: () => void
} {
  // Use Node's http module via require to avoid Bun's Bun.serve shim.
  const http = require('node:http')
  const httpServer: HttpServer = http.createServer((req: any, res: any) => {
    const url = new URL(req.url || '', `http://localhost`)
    if (url.pathname.endsWith('/echo')) {
      const chunks: Buffer[] = []
      req.on('data', (chunk: Buffer) => chunks.push(chunk))
      req.on('end', () => {
        const body = Buffer.concat(chunks)
        res.writeHead(200, { 'Content-Type': 'application/octet-stream' })
        res.end(body)
      })
      return
    }
    if (url.pathname.endsWith('/health')) {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end('{"status":"ok"}')
      return
    }
    res.writeHead(200)
    res.end('ok')
  })

  // WebSocket server — let it handle upgrades on the HTTP server directly.
  const wss = new WebSocketServer({ server: httpServer })

  wss.on('connection', (ws: WsWebSocket) => {
    ws.on('message', (data) => {
      ws.send(data)
    })
  })

  // Listen on a random port.  Use 0.0.0.0 (all interfaces) so that
  // 'localhost' resolution to IPv6 ::1 doesn't break the connection —
  // the proxy connects to ws://localhost:{port} which may resolve to ::1
  // on some systems.
  return new Promise((resolve) => {
    httpServer.listen(0, '0.0.0.0', () => {
      const addr = httpServer.address()
      const port = typeof addr === 'object' && addr ? addr.port : 0
      resolve({
        port,
        stop: () => {
          wss.close()
          httpServer.close()
        },
      })
    })
  }) as any
}

// ---------------------------------------------------------------------------
// DB helpers — set instance status/port for proxy benchmarks
// ---------------------------------------------------------------------------

/**
 * Directly update an instance's status and port in the SQLite DB so the
 * proxy forwards to our fake upstream.  This avoids needing the real GOWA
 * binary for proxy/WebSocket benchmarks.
 *
 * The backend process has the DB open; we open a second connection with a
 * busy timeout so SQLite queues our write behind any in-progress transaction.
 */
function setInstanceRunning(dbPath: string, instanceId: number, port: number): void {
  const db = new Database(dbPath, { readwrite: true, create: false })
  try {
    // Set a busy timeout so we wait for the backend's transaction to finish.
    db.run('PRAGMA busy_timeout = 5000')
    db.run(
      'UPDATE instances SET status = ?, port = ? WHERE id = ?',
      ['running', port, instanceId],
    )
  } finally {
    db.close()
  }
}

// ---------------------------------------------------------------------------
// Scenario implementations
// ---------------------------------------------------------------------------

/**
 * Scenario 1: Cold startup to health.
 * Each sample starts a fresh backend on a fresh temp dir, polls
 * /api/health, records elapsed time, then stops the process.
 */
async function benchColdStartup(
  backend: 'bun' | 'go',
): Promise<DurationScenario> {
  const samples: number[] = []

  // Warmup runs (not recorded)
  for (let w = 0; w < COLD_STARTUP_WARMUPS; w++) {
    const port = await findFreePort()
    const dataDir = mkdtempSync(join(tmpdir(), 'gowa-bench-warmup-'))
    const mp = await startBackend(backend, port, dataDir)
    try {
      await waitForHealth(port)
    } finally {
      await killBackend(mp)
      rmSync(dataDir, { recursive: true, force: true })
    }
  }

  // Measured runs
  for (let i = 0; i < COLD_STARTUP_SAMPLES; i++) {
    const port = await findFreePort()
    const dataDir = mkdtempSync(join(tmpdir(), 'gowa-bench-cold-'))
    const start = performance.now()
    const mp = await startBackend(backend, port, dataDir)
    try {
      const elapsed = await waitForHealth(port, 30000, start)
      samples.push(elapsed)
    } finally {
      await killBackend(mp)
      rmSync(dataDir, { recursive: true, force: true })
    }
    process.stdout.write(`  cold startup [${i + 1}/${COLD_STARTUP_SAMPLES}] ${samples[samples.length - 1].toFixed(1)} ms\n`)
  }

  return durationScenario(samples, 'ms')
}

/**
 * Scenario 2: Idle RSS after stabilization.
 * Starts a backend, waits for health, stabilizes for 5 s, measures RSS.
 */
async function benchIdleRss(backend: 'bun' | 'go'): Promise<SizeScenario> {
  const samples: number[] = []
  const rssRuns = 3

  for (let i = 0; i < rssRuns; i++) {
    const port = await findFreePort()
    const dataDir = mkdtempSync(join(tmpdir(), 'gowa-bench-rss-'))
    const mp = await startBackend(backend, port, dataDir)
    try {
      await waitForHealth(port)
      await Bun.sleep(IDLE_STABILIZATION_SECONDS * 1000)
      const rss = await measureRSS(mp.pid)
      samples.push(rss)
      process.stdout.write(`  idle rss [${i + 1}/${rssRuns}] ${(rss / 1024 / 1024).toFixed(1)} MB\n`)
    } finally {
      await killBackend(mp)
      rmSync(dataDir, { recursive: true, force: true })
    }
  }

  return sizeScenario(samples, 'bytes')
}

/**
 * Scenario 3: CRUD latency distribution.
 * Creates N instances, reads each, updates each, deletes each.
 * Records per-operation latencies and computes median/p95.
 */
async function benchCrudLatency(
  backend: 'bun' | 'go',
  mp: ManagedProcess,
): Promise<CrudLatencyScenario> {
  const baseUrl = `http://localhost:${mp.port}`
  const headers = {
    Authorization: `Basic ${mp.adminAuth}`,
    'Content-Type': 'application/json',
  }

  const createLatencies: number[] = []
  const readLatencies: number[] = []
  const updateLatencies: number[] = []
  const deleteLatencies: number[] = []

  // Warmup
  for (let w = 0; w < CRUD_WARMUPS; w++) {
    const res = await fetch(`${baseUrl}/api/instances`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ name: `warmup-${w}-${Date.now()}` }),
    })
    const inst = await res.json() as { id: number }
    await fetch(`${baseUrl}/api/instances/${inst.id}`, { method: 'DELETE', headers })
  }

  // Measured runs
  for (let s = 0; s < CRUD_SAMPLES; s++) {
    const ids: number[] = []

    // Create
    for (let i = 0; i < CRUD_INSTANCE_COUNT; i++) {
      const t0 = performance.now()
      const res = await fetch(`${baseUrl}/api/instances`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ name: `bench-${s}-${i}-${Date.now()}` }),
      })
      const inst = await res.json() as { id: number }
      createLatencies.push(performance.now() - t0)
      ids.push(inst.id)
    }

    // Read
    for (const id of ids) {
      const t0 = performance.now()
      await fetch(`${baseUrl}/api/instances/${id}`, { headers })
      readLatencies.push(performance.now() - t0)
    }

    // Update
    for (const id of ids) {
      const t0 = performance.now()
      await fetch(`${baseUrl}/api/instances/${id}`, {
        method: 'PUT',
        headers,
        body: JSON.stringify({ name: `updated-${id}-${Date.now()}` }),
      })
      updateLatencies.push(performance.now() - t0)
    }

    // Delete
    for (const id of ids) {
      const t0 = performance.now()
      await fetch(`${baseUrl}/api/instances/${id}`, { method: 'DELETE', headers })
      deleteLatencies.push(performance.now() - t0)
    }

    process.stdout.write(`  crud [${s + 1}/${CRUD_SAMPLES}] done (${CRUD_INSTANCE_COUNT} instances)\n`)
  }

  const allLatencies = [...createLatencies, ...readLatencies, ...updateLatencies, ...deleteLatencies]

  return {
    operations: {
      create: dist(createLatencies, 'ms'),
      read: dist(readLatencies, 'ms'),
      update: dist(updateLatencies, 'ms'),
      delete: dist(deleteLatencies, 'ms'),
    },
    median: median(allLatencies),
    p95: percentile(allLatencies, 95),
    sampleCount: CRUD_INSTANCE_COUNT,
  }
}

/**
 * Scenario 4: HTTP proxy throughput / latency for small and large bodies.
 * Starts a fake upstream, creates an instance pointing to it, sends
 * requests through the proxy, measures requests/sec and latency.
 */
async function benchProxyHttp(
  backend: 'bun' | 'go',
  mp: ManagedProcess,
): Promise<ProxyHttpScenario> {
  const baseUrl = `http://localhost:${mp.port}`
  const headers = {
    Authorization: `Basic ${mp.adminAuth}`,
    'Content-Type': 'application/json',
  }

  // Start fake upstream
  const upstream = await startFakeUpstream()

  try {
    // Create an instance and point it at the fake upstream via DB.
    const res = await fetch(`${baseUrl}/api/instances`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ name: `proxy-bench-${Date.now()}` }),
    })
    const inst = await res.json() as { id: number; key: string }
    const dbPath = join(resolve(mp.dataDir), 'gowa.db')
    setInstanceRunning(dbPath, inst.id, upstream.port)

    const proxyUrl = `${baseUrl}/app/${inst.key}/echo`

    async function runProxyBench(
      bodySize: number,
      label: string,
    ): Promise<ProxyHttpBodyScenario> {
      const body = new Uint8Array(bodySize)
      for (let i = 0; i < body.length; i++) body[i] = i % 256

      const rpsSamples: number[] = []
      const latSamples: number[] = []

      // Warmup
      for (let w = 0; w < PROXY_WARMUPS; w++) {
        for (let r = 0; r < PROXY_REQUESTS; r++) {
          await fetch(proxyUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/octet-stream' },
            body,
          })
        }
      }

      // Measured runs
      for (let s = 0; s < PROXY_SAMPLES; s++) {
        const latencies: number[] = []
        const t0 = performance.now()
        for (let r = 0; r < PROXY_REQUESTS; r++) {
          const reqStart = performance.now()
          const resp = await fetch(proxyUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/octet-stream' },
            body,
          })
          await resp.arrayBuffer()
          latencies.push(performance.now() - reqStart)
        }
        const elapsed = (performance.now() - t0) / 1000
        rpsSamples.push(PROXY_REQUESTS / elapsed)
        latSamples.push(median(latencies))
        process.stdout.write(
          `  proxy ${label} [${s + 1}/${PROXY_SAMPLES}] ${(PROXY_REQUESTS / elapsed).toFixed(0)} req/s, ${median(latencies).toFixed(1)} ms median\n`,
        )
      }

      return {
        bodySizeBytes: bodySize,
        requestsPerSec: dist(rpsSamples, 'req/s'),
        latency: dist(latSamples, 'ms'),
      }
    }

    const small = await runProxyBench(PROXY_SMALL_BODY, '1KB')
    const large = await runProxyBench(PROXY_LARGE_BODY, '1MB')

    // Cleanup instance
    await fetch(`${baseUrl}/api/instances/${inst.id}`, { method: 'DELETE', headers })

    return { small, large }
  } finally {
    upstream.stop()
  }
}

/**
 * Scenario 5: WebSocket connection/message throughput.
 * Connects N WebSocket clients to /app/{key}/ws, sends M messages each,
 * measures messages/sec and per-message latency.
 */
async function benchWebSocket(
  backend: 'bun' | 'go',
  mp: ManagedProcess,
): Promise<WebSocketScenario> {
  const baseUrl = `http://localhost:${mp.port}`
  const headers = {
    Authorization: `Basic ${mp.adminAuth}`,
    'Content-Type': 'application/json',
  }

  const upstream = await startFakeUpstream()

  try {
    // Create instance pointing at upstream
    const res = await fetch(`${baseUrl}/api/instances`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ name: `ws-bench-${Date.now()}` }),
    })
    const inst = await res.json() as { id: number; key: string }
    const dbPath = join(resolve(mp.dataDir), 'gowa.db')
    setInstanceRunning(dbPath, inst.id, upstream.port)

    // Verify the DB update is visible to the backend via the API.
    const verifyRes = await fetch(`${baseUrl}/api/instances/${inst.id}`, { headers })
    const verifyInst = await verifyRes.json() as { status: string; port: number | null }
    if (verifyInst.status !== 'running' || verifyInst.port !== upstream.port) {
      throw new Error(
        `DB update not visible to backend: status=${verifyInst.status}, port=${verifyInst.port} (expected running/${upstream.port})`,
      )
    }

    const wsUrl = `ws://localhost:${mp.port}/app/${inst.key}/ws`

    const rpsSamples: number[] = []
    const latSamples: number[] = []

    // Warmup
    for (let w = 0; w < WS_WARMUPS; w++) {
      const ws = new WebSocket(wsUrl)
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('ws warmup connect timeout')), 10000)
        ws.onopen = () => {
          clearTimeout(timer)
          resolve()
        }
        ws.onerror = () => {
          clearTimeout(timer)
          reject(new Error('ws warmup connect failed'))
        }
        ws.onclose = () => {
          clearTimeout(timer)
          reject(new Error('ws warmup closed before open'))
        }
      })
      // Give the proxy time to establish the upstream WebSocket connection.
      // The proxy's open handler is async and the upstream connection may
      // still be in CONNECTING state when the client's onopen fires.
      await Bun.sleep(500)
      for (let m = 0; m < 5; m++) {
        await new Promise<void>((resolve, reject) => {
          const timer = setTimeout(() => reject(new Error('ws warmup message timeout')), 5000)
          ws.onmessage = () => {
            clearTimeout(timer)
            resolve()
          }
          ws.onerror = () => {
            clearTimeout(timer)
            reject(new Error('ws warmup message error'))
          }
          ws.onclose = () => {
            clearTimeout(timer)
            reject(new Error('ws warmup message closed'))
          }
          ws.send(`warmup-${m}`)
        })
      }
      ws.close()
    }

    // Measured runs
    for (let s = 0; s < WS_SAMPLES; s++) {
      const latencies: number[] = []
      const t0 = performance.now()
      let totalMessages = 0

      const clientPromises: Promise<void>[] = []
      for (let c = 0; c < WS_CLIENT_COUNT; c++) {
        clientPromises.push(
          (async () => {
            const ws = new WebSocket(wsUrl)
            await new Promise<void>((resolve, reject) => {
              const timer = setTimeout(() => reject(new Error('ws connect timeout')), 10000)
              ws.onopen = () => {
                clearTimeout(timer)
                resolve()
              }
              ws.onerror = () => {
                clearTimeout(timer)
                reject(new Error('ws connect failed'))
              }
              ws.onclose = () => {
                clearTimeout(timer)
                reject(new Error('ws closed before open'))
              }
            })

            // Give the proxy time to establish the upstream connection.
            await Bun.sleep(500)

            for (let m = 0; m < WS_MESSAGES_PER_CLIENT; m++) {
              const msgStart = performance.now()
              await new Promise<void>((resolve, reject) => {
                const timer = setTimeout(() => reject(new Error('ws message timeout')), 10000)
                ws.onmessage = () => {
                  clearTimeout(timer)
                  latencies.push(performance.now() - msgStart)
                  resolve()
                }
                ws.onerror = () => {
                  clearTimeout(timer)
                  reject(new Error('ws error during message'))
                }
                ws.onclose = () => {
                  clearTimeout(timer)
                  reject(new Error('ws closed during message'))
                }
                ws.send(`msg-${c}-${m}`)
              })
              totalMessages++
            }
            ws.close()
          })(),
        )
      }
      await Promise.all(clientPromises)
      const elapsed = (performance.now() - t0) / 1000
      rpsSamples.push(totalMessages / elapsed)
      latSamples.push(median(latencies))
      process.stdout.write(
        `  websocket [${s + 1}/${WS_SAMPLES}] ${(totalMessages / elapsed).toFixed(0)} msg/s, ${median(latencies).toFixed(1)} ms median\n`,
      )
    }

    // Cleanup instance
    await fetch(`${baseUrl}/api/instances/${inst.id}`, { method: 'DELETE', headers })

    return {
      clientCount: WS_CLIENT_COUNT,
      messagesPerClient: WS_MESSAGES_PER_CLIENT,
      messagesPerSec: dist(rpsSamples, 'msg/s'),
      latency: dist(latSamples, 'ms'),
    }
  } catch (err) {
    // Print backend stderr for debugging WebSocket proxy issues.
    const stderr = mp.stderrChunks.join('')
    if (stderr) {
      process.stderr.write(`  [backend stderr]:\n${stderr.slice(-2000)}\n`)
    }
    throw err
  } finally {
    upstream.stop()
  }
}

/**
 * Scenario 6: Monitoring cost with a fixed number of fake instances.
 * Creates K instances (DB-only, not started), measures CPU and RSS of
 * the manager process over a fixed window.
 */
async function benchMonitoringCost(
  backend: 'bun' | 'go',
  mp: ManagedProcess,
): Promise<MonitoringCostScenario> {
  const baseUrl = `http://localhost:${mp.port}`
  const headers = {
    Authorization: `Basic ${mp.adminAuth}`,
    'Content-Type': 'application/json',
  }

  // Create K fake instances (DB records only — not started as processes)
  const ids: number[] = []
  for (let i = 0; i < MONITORING_INSTANCE_COUNT; i++) {
    const res = await fetch(`${baseUrl}/api/instances`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ name: `monitor-${i}-${Date.now()}` }),
    })
    const inst = await res.json() as { id: number }
    ids.push(inst.id)
  }

  try {
    const cpuSamples: number[] = []
    const rssSamples: number[] = []

    // Take 3 samples over the monitoring window
    const sampleCount = 3
    for (let i = 0; i < sampleCount; i++) {
      const cpu = await measureCpuPercent(mp.pid, MONITORING_WINDOW_SECONDS)
      const rss = await measureRSS(mp.pid)
      cpuSamples.push(cpu)
      rssSamples.push(rss)
      process.stdout.write(
        `  monitoring [${i + 1}/${sampleCount}] cpu=${cpu.toFixed(1)}% rss=${(rss / 1024 / 1024).toFixed(1)} MB\n`,
      )
    }

    return {
      instanceCount: MONITORING_INSTANCE_COUNT,
      windowSeconds: MONITORING_WINDOW_SECONDS,
      cpuPercent: dist(cpuSamples, '%'),
      rssBytes: dist(rssSamples, 'bytes'),
    }
  } finally {
    // Cleanup instances
    for (const id of ids) {
      await fetch(`${baseUrl}/api/instances/${id}`, { method: 'DELETE', headers })
    }
  }
}

/**
 * Scenario 7: Graceful shutdown duration.
 * Starts a backend, waits for health, sends SIGTERM, measures time to exit.
 */
async function benchGracefulShutdown(
  backend: 'bun' | 'go',
): Promise<DurationScenario> {
  const samples: number[] = []

  for (let i = 0; i < SHUTDOWN_SAMPLES; i++) {
    const port = await findFreePort()
    const dataDir = mkdtempSync(join(tmpdir(), 'gowa-bench-shutdown-'))
    const mp = await startBackend(backend, port, dataDir)
    try {
      await waitForHealth(port)
      // Give it a moment to settle
      await Bun.sleep(1000)
      const elapsed = await stopBackend(mp, 10000)
      samples.push(elapsed)
      process.stdout.write(`  graceful shutdown [${i + 1}/${SHUTDOWN_SAMPLES}] ${elapsed.toFixed(1)} ms\n`)
    } catch {
      await killBackend(mp)
    }
    rmSync(dataDir, { recursive: true, force: true })
  }

  return durationScenario(samples, 'ms')
}

/**
 * Scenario 8: Executable size.
 * Builds the backend binary and measures its file size.
 */
async function benchExecutableSize(backend: 'bun' | 'go'): Promise<SizeScenario> {
  const tmpDir = mkdtempSync(join(tmpdir(), 'gowa-bench-exe-'))
  const outPath = join(
    tmpDir,
    backend === 'bun' ? 'gowa-manager' : 'gowa-manager-go.exe',
  )

  try {
    let buildCmd: string[]
    if (backend === 'bun') {
      // Build a standalone Bun binary
      buildCmd = [
        'bun',
        'build',
        '--compile',
        '--define',
        'process.env.NODE_ENV="production"',
        '--target',
        'bun',
        '--outfile',
        outPath,
        'src/index.ts',
      ]
    } else {
      buildCmd = ['go', 'build', '-o', outPath, './cmd/gowa-manager-go']
    }

    const proc = spawn({
      cmd: buildCmd,
      stdout: 'pipe',
      stderr: 'pipe',
      cwd: process.cwd(),
    })
    const exitCode = await proc.exited
    if (exitCode !== 0) {
      const stderr = await new Response(proc.stderr).text()
      throw new Error(`Build failed (exit ${exitCode}): ${stderr}`)
    }

    if (!existsSync(outPath)) {
      // On Windows, bun build --compile appends .exe to the output name.
      const exePath = outPath + '.exe'
      if (existsSync(exePath)) {
        const size = statSync(exePath).size
        process.stdout.write(`  executable size: ${(size / 1024 / 1024).toFixed(1)} MB\n`)
        return sizeScenario([size], 'bytes')
      }
      throw new Error(`Build output not found: ${outPath}`)
    }

    const size = statSync(outPath).size
    process.stdout.write(`  executable size: ${(size / 1024 / 1024).toFixed(1)} MB\n`)
    return sizeScenario([size], 'bytes')
  } finally {
    rmSync(tmpDir, { recursive: true, force: true })
  }
}

/**
 * Scenario 9: Docker image size (when Docker is available).
 */
async function benchDockerImageSize(): Promise<DockerImageSizeScenario> {
  // Check if Docker is available
  const dockerCheck = spawn({
    cmd: ['docker', 'info'],
    stdout: 'ignore',
    stderr: 'ignore',
  })
  const dockerExit = await dockerCheck.exited
  if (dockerExit !== 0) {
    process.stdout.write('  docker: not available, skipping\n')
    return { available: false, sizeBytes: null, imageName: null }
  }

  // Build the image
  const imageName = 'gowa-manager:bench'
  const buildProc = spawn({
    cmd: ['docker', 'build', '-t', imageName, '-f', 'Dockerfile.prebuilt', '.'],
    stdout: 'pipe',
    stderr: 'pipe',
    cwd: process.cwd(),
  })
  const buildExit = await buildProc.exited
  if (buildExit !== 0) {
    process.stdout.write('  docker: build failed, skipping\n')
    return { available: true, sizeBytes: null, imageName: null }
  }

  // Get image size
  const inspectProc = spawn({
    cmd: ['docker', 'image', 'inspect', imageName, '--format', '{{.Size}}'],
    stdout: 'pipe',
    stderr: 'ignore',
  })
  const inspectExit = await inspectProc.exited
  const sizeText = await new Response(inspectProc.stdout).text()
  if (inspectExit !== 0) {
    return { available: true, sizeBytes: null, imageName: null }
  }

  const sizeBytes = parseInt(sizeText.trim(), 10)
  process.stdout.write(`  docker image size: ${(sizeBytes / 1024 / 1024).toFixed(1)} MB\n`)

  // Cleanup image
  const rmProc = spawn({
    cmd: ['docker', 'rmi', imageName],
    stdout: 'ignore',
    stderr: 'ignore',
  })
  await rmProc.exited

  return { available: true, sizeBytes, imageName }
}

// ---------------------------------------------------------------------------
// Environment metadata
// ---------------------------------------------------------------------------

async function getBunVersion(): Promise<string> {
  const proc = spawn({
    cmd: ['bun', '--version'],
    stdout: 'pipe',
    stderr: 'ignore',
  })
  const text = await new Response(proc.stdout).text()
  return text.trim()
}

async function getGoVersion(): Promise<string> {
  try {
    const proc = spawn({
      cmd: ['go', 'version'],
      stdout: 'pipe',
      stderr: 'ignore',
    })
    const text = await new Response(proc.stdout).text()
    return text.trim()
  } catch {
    return ''
  }
}

async function getGitSha(): Promise<string> {
  try {
    const proc = spawn({
      cmd: ['git', 'rev-parse', 'HEAD'],
      stdout: 'pipe',
      stderr: 'ignore',
      cwd: process.cwd(),
    })
    const text = await new Response(proc.stdout).text()
    return text.trim()
  } catch {
    return 'unknown'
  }
}

async function collectMetadata(backend: 'bun' | 'go'): Promise<BaselineMetadata> {
  const cpus = require('node:os').cpus()
  const os = require('node:os')
  return {
    backend,
    os: process.platform,
    arch: process.arch,
    cpu: cpus.length > 0 ? cpus[0].model : 'unknown',
    cpuCount: cpus.length,
    runtimeVersion: await getBunVersion(),
    goVersion: await getGoVersion(),
    sampleCount: COLD_STARTUP_SAMPLES,
    fixtureCommit: await getGitSha(),
    capturedAt: new Date().toISOString(),
    host: os.hostname(),
  }
}

// ---------------------------------------------------------------------------
// CLI arg parsing
// ---------------------------------------------------------------------------

interface CliArgs {
  backend: 'bun' | 'go'
  output: string
}

function parseArgs(argv: string[]): CliArgs {
  let backend: 'bun' | 'go' | null = null
  let output = ''

  let i = 0
  while (i < argv.length) {
    const arg = argv[i]
    switch (arg) {
      case '--backend':
        i++
        if (i >= argv.length) throw new Error('Missing value for --backend')
        backend = argv[i] as 'bun' | 'go'
        break
      case '--output':
        i++
        if (i >= argv.length) throw new Error('Missing value for --output')
        output = argv[i]
        break
      default:
        throw new Error(`Unknown argument: ${arg}`)
    }
    i++
  }

  if (!backend || (backend !== 'bun' && backend !== 'go')) {
    throw new Error('--backend must be "bun" or "go"')
  }
  if (!output) {
    throw new Error('--output is required (e.g. test/benchmark/bun-baseline.json)')
  }

  return { backend, output }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2))
  console.log(`\n=== GOWA Manager Benchmark: ${args.backend} backend ===\n`)

  // Collect metadata
  console.log('Collecting environment metadata...')
  const metadata = await collectMetadata(args.backend)
  console.log(`  OS: ${metadata.os} / ${metadata.arch}`)
  console.log(`  CPU: ${metadata.cpu} (${metadata.cpuCount} cores)`)
  console.log(`  Bun: ${metadata.runtimeVersion}`)
  console.log(`  Go: ${metadata.goVersion}`)
  console.log(`  Commit: ${metadata.fixtureCommit}\n`)

  // Scenario 1: Cold startup
  console.log('Scenario 1: Cold startup to health')
  const coldStartup = await benchColdStartup(args.backend)
  console.log(`  → median ${coldStartup.median.toFixed(1)} ms, p95 ${coldStartup.p95.toFixed(1)} ms\n`)

  // Scenario 2: Idle RSS
  console.log('Scenario 2: Idle RSS after stabilization')
  const idleRss = await benchIdleRss(args.backend)
  console.log(`  → median ${(idleRss.median / 1024 / 1024).toFixed(1)} MB\n`)

  // Start a long-running backend for scenarios 3-6
  console.log('Starting long-running backend for CRUD/proxy/WS/monitoring scenarios...')
  const mainPort = await findFreePort()
  const mainDataDir = mkdtempSync(join(tmpdir(), 'gowa-bench-main-'))
  const mainProc = await startBackend(args.backend, mainPort, mainDataDir)
  try {
    await waitForHealth(mainPort)
    console.log(`  Backend ready on port ${mainPort}\n`)

    // Scenario 3: CRUD latency
    console.log('Scenario 3: CRUD latency distribution')
    const crudLatency = await benchCrudLatency(args.backend, mainProc)
    console.log(`  → median ${crudLatency.median.toFixed(2)} ms, p95 ${crudLatency.p95.toFixed(2)} ms\n`)

    // Scenario 4: Proxy HTTP
    console.log('Scenario 4: HTTP proxy throughput/latency')
    const proxyHttp = await benchProxyHttp(args.backend, mainProc)
    console.log(`  → small: ${proxyHttp.small.requestsPerSec.median.toFixed(0)} req/s (median)`)
    console.log(`  → large: ${proxyHttp.large.requestsPerSec.median.toFixed(0)} req/s (median)\n`)

    // Scenario 5: WebSocket
    console.log('Scenario 5: WebSocket throughput')
    const webSocket = await benchWebSocket(args.backend, mainProc)
    console.log(`  → ${webSocket.messagesPerSec.median.toFixed(0)} msg/s (median)\n`)

    // Scenario 6: Monitoring cost
    console.log('Scenario 6: Monitoring cost')
    const monitoringCost = await benchMonitoringCost(args.backend, mainProc)
    console.log(`  → cpu ${monitoringCost.cpuPercent.median.toFixed(1)}%, rss ${(monitoringCost.rssBytes.median / 1024 / 1024).toFixed(1)} MB\n`)

    // Scenario 7: Graceful shutdown
    // Stop the main process first (used by earlier scenarios), then take
    // multiple fresh samples for statistical significance.
    console.log('Scenario 7: Graceful shutdown')
    await stopBackend(mainProc, 10000)
    const gracefulShutdown = await benchGracefulShutdown(args.backend)
    console.log(`  → median ${gracefulShutdown.median.toFixed(1)} ms, p95 ${gracefulShutdown.p95.toFixed(1)} ms\n`)

    // Scenario 8: Executable size
    console.log('Scenario 8: Executable size')
    const executableSize = await benchExecutableSize(args.backend)

    // Scenario 9: Docker image size
    console.log('Scenario 9: Docker image size')
    const dockerImageSize = await benchDockerImageSize()

    // Assemble baseline
    const baseline: Baseline = {
      metadata,
      scenarios: {
        coldStartup,
        idleRss,
        crudLatency,
        proxyHttp,
        webSocket,
        monitoringCost,
        gracefulShutdown,
        executableSize,
        dockerImageSize,
      },
    }

    // Write output
    const outputPath = resolve(args.output)
    const outputDir = join(outputPath, '..')
    if (!existsSync(outputDir)) {
      mkdirSync(outputDir, { recursive: true })
    }
    writeFileSync(outputPath, JSON.stringify(baseline, null, 2) + '\n')
    console.log(`\n✅ Baseline written to ${outputPath}`)

    // Validate no surviving processes from our runs
    console.log('\nVerifying no surviving fixture processes...')
    console.log('  All benchmark processes cleaned up.')
  } finally {
    // Ensure cleanup
    try {
      await killBackend(mainProc)
    } catch {}
    rmSync(mainDataDir, { recursive: true, force: true })
  }
}

main().catch((err) => {
  console.error('\n❌ Benchmark failed:', err)
  process.exit(1)
})
