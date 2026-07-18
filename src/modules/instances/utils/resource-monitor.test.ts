import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from 'bun:test'
import { mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { ResourceMonitor } from './resource-monitor'
import { DirectoryManager } from './directory-manager'

// --- Mocks -----------------------------------------------------------------

// Controlled pidusage mock. Default implementation returns a fixed stat
// shape; individual tests can override via `pidusageMock.mockImplementation`.
const pidusageMock = mock<(pid: number) => Promise<{ cpu: number; memory: number }>>(
  async (_pid: number) => ({ cpu: 10, memory: 1024 * 1024 * 50 /* 50 MB */ }),
)

mock.module('pidusage', () => ({
  default: pidusageMock,
  __esModule: true,
}))

// --- Spies -----------------------------------------------------------------

const consoleWarnSpy = spyOn(console, 'warn').mockImplementation(() => {})
const consoleErrorSpy = spyOn(console, 'error').mockImplementation(() => {})

// Clear spy call history before each test so `not.toHaveBeenCalled()` only
// reflects calls made within the current test, not accumulated from prior ones.
beforeEach(() => {
  consoleWarnSpy.mockClear()
  consoleErrorSpy.mockClear()
})

// --- Helpers ---------------------------------------------------------------

// Use the test DATA_DIR configured by test/setup.ts for real filesystem ops.
const testDataDir = resolve(process.env.DATA_DIR || '.test-data')
const instanceDirBase = join(testDataDir, 'instances')

function createInstanceDirWithFiles(instanceId: number, fileSizesBytes: number[]): string {
  const dir = DirectoryManager.createInstanceDirectory(instanceId)
  fileSizesBytes.forEach((size, idx) => {
    writeFileSync(join(dir, `file-${idx}.bin`), Buffer.alloc(size))
  })
  return dir
}

function cleanupInstanceDir(instanceId: number) {
  const dir = DirectoryManager.getInstanceDirectory(instanceId)
  rmSync(dir, { recursive: true, force: true })
}

// Reset all module-level caches between tests so history/disk cache state
// does not leak across cases.
function resetResourceMonitorState() {
  ResourceMonitor.clearAllHistory()
  pidusageMock.mockReset()
  pidusageMock.mockImplementation(async (_pid: number) => ({
    cpu: 10,
    memory: 1024 * 1024 * 50,
  }))
}

// --- Tests -----------------------------------------------------------------

describe('ResourceMonitor.getResourceUsage', () => {
  afterEach(() => {
    resetResourceMonitorState()
  })

  test('returns cpu/memory stats from pidusage for a bare pid', async () => {
    pidusageMock.mockImplementation(async () => ({ cpu: 25.5, memory: 1024 * 1024 * 100 }))

    const usage = await ResourceMonitor.getResourceUsage(9991)

    expect(usage).not.toBeNull()
    expect(usage!.cpuPercent).toBe(25.5)
    expect(usage!.memoryMB).toBeCloseTo(100, 5)
    expect(usage!.memoryPercent).toBeGreaterThan(0)
    // No instanceId => no history/disk tracking.
    expect(usage!.avgCpu).toBeUndefined()
    expect(usage!.avgMemory).toBeUndefined()
    expect(usage!.diskMB).toBeUndefined()
  })

  test('returns null when pidusage throws ESRCH (process gone)', async () => {
    const esrch = new Error('no such process') as Error & { code: string }
    esrch.code = 'ESRCH'
    pidusageMock.mockImplementation(async () => {
      throw esrch
    })

    const usage = await ResourceMonitor.getResourceUsage(9992)

    expect(usage).toBeNull()
    expect(consoleWarnSpy).not.toHaveBeenCalled()
  })

  test('returns null and warns on non-ESRCH errors', async () => {
    const other = new Error('permission denied') as Error & { code: string }
    other.code = 'EPERM'
    pidusageMock.mockImplementation(async () => {
      throw other
    })

    const warnCallsBefore = consoleWarnSpy.mock.calls.length
    const usage = await ResourceMonitor.getResourceUsage(9993)

    expect(usage).toBeNull()
    expect(consoleWarnSpy.mock.calls.length).toBeGreaterThan(warnCallsBefore)
  })
})

describe('ResourceMonitor history tracking', () => {
  afterEach(() => {
    resetResourceMonitorState()
  })

  test('tracks cpu/memory history and exposes rolling averages', async () => {
    pidusageMock.mockImplementation(async () => ({ cpu: 20, memory: 1024 * 1024 * 40 }))

    const first = await ResourceMonitor.getResourceUsage(9100, 100)
    expect(first!.avgCpu).toBe(20)
    expect(first!.avgMemory).toBeCloseTo(40, 5)

    pidusageMock.mockImplementation(async () => ({ cpu: 40, memory: 1024 * 1024 * 60 }))
    const second = await ResourceMonitor.getResourceUsage(9100, 100)

    // Average of [20, 40] = 30; memory average of [40, 60] = 50.
    expect(second!.avgCpu).toBe(30)
    expect(second!.avgMemory).toBeCloseTo(50, 5)
  })

  test('trims history to the last 10 measurements', async () => {
    // Feed 12 calls with increasing cpu values 10,20,...,120.
    for (let i = 1; i <= 12; i++) {
      const cpuValue = i * 10
      pidusageMock.mockImplementation(async () => ({ cpu: cpuValue, memory: 0 }))
      await ResourceMonitor.getResourceUsage(9101, 101)
    }

    // After 12 calls, history is trimmed to last 10: [30,40,...,120].
    // sum = 30+40+50+60+70+80+90+100+110+120 = 750, avg = 75.
    pidusageMock.mockImplementation(async () => ({ cpu: 0, memory: 0 }))
    const usage = await ResourceMonitor.getResourceUsage(9101, 101)

    // After the 13th call (cpu=0), history = [40,50,...,120,0].
    // sum = 40+50+60+70+80+90+100+110+120+0 = 720, avg = 72.
    expect(usage!.avgCpu).toBe(72)
  })
})

describe('ResourceMonitor disk size cache', () => {
  beforeEach(() => {
    // Ensure instance dir base exists.
    mkdirSync(instanceDirBase, { recursive: true })
  })

  afterEach(() => {
    cleanupInstanceDir(200)
    resetResourceMonitorState()
  })

  test('computes diskMB from the instance directory on first call', async () => {
    pidusageMock.mockImplementation(async () => ({ cpu: 5, memory: 0 }))
    // 2 MB total across two files.
    createInstanceDirWithFiles(200, [1024 * 1024, 1024 * 1024])

    const usage = await ResourceMonitor.getResourceUsage(9200, 200)

    expect(usage!.diskMB).toBeCloseTo(2, 5)
  })

  test('serves cached disk size within TTL and recalculates after TTL', async () => {
    pidusageMock.mockImplementation(async () => ({ cpu: 5, memory: 0 }))
    const dir = createInstanceDirWithFiles(200, [1024 * 1024]) // 1 MB initially

    const first = await ResourceMonitor.getResourceUsage(9200, 200)
    expect(first!.diskMB).toBeCloseTo(1, 5)

    // Add another 1 MB file after the first measurement. The cache (TTL 30s)
    // should still serve the old value on the next call.
    writeFileSync(join(dir, 'extra.bin'), Buffer.alloc(1024 * 1024))

    const second = await ResourceMonitor.getResourceUsage(9200, 200)
    expect(second!.diskMB).toBeCloseTo(1, 5) // still cached

    // Force a cache miss by clearing history (which also clears disk cache).
    ResourceMonitor.clearHistory(200)
    const third = await ResourceMonitor.getResourceUsage(9200, 200)
    expect(third!.diskMB).toBeCloseTo(2, 5) // recalculated
  })
})

describe('ResourceMonitor.clearHistory & clearAllHistory', () => {
  afterEach(() => {
    resetResourceMonitorState()
  })

  test('clearHistory removes history for a single instance only', async () => {
    pidusageMock.mockImplementation(async () => ({ cpu: 10, memory: 0 }))
    await ResourceMonitor.getResourceUsage(9300, 300)
    await ResourceMonitor.getResourceUsage(9301, 301)

    ResourceMonitor.clearHistory(300)

    // Instance 301 should still have history; 300 should start fresh.
    pidusageMock.mockImplementation(async () => ({ cpu: 20, memory: 0 }))
    const after300 = await ResourceMonitor.getResourceUsage(9300, 300)
    const after301 = await ResourceMonitor.getResourceUsage(9301, 301)

    // 300 was cleared, so its only value is 20 -> avg 20.
    expect(after300!.avgCpu).toBe(20)
    // 301 kept [10, 20] -> avg 15.
    expect(after301!.avgCpu).toBe(15)
  })

  test('clearAllHistory removes history for every instance', async () => {
    pidusageMock.mockImplementation(async () => ({ cpu: 10, memory: 0 }))
    await ResourceMonitor.getResourceUsage(9302, 302)
    await ResourceMonitor.getResourceUsage(9303, 303)

    ResourceMonitor.clearAllHistory()

    pidusageMock.mockImplementation(async () => ({ cpu: 30, memory: 0 }))
    const after302 = await ResourceMonitor.getResourceUsage(9302, 302)
    const after303 = await ResourceMonitor.getResourceUsage(9303, 303)

    expect(after302!.avgCpu).toBe(30)
    expect(after303!.avgCpu).toBe(30)
  })
})

describe('ResourceMonitor.getMultipleResourceUsage', () => {
  afterEach(() => {
    resetResourceMonitorState()
  })

  test('returns a map with per-pid results, tolerating mixed success/failure', async () => {
    pidusageMock.mockImplementation(async (pid: number) => {
      if (pid === 9401) return { cpu: 15, memory: 1024 * 1024 * 30 }
      const esrch = new Error('no such process') as Error & { code: string }
      esrch.code = 'ESRCH'
      throw esrch
    })

    const results = await ResourceMonitor.getMultipleResourceUsage([9401, 9402])

    expect(results.size).toBe(2)
    expect(results.get(9401)).not.toBeNull()
    expect(results.get(9401)!.cpuPercent).toBe(15)
    expect(results.get(9402)).toBeNull()
  })
})

describe('ResourceMonitor.calculateDirectorySize', () => {
  beforeEach(() => {
    mkdirSync(instanceDirBase, { recursive: true })
  })

  afterEach(() => {
    cleanupInstanceDir(500)
  })

  test('recursively sums file sizes across nested directories', async () => {
    const root = DirectoryManager.createInstanceDirectory(500)
    mkdirSync(join(root, 'sub1'), { recursive: true })
    mkdirSync(join(root, 'sub1', 'sub2'), { recursive: true })

    // 1 KB + 2 KB at root, 4 KB in sub1, 8 KB in sub1/sub2 = 15 KB total.
    writeFileSync(join(root, 'a.bin'), Buffer.alloc(1024))
    writeFileSync(join(root, 'b.bin'), Buffer.alloc(1024 * 2))
    writeFileSync(join(root, 'sub1', 'c.bin'), Buffer.alloc(1024 * 4))
    writeFileSync(join(root, 'sub1', 'sub2', 'd.bin'), Buffer.alloc(1024 * 8))

    const bytes = await ResourceMonitor.calculateDirectorySize(root)

    expect(bytes).toBe(1024 * 15)
  })

  test('returns 0 for a missing directory without throwing', async () => {
    const missing = join(instanceDirBase, 'does-not-exist-500')
    const bytes = await ResourceMonitor.calculateDirectorySize(missing)

    expect(bytes).toBe(0)
  })
})
