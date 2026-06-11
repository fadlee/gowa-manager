import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { proxyModule } from './index'
import { ProxyService } from './service'

const originalIsInstanceAvailable = ProxyService.isInstanceAvailable
const originalGetProxyStatus = ProxyService.getProxyStatus
const originalForwardRequest = ProxyService.forwardRequest

function createTestApp() {
  return new Elysia()
    .use(proxyModule)
    .guard(
      {
        beforeHandle: basicAuth('manager', 'secret'),
      },
      (app) => app.get('/api/protected', () => ({ success: true }))
    )
}

function basicHeader(username: string, password: string) {
  return `Basic ${btoa(`${username}:${password}`)}`
}

describe('proxy route auth behavior', () => {
  afterEach(() => {
    ProxyService.isInstanceAvailable = originalIsInstanceAvailable
    ProxyService.getProxyStatus = originalGetProxyStatus
    ProxyService.forwardRequest = originalForwardRequest
  })

  test('keeps manager API routes protected by manager basic auth', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/protected'))

    expect(response.status).toBe(401)
  })

  test('allows manager API routes with manager basic auth', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/protected', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({ success: true })
  })

  test('does not require manager basic auth for proxy status routes', async () => {
    ProxyService.getProxyStatus = () => ({
      instanceKey: 'ABC12345',
      instanceName: 'test-instance',
      status: 'running',
      port: 8000,
      targetPort: 8000,
      proxyPath: 'app/ABC12345',
    })
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/ABC12345/status'))

    expect(response.status).toBe(200)
    expect(await response.json()).toMatchObject({ instanceKey: 'ABC12345', status: 'running' })
  })

  test('does not require manager basic auth for proxied wildcard requests', async () => {
    ProxyService.isInstanceAvailable = () => true
    ProxyService.forwardRequest = async (_instanceKey, path, method, headers) => ({
      status: 200,
      headers: { 'content-type': 'application/json' },
      body: { path, method, managerAuthSeen: headers.authorization === basicHeader('manager', 'secret') },
      isBinary: false,
    })
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/ABC12345/app/devices?limit=1'))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({
      path: '/app/ABC12345/app/devices?limit=1',
      method: 'GET',
      managerAuthSeen: false,
    })
  })

  test('returns sanitized 502 when proxy forwarding throws', async () => {
    const consoleError = spyOn(console, 'error').mockImplementation(() => {})
    ProxyService.isInstanceAvailable = () => true
    ProxyService.forwardRequest = async () => {
      throw new Error('sensitive upstream failure details')
    }
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/ABC12345/app/devices'))

    expect(response.status).toBe(502)
    expect(await response.json()).toEqual({ error: 'Proxy request failed', success: false })
    consoleError.mockRestore()
  })
})
