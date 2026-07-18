import { afterEach, describe, expect, test } from 'bun:test'
import { queries } from '../../db'
import { ProxyService } from './service'

const originalGetInstanceByKey = queries.getInstanceByKey.get
const originalFetch = globalThis.fetch

function mockInstance(overrides: Partial<{ status: string; port: number | null }> = {}) {
  queries.getInstanceByKey.get = (() => ({
    id: 1,
    key: 'ABC12345',
    name: 'proxy-test',
    status: overrides.status ?? 'running',
    port: Object.hasOwn(overrides, 'port') ? overrides.port : 18080,
    config: '{}',
    gowa_version: 'latest',
    created_at: '',
    updated_at: '',
  })) as any
}

function mockRunningInstance() {
  mockInstance()
}

function createJsonResponse(body: unknown, init: ResponseInit = {}) {
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    headers: {
      'content-type': 'application/json',
      ...(init.headers as Record<string, string> | undefined),
    },
  })
}

describe('ProxyService.forwardRequest', () => {
  afterEach(() => {
    queries.getInstanceByKey.get = originalGetInstanceByKey
    globalThis.fetch = originalFetch
  })

  test('forwards auth and rewrites host/forwarded headers', async () => {
    mockRunningInstance()
    let captured: { url: string; init: RequestInit } | null = null
    globalThis.fetch = (async (url, init) => {
      captured = { url: String(url), init: init ?? {} }
      return createJsonResponse({ ok: true })
    }) as typeof fetch

    await ProxyService.forwardRequest(
      'ABC12345',
      '/app/ABC12345/app/devices?limit=1',
      'GET',
      {
        host: 'localhost:3001',
        authorization: 'Basic instance-token',
        cookie: 'session=abc',
      }
    )

    expect(captured?.url).toBe('http://localhost:18080/app/ABC12345/app/devices?limit=1')
    expect(captured?.init.method).toBe('GET')
    expect(captured?.init.headers).toMatchObject({
      authorization: 'Basic instance-token',
      cookie: 'session=abc',
      'X-Forwarded-For': 'localhost',
      'X-Forwarded-Proto': 'http',
      'X-Forwarded-Host': 'localhost:3001',
    })
    expect((captured?.init.headers as Record<string, string>).host).toBeUndefined()
  })

  test('uses incoming x-forwarded-for when present', async () => {
    mockRunningInstance()
    let forwardedFor: string | undefined
    globalThis.fetch = (async (_url, init) => {
      forwardedFor = (init?.headers as Record<string, string>)['X-Forwarded-For']
      return createJsonResponse({ ok: true })
    }) as typeof fetch

    await ProxyService.forwardRequest('ABC12345', '/app/devices', 'GET', {
      'x-forwarded-for': '203.0.113.10',
    })

    expect(forwardedFor).toBe('203.0.113.10')
  })

  test('serializes object body as JSON and sets content type when missing', async () => {
    mockRunningInstance()
    let captured: RequestInit | null = null
    globalThis.fetch = (async (_url, init) => {
      captured = init ?? null
      return createJsonResponse({ ok: true })
    }) as typeof fetch

    await ProxyService.forwardRequest('ABC12345', '/app/send', 'POST', {}, { text: 'hello' })

    expect(captured?.body).toBe(JSON.stringify({ text: 'hello' }))
    expect(captured?.headers).toMatchObject({ 'content-type': 'application/json' })
  })

  test('preserves text body and existing content type', async () => {
    mockRunningInstance()
    let captured: RequestInit | null = null
    globalThis.fetch = (async (_url, init) => {
      captured = init ?? null
      return new Response('ok', { headers: { 'content-type': 'text/plain' } })
    }) as typeof fetch

    const response = await ProxyService.forwardRequest(
      'ABC12345',
      '/app/send',
      'POST',
      { 'content-type': 'text/plain' },
      'hello'
    )

    expect(captured?.body).toBe('hello')
    expect(captured?.headers).toMatchObject({ 'content-type': 'text/plain' })
    expect(response.body).toBe('ok')
  })

  test('strips absolute URLs in JSON responses', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () => createJsonResponse({
      url: 'http://localhost:18080/app/devices?limit=1#top',
      nested: { callback: 'https://example.com/hook' },
    })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/app/devices', 'GET', {})

    expect(response.body).toEqual({
      url: '/app/devices?limit=1#top',
      nested: { callback: '/hook' },
    })
    expect(response.isBinary).toBe(false)
  })

  test('passes binary responses through as array buffer', async () => {
    mockRunningInstance()
    const bytes = new Uint8Array([1, 2, 3])
    globalThis.fetch = (async () => new Response(bytes, {
      headers: { 'content-type': 'application/octet-stream' },
    })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/file.bin', 'GET', {})

    expect(response.isBinary).toBe(true)
    expect(new Uint8Array(response.body)).toEqual(bytes)
  })

  test('healthCheck returns false for missing instance', async () => {
    queries.getInstanceByKey.get = (() => null) as any

    expect(await ProxyService.healthCheck('MISSING1')).toBe(false)
  })

  test('healthCheck returns false for stopped instance', async () => {
    mockInstance({ status: 'stopped', port: 18080 })

    expect(await ProxyService.healthCheck('ABC12345')).toBe(false)
  })

  test('healthCheck returns false when running instance has no port', async () => {
    mockInstance({ status: 'running', port: null })

    expect(await ProxyService.healthCheck('ABC12345')).toBe(false)
  })

  test('healthCheck returns true when upstream responds ok', async () => {
    mockRunningInstance()
    let capturedUrl = ''
    globalThis.fetch = (async (url) => {
      capturedUrl = String(url)
      return new Response('ok', { status: 200 })
    }) as typeof fetch

    expect(await ProxyService.healthCheck('ABC12345')).toBe(true)
    expect(capturedUrl).toBe('http://localhost:18080/')
  })

  test('healthCheck returns false when upstream times out or throws', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () => {
      throw new Error('timeout')
    }) as unknown as typeof fetch

    expect(await ProxyService.healthCheck('ABC12345')).toBe(false)
  })
})

