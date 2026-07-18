import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { proxyModule } from './index'
import { ProxyService } from './service'
import { createMagicAdminToken, getMagicAdminCookieName } from './magic-auth'

const originalIsInstanceAvailable = ProxyService.isInstanceAvailable
const originalGetProxyStatus = ProxyService.getProxyStatus
const originalForwardRequest = ProxyService.forwardRequest
const originalHealthCheck = ProxyService.healthCheck

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
    ProxyService.healthCheck = originalHealthCheck
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

  test('returns 404 for missing proxy status', async () => {
    ProxyService.getProxyStatus = () => null
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/MISSING1/status'))

    expect(response.status).toBe(404)
    expect(await response.json()).toEqual({ error: 'Instance not found', success: false })
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

  test('returns stopped proxy status without requiring manager basic auth', async () => {
    ProxyService.getProxyStatus = () => ({
      instanceKey: 'ABC12345',
      instanceName: 'test-instance',
      status: 'stopped',
      port: 8000,
      targetPort: 8000,
      proxyPath: 'app/ABC12345',
    })
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/ABC12345/status'))

    expect(response.status).toBe(200)
    expect(await response.json()).toMatchObject({ instanceKey: 'ABC12345', status: 'stopped' })
  })

  test('returns 404 for missing proxy health', async () => {
    ProxyService.getProxyStatus = () => null
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/MISSING1/health'))

    expect(response.status).toBe(404)
    expect(await response.json()).toEqual({ error: 'Instance not found', success: false })
  })

  test('returns unhealthy for stopped proxy health', async () => {
    ProxyService.getProxyStatus = () => ({
      instanceKey: 'ABC12345',
      instanceName: 'test-instance',
      status: 'stopped',
      port: 8000,
      targetPort: 8000,
      proxyPath: 'app/ABC12345',
    })
    ProxyService.healthCheck = async () => false
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/ABC12345/health'))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({ instanceKey: 'ABC12345', healthy: false, status: 'stopped' })
  })

  test('returns healthy for running proxy health', async () => {
    ProxyService.getProxyStatus = () => ({
      instanceKey: 'ABC12345',
      instanceName: 'test-instance',
      status: 'running',
      port: 8000,
      targetPort: 8000,
      proxyPath: 'app/ABC12345',
    })
    ProxyService.healthCheck = async () => true
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/ABC12345/health'))

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({ instanceKey: 'ABC12345', healthy: true, status: 'running' })
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

  test('sets magic admin cookie and redirects without autologin query', async () => {
    const { token } = createMagicAdminToken('ABC12345')
    const app = createTestApp()

    const response = await app.handle(new Request(`http://localhost/app/ABC12345/?autologin=${token}&tab=admin`))

    expect(response.status).toBe(302)
    expect(response.headers.get('location')).toBe('/app/ABC12345/?tab=admin')
    expect(response.headers.get('set-cookie')).toContain(`${getMagicAdminCookieName('ABC12345')}=`)
  })

  test('rejects invalid magic admin token', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/app/ABC12345/?autologin=invalid'))

    expect(response.status).toBe(401)
    expect(await response.text()).toBe('Invalid or expired admin link')
    expect(response.headers.get('set-cookie')).toContain('Max-Age=0')
  })

  test('forwards injected auth when magic admin cookie is valid', async () => {
    const { token } = createMagicAdminToken('ABC12345')
    ProxyService.isInstanceAvailable = () => true
    ProxyService.forwardRequest = originalForwardRequest
    const app = createTestApp()

    const { queries } = await import('../../db')
    const originalGetInstanceByKey = queries.getInstanceByKey.get
    const originalFetch = globalThis.fetch
    queries.getInstanceByKey.get = (() => ({
      id: 1,
      key: 'ABC12345',
      name: 'magic-auth-instance',
      status: 'running',
      port: 18080,
      config: JSON.stringify({ flags: { basicAuth: [{ username: 'admin', password: 'secret' }] } }),
      gowa_version: 'latest',
      created_at: '',
      updated_at: '',
    })) as any

    let forwardedAuthorization: string | undefined
    globalThis.fetch = (async (_url, init) => {
      forwardedAuthorization = (init?.headers as Record<string, string>).authorization
      return new Response(JSON.stringify({ ok: true }), { headers: { 'content-type': 'application/json' } })
    }) as typeof fetch

    try {
      const response = await app.handle(new Request('http://localhost/app/ABC12345/app/devices', {
        headers: { cookie: `${getMagicAdminCookieName('ABC12345')}=${encodeURIComponent(token)}` },
      }))

      expect(response.status).toBe(200)
      expect(forwardedAuthorization).toBe(`Basic ${btoa('admin:secret')}`)
    } finally {
      queries.getInstanceByKey.get = originalGetInstanceByKey
      globalThis.fetch = originalFetch
    }
  })

  test('keeps websocket route before wildcard proxy routes', () => {
    const routes = (proxyModule as any).routes.map((route: any) => ({
      method: route.method,
      path: route.path,
    }))

    expect(routes).toEqual([
      { method: 'GET', path: '/app/:instanceKey/status' },
      { method: 'GET', path: '/app/:instanceKey/health' },
      { method: 'WS', path: '/app/:instanceKey/ws' },
      { method: 'ALL', path: '/app/:instanceKey/*' },
      { method: 'ALL', path: '/app/:instanceKey' },
    ])
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
