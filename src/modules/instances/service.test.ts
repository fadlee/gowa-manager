import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { queries } from '../../db'
import { InstanceService } from './service'
import { DirectoryManager } from './utils/directory-manager'
import { ResourceMonitor } from './utils/resource-monitor'
import { SystemService } from '../system/service'
import { VersionManager } from '../system/version-manager'
import { ProcessManager } from './utils/process-manager'
import { NameGenerator } from './utils/name-generator'
import { DeviceClient } from './utils/device-client'

const createdIds: number[] = []
const originalStopInstance = InstanceService.stopInstance
const originalStartInstance = InstanceService.startInstance
const originalRestartDelayMs = process.env.INSTANCE_RESTART_DELAY_MS
const originalGetNextAvailablePort = SystemService.getNextAvailablePort
const originalGenerateRandomName = NameGenerator.generateRandomName

function createStoredInstance(overrides: Partial<{
  key: string
  name: string
  port: number
  config: string
  gowa_version: string
}> = {}) {
  const suffix = `${Date.now()}-${Math.random().toString(16).slice(2)}`
  const instance = queries.createInstance.get(
    overrides.key ?? `T${Math.random().toString(36).slice(2, 9).toUpperCase()}`.slice(0, 8),
    overrides.name ?? `test-update-${suffix}`,
    overrides.port ?? 19000,
    overrides.config ?? JSON.stringify({ flags: { basePath: '/wrong', os: 'Chrome' } }),
    overrides.gowa_version ?? 'latest'
  ) as any

  createdIds.push(instance.id)
  return instance
}

function cleanupCreatedInstances() {
  while (createdIds.length > 0) {
    const id = createdIds.pop()
    if (id !== undefined) queries.deleteInstance.run(id)
  }
}

describe('InstanceService.updateInstance', () => {
  afterEach(() => {
    cleanupCreatedInstances()
  })

  test('persists enforced basePath when updating config', () => {
    const instance = createStoredInstance({ key: 'TSTUPD01' })

    const updated = InstanceService.updateInstance(instance.id, {
      config: JSON.stringify({
        flags: {
          basePath: '/user-supplied',
          basicAuth: [{ username: 'admin', password: 'secret' }],
        },
      }),
    })

    expect(updated).not.toBeNull()
    const config = JSON.parse(updated!.config)
    expect(config.flags.basePath).toBe('/app/TSTUPD01')
    expect(config.flags.basicAuth).toEqual([{ username: 'admin', password: 'secret' }])
  })

  test('preserves existing name, port, and version when omitted', () => {
    const instance = createStoredInstance({
      key: 'TSTUPD02',
      name: 'test-preserve-existing-service',
      port: 19002,
      gowa_version: 'v8.7.0',
    })

    const updated = InstanceService.updateInstance(instance.id, {})

    expect(updated).not.toBeNull()
    expect(updated!.key).toBe('TSTUPD02')
    expect(updated!.name).toBe('test-preserve-existing-service')
    expect(updated!.port).toBe(19002)
    expect(updated!.gowa_version).toBe('v8.7.0')
    expect(JSON.parse(updated!.config).flags.basePath).toBe('/app/TSTUPD02')
  })

  test('returns null when instance does not exist', () => {
    const updated = InstanceService.updateInstance(-999999, {
      name: 'missing',
    })

    expect(updated).toBeNull()
  })
})

