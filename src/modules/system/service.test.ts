import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { createServer, type Server } from 'node:net'
import { queries } from '../../db'
import { SystemService } from './service'

const createdIds: number[] = []

function listen(server: Server, port = 0): Promise<number> {
  return new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(port, '127.0.0.1', () => {
      const address = server.address()
      if (address && typeof address === 'object') resolve(address.port)
      else reject(new Error('Unable to resolve test server port'))
    })
  })
}

function close(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve())
  })
}

function createInstanceWithPort(port: number) {
  const instance = queries.createInstance.get(
    `P${Math.random().toString(36).slice(2, 9).toUpperCase()}`.slice(0, 8),
    `system-port-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    port,
    '{}',
    'latest'
  ) as any
  createdIds.push(instance.id)
  return instance
}

describe('SystemService ports', () => {
  afterEach(() => {
    while (createdIds.length > 0) {
      const id = createdIds.pop()
      if (id !== undefined) queries.deleteInstance.run(id)
    }
  })

  test('treats manager and reserved ports as unavailable', async () => {
    expect(await SystemService.isPortAvailable(3000)).toBe(false)
    expect(await SystemService.isPortAvailable(80)).toBe(false)
  })

  test('detects an actively listening port as unavailable', async () => {
    const server = createServer()
    const port = await listen(server)

    try {
      expect(await SystemService.isPortAvailable(port)).toBe(false)
      expect(await SystemService.isHttpPortAvailable(port)).toBe(false)
    } finally {
      await close(server)
    }
  })

  test('detects a closed high port as available', async () => {
    const server = createServer()
    const port = await listen(server)
    await close(server)

    expect(await SystemService.isPortAvailable(port)).toBe(true)
  })

  test('getNextAvailablePort skips database ports and unavailable network ports', async () => {
    createInstanceWithPort(8000)
    createInstanceWithPort(8001)
    const availability = spyOn(SystemService, 'isPortAvailable').mockImplementation(async (port: number) => port >= 8003)

    const nextPort = await SystemService.getNextAvailablePort()

    expect(nextPort).toBe(8003)
    expect(availability).toHaveBeenCalledWith(8002)
    expect(availability).toHaveBeenCalledWith(8003)
    availability.mockRestore()
  })
})

describe('SystemService.getSystemStatus', () => {
  afterEach(() => {
    while (createdIds.length > 0) {
      const id = createdIds.pop()
      if (id !== undefined) queries.deleteInstance.run(id)
    }
  })

  test('returns status shape and counts for mixed instances', () => {
    const running = createInstanceWithPort(8005)
    const stopped = createInstanceWithPort(8008)
    const noPort = queries.createInstance.get(
      `P${Math.random().toString(36).slice(2, 9).toUpperCase()}`.slice(0, 8),
      `system-nil-port-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      null,
      '{}',
      'latest'
    ) as any
    createdIds.push(noPort.id)

    queries.updateInstanceStatus.run('running', running.id)
    queries.updateInstanceStatus.run('stopped', stopped.id)
    queries.updateInstanceStatus.run('running', noPort.id)

    const status = SystemService.getSystemStatus()

    expect(status.status).toBe('running')
    expect(status.uptime).toEqual(expect.any(Number))
    expect(status.uptime).toBeGreaterThanOrEqual(0)
    expect(status.managerVersion).toEqual(expect.any(String))
    expect(status.instances).toEqual({
      total: 3,
      running: 2,
      stopped: 1,
    })
    expect(status.ports).toEqual({
      allocated: 2,
      next_available: 8009,
    })
  })

  test('falls back to base next port when no allocated ports exist', () => {
    const noPort = queries.createInstance.get(
      `P${Math.random().toString(36).slice(2, 9).toUpperCase()}`.slice(0, 8),
      `system-no-port-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      null,
      '{}',
      'latest'
    ) as any
    createdIds.push(noPort.id)

    const status = SystemService.getSystemStatus()

    expect(status.instances.total).toBe(1)
    expect(status.ports).toEqual({
      allocated: 0,
      next_available: 8000,
    })
  })
})
