import { mkdirSync, rmSync } from 'node:fs'
import { join, resolve } from 'node:path'

// Shared Bun test setup.
// Keep this file lightweight so backend unit tests can run without app bootstrapping.
const testDataDir = resolve(join('.test-data', `bun-${process.pid}`))

rmSync(testDataDir, { recursive: true, force: true })
mkdirSync(testDataDir, { recursive: true })

process.env.DATA_DIR = testDataDir

const originalConsoleLog = console.log.bind(console)
const originalConsoleError = console.error.bind(console)
const noisyLogPrefixes = [
  'Database initialized successfully',
  'Added error_message column to existing instances table',
  'Added gowa_version column to existing instances table',
  '🗑️  Cleaned up instance directory:',
  'Port ',
  'Instance ',
  'Starting instance ',
]

const noisyErrorPrefixes = [
  'Failed to start instance ',
]

console.log = (...args: unknown[]) => {
  const first = String(args[0] ?? '')
  if (noisyLogPrefixes.some((prefix) => first.startsWith(prefix))) return
  originalConsoleLog(...args)
}

console.error = (...args: unknown[]) => {
  const first = String(args[0] ?? '')
  if (noisyErrorPrefixes.some((prefix) => first.startsWith(prefix))) return
  originalConsoleError(...args)
}

process.on('exit', () => {
  rmSync(testDataDir, { recursive: true, force: true })
})