describe('InstanceService.deleteInstance', () => {
  afterEach(() => {
    InstanceService.stopInstance = originalStopInstance
    cleanupCreatedInstances()
  })

  test('returns false when instance does not exist', () => {
    expect(InstanceService.deleteInstance(-999999)).toBe(false)
  })

  test('deletes stopped instance and cleans up directory/resource history', () => {
    const instance = createStoredInstance({ key: 'TSTDEL01', name: 'test-delete-stopped' })
    const cleanup = spyOn(DirectoryManager, 'cleanupInstanceDirectory').mockImplementation(() => {})
    const clearHistory = spyOn(ResourceMonitor, 'clearHistory').mockImplementation(() => {})

    const deleted = InstanceService.deleteInstance(instance.id)
    const stored = queries.getInstanceById.get(instance.id)
    createdIds.pop()

    expect(deleted).toBe(true)
    expect(stored).toBeNull()
    expect(cleanup).toHaveBeenCalledWith(instance.id)
    expect(clearHistory).toHaveBeenCalledWith(instance.id)

    cleanup.mockRestore()
    clearHistory.mockRestore()
  })

  test('stops running instance before deleting it', () => {
    const instance = createStoredInstance({ key: 'TSTDEL02', name: 'test-delete-running' })
    queries.updateInstanceStatus.run('running', instance.id)
    const stop = spyOn(InstanceService, 'stopInstance').mockResolvedValue({
      id: instance.id,
      name: instance.name,
      status: 'stopped',
      port: instance.port,
      pid: null,
      uptime: null,
    })
    const cleanup = spyOn(DirectoryManager, 'cleanupInstanceDirectory').mockImplementation(() => {})
    const clearHistory = spyOn(ResourceMonitor, 'clearHistory').mockImplementation(() => {})

    const deleted = InstanceService.deleteInstance(instance.id)
    const stored = queries.getInstanceById.get(instance.id)
    createdIds.pop()

    expect(deleted).toBe(true)
    expect(stored).toBeNull()
    expect(stop).toHaveBeenCalledWith(instance.id)
    expect(cleanup).toHaveBeenCalledWith(instance.id)
    expect(clearHistory).toHaveBeenCalledWith(instance.id)

    stop.mockRestore()
    cleanup.mockRestore()
    clearHistory.mockRestore()
  })
})

describe('InstanceService.resetInstanceData', () => {
  afterEach(() => {
    InstanceService.stopInstance = originalStopInstance
    cleanupCreatedInstances()
  })

  test('returns false when instance does not exist', () => {
    expect(InstanceService.resetInstanceData(-999999)).toBe(false)
  })

  test('cleans generated data while preserving instance identity and config', () => {
    const instance = createStoredInstance({
      key: 'TSTRST01',
      name: 'test-reset-data',
      port: 19011,
      config: JSON.stringify({ flags: { basePath: '/app/TSTRST01', os: 'Chrome' } }),
      gowa_version: 'v8.7.0',
    })
    const cleanup = spyOn(DirectoryManager, 'cleanupInstanceDirectory').mockImplementation(() => {})
    const createDirectory = spyOn(DirectoryManager, 'createInstanceDirectory').mockReturnValue('/tmp/test-reset-data')
    const clearHistory = spyOn(ResourceMonitor, 'clearHistory').mockImplementation(() => {})
    const clearDeviceCache = spyOn(DeviceClient, 'clearCache').mockImplementation(() => {})

    const reset = InstanceService.resetInstanceData(instance.id)
    const stored = queries.getInstanceById.get(instance.id) as any

    expect(reset).toBe(true)
    expect(stored).toMatchObject({
      id: instance.id,
      key: 'TSTRST01',
      name: 'test-reset-data',
      port: 19011,
      config: instance.config,
      gowa_version: 'v8.7.0',
      status: 'stopped',
      error_message: null,
    })
    expect(cleanup).toHaveBeenCalledWith(instance.id)
    expect(createDirectory).toHaveBeenCalledWith(instance.id)
    expect(clearHistory).toHaveBeenCalledWith(instance.id)
    expect(clearDeviceCache).toHaveBeenCalledWith(instance.id)

    cleanup.mockRestore()
    createDirectory.mockRestore()
    clearHistory.mockRestore()
    clearDeviceCache.mockRestore()
  })

  test('stops running instance before resetting data', () => {
    const instance = createStoredInstance({ key: 'TSTRST02', name: 'test-reset-running' })
    queries.updateInstanceStatus.run('running', instance.id)
    const stop = spyOn(InstanceService, 'stopInstance').mockResolvedValue({
      id: instance.id,
      name: instance.name,
      status: 'stopped',
      port: instance.port,
      pid: null,
      uptime: null,
    })
    const cleanup = spyOn(DirectoryManager, 'cleanupInstanceDirectory').mockImplementation(() => {})
    const createDirectory = spyOn(DirectoryManager, 'createInstanceDirectory').mockReturnValue('/tmp/test-reset-running')

    const reset = InstanceService.resetInstanceData(instance.id)

    expect(reset).toBe(true)
    expect(stop).toHaveBeenCalledWith(instance.id)
    expect(cleanup).toHaveBeenCalledWith(instance.id)
    expect(createDirectory).toHaveBeenCalledWith(instance.id)

    stop.mockRestore()
    cleanup.mockRestore()
    createDirectory.mockRestore()
  })
})

