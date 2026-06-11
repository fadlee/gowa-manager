import { afterEach, describe, expect, test } from 'bun:test'
import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { queries } from '../../db'
import { SystemService } from '../system/service'
import { instancesModule } from './index'

const originalGetNextAvailablePort = SystemService.getNextAvailablePort
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
})