describe('ProxyService.forwardRequest error paths', () => {
  afterEach(() => {
    queries.getInstanceByKey.get = originalGetInstanceByKey
    globalThis.fetch = originalFetch
  })

  test('throws "Instance not found" when instance is missing', async () => {
    queries.getInstanceByKey.get = (() => null) as any

    await expect(
      ProxyService.forwardRequest('MISSING', '/path', 'GET', {})
    ).rejects.toThrow('Instance not found')
  })

  test('throws "Instance is not running" when instance status is not running', async () => {
    mockInstance({ status: 'stopped', port: 18080 })

    await expect(
      ProxyService.forwardRequest('ABC12345', '/path', 'GET', {})
    ).rejects.toThrow('Instance is not running')
  })

  test('throws "Instance is not running" when running instance has no port', async () => {
    mockInstance({ status: 'running', port: null })

    await expect(
      ProxyService.forwardRequest('ABC12345', '/path', 'GET', {})
    ).rejects.toThrow('Instance is not running')
  })

  test('wraps upstream fetch errors as "Proxy request failed"', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () => {
      throw new Error('network down')
    }) as unknown as typeof fetch

    await expect(
      ProxyService.forwardRequest('ABC12345', '/path', 'GET', {})
    ).rejects.toThrow('Proxy request failed')
  })

  test('forwards ArrayBuffer body as-is without JSON serialization', async () => {
    mockRunningInstance()
    let captured: RequestInit | null = null
    globalThis.fetch = (async (_url, init) => {
      captured = init ?? null
      return createJsonResponse({ ok: true })
    }) as typeof fetch

    const buffer = new ArrayBuffer(4)
    const view = new Uint8Array(buffer)
    view.set([1, 2, 3, 4])

    await ProxyService.forwardRequest('ABC12345', '/upload', 'POST', {}, buffer)

    expect(captured?.body).toBe(buffer)
  })

  test('returns raw text when JSON response fails to parse', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () =>
      new Response('not-valid-json{', {
        headers: { 'content-type': 'application/json' },
      })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/data', 'GET', {})

    expect(response.body).toBe('not-valid-json{')
    expect(response.isBinary).toBe(false)
  })

  test('strips absolute URLs inside JSON arrays', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () =>
      createJsonResponse({
        items: [
          { link: 'http://localhost:18080/a' },
          { link: 'https://example.com/b' },
        ],
      })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/list', 'GET', {})

    expect(response.body).toEqual({
      items: [
        { link: '/a' },
        { link: '/b' },
      ],
    })
  })

  test('preserves non-URL string values in JSON responses', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () =>
      createJsonResponse({
        name: 'plain text',
        count: 5,
        flag: true,
      })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/meta', 'GET', {})

    expect(response.body).toEqual({ name: 'plain text', count: 5, flag: true })
  })

  test('preserves malformed URL strings as-is in JSON responses', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () =>
      createJsonResponse({
        bad: 'http://[invalid-url',
      })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/meta', 'GET', {})

    expect(response.body).toEqual({ bad: 'http://[invalid-url' })
  })

  test('passes through various binary content types as array buffer', async () => {
    mockRunningInstance()
    const bytes = new Uint8Array([10, 20, 30])
    globalThis.fetch = (async () =>
      new Response(bytes, {
        headers: { 'content-type': 'image/png' },
      })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/img.png', 'GET', {})

    expect(response.isBinary).toBe(true)
    expect(new Uint8Array(response.body)).toEqual(bytes)
  })

  test('returns text body for non-JSON, non-binary content types', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () =>
      new Response('<html>hi</html>', {
        headers: { 'content-type': 'text/html' },
      })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/page', 'GET', {})

    expect(response.body).toBe('<html>hi</html>')
    expect(response.isBinary).toBe(false)
  })

  test('includes response status and headers in result', async () => {
    mockRunningInstance()
    globalThis.fetch = (async () =>
      new Response('created', {
        status: 201,
        headers: { 'content-type': 'text/plain', 'x-custom': 'yes' },
      })) as unknown as typeof fetch

    const response = await ProxyService.forwardRequest('ABC12345', '/create', 'POST', {}, 'data')

    expect(response.status).toBe(201)
    expect(response.headers['x-custom']).toBe('yes')
  })
})