describe('InstanceService.stopKillRestartInstance', () => {
  afterEach(() => {
    InstanceService.stopInstance = originalStopInstance
    InstanceService.startInstance = originalStartInstance
    process.env.INSTANCE_RESTART_DELAY_MS = originalRestartDelayMs
    cleanupCreatedInstances()
  })

  test('stopInstance returns null when instance does not exist', async () => {
    expect(await InstanceService.stopInstance(-999999)).toBeNull()
  })

  test('stopInstance stops process, clears history, and updates status', async () => {
    const instance = createStoredInstance({ key: 'TSTSTP01', name: 'test-stop-service' })
    queries.updateInstanceStatus.run('running', instance.id)
    const stopProcess = spyOn(ProcessManager, 'stopProcess').mockReturnValue(true)
    const clearHistory = spyOn(ResourceMonitor, 'clearHistory').mockImplementation(() => {})

    const status = await InstanceService.stopInstance(instance.id)
    const updated = queries.getInstanceById.get(instance.id) as any

    expect(status).toMatchObject({ id: instance.id, status: 'stopped', pid: null, uptime: null })
    expect(updated.status).toBe('stopped')
    expect(updated.error_message).toBeNull()
    expect(stopProcess).toHaveBeenCalledWith(instance.id)
    expect(clearHistory).toHaveBeenCalledWith(instance.id)

    stopProcess.mockRestore()
    clearHistory.mockRestore()
  })

  test('killInstance returns null when instance does not exist', async () => {
    expect(await InstanceService.killInstance(-999999)).toBeNull()
  })

  test('killInstance kills process, clears history, and updates status', async () => {
    const instance = createStoredInstance({ key: 'TSTKIL01', name: 'test-kill-service' })
    queries.updateInstanceStatus.run('running', instance.id)
    const killProcess = spyOn(ProcessManager, 'killProcess').mockReturnValue(true)
    const clearHistory = spyOn(ResourceMonitor, 'clearHistory').mockImplementation(() => {})

    const status = await InstanceService.killInstance(instance.id)
    const updated = queries.getInstanceById.get(instance.id) as any

    expect(status).toMatchObject({ id: instance.id, status: 'stopped', pid: null, uptime: null })
    expect(updated.status).toBe('stopped')
    expect(updated.error_message).toBeNull()
    expect(killProcess).toHaveBeenCalledWith(instance.id)
    expect(clearHistory).toHaveBeenCalledWith(instance.id)

    killProcess.mockRestore()
    clearHistory.mockRestore()
  })

  test('restartInstance stops then starts instance', async () => {
    process.env.INSTANCE_RESTART_DELAY_MS = '0'
    const stop = spyOn(InstanceService, 'stopInstance').mockResolvedValue({
      id: 321,
      name: 'restart-service',
      status: 'stopped',
      port: 19321,
      pid: null,
      uptime: null,
    })
    const start = spyOn(InstanceService, 'startInstance').mockResolvedValue({
      id: 321,
      name: 'restart-service',
      status: 'running',
      port: 19321,
      pid: 9321,
      uptime: 0,
    })

    const status = await InstanceService.restartInstance(321)

    expect(status).toMatchObject({ id: 321, status: 'running', pid: 9321 })
    expect(stop).toHaveBeenCalledWith(321)
    expect(start).toHaveBeenCalledWith(321)

    stop.mockRestore()
    start.mockRestore()
  })
})

