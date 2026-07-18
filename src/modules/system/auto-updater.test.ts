import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from 'bun:test'
import { AutoUpdater } from './auto-updater'
import { VersionManager } from './version-manager'
import { InstanceService } from '../instances/service'
import { queries } from '../../db'

// --- Spies -----------------------------------------------------------------

// Silence console output and allow asserting on log/error calls.
const consoleLogSpy = spyOn(console, 'log').mockImplementation(() => {})
const consoleErrorSpy = spyOn(console, 'error').mockImplementation(() => {})

// Clear spy call history before each test so call-count assertions only
// reflect calls made within the current test.
beforeEach(() => {
  consoleLogSpy.mockClear()
  consoleErrorSpy.mockClear()
})

// --- Helpers ---------------------------------------------------------------

// Reset the private static state of AutoUpdater between tests so cases do not
// leak `isChecking`, `checkInterval`, or status values into each other.
function resetAutoUpdaterState() {
  AutoUpdater.stop()
  const internal = AutoUpdater as any
  internal.status = {
    lastCheck: null,
    lastUpdate: null,
    latestVersion: null,
    isChecking: false,
    nextCheck: null,
  }
  internal.intervalMs = 60 * 60 * 1000
  if (internal.checkInterval) {
    clearInterval(internal.checkInterval)
    internal.checkInterval = null
  }
}

// Build a VersionInfo-like object used by VersionManager.getAvailableVersions.
function versionInfo(version: string, overrides: Partial<{
  installed: boolean
  isLatest: boolean
}> = {}) {
  return {
    version,
    path: `/fake/${version}`,
    installed: overrides.installed ?? false,
    isLatest: overrides.isLatest ?? false,
  }
}

// --- Tests -----------------------------------------------------------------

describe('AutoUpdater.getStatus', () => {
  afterEach(() => {
    resetAutoUpdaterState()
  })

  test('returns a copy of the current status with null defaults', () => {
    const status = AutoUpdater.getStatus()

    expect(status).toEqual({
      lastCheck: null,
      lastUpdate: null,
      latestVersion: null,
      isChecking: false,
      nextCheck: null,
    })
  })

  test('returns a new object instance each call (not a shared reference)', () => {
    const a = AutoUpdater.getStatus()
    const b = AutoUpdater.getStatus()

    expect(a).not.toBe(b)
    expect(a).toEqual(b)
  })
})

describe('AutoUpdater.start & stop', () => {
  let setTimeoutSpy: ReturnType<typeof spyOn>
  let setIntervalSpy: ReturnType<typeof spyOn>
  let clearIntervalSpy: ReturnType<typeof spyOn>

  beforeEach(() => {
    setTimeoutSpy = spyOn(globalThis, 'setTimeout').mockImplementation((() => {}) as any)
    setIntervalSpy = spyOn(globalThis, 'setInterval').mockImplementation((() => 12345) as any)
    clearIntervalSpy = spyOn(globalThis, 'clearInterval').mockImplementation(() => {})
  })

  afterEach(() => {
    setTimeoutSpy.mockRestore()
    setIntervalSpy.mockRestore()
    clearIntervalSpy.mockRestore()
    resetAutoUpdaterState()
  })

  test('start schedules a delayed first check and a periodic interval', () => {
    AutoUpdater.start(30 * 60 * 1000)

    expect(setTimeoutSpy).toHaveBeenCalledTimes(1)
    expect(setIntervalSpy).toHaveBeenCalledTimes(1)
    // nextCheck should be set because checkInterval is now populated.
    expect(AutoUpdater.getStatus().nextCheck).not.toBeNull()
    expect(consoleLogSpy).toHaveBeenCalled()
  })

  test('start clears any previously scheduled interval before scheduling a new one', () => {
    AutoUpdater.start(10 * 60 * 1000)
    const firstInterval = (AutoUpdater as any).checkInterval
    expect(firstInterval).toBeTruthy()

    clearIntervalSpy.mockClear()
    AutoUpdater.start(20 * 60 * 1000)

    expect(clearIntervalSpy).toHaveBeenCalledTimes(1)
  })

  test('stop clears the interval and resets nextCheck when running', () => {
    AutoUpdater.start(15 * 60 * 1000)
    expect(AutoUpdater.getStatus().nextCheck).not.toBeNull()

    AutoUpdater.stop()

    expect(clearIntervalSpy).toHaveBeenCalled()
    expect(AutoUpdater.getStatus().nextCheck).toBeNull()
    expect((AutoUpdater as any).checkInterval).toBeNull()
  })

  test('stop is a no-op when no interval is active', () => {
    // Ensure no interval is active.
    AutoUpdater.stop()
    clearIntervalSpy.mockClear()

    AutoUpdater.stop()

    expect(clearIntervalSpy).not.toHaveBeenCalled()
  })
})

