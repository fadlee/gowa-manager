// One-command local development: builds and runs the Go backend, and runs
// the Vite dev server (hot reload) that proxies /api and /app to it.
//
//   bun run dev                 # http://localhost:5173, login admin / password123
//   bun run dev -- --verbose    # extra args go to the backend
//
// Data lives in .contract-tmp/dev (gitignored); delete it to start fresh.
// Restart after changing Go code (Ctrl+C, then `bun run dev` again).
import { mkdir } from 'node:fs/promises'
import { createServer } from 'node:net'
import { join } from 'node:path'

const root = join(import.meta.dir, '..')
const devDir = join(root, '.contract-tmp', 'dev')
const binary = join(devDir, process.platform === 'win32' ? 'gowa-manager.exe' : 'gowa-manager')
const username = process.env.ADMIN_USERNAME || 'admin'
const password = process.env.ADMIN_PASSWORD || 'password123'

function portIsFree(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const server = createServer()
    server.once('error', () => resolve(false))
    server.listen(port, '127.0.0.1', () => server.close(() => resolve(true)))
  })
}

async function firstFreePort(start: number): Promise<number> {
  for (let port = start; port < start + 100; port++) {
    if (await portIsFree(port)) return port
  }
  throw new Error(`no free port from ${start} to ${start + 99}`)
}

// pipe prefixes each output line so backend and frontend logs stay readable.
async function pipe(stream: ReadableStream<Uint8Array>, prefix: string, out: NodeJS.WriteStream) {
  const decoder = new TextDecoder()
  let buffered = ''
  for await (const chunk of stream) {
    buffered += decoder.decode(chunk, { stream: true })
    const lines = buffered.split('\n')
    buffered = lines.pop() ?? ''
    for (const line of lines) out.write(`${prefix} ${line}\n`)
  }
  if (buffered) out.write(`${prefix} ${buffered}\n`)
}

await mkdir(devDir, { recursive: true })

console.log('Building backend...')
const build = Bun.spawnSync(['go', 'build', '-o', binary, './cmd/gowa-manager-go'], { cwd: root, stdout: 'inherit', stderr: 'inherit' })
if (build.exitCode !== 0) process.exit(build.exitCode ?? 1)

const apiPort = await firstFreePort(3000)
const apiURL = `http://127.0.0.1:${apiPort}`

const backend = Bun.spawn([binary, '--port', String(apiPort), '--data-dir', join(devDir, 'data'), ...process.argv.slice(2)], {
  cwd: root,
  env: { ...process.env, ADMIN_USERNAME: username, ADMIN_PASSWORD: password },
  stdout: 'pipe',
  stderr: 'pipe',
})
const frontend = Bun.spawn(['bun', 'run', 'dev'], {
  cwd: join(root, 'client'),
  env: { ...process.env, GOWA_DEV_API_URL: apiURL },
  stdout: 'pipe',
  stderr: 'pipe',
})

void pipe(backend.stdout, '[api]', process.stdout)
void pipe(backend.stderr, '[api]', process.stderr)
void pipe(frontend.stdout, '[web]', process.stdout)
void pipe(frontend.stderr, '[web]', process.stderr)

console.log(`\nGOWA Manager dev: open the [web] Local URL below, usually http://localhost:5173 (API on ${apiURL})`)
console.log(`Login: ${username} / ${password} (created only while the dev database has no users)\n`)

let stopping = false
function stop(code: number) {
  if (stopping) return
  stopping = true
  backend.kill()
  frontend.kill()
  void Promise.all([backend.exited, frontend.exited]).then(() => process.exit(code))
}

process.on('SIGINT', () => stop(0))
process.on('SIGTERM', () => stop(0))
// If either side dies, stop the other so nothing is left running.
void backend.exited.then((code) => stop(code ?? 1))
void frontend.exited.then((code) => stop(code ?? 1))
