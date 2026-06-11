import { afterEach, describe, expect, test } from 'bun:test'
import { existsSync, rmSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { DirectoryManager } from './directory-manager'

const originalDataDir = process.env.DATA_DIR
const testDir = resolve(join('.test-data', `directory-manager-${process.pid}`))

describe('DirectoryManager', () => {
  afterEach(() => {
    process.env.DATA_DIR = originalDataDir
    rmSync(testDir, { recursive: true, force: true })
  })

  test('creates instance directory inside DATA_DIR', () => {
    process.env.DATA_DIR = testDir

    const instanceDir = DirectoryManager.createInstanceDirectory(42)

    expect(instanceDir).toBe(join(testDir, 'instances', '42'))
    expect(existsSync(instanceDir)).toBe(true)
  })

  test('returns instance directory path without creating it', () => {
    process.env.DATA_DIR = testDir

    const instanceDir = DirectoryManager.getInstanceDirectory(43)

    expect(instanceDir).toBe(join(testDir, 'instances', '43'))
    expect(existsSync(instanceDir)).toBe(false)
  })

  test('ensures instances root directory exists', () => {
    process.env.DATA_DIR = testDir

    const rootDir = DirectoryManager.ensureInstancesRootDirectory()

    expect(rootDir).toBe(join(testDir, 'instances'))
    expect(existsSync(rootDir)).toBe(true)
  })

  test('returns instances root directory path', () => {
    process.env.DATA_DIR = testDir

    expect(DirectoryManager.getInstancesRootDirectory()).toBe(join(testDir, 'instances'))
  })

  test('cleans up instance directory recursively', () => {
    process.env.DATA_DIR = testDir
    const instanceDir = DirectoryManager.createInstanceDirectory(44)
    writeFileSync(join(instanceDir, 'test.txt'), 'hello')

    DirectoryManager.cleanupInstanceDirectory(44)

    expect(existsSync(instanceDir)).toBe(false)
  })

  test('cleanup is safe when instance directory does not exist', () => {
    process.env.DATA_DIR = testDir

    expect(() => DirectoryManager.cleanupInstanceDirectory(45)).not.toThrow()
  })
})
