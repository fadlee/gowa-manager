import { afterEach, describe, expect, mock, spyOn, test } from 'bun:test'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { VersionManager } from './version-manager'

const dataDir = process.env.DATA_DIR!
const versionsDir = join(dataDir, 'bin', 'versions')
const binaryName = process.platform === 'win32' ? 'gowa.exe' : 'gowa'

function createVersion(version: string, content = 'binary') {
  const dir = join(versionsDir, version)
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, binaryName), content)
}

describe('VersionManager', () => {
  afterEach(() => {
    mock.restore()
    rmSync(join(dataDir, 'bin'), { recursive: true, force: true })
  })

  test('returns legacy latest binary path when no versions are installed', () => {
    expect(VersionManager.getVersionBinaryPath('latest')).toBe(join(dataDir, 'bin', binaryName))
  })

  test('resolves latest to highest installed version path', () => {
    createVersion('v1.0.0')
    createVersion('v1.10.0')
    createVersion('v1.2.0')

    expect(VersionManager.getVersionBinaryPath('latest')).toBe(join(versionsDir, 'v1.10.0', binaryName))
  })

  test('returns explicit version binary path', () => {
    expect(VersionManager.getVersionBinaryPath('v8.7.0')).toBe(join(versionsDir, 'v8.7.0', binaryName))
  })

  test('lists installed versions with latest marker', async () => {
    createVersion('v1.0.0')
    createVersion('v2.0.0')

    const versions = await VersionManager.getInstalledVersions()

    expect(versions.map((version) => version.version).sort()).toEqual(['v1.0.0', 'v2.0.0'])
    expect(versions.find((version) => version.version === 'v2.0.0')?.isLatest).toBe(true)
    expect(versions.every((version) => version.installed)).toBe(true)
  })

  test('returns available GitHub versions with installed metadata', async () => {
    createVersion('v2.0.0', 'installed-binary')
    const fetchMock = spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ([
        { tag_name: 'v3.0.0', published_at: '2024-01-01T00:00:00Z', assets: [] },
        { tag_name: 'v2.0.0', published_at: '2023-12-01T00:00:00Z', assets: [] },
      ]),
    } as any)

    const versions = await VersionManager.getAvailableVersions(2)

    expect(versions.map((version) => version.version)).toEqual(['latest', 'v3.0.0', 'v2.0.0'])
    expect(versions[0]?.isLatest).toBe(true)
    expect(versions[0]?.installed).toBe(false)
    expect(versions[2]).toMatchObject({ version: 'v2.0.0', installed: true })

    fetchMock.mockRestore()
  })

  test('delegates installVersion to binary download helper', async () => {
    const downloadSpecificVersion = mock(() => Promise.resolve())
    mock.module('../../binary-download', () => ({ downloadSpecificVersion }))

    await VersionManager.installVersion('v9.9.9')

    expect(downloadSpecificVersion).toHaveBeenCalledWith('v9.9.9')
  })

  test('checks explicit version availability by binary existence', async () => {
    createVersion('v3.0.0')

    expect(await VersionManager.isVersionAvailable('v3.0.0')).toBe(true)
    expect(await VersionManager.isVersionAvailable('missing')).toBe(false)
  })

  test('removes installed version directory', async () => {
    createVersion('v4.0.0')

    await VersionManager.removeVersion('v4.0.0')

    expect(existsSync(join(versionsDir, 'v4.0.0'))).toBe(false)
  })
})
