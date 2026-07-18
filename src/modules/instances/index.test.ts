import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test'
import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { instancesModule } from './index'
import { InstanceService } from './service'

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

const authHeaders = {
  authorization: basicHeader('manager', 'secret'),
  'content-type': 'application/json',
}

// A reusable instance shape matching InstanceModel.instanceResponse.
function mockInstance(overrides: Partial<Record<string, any>> = {}) {
  return {
    id: 1,
    key: 'TESTKEY1',
    name: 'test-instance',
    port: 19500,
    status: 'stopped',
    config: '{}',
    gowa_version: 'latest',
    error_message: null,
    created_at: '2026-01-01T00:00:00.000Z',
    updated_at: '2026-01-01T00:00:00.000Z',
    ...overrides,
  }
}

describe('instances module index – error & edge paths', () => {
  let createInstanceSpy: ReturnType<typeof spyOn>
  let updateInstanceSpy: ReturnType<typeof spyOn>
  let deleteInstanceSpy: ReturnType<typeof spyOn>
  let getInstanceByIdSpy: ReturnType<typeof spyOn>
  let getInstanceStatusSpy: ReturnType<typeof spyOn>
  let fetchSpy: ReturnType<typeof spyOn>

  beforeEach(() => {
    createInstanceSpy = spyOn(InstanceService, 'createInstance')
    updateInstanceSpy = spyOn(InstanceService, 'updateInstance')
    deleteInstanceSpy = spyOn(InstanceService, 'deleteInstance')
    getInstanceByIdSpy = spyOn(InstanceService, 'getInstanceById')
    getInstanceStatusSpy = spyOn(InstanceService, 'getInstanceStatus')
    fetchSpy = spyOn(globalThis, 'fetch')
  })

  afterEach(() => {
    createInstanceSpy.mockRestore()
    updateInstanceSpy.mockRestore()
    deleteInstanceSpy.mockRestore()
    getInstanceByIdSpy.mockRestore()
    getInstanceStatusSpy.mockRestore()
    fetchSpy.mockRestore()
  })

  // --- POST / (create) catch block: lines 56-60 ------------------------------
  describe('POST /', () => {
    test('returns 400 with the error message when createInstance throws an Error', async () => {
      createInstanceSpy.mockRejectedValue(new Error('name already exists'))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/', {
        method: 'POST',
        headers: authHeaders,
        body: JSON.stringify({ name: 'duplicate' }),
      }))

      expect(response.status).toBe(400)
      expect(await json(response)).toEqual({ error: 'name already exists', success: false })
    })

    test('returns 400 with a generic message when createInstance throws a non-Error value', async () => {
      createInstanceSpy.mockRejectedValue('boom' as any)
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/', {
        method: 'POST',
        headers: authHeaders,
        body: JSON.stringify({ name: 'whatever' }),
      }))

      expect(response.status).toBe(400)
      expect(await json(response)).toEqual({ error: 'Failed to create instance', success: false })
    })
  })

  // --- PUT /:id 404 path: lines 75-76 ---------------------------------------
  describe('PUT /:id', () => {
    test('returns 404 when updateInstance reports the instance is missing', async () => {
      updateInstanceSpy.mockReturnValue(null)
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/999999', {
        method: 'PUT',
        headers: authHeaders,
        body: JSON.stringify({ name: 'new-name' }),
      }))

      expect(response.status).toBe(404)
      expect(await json(response)).toEqual({ error: 'Instance not found', success: false })
    })
  })

  // --- DELETE /:id 404 path: lines 91-92 ------------------------------------
  describe('DELETE /:id', () => {
    test('returns 404 when deleteInstance reports the instance is missing', async () => {
      deleteInstanceSpy.mockReturnValue(false)
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/999999', {
        method: 'DELETE',
        headers: authHeaders,
      }))

      expect(response.status).toBe(404)
      expect(await json(response)).toEqual({ error: 'Instance not found', success: false })
    })
  })

  // --- GET /:id/status 404 path: lines 205-210 ------------------------------
  describe('GET /:id/status', () => {
    test('returns 404 when getInstanceStatus reports the instance is missing', async () => {
      getInstanceStatusSpy.mockResolvedValue(null)
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/999999/status', {
        headers: authHeaders,
      }))

      expect(response.status).toBe(404)
      expect(await json(response)).toEqual({ error: 'Instance not found', success: false })
    })

    test('returns the status payload when the instance exists', async () => {
      getInstanceStatusSpy.mockResolvedValue({
        id: 7,
        name: 'running-instance',
        status: 'running',
        port: 19500,
        pid: 4321,
        uptime: 10,
      })
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/7/status', {
        headers: authHeaders,
      }))

      expect(response.status).toBe(200)
      expect(await json(response)).toMatchObject({ id: 7, status: 'running', pid: 4321 })
    })
  })

  // --- POST /:id/admin-link catch block: line 232 ---------------------------
  describe('POST /:id/admin-link', () => {
    test('returns a plain admin link when the instance config is invalid JSON', async () => {
      getInstanceByIdSpy.mockReturnValue(mockInstance({ config: '{not valid json' }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/admin-link', {
        method: 'POST',
        headers: authHeaders,
      }))

      expect(response.status).toBe(200)
      expect(await json(response)).toEqual({ url: '/app/TESTKEY1/' })
    })

    test('returns a plain admin link when basicAuth entry is missing username/password', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({
          config: JSON.stringify({ flags: { basicAuth: [{ username: '', password: '' }] } }),
        })
      )
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/admin-link', {
        method: 'POST',
        headers: authHeaders,
      }))

      expect(response.status).toBe(200)
      expect(await json(response)).toEqual({ url: '/app/TESTKEY1/' })
    })

    test('returns a magic admin link when basicAuth credentials are present', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({
          config: JSON.stringify({ flags: { basicAuth: [{ username: 'admin', password: 'secret' }] } }),
        })
      )
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/admin-link', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body.url).toStartWith('/app/TESTKEY1/?autologin=')
      expect(new Date(body.expiresAt).getTime()).toBeGreaterThan(Date.now())
    })

    test('returns 404 when the instance does not exist', async () => {
      getInstanceByIdSpy.mockReturnValue(null)
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/999999/admin-link', {
        method: 'POST',
        headers: authHeaders,
      }))

      expect(response.status).toBe(404)
      expect(await json(response)).toEqual({ error: 'Instance not found', success: false })
    })
  })

  // --- POST /:id/test-connection: lines 253-299 -----------------------------
  describe('POST /:id/test-connection', () => {
    test('returns 404 when the instance does not exist', async () => {
      getInstanceByIdSpy.mockReturnValue(null)
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/999999/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))

      expect(response.status).toBe(404)
      expect(await json(response)).toEqual({ error: 'Instance not found', success: false })
    })

    test('reports not-running when the instance status is not running', async () => {
      getInstanceByIdSpy.mockReturnValue(mockInstance({ status: 'stopped', port: 19500 }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body).toEqual({
        ok: false,
        message: 'Instance is not running. Start it before testing the GOWA API connection.',
      })
      expect(fetchSpy).not.toHaveBeenCalled()
    })

    test('reports not-running when the instance has no port assigned', async () => {
      getInstanceByIdSpy.mockReturnValue(mockInstance({ status: 'running', port: null }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body.ok).toBe(false)
      expect(body.message).toContain('not running')
      expect(fetchSpy).not.toHaveBeenCalled()
    })

    test('forwards basic auth header and reports a successful connection', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({
          status: 'running',
          port: 19500,
          config: JSON.stringify({ flags: { basicAuth: [{ username: 'admin', password: 'secret' }] } }),
        })
      )
      fetchSpy.mockResolvedValue(new Response('{"devices":[]}', { status: 200 }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body).toEqual({
        ok: true,
        status: 200,
        message: 'Connection successful. The instance responded to GET /devices.',
        body: '{"devices":[]}',
      })
      expect(fetchSpy).toHaveBeenCalledTimes(1)
      const [url, init] = fetchSpy.mock.calls[0]
      expect(url).toBe('http://localhost:19500/app/TESTKEY1/devices')
      expect((init as any).headers.Authorization).toBe(`Basic ${btoa('admin:secret')}`)
    })

    test('reports a failed connection when the instance responds with an error status', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({ status: 'running', port: 19500, config: '{}' })
      )
      fetchSpy.mockResolvedValue(new Response('Internal Server Error', { status: 500 }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body.ok).toBe(false)
      expect(body.status).toBe(500)
      expect(body.message).toContain('Connection failed')
      expect(body.body).toBe('Internal Server Error')
      // No auth header should be sent when basicAuth is absent.
      const [, init] = fetchSpy.mock.calls[0]
      expect((init as any).headers.Authorization).toBeUndefined()
    })

    test('truncates a large response body to 600 characters plus an ellipsis', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({ status: 'running', port: 19500, config: '{}' })
      )
      const bigBody = 'x'.repeat(1000)
      fetchSpy.mockResolvedValue(new Response(bigBody, { status: 200 }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body.body).toBe(`${'x'.repeat(600)}...`)
    })

    test('reports "No response body." when the response body is empty', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({ status: 'running', port: 19500, config: '{}' })
      )
      fetchSpy.mockResolvedValue(new Response('', { status: 204 }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body.body).toBe('No response body.')
    })

    test('tolerates invalid config JSON and tests the connection without auth', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({ status: 'running', port: 19500, config: '{bad json' })
      )
      fetchSpy.mockResolvedValue(new Response('ok', { status: 200 }))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body.ok).toBe(true)
      const [, init] = fetchSpy.mock.calls[0]
      expect((init as any).headers.Authorization).toBeUndefined()
    })

    test('returns a connection-failed payload when fetch throws', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({ status: 'running', port: 19500, config: '{}' })
      )
      fetchSpy.mockRejectedValue(new Error('ECONNREFUSED'))
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body).toEqual({
        ok: false,
        message: 'ECONNREFUSED',
      })
    })

    test('returns a generic message when fetch throws a non-Error value', async () => {
      getInstanceByIdSpy.mockReturnValue(
        mockInstance({ status: 'running', port: 19500, config: '{}' })
      )
      fetchSpy.mockRejectedValue('network down' as any)
      const app = createTestApp()

      const response = await app.handle(new Request('http://localhost/api/instances/1/test-connection', {
        method: 'POST',
        headers: authHeaders,
      }))
      const body = await json(response)

      expect(response.status).toBe(200)
      expect(body).toEqual({
        ok: false,
        message: 'Connection failed before receiving a response.',
      })
    })
  })
})