describe('AutoUpdater.checkAndUpdate', () => {
  let getAvailableVersionsSpy: ReturnType<typeof spyOn>
  let installVersionSpy: ReturnType<typeof spyOn>
  let restartInstanceSpy: ReturnType<typeof spyOn>
  let getAllInstancesSpy: ReturnType<typeof spyOn>

  beforeEach(() => {
    getAvailableVersionsSpy = spyOn(VersionManager, 'getAvailableVersions')
    installVersionSpy = spyOn(VersionManager, 'installVersion')
    restartInstanceSpy = spyOn(InstanceService, 'restartInstance')
    getAllInstancesSpy = spyOn(queries.getAllInstances, 'all')
    // Safe defaults so tests do not accidentally hit GitHub or the FS.
    getAvailableVersionsSpy.mockResolvedValue([])
    installVersionSpy.mockResolvedValue(undefined)
    restartInstanceSpy.mockResolvedValue(null)
    getAllInstancesSpy.mockReturnValue([] as any)
  })

  afterEach(() => {
    getAvailableVersionsSpy.mockRestore()
    installVersionSpy.mockRestore()
    restartInstanceSpy.mockRestore()
    getAllInstancesSpy.mockRestore()
    resetAutoUpdaterState()
  })

  test('skips when a check is already in progress', async () => {
    // Force the isChecking flag on to simulate a concurrent check.
    ;(AutoUpdater as any).status.isChecking = true

    const result = await AutoUpdater.checkAndUpdate()

    expect(result).toEqual({ updated: false })
    expect(getAvailableVersionsSpy).not.toHaveBeenCalled()
    expect(consoleLogSpy).toHaveBeenCalledWith(
      '[AutoUpdater] Check already in progress, skipping',
    )
  })

  test('returns not-updated when no versions are available from GitHub', async () => {
    getAvailableVersionsSpy.mockResolvedValue([])

    const result = await AutoUpdater.checkAndUpdate()

    expect(result).toEqual({ updated: false })
    expect(consoleLogSpy).toHaveBeenCalledWith(
      '[AutoUpdater] No versions available from GitHub',
    )
  })

  test('returns not-updated when latest release cannot be determined', async () => {
    // Only the 'latest' alias entry is present; no concrete isLatest entry.
    getAvailableVersionsSpy.mockResolvedValue([
      versionInfo('latest', { isLatest: true }),
    ])

    const result = await AutoUpdater.checkAndUpdate()

    expect(result).toEqual({ updated: false })
    expect(consoleLogSpy).toHaveBeenCalledWith(
      '[AutoUpdater] Could not determine latest version',
    )
  })

  test('returns not-updated when the latest version is already installed', async () => {
    getAvailableVersionsSpy.mockResolvedValue([
      versionInfo('latest', { isLatest: true }),
      versionInfo('v2.0.0', { installed: true, isLatest: true }),
    ])

    const result = await AutoUpdater.checkAndUpdate()

    expect(result).toEqual({ updated: false })
    expect(installVersionSpy).not.toHaveBeenCalled()
    expect(AutoUpdater.getStatus().latestVersion).toBe('v2.0.0')
  })

  test('downloads, installs and reports a successful update with no running instances', async () => {
    getAvailableVersionsSpy.mockResolvedValue([
      versionInfo('latest', { isLatest: true }),
      versionInfo('v3.0.0', { installed: false, isLatest: true }),
    ])
    getAllInstancesSpy.mockReturnValue([] as any)

    const result = await AutoUpdater.checkAndUpdate()

    expect(installVersionSpy).toHaveBeenCalledWith('v3.0.0')
    expect(result).toEqual({ updated: true, version: 'v3.0.0', restartedInstances: 0 })
    expect(AutoUpdater.getStatus().latestVersion).toBe('v3.0.0')
    expect(AutoUpdater.getStatus().lastUpdate).not.toBeNull()
    expect(consoleLogSpy).toHaveBeenCalledWith(
      '[AutoUpdater] No running instances using "latest" version',
    )
  })

  test('restarts running instances that use the latest version', async () => {
    getAvailableVersionsSpy.mockResolvedValue([
      versionInfo('latest', { isLatest: true }),
      versionInfo('v3.1.0', { installed: false, isLatest: true }),
    ])
    getAllInstancesSpy.mockReturnValue([
      { id: 1, name: 'a', status: 'running', gowa_version: 'latest' },
      { id: 2, name: 'b', status: 'stopped', gowa_version: 'latest' },
      { id: 3, name: 'c', status: 'running', gowa_version: 'v1.0.0' },
    ] as any)
    restartInstanceSpy.mockResolvedValue(null)

    const result = await AutoUpdater.checkAndUpdate()

    // Only instance 1 is running + uses 'latest'.
    expect(restartInstanceSpy).toHaveBeenCalledTimes(1)
    expect(restartInstanceSpy).toHaveBeenCalledWith(1)
    expect(result).toEqual({ updated: true, version: 'v3.1.0', restartedInstances: 1 })
  })

  test('counts only successfully restarted instances when some restarts fail', async () => {
    getAvailableVersionsSpy.mockResolvedValue([
      versionInfo('latest', { isLatest: true }),
      versionInfo('v3.2.0', { installed: false, isLatest: true }),
    ])
    getAllInstancesSpy.mockReturnValue([
      { id: 10, name: 'ok', status: 'running', gowa_version: 'latest' },
      { id: 11, name: 'bad', status: 'running', gowa_version: 'latest' },
    ] as any)
    restartInstanceSpy.mockImplementation(async (id: number) => {
      if (id === 11) throw new Error('restart failed')
      return null
    })

    const result = await AutoUpdater.checkAndUpdate()

    expect(restartInstanceSpy).toHaveBeenCalledTimes(2)
    expect(result).toEqual({ updated: true, version: 'v3.2.0', restartedInstances: 1 })
    expect(consoleErrorSpy).toHaveBeenCalled()
  })

  test('returns not-updated and logs when installVersion throws', async () => {
    getAvailableVersionsSpy.mockResolvedValue([
      versionInfo('latest', { isLatest: true }),
      versionInfo('v4.0.0', { installed: false, isLatest: true }),
    ])
    installVersionSpy.mockRejectedValue(new Error('download failed'))

    const result = await AutoUpdater.checkAndUpdate()

    expect(result).toEqual({ updated: false })
    expect(consoleErrorSpy).toHaveBeenCalledWith(
      '[AutoUpdater] Error during update check:',
      expect.any(Error),
    )
    // isChecking must be reset in the finally block.
    expect(AutoUpdater.getStatus().isChecking).toBe(false)
  })

  test('resets isChecking in finally even on success', async () => {
    getAvailableVersionsSpy.mockResolvedValue([
      versionInfo('latest', { isLatest: true }),
      versionInfo('v4.1.0', { installed: false, isLatest: true }),
    ])
    getAllInstancesSpy.mockReturnValue([] as any)

    await AutoUpdater.checkAndUpdate()

    expect(AutoUpdater.getStatus().isChecking).toBe(false)
  })
})

