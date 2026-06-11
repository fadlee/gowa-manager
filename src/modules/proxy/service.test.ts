import { afterEach, describe, expect, test } from 'bun:test'
import { queries } from '../../db'
import { ProxyService } from './service'

const originalGetInstanceByKey = queries.getInstanceByKey.get
const originalFetch = globalThis.fetch

function mockRunningInstance() {
  queries.getInstanceByKey.get = (() => ({
    id: 1,
    key: 'ABC12345',
    name: 'proxy-test',
    status: 'running',
    port: 18080,
    config: '{}',
    gowa_version: 'latest',
    created_at: '',
    updated_at: '',
  })) as any
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
})