describe('ProxyService.isInstanceAvailable', () => {
  afterEach(() => {
    queries.getInstanceByKey.get = originalGetInstanceByKey
  })

  test('returns a truthy value for running instance with a port', () => {
    mockRunningInstance()

    expect(ProxyService.isInstanceAvailable('ABC12345')).toBeTruthy()
  })

  test('returns a falsy value when instance is missing', () => {
    queries.getInstanceByKey.get = (() => null) as any

    expect(ProxyService.isInstanceAvailable('MISSING')).toBeFalsy()
  })

  test('returns a falsy value when instance is not running', () => {
    mockInstance({ status: 'stopped', port: 18080 })

    expect(ProxyService.isInstanceAvailable('ABC12345')).toBeFalsy()
  })

  test('returns a falsy value when running instance has no port', () => {
    mockInstance({ status: 'running', port: null })

    expect(ProxyService.isInstanceAvailable('ABC12345')).toBeFalsy()
  })
})

describe('ProxyService.getProxyStatus', () => {
  afterEach(() => {
    queries.getInstanceByKey.get = originalGetInstanceByKey
  })

  test('returns null when instance is missing', () => {
    queries.getInstanceByKey.get = (() => null) as any

    expect(ProxyService.getProxyStatus('MISSING')).toBeNull()
  })

  test('returns status shape for a running instance', () => {
    mockRunningInstance()

    const status = ProxyService.getProxyStatus('ABC12345')

    expect(status).toEqual({
      instanceKey: 'ABC12345',
      instanceName: 'proxy-test',
      status: 'running',
      port: 18080,
      targetPort: 18080,
      proxyPath: 'app/ABC12345',
    })
  })

  test('returns status shape for a stopped instance with null port', () => {
    mockInstance({ status: 'stopped', port: null })

    const status = ProxyService.getProxyStatus('ABC12345')

    expect(status).toEqual({
      instanceKey: 'ABC12345',
      instanceName: 'proxy-test',
      status: 'stopped',
      port: null,
      targetPort: null,
      proxyPath: 'app/ABC12345',
    })
  })
})

describe('ProxyService.getAvailableProxyTargets', () => {
  const originalGetAllInstances = queries.getAllInstances.all

  afterEach(() => {
    queries.getAllInstances.all = originalGetAllInstances
  })

  test('returns only running instances with a port', () => {
    queries.getAllInstances.all = (() => [
      { id: 1, key: 'AAA11111', name: 'one', status: 'running', port: 18080 },
      { id: 2, key: 'BBB22222', name: 'two', status: 'stopped', port: 18081 },
      { id: 3, key: 'CCC33333', name: 'three', status: 'running', port: null },
      { id: 4, key: 'DDD44444', name: 'four', status: 'running', port: 18082 },
    ]) as any

    const targets = ProxyService.getAvailableProxyTargets()

    expect(targets).toEqual([
      {
        instanceKey: 'AAA11111',
        instanceName: 'one',
        status: 'running',
        port: 18080,
        targetPort: 18080,
        proxyPath: 'app/AAA11111',
      },
      {
        instanceKey: 'DDD44444',
        instanceName: 'four',
        status: 'running',
        port: 18082,
        targetPort: 18082,
        proxyPath: 'app/DDD44444',
      },
    ])
  })

  test('returns empty array when no instances are running', () => {
    queries.getAllInstances.all = (() => [
      { id: 1, key: 'AAA11111', name: 'one', status: 'stopped', port: 18080 },
    ]) as any

    expect(ProxyService.getAvailableProxyTargets()).toEqual([])
  })

  test('returns empty array when there are no instances', () => {
    queries.getAllInstances.all = (() => []) as any

    expect(ProxyService.getAvailableProxyTargets()).toEqual([])
  })
})

// NOTE: `ProxyService.modifyJsonUrls` (service.ts lines 136-161) is a private
// static method that is currently unused by any code path. It cannot be
// invoked directly (TypeScript private) and is never called internally, so it
// is intentionally left uncovered. Reaching 85% line coverage does not
// require exercising it.
