import { afterEach, describe, expect, test } from 'bun:test'
import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { queries } from '../../db'
import { SystemService } from '../system/service'
import { instancesModule } from './index'
import { InstanceService } from './service'

const originalGetNextAvailablePort = SystemService.getNextAvailablePort
const originalStartInstance = InstanceService.startInstance
const originalStopInstance = InstanceService.stopInstance
const originalRestartInstance = InstanceService.restartInstance
const originalKillInstance = InstanceService.killInstance
const createdIds: number[] = []

function createTestApp() {
  return new Elysia().guard(
    {
      beforeHandle: basicAuth('manager', 'secret'),
    },
    (app) => app.use(instancesModule)
  )
}

function basicHeader(username: string, password: string) {
  return `Basic ${btoa(`${username}:${password}`)}`
}

async function json(response: Response) {
  return await response.json() as any
}

describe('instances routes', () => {
  afterEach(() => {
    SystemService.getNextAvailablePort = originalGetNextAvailablePort
    InstanceService.startInstance = originalStartInstance
    InstanceService.stopInstance = originalStopInstance
    InstanceService.restartInstance = originalRestartInstance
    InstanceService.killInstance = originalKillInstance
    while (createdIds.length > 0) {
      const id = createdIds.pop()
      if (id !== undefined) queries.deleteInstance.run(id)
    }
  })

  test('requires manager basic auth', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/instances/'))

    expect(response.status).toBe(401)
  })

  test('creates, lists, gets, updates, and deletes an instance', async () => {
    SystemService.getNextAvailablePort = async () => 18999
    const app = createTestApp()
    const headers = {
      authorization: basicHeader('manager', 'secret'),
      'content-type': 'application/json',
    }

    const createResponse = await app.handle(new Request('http://localhost/api/instances/', {
      method: 'POST',
      headers,
      body: JSON.stringify({
        name: 'route-crud-instance',
        gowa_version: 'v8.7.0',
        config: JSON.stringify({
          flags: {
            basePath: '/wrong',
            basicAuth: [{ username: 'admin', password: 'admin123' }],
          },
        }),
      }),
    }))
    const created = await json(createResponse)
    createdIds.push(created.id)

    expect(createResponse.status).toBe(201)
    expect(created.name).toBe('route-crud-instance')
    expect(created.port).toBe(18999)
    expect(created.gowa_version).toBe('v8.7.0')
    expect(JSON.parse(created.config).flags.basePath).toBe(`/app/${created.key}`)

    const listResponse = await app.handle(new Request('http://localhost/api/instances/', { headers }))
    const instances = await json(listResponse)

    expect(listResponse.status).toBe(200)
    expect(instances.some((instance: any) => instance.id === created.id)).toBe(true)

    const getResponse = await app.handle(new Request(`http://localhost/api/instances/${created.id}`, { headers }))
    const fetched = await json(getResponse)

    expect(getResponse.status).toBe(200)
    expect(fetched.id).toBe(created.id)

    const updateResponse = await app.handle(new Request(`http://localhost/api/instances/${created.id}`, {
      method: 'PUT',
      headers,
      body: JSON.stringify({
        name: 'route-crud-updated',
        config: JSON.stringify({ flags: { basePath: '/still-wrong', os: 'Chrome' } }),
      }),
    }))
    const updated = await json(updateResponse)

    expect(updateResponse.status).toBe(200)
    expect(updated.name).toBe('route-crud-updated')
    expect(JSON.parse(updated.config).flags.basePath).toBe(`/app/${created.key}`)
    expect(JSON.parse(updated.config).flags.os).toBe('Chrome')

    const deleteResponse = await app.handle(new Request(`http://localhost/api/instances/${created.id}`, {
      method: 'DELETE',
      headers,
    }))
    const deleted = await json(deleteResponse)
    createdIds.pop()

    expect(deleteResponse.status).toBe(200)
    expect(deleted).toEqual({ success: true, message: 'Instance deleted successfully' })

    const missingResponse = await app.handle(new Request(`http://localhost/api/instances/${created.id}`, { headers }))

    expect(missingResponse.status).toBe(404)
  })

  test('returns 404 for missing instance', async () => {
    const app = createTestApp()
    const headers = { authorization: basicHeader('manager', 'secret') }

    const response = await app.handle(new Request('http://localhost/api/instances/999999', { headers }))

    expect(response.status).toBe(404)
    expect(await json(response)).toEqual({ error: 'Instance not found', success: false })
  })

  test('starts an instance through the lifecycle route', async () => {
    const app = createTestApp()
    const headers = { authorization: basicHeader('manager', 'secret') }
    InstanceService.startInstance = async (id: number) => ({
      id,
      name: 'started-instance',
      status: 'running',
      port: 19001,
      pid: 1234,
      uptime: 0,
    })

    const response = await app.handle(new Request('http://localhost/api/instances/101/start', {
      method: 'POST',
      headers,
    }))

    expect(response.status).toBe(200)
    expect(await json(response)).toMatchObject({ id: 101, status: 'running', pid: 1234 })
  })

  test('stops an instance through the lifecycle route', async () => {
    const app = createTestApp()
    const headers = { authorization: basicHeader('manager', 'secret') }
    InstanceService.stopInstance = async (id: number) => ({
      id,
      name: 'stopped-instance',
      status: 'stopped',
      port: 19002,
      pid: null,
      uptime: null,
    })

    const response = await app.handle(new Request('http://localhost/api/instances/102/stop', {
      method: 'POST',
      headers,
    }))

    expect(response.status).toBe(200)
    expect(await json(response)).toMatchObject({ id: 102, status: 'stopped', pid: null })
  })

  test('restarts an instance through the lifecycle route', async () => {
    const app = createTestApp()
    const headers = { authorization: basicHeader('manager', 'secret') }
    InstanceService.restartInstance = async (id: number) => ({
      id,
      name: 'restarted-instance',
      status: 'running',
      port: 19003,
      pid: 5678,
      uptime: 0,
    })

    const response = await app.handle(new Request('http://localhost/api/instances/103/restart', {
      method: 'POST',
      headers,
    }))

    expect(response.status).toBe(200)
    expect(await json(response)).toMatchObject({ id: 103, status: 'running', pid: 5678 })
  })

  test('kills an instance through the lifecycle route', async () => {
    const app = createTestApp()
    const headers = { authorization: basicHeader('manager', 'secret') }
    InstanceService.killInstance = async (id: number) => ({
      id,
      name: 'killed-instance',
      status: 'stopped',
      port: 19004,
      pid: null,
      uptime: null,
    })

    const response = await app.handle(new Request('http://localhost/api/instances/104/kill', {
      method: 'POST',
      headers,
    }))

    expect(response.status).toBe(200)
    expect(await json(response)).toMatchObject({ id: 104, status: 'stopped', pid: null })
  })

  test('returns 404 from lifecycle routes when instance is missing', async () => {
    const app = createTestApp()
    const headers = { authorization: basicHeader('manager', 'secret') }
    InstanceService.startInstance = async () => null
    InstanceService.stopInstance = async () => null
    InstanceService.restartInstance = async () => null
    InstanceService.killInstance = async () => null

    for (const action of ['start', 'stop', 'restart', 'kill']) {
      const response = await app.handle(new Request(`http://localhost/api/instances/999999/${action}`, {
        method: 'POST',
        headers,
      }))

      expect(response.status).toBe(404)
      expect(await json(response)).toEqual({ error: 'Instance not found', success: false })
    }
  })

  test('returns sanitized 500 when start, restart, or kill throws', async () => {
    const app = createTestApp()
    const headers = { authorization: basicHeader('manager', 'secret') }
    InstanceService.startInstance = async () => { throw new Error('version missing') }
    InstanceService.restartInstance = async () => { throw new Error('restart failed') }
    InstanceService.killInstance = async () => { throw new Error('kill failed') }

    const startResponse = await app.handle(new Request('http://localhost/api/instances/104/start', {
      method: 'POST',
      headers,
    }))
    const restartResponse = await app.handle(new Request('http://localhost/api/instances/104/restart', {
      method: 'POST',
      headers,
    }))
    const killResponse = await app.handle(new Request('http://localhost/api/instances/104/kill', {
      method: 'POST',
      headers,
    }))

    expect(startResponse.status).toBe(500)
    expect(await json(startResponse)).toEqual({ error: 'version missing', success: false })
    expect(restartResponse.status).toBe(500)
    expect(await json(restartResponse)).toEqual({ error: 'restart failed', success: false })
    expect(killResponse.status).toBe(500)
    expect(await json(killResponse)).toEqual({ error: 'kill failed', success: false })
  })
})
