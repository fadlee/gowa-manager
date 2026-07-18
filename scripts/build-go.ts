import { mkdir, rm, cp, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import packageJson from '../package.json'

const version = packageJson.version || 'dev'

async function run(command: string[], cwd = process.cwd()) {
  const proc = Bun.spawn(command, { cwd, stdout: 'inherit', stderr: 'inherit' })
  const code = await proc.exited
  if (code !== 0) {
    throw new Error(`${command.join(' ')} exited with code ${code}`)
  }
}

await run(['bun', 'run', 'build:client'])
await rm(join('internal', 'static', 'web'), { recursive: true, force: true })
await mkdir(join('internal', 'static', 'web'), { recursive: true })
await cp('public', join('internal', 'static', 'web'), { recursive: true })
await writeFile(join('internal', 'static', 'web', '.gitkeep'), '')
await mkdir('dist-go', { recursive: true })

const outfile = process.platform === 'win32' ? join('dist-go', 'gowa-manager-go.exe') : join('dist-go', 'gowa-manager-go')
await run([
  'go',
  'build',
  '-ldflags',
  `-X github.com/fadlee/gowa-manager/internal/buildinfo.Version=${version}`,
  '-o',
  outfile,
  './cmd/gowa-manager-go'
])
