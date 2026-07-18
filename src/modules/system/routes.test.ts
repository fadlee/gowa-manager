import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { systemModule } from './index'
import { SystemService } from './service'
import { AutoUpdater } from './auto-updater'

function createTestApp() {
  return new Elysia().guard(
    {
      beforeHandle: basicAuth('manager', 'secret'),
    },
    (app) => app.use(systemModule)
  )
}

function basicHeader(username: string, password: string) {
  return `Basic ${btoa(`${username}:${password}`)}`
}

async function json(response: Response) {
  return await response.json() as any
}

describe('system routes', () => {
  const spies: ReturnType<typeof spyOn>[] = []

  afterEach(() => {
    for (const spy of spies) {
      spy.mockRestore()
    }
    spies.length = 0
  })

  test('requires manager basic auth', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/status'))

    expect(response.status).toBe(401)
  })

  test('returns system status with manager basic auth', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/status', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))
    const body = await json(response)

    expect(response.status).toBe(200)
    expect(body.status).toBe('running')
    expect(body.instances).toMatchObject({
      total: expect.any(Number),
      running: expect.any(Number),
      stopped: expect.any(Number),
    })
    expect(body.ports).toMatchObject({
      allocated: expect.any(Number),
      next_available: expect.any(Number),
    })
  })

  test('returns system config with isolated test data directory', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/config', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))
    const body = await json(response)

    expect(response.status).toBe(200)
    expect(body.data_directory).toBe(process.env.DATA_DIR)
    expect(body.port_range).toEqual({ min: 8000, max: 9000 })
    expect(body.binaries_directory).toContain(process.env.DATA_DIR!)
  })

  test('returns next available port', async () => {
    const spy = spyOn(SystemService, 'getNextAvailablePort').mockResolvedValue(8080)
    spies.push(spy)
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/ports/next', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({ port: 8080 })
  })

  test('checks if a specific port is available', async () => {
    const spy = spyOn(SystemService, 'isPortAvailable').mockResolvedValue(true)
    spies.push(spy)
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/ports/8080/available', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({ port: 8080, available: true })
    expect(spy).toHaveBeenCalledWith(8080)
  })

  test('checks if a specific port is unavailable', async () => {
    const spy = spyOn(SystemService, 'isPortAvailable').mockResolvedValue(false)
    spies.push(spy)
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/ports/9000/available', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({ port: 9000, available: false })
    expect(spy).toHaveBeenCalledWith(9000)
  })

  test('returns auto-update status', async () => {
    const status = {
      lastCheck: new Date('2024-01-01T00:00:00Z'),
      lastUpdate: new Date('2024-01-02T00:00:00Z'),
      latestVersion: 'v1.2.3',
      isChecking: false,
      nextCheck: new Date('2024-01-03T00:00:00Z'),
    }
    const spy = spyOn(AutoUpdater, 'getStatus').mockReturnValue(status)
    spies.push(spy)
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/auto-update/status', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    const body = await json(response)
    expect(body.latestVersion).toBe('v1.2.3')
    expect(body.isChecking).toBe(false)
  })

  test('triggers manual update check and returns success', async () => {
    const spy = spyOn(AutoUpdater, 'checkAndUpdate').mockResolvedValue({
      updated: true,
      version: 'v2.0.0',
      restartedInstances: 1,
    })
    spies.push(spy)
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/auto-update/check', {
      method: 'POST',
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({
      success: true,
      updated: true,
      version: 'v2.0.0',
      restartedInstances: 1,
    })
  })

  test('returns null version when update not applied', async () => {
    const spy = spyOn(AutoUpdater, 'checkAndUpdate').mockResolvedValue({ updated: false })
    spies.push(spy)
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/auto-update/check', {
      method: 'POST',
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({
      success: true,
      updated: false,
      version: null,
      restartedInstances: 0,
    })
  })

  test('returns instances using latest version', async () => {
    const instances = [{ id: 1, name: 'test', status: 'running' }]
    const spy = spyOn(AutoUpdater, 'getLatestInstances').mockReturnValue(instances)
    spies.push(spy)
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/auto-update/instances', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual(instances)
  })
})
