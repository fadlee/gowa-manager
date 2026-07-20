/**
 * Release artifact verifier for the Go backend.
 *
 * For each binary in dist-go/:
 *   - Verifies the filename matches an expected platform pattern.
 *   - Runs `--version` and checks the output contains "GOWA Manager".
 *   - Runs `--help` and checks the output contains usage text.
 *   - Checks for an embedded SPA marker (a known string from the frontend).
 *   - Verifies the SHA-256 checksum against dist-go/checksums-go.txt.
 *
 * Native smoke test:
 *   - Only for the binary matching the current host platform.
 *   - Starts the binary on a temp port, hits /api/health, expects 200,
 *     then shuts it down.
 *
 * Emits a JSON report on stdout and exits non-zero on any failure.
 */

import { readFile, readdir, access } from 'node:fs/promises'
import { join } from 'node:path'
import { createHash } from 'node:crypto'
import { existsSync } from 'node:fs'

const DIST_DIR = 'dist-go'
const CHECKSUMS_FILE = join(DIST_DIR, 'checksums-go.txt')

// A stable string present in the embedded SPA index.html. The built frontend
// always contains the root mount point and the Gowa Manager title.
const SPA_MARKER = 'id="root"'

interface ExpectedTarget {
  pattern: RegExp
  goos: string
  goarch: string
  outfile: string
}

const TARGETS: ExpectedTarget[] = [
  { pattern: /^gowa-manager-go-linux-amd64$/, goos: 'linux', goarch: 'amd64', outfile: 'gowa-manager-go-linux-amd64' },
  { pattern: /^gowa-manager-go-linux-arm64$/, goos: 'linux', goarch: 'arm64', outfile: 'gowa-manager-go-linux-arm64' },
  { pattern: /^gowa-manager-go-windows-amd64\.exe$/, goos: 'windows', goarch: 'amd64', outfile: 'gowa-manager-go-windows-amd64.exe' },
]

interface CheckResult {
  file: string
  filenameValid: boolean
  versionOk: boolean | null
  helpOk: boolean | null
  spaMarkerOk: boolean
  checksumOk: boolean
  native: boolean
  errors: string[]
}

interface SmokeResult {
  file: string
  ran: boolean
  healthOk: boolean
  error?: string
}

interface Report {
  version: string
  results: CheckResult[]
  smoke: SmokeResult[]
  success: boolean
}

function parseChecksums(content: string): Map<string, string> {
  const map = new Map<string, string>()
  for (const line of content.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    // Format: "<sha256>  <filename>"
    const match = trimmed.match(/^([0-9a-f]{64})\s+\*?(.+)$/)
    if (match) {
      map.set(match[2], match[1])
    }
  }
  return map
}

async function sha256(file: string): Promise<string> {
  const data = await readFile(file)
  return createHash('sha256').update(data).digest('hex')
}

async function runCommand(bin: string, args: string[]): Promise<{ code: number; stdout: string; stderr: string }> {
  const proc = Bun.spawn([bin, ...args], { stdout: 'pipe', stderr: 'pipe' })
  const [stdout, stderr, code] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ])
  return { code, stdout, stderr }
}

function fileContainsString(file: string, needle: string): Promise<boolean> {
  // Read as binary and search for the UTF-8 marker. The embedded SPA HTML is
  // stored verbatim in the Go embed FS, so the marker survives compilation.
  return readFile(file).then((buf) => buf.includes(Buffer.from(needle, 'utf8')))
}

function nativeTarget(): ExpectedTarget | undefined {
  const goos = process.platform === 'win32' ? 'windows' : process.platform === 'darwin' ? 'darwin' : 'linux'
  const arch = process.arch
  const goarch = arch === 'arm64' ? 'arm64' : 'amd64'
  return TARGETS.find((t) => t.goos === goos && t.goarch === goarch)
}

async function freePort(): Promise<number> {
  // Listen on an ephemeral port then close to discover a free one.
  const { Server } = await import('node:net')
  return new Promise((resolve, reject) => {
    const srv = new Server()
    srv.unref()
    srv.on('error', reject)
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address()
      if (addr && typeof addr === 'object') {
        const port = addr.port
        srv.close(() => resolve(port))
      } else {
        srv.close()
        reject(new Error('could not determine free port'))
      }
    })
  })
}