describe('AutoUpdater.getLatestInstances', () => {
  let getAllInstancesSpy: ReturnType<typeof spyOn>

  beforeEach(() => {
    getAllInstancesSpy = spyOn(queries.getAllInstances, 'all')
  })

  afterEach(() => {
    getAllInstancesSpy.mockRestore()
    resetAutoUpdaterState()
  })

  test('returns only instances using latest or missing gowa_version', () => {
    getAllInstancesSpy.mockReturnValue([
      { id: 1, name: 'one', status: 'running', gowa_version: 'latest' },
      { id: 2, name: 'two', status: 'stopped', gowa_version: 'v1.0.0' },
      { id: 3, name: 'three', status: 'running', gowa_version: null },
      { id: 4, name: 'four', status: 'stopped', gowa_version: '' },
    ] as any)

    const result = AutoUpdater.getLatestInstances()

    expect(result).toEqual([
      { id: 1, name: 'one', status: 'running' },
      { id: 3, name: 'three', status: 'running' },
      { id: 4, name: 'four', status: 'stopped' },
    ])
  })

  test('returns empty array when no instances match', () => {
    getAllInstancesSpy.mockReturnValue([
      { id: 5, name: 'pinned', status: 'running', gowa_version: 'v2.0.0' },
    ] as any)

    const result = AutoUpdater.getLatestInstances()

    expect(result).toEqual([])
  })
})
