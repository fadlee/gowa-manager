/**
 * Release build orchestrator for the Go backend.
 *
 * Builds the production React SPA once from locked dependencies, embeds it
 * into the Go static FS, then cross-compiles three release binaries:
 *   - gowa-manager-linux-amd64
 *   - gowa-manager-linux-arm64
 *   - gowa-manager-windows-amd64.exe
 *
 * Reproducibility:
 *   - The frontend is built once and shared across all targets.
 *   - SOURCE_DATE_EPOCH (when set) is forwarded to `go build` so the
 *     compiler embeds a deterministic build timestamp. When unset, no
 *     timestamp is injected (the buildinfo package only carries Version).
 *   - Symbols are NOT stripped; the owner has not approved that trade-off.
 *
 * Outputs:
 *   - dist-go/gowa-manager-linux-amd64
 *   - dist-go/gowa-manager-linux-arm64
 *   - dist-go/gowa-manager-windows-amd64.exe
 *   - dist-go/checksums.txt   (SHA-256 manifest)
 */

import { mkdir, rm, cp, writeFile, readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { createHash } from 'node:crypto'
import packageJson from '../../package.json'

const VERSION = packageJson.version || 'dev'
const DIST_DIR = 'dist-go'
const STATIC_WEB = join('internal', 'static', 'web')

interface Target {
  goos: string
  goarch: string
  outfile: string
}

const TARGETS: Target[] = [
  { goos: 'linux', goarch: 'amd64', outfile: 'gowa-manager-linux-amd64' },
  { goos: 'linux', goarch: 'arm64', outfile: 'gowa-manager-linux-arm64' },
  { goos: 'windows', goarch: 'amd64', outfile: 'gowa-manager-windows-amd64.exe' },
]

async function run(command: string[], env?: Record<string, string>, cwd = process.cwd()) {
  const proc = Bun.spawn(command, { cwd, stdout: 'inherit', stderr: 'inherit', env: { ...process.env, ...env } })
  const code = await proc.exited
  if (code !== 0) {
    throw new Error(`${command.join(' ')} exited with code ${code}`)
  }
}

async function sha256(file: string): Promise<string> {
  const data = await readFile(file)
  return createHash('sha256').update(data).digest('hex')
}

async function buildFrontendOnce() {
  console.log('▶ Building frontend (once, shared across targets)...')
  await run(['bun', 'run', 'build:client'])
  await rm(STATIC_WEB, { recursive: true, force: true })
  await mkdir(STATIC_WEB, { recursive: true })
  await cp('public', STATIC_WEB, { recursive: true })
  await writeFile(join(STATIC_WEB, '.gitkeep'), '')
  console.log('✔ Frontend embedded into internal/static/web/')
}

function ldflags(): string {
  // Only inject the version. We deliberately omit a build timestamp unless
  // SOURCE_DATE_EPOCH is provided, keeping builds reproducible by default.
  const flags = [`-X github.com/fadlee/gowa-manager/internal/buildinfo.Version=${VERSION}`]
  return flags.join(' ')
}

function buildEnv(target: Target): Record<string, string> {
  const env: Record<string, string> = {
    GOOS: target.goos,
    GOARCH: target.goarch,
    CGO_ENABLED: '0',
  }
  // Forward SOURCE_DATE_EPOCH for reproducible timestamps when available.
  if (process.env.SOURCE_DATE_EPOCH) {
    env.SOURCE_DATE_EPOCH = process.env.SOURCE_DATE_EPOCH
  }
  return env
}

async function buildTarget(target: Target) {
  const outfile = join(DIST_DIR, target.outfile)
  console.log(`▶ Building ${target.outfile} (${target.goos}/${target.goarch})...`)
  await run(
    ['go', 'build', '-ldflags', ldflags(), '-trimpath', '-o', outfile, './cmd/gowa-manager-go'],
    buildEnv(target),
  )
  console.log(`✔ ${target.outfile}`)
}

async function writeChecksums(files: string[]) {
  const lines: string[] = []
  for (const file of files) {
    const hash = await sha256(file)
    const name = file.split(/[\\/]/).pop()!
    lines.push(`${hash}  ${name}`)
  }
  const manifest = lines.join('\n') + '\n'
  const manifestPath = join(DIST_DIR, 'checksums.txt')
  await writeFile(manifestPath, manifest)
  console.log('✔ checksums.txt')
  console.log(manifest)
}

async function main() {
  console.log(`=== Go release build (version ${VERSION}) ===`)
  await rm(DIST_DIR, { recursive: true, force: true })
  await mkdir(DIST_DIR, { recursive: true })

  await buildFrontendOnce()

  for (const target of TARGETS) {
    await buildTarget(target)
  }

  const built = await readdir(DIST_DIR)
  const binaries = built
    .filter((f) => f.startsWith('gowa-manager-'))
    .map((f) => join(DIST_DIR, f))
  await writeChecksums(binaries)

  console.log('=== Go release build complete ===')
  console.log(`Artifacts in ${DIST_DIR}/:`)
  for (const f of built) console.log(`  ${f}`)
}

await main()
