import { mkdirSync, rmSync } from 'node:fs'
import { join, resolve } from 'node:path'

// Shared Bun test setup.
// Keep this file lightweight so backend unit tests can run without app bootstrapping.
const testDataDir = resolve(join('.test-data', `bun-${process.pid}`))

rmSync(testDataDir, { recursive: true, force: true })
mkdirSync(testDataDir, { recursive: true })

process.env.DATA_DIR = testDataDir

process.on('exit', () => {
  rmSync(testDataDir, { recursive: true, force: true })
})