describe('InstanceService.startInstance', () => {
  afterEach(() => {
    cleanupCreatedInstances()
  })

  test('starts an instance with mocked version, port, and spawn dependencies', async () => {
    const instance = createStoredInstance({
      key: 'TSTSTA01',
      name: 'test-start-success',
      port: 19101,
      config: JSON.stringify({ args: ['rest', '--port=PORT'], flags: { basePath: '/app/TSTSTA01' } }),
      gowa_version: 'v-test',
    })
    const versionAvailable = spyOn(VersionManager, 'isVersionAvailable').mockResolvedValue(true)
    const binaryPath = spyOn(VersionManager, 'getVersionBinaryPath').mockReturnValue('/fake/gowa')
    const portAvailable = spyOn(SystemService, 'isPortAvailable').mockResolvedValue(true)
    const resourceUsage = spyOn(ResourceMonitor, 'getResourceUsage').mockResolvedValue(null)
    const spawn = spyOn(Bun, 'spawn').mockReturnValue({
      pid: 9001,
      kill: () => {},
      exited: Promise.resolve(),
    } as any)

    const status = await InstanceService.startInstance(instance.id)
    const updated = queries.getInstanceById.get(instance.id) as any

    expect(status).toMatchObject({
      id: instance.id,
      name: 'test-start-success',
      status: 'running',
      port: 19101,
      pid: 9001,
    })
    expect(updated.status).toBe('running')
    expect(updated.error_message).toBeNull()
    expect(versionAvailable).toHaveBeenCalledWith('v-test')
    expect(binaryPath).toHaveBeenCalledWith('v-test')
    expect(portAvailable).toHaveBeenCalledWith(19101)
    const spawnArg = spawn.mock.calls[0][0] as any
    expect(spawnArg.cmd).toEqual(['/fake/gowa', 'rest', '--port=19101', '--base-path=/app/TSTSTA01'])
    expect(spawnArg.env.PORT).toBe('19101')

    ProcessManager.removeProcess(instance.id)
    queries.deleteInstance.run(instance.id)
    createdIds.pop()
    spawn.mockRestore()
    resourceUsage.mockRestore()
    portAvailable.mockRestore()
    binaryPath.mockRestore()
    versionAvailable.mockRestore()
  })

  test('allocates a new port when stored port is unavailable', async () => {
    const instance = createStoredInstance({ key: 'TSTSTA02', name: 'test-start-new-port', port: 19102 })
    const versionAvailable = spyOn(VersionManager, 'isVersionAvailable').mockResolvedValue(true)
    const binaryPath = spyOn(VersionManager, 'getVersionBinaryPath').mockReturnValue('/fake/gowa')
    const portAvailable = spyOn(SystemService, 'isPortAvailable').mockResolvedValue(false)
    const nextPort = spyOn(SystemService, 'getNextAvailablePort').mockResolvedValue(19222)
    const resourceUsage = spyOn(ResourceMonitor, 'getResourceUsage').mockResolvedValue(null)
    const spawn = spyOn(Bun, 'spawn').mockReturnValue({
      pid: 9002,
      kill: () => {},
      exited: Promise.resolve(),
    } as any)

    const status = await InstanceService.startInstance(instance.id)
    const updated = queries.getInstanceById.get(instance.id) as any

    expect(status?.port).toBe(19222)
    expect(updated.port).toBe(19222)
    const spawnArg = spawn.mock.calls[0][0] as any
    expect(spawnArg.cmd).toEqual(['/fake/gowa', '--os=Chrome', '--base-path=/wrong'])
    expect(spawnArg.env.PORT).toBe('19222')

    ProcessManager.removeProcess(instance.id)
    queries.deleteInstance.run(instance.id)
    createdIds.pop()
    spawn.mockRestore()
    resourceUsage.mockRestore()
    nextPort.mockRestore()
    portAvailable.mockRestore()
    binaryPath.mockRestore()
    versionAvailable.mockRestore()
  })

  test('marks instance as error when version is unavailable', async () => {
    const instance = createStoredInstance({ key: 'TSTSTA03', name: 'test-start-version-error', gowa_version: 'missing' })
    const versionAvailable = spyOn(VersionManager, 'isVersionAvailable').mockResolvedValue(false)

    await expect(InstanceService.startInstance(instance.id)).rejects.toThrow("GOWA version 'missing' is not installed")
    const updated = queries.getInstanceById.get(instance.id) as any

    expect(updated.status).toBe('error')
    expect(updated.error_message).toContain("GOWA version 'missing' is not installed")

    versionAvailable.mockRestore()
  })

  test('marks instance as error when spawn fails', async () => {
    const instance = createStoredInstance({ key: 'TSTSTA04', name: 'test-start-spawn-error', port: 19104 })
    const versionAvailable = spyOn(VersionManager, 'isVersionAvailable').mockResolvedValue(true)
    const binaryPath = spyOn(VersionManager, 'getVersionBinaryPath').mockReturnValue('/fake/gowa')
    const portAvailable = spyOn(SystemService, 'isPortAvailable').mockResolvedValue(true)
    const spawn = spyOn(Bun, 'spawn').mockImplementation(() => {
      throw new Error('spawn failed')
    })

    await expect(InstanceService.startInstance(instance.id)).rejects.toThrow('spawn failed')
    const updated = queries.getInstanceById.get(instance.id) as any

    expect(updated.status).toBe('error')
    expect(updated.error_message).toBe('spawn failed')

    spawn.mockRestore()
    portAvailable.mockRestore()
    binaryPath.mockRestore()
    versionAvailable.mockRestore()
  })
})

