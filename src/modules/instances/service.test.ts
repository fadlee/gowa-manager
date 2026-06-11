import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { queries } from '../../db'
import { InstanceService } from './service'
import { DirectoryManager } from './utils/directory-manager'
import { ResourceMonitor } from './utils/resource-monitor'
import { SystemService } from '../system/service'
import { VersionManager } from '../system/version-manager'
import { ProcessManager } from './utils/process-manager'

const createdIds: number[] = []
const originalStopInstance = InstanceService.stopInstance

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
