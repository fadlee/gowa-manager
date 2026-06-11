import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { queries } from '../../db'
import { InstanceService } from './service'
import { DirectoryManager } from './utils/directory-manager'
import { ResourceMonitor } from './utils/resource-monitor'

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