describe('InstanceService.getInstanceStatus', () => {
  afterEach(() => {
    ProcessManager.removeProcess(9101)
    ProcessManager.removeProcess(9102)
    cleanupCreatedInstances()
  })

  test('returns stopped status without process info', async () => {
    const instance = createStoredInstance({ key: 'TSTSTS01', name: 'test-status-stopped', port: 19401 })

    const status = await InstanceService.getInstanceStatus(instance.id)

    expect(status).toMatchObject({
      id: instance.id,
      name: 'test-status-stopped',
      status: 'stopped',
      port: 19401,
      pid: null,
      uptime: null,
    })
    expect(status?.resources).toBeUndefined()
  })

  test('returns running status with process info and resource usage', async () => {
    const instance = createStoredInstance({ key: 'TSTSTS02', name: 'test-status-running', port: 19402 })
    queries.updateInstanceStatus.run('running', instance.id)
    ProcessManager.addProcess(instance.id, {
      process: {} as any,
      pid: 9101,
      startTime: Date.now() - 5000,
    })
    const resources = {
      cpuPercent: 12,
      memoryMB: 34,
      memoryPercent: 2,
      avgCpu: 10,
      avgMemory: 30,
    }
    const resourceUsage = spyOn(ResourceMonitor, 'getResourceUsage').mockResolvedValue(resources)

    const status = await InstanceService.getInstanceStatus(instance.id)

    expect(status).toMatchObject({
      id: instance.id,
      status: 'running',
      port: 19402,
      pid: 9101,
      resources,
    })
    expect(status!.uptime).toBeGreaterThanOrEqual(0)
    expect(resourceUsage).toHaveBeenCalledWith(9101, instance.id)

    ProcessManager.removeProcess(instance.id)
    resourceUsage.mockRestore()
  })

  test('returns error status with error message', async () => {
    const instance = createStoredInstance({ key: 'TSTSTS03', name: 'test-status-error', port: 19403 })
    queries.updateInstanceStatusWithError.run('error', 'spawn failed', instance.id)

    const status = await InstanceService.getInstanceStatus(instance.id)

    expect(status).toMatchObject({
      id: instance.id,
      status: 'error',
      port: 19403,
      pid: null,
      uptime: null,
      error_message: 'spawn failed',
    })
  })

  test('returns running status without resources when monitor fails', async () => {
    const instance = createStoredInstance({ key: 'TSTSTS04', name: 'test-status-resource-fallback', port: 19404 })
    queries.updateInstanceStatus.run('running', instance.id)
    ProcessManager.addProcess(instance.id, {
      process: {} as any,
      pid: 9102,
      startTime: Date.now() - 1000,
    })
    const resourceUsage = spyOn(ResourceMonitor, 'getResourceUsage').mockRejectedValue(new Error('pid unavailable'))

    const status = await InstanceService.getInstanceStatus(instance.id)

    expect(status).toMatchObject({
      id: instance.id,
      status: 'running',
      pid: 9102,
    })
    expect(status?.resources).toBeUndefined()

    ProcessManager.removeProcess(instance.id)
    resourceUsage.mockRestore()
  })

  test('returns device summary on status response', async () => {
    const instance = createStoredInstance({ key: 'TSTSTS05', name: 'test-status-devices', port: 19405 })
    queries.updateInstanceStatus.run('running', instance.id)
    const summary = spyOn(DeviceClient, 'getDevicesSummary').mockResolvedValue({
      count: 2,
      connected: true,
      stale: false,
      fetchedAt: '2026-06-12T00:00:00.000Z',
    })

    const status = await InstanceService.getInstanceStatus(instance.id)

    expect(status?.devices).toEqual({
      count: 2,
      connected: true,
      stale: false,
      fetchedAt: '2026-06-12T00:00:00.000Z',
    })
    expect(summary).toHaveBeenCalledWith(expect.objectContaining({ id: instance.id }))

    summary.mockRestore()
  })
})