async function smokeTest(bin: string): Promise<SmokeResult> {
  const port = await freePort()
  const dataDir = join(DIST_DIR, `_smoke-${port}`)
  const env = {
    ...process.env,
    PORT: String(port),
    DATA_DIR: dataDir,
    ADMIN_USERNAME: 'smoke',
    ADMIN_PASSWORD: 'smokepass',
  }
  let proc: ReturnType<typeof Bun.spawn> | undefined
  try {
    proc = Bun.spawn([bin, '--port', String(port), '--data-dir', dataDir], {
      stdout: 'pipe',
      stderr: 'pipe',
      env,
    })
    // Poll /api/health for up to ~10s.
    const deadline = Date.now() + 10_000
    let ok = false
    let lastErr = ''
    while (Date.now() < deadline) {
      try {
        const res = await fetch(`http://127.0.0.1:${port}/api/health`)
        if (res.ok) {
          ok = true
          break
        }
      } catch (e) {
        lastErr = String(e)
      }
      await Bun.sleep(200)
    }
    if (!ok) {
      return { file: bin, ran: true, healthOk: false, error: `health endpoint never returned 200 (${lastErr})` }
    }
    return { file: bin, ran: true, healthOk: true }
  } finally {
    if (proc) {
      try { proc.kill() } catch {}
      try { await proc.exited } catch {}
    }
  }
}

async function main() {
  if (!existsSync(CHECKSUMS_FILE)) {
    console.error(`Missing ${CHECKSUMS_FILE}; run build-go.ts first.`)
    process.exit(1)
  }
  const checksums = parseChecksums(await readFile(CHECKSUMS_FILE, 'utf8'))
  const entries = await readdir(DIST_DIR)
  const binaries = entries.filter((f) => f.startsWith('gowa-manager-') && !f.endsWith('.txt'))

  const results: CheckResult[] = []
  const native = nativeTarget()
  for (const name of binaries) {
    const file = join(DIST_DIR, name)
    const errors: string[] = []
    const target = TARGETS.find((t) => t.pattern.test(name))
    const filenameValid = !!target
    if (!filenameValid) errors.push(`filename ${name} does not match any expected target pattern`)

    // Determine whether this binary is native to the current host. We only
    // run --version/--help for native binaries; cross-compiled binaries
    // cannot be executed on this host.
    const isNative = !!target && !!native && target.goos === native.goos && target.goarch === native.goarch

    // --version (native only)
    let versionOk: boolean | null = null
    if (isNative) {
      try {
        const { stdout, code } = await runCommand(file, ['--version'])
        versionOk = code === 0 && stdout.includes('GOWA Manager')
        if (!versionOk) errors.push(`--version output unexpected: ${stdout.trim()}`)
      } catch (e) {
        errors.push(`--version failed: ${e}`)
      }
    }

    // --help (native only)
    let helpOk: boolean | null = null
    if (isNative) {
      try {
        const { stdout, code } = await runCommand(file, ['--help'])
        helpOk = code === 0 && stdout.includes('USAGE')
        if (!helpOk) errors.push(`--help output unexpected`)
      } catch (e) {
        errors.push(`--help failed: ${e}`)
      }
    }

    // embedded SPA marker
    let spaMarkerOk = false
    try {
      spaMarkerOk = await fileContainsString(file, SPA_MARKER)
      if (!spaMarkerOk) errors.push(`embedded SPA marker "${SPA_MARKER}" not found in binary`)
    } catch (e) {
      errors.push(`SPA marker check failed: ${e}`)
    }

    // checksum
    let checksumOk = false
    try {
      const actual = await sha256(file)
      const expected = checksums.get(name)
      checksumOk = !!expected && actual === expected
      if (!checksumOk) errors.push(`checksum mismatch for ${name}`)
    } catch (e) {
      errors.push(`checksum verification failed: ${e}`)
    }

    results.push({ file: name, filenameValid, versionOk, helpOk, spaMarkerOk, checksumOk, native: isNative, errors })
  }

  // Native smoke test
  const smoke: SmokeResult[] = []
  if (native) {
    const nativeBin = entries.find((f) => f === native.outfile)
    if (nativeBin) {
      console.log(`▶ Native smoke test for ${nativeBin}...`)
      smoke.push(await smokeTest(join(DIST_DIR, nativeBin)))
    } else {
      console.log(`ℹ No native binary for ${native.goos}/${native.goarch}; skipping smoke test.`)
    }
  }

  const success = results.every((r) =>
    r.filenameValid
    && r.spaMarkerOk
    && r.checksumOk
    && (r.versionOk === null || r.versionOk)
    && (r.helpOk === null || r.helpOk)
  ) && smoke.every((s) => !s.ran || s.healthOk)

  const report: Report = { version: 'release', results, smoke, success }
  console.log(JSON.stringify(report, null, 2))
  if (!success) process.exit(1)
}

await main()