describe('InstanceService.createInstance', () => {
  afterEach(() => {
    SystemService.getNextAvailablePort = originalGetNextAvailablePort
    NameGenerator.generateRandomName = originalGenerateRandomName
    cleanupCreatedInstances()
  })

  test('creates instance with auto port, generated name, key, basePath, and default config', async () => {
    SystemService.getNextAvailablePort = async () => 19501
    NameGenerator.generateRandomName = () => 'generated-service-name'

    const instance = await InstanceService.createInstance({})
    createdIds.push(instance.id)
    const config = JSON.parse(instance.config)

    expect(instance.name).toBe('generated-service-name')
    expect(instance.port).toBe(19501)
    expect(instance.key).toMatch(/^[A-Z0-9]{8}$/)
    expect(instance.gowa_version).toBe('latest')
    expect(config.args).toEqual(['rest', '--port=PORT'])
    expect(config.flags).toEqual({
      accountValidation: true,
      os: 'GowaManager',
      basePath: `/app/${instance.key}`,
    })
  })

  test('merges provided config and forces generated basePath', async () => {
    SystemService.getNextAvailablePort = async () => 19502

    const instance = await InstanceService.createInstance({
      name: 'provided-create-name',
      gowa_version: 'v8.7.0',
      config: JSON.stringify({
        flags: {
          os: 'Chrome',
          basePath: '/wrong',
          basicAuth: [{ username: 'admin', password: 'secret' }],
        },
      }),
    })
    createdIds.push(instance.id)
    const config = JSON.parse(instance.config)

    expect(instance.name).toBe('provided-create-name')
    expect(instance.port).toBe(19502)
    expect(instance.gowa_version).toBe('v8.7.0')
    expect(config.args).toEqual(['rest', '--port=PORT'])
    expect(config.flags).toEqual({
      os: 'Chrome',
      basePath: `/app/${instance.key}`,
      basicAuth: [{ username: 'admin', password: 'secret' }],
    })
  })

  test('falls back to default config when provided config is invalid JSON', async () => {
    SystemService.getNextAvailablePort = async () => 19503

    const instance = await InstanceService.createInstance({
      name: 'invalid-config-create',
      config: '{bad-json',
    })
    createdIds.push(instance.id)
    const config = JSON.parse(instance.config)

    expect(instance.name).toBe('invalid-config-create')
    expect(instance.port).toBe(19503)
    expect(config.args).toEqual(['rest', '--port=PORT'])
    expect(config.flags).toEqual({
      accountValidation: true,
      os: 'GowaManager',
      basePath: `/app/${instance.key}`,
    })
  })

  test('initializes empty flags object when config has no flags key', async () => {
    SystemService.getNextAvailablePort = async () => 19504

    const instance = await InstanceService.createInstance({
      name: 'no-flags-create',
      config: JSON.stringify({ args: ['rest'] }),
    })
    createdIds.push(instance.id)
    const config = JSON.parse(instance.config)

    expect(config.flags).toBeDefined()
    expect(config.flags.basePath).toBe(`/app/${instance.key}`)
  })
})

describe('InstanceService.cleanupAllInstances', () => {
  test('delegates to ProcessManager.cleanupAllInstances', async () => {
    const spy = spyOn(ProcessManager, 'cleanupAllInstances').mockResolvedValue(undefined)

    await InstanceService.cleanupAllInstances()

    expect(spy).toHaveBeenCalledTimes(1)
    spy.mockRestore()
  })
})

describe('InstanceService.startInstance — already running', () => {
  afterEach(() => {
    ProcessManager.removeProcess(9201)
    cleanupCreatedInstances()
  })

  test('returns current status when instance is already running and process is alive', async () => {
    const instance = createStoredInstance({ key: 'TSTRUN01', name: 'test-already-running', port: 19601 })
    queries.updateInstanceStatus.run('running', instance.id)
    ProcessManager.addProcess(instance.id, {
      process: {} as any,
      pid: 9201,
      startTime: Date.now() - 1000,
    })

    const status = await InstanceService.startInstance(instance.id)

    expect(status).toMatchObject({
      id: instance.id,
      status: 'running',
      pid: 9201,
    })

    ProcessManager.removeProcess(instance.id)
  })
})
