import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test'
import { Elysia } from 'elysia'
import { proxyModule } from './index'
import { ProxyService } from './service'
import { WebSocketProxyService } from './service.websocket'

const originalIsInstanceAvailable = ProxyService.isInstanceAvailable
const originalGetProxyStatus = ProxyService.getProxyStatus
const originalForwardRequest = ProxyService.forwardRequest
const originalCreateWebSocketConnection = WebSocketProxyService.createWebSocketConnection
const originalGetWebSocketConnection = WebSocketProxyService.getWebSocketConnection
const originalCloseWebSocketConnection = WebSocketProxyService.closeWebSocketConnection

// Extract the WebSocket route handlers from the registered Elysia routes so we
// can invoke them directly with mock `ws` objects (Elysia WS routes are not
// easily exercised via app.handle()).
function getWsHandlers() {
  const wsRoute = (proxyModule as any).routes.find((r: any) => r.method === 'WS')
  return {
    open: wsRoute.hooks.open as (ws: any) => Promise<void>,
    message: wsRoute.hooks.message as (ws: any, message: any) => void,
    close: wsRoute.hooks.close as (ws: any) => void,
  }
}

describe('proxy index — handleProxyRequest edge cases', () => {
  afterEach(() => {
    ProxyService.isInstanceAvailable = originalIsInstanceAvailable
    ProxyService.getProxyStatus = originalGetProxyStatus
    ProxyService.forwardRequest = originalForwardRequest
  })

  test('returns 404 when instance is not available and not found', async () => {
    ProxyService.isInstanceAvailable = () => false
    ProxyService.getProxyStatus = () => null
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(new Request('http://localhost/app/KEY1/some/path'))

    expect(response.status).toBe(404)
    expect(await response.json()).toEqual({ error: 'Instance not found', success: false })
  })

  test('returns 503 when instance is not available but found (not running)', async () => {
    ProxyService.isInstanceAvailable = () => false
    ProxyService.getProxyStatus = () => ({
      instanceKey: 'KEY1',
      instanceName: 'stopped-instance',
      status: 'stopped',
      port: 8000,
      targetPort: 8000,
      proxyPath: 'app/KEY1',
    })
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(new Request('http://localhost/app/KEY1/some/path'))

    expect(response.status).toBe(503)
    expect(await response.json()).toEqual({
      error: 'Instance is not running',
      success: false,
      instanceKey: 'KEY1',
    })
  })

  test('forwards POST request body via arrayBuffer path', async () => {
    ProxyService.isInstanceAvailable = () => true
    const forwarded: { method: string; body?: any } = { method: '' }
    ProxyService.forwardRequest = async (_instanceKey, _path, method, _headers, body) => {
      forwarded.method = method
      forwarded.body = body
      return {
        status: 201,
        headers: { 'content-type': 'application/json' },
        body: { ok: true },
        isBinary: false,
      }
    }
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(
      new Request('http://localhost/app/KEY1/api/thing', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ hello: 'world' }),
      })
    )

    expect(response.status).toBe(201)
    expect(await response.json()).toEqual({ ok: true })
    expect(forwarded.method).toBe('POST')
    expect(forwarded.body).toBeInstanceOf(ArrayBuffer)
    expect(forwarded.body.byteLength).toBe(JSON.stringify({ hello: 'world' }).length)
  })

  test('forwards PUT request with body', async () => {
    ProxyService.isInstanceAvailable = () => true
    const forwarded: { method: string; body?: any } = { method: '' }
    ProxyService.forwardRequest = async (_instanceKey, _path, method, _headers, body) => {
      forwarded.method = method
      forwarded.body = body
      return {
        status: 200,
        headers: { 'content-type': 'application/json' },
        body: { updated: true },
        isBinary: false,
      }
    }
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(
      new Request('http://localhost/app/KEY1/api/thing/1', {
        method: 'PUT',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ value: 42 }),
      })
    )

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({ updated: true })
    expect(forwarded.method).toBe('PUT')
    expect(forwarded.body).toBeInstanceOf(ArrayBuffer)
  })

  test('returns binary response as a raw Response with status and headers', async () => {
    ProxyService.isInstanceAvailable = () => true
    const binaryData = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a])
    ProxyService.forwardRequest = async () => ({
      status: 200,
      headers: { 'content-type': 'image/png', 'content-length': '6' },
      body: binaryData.buffer,
      isBinary: true,
    })
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(new Request('http://localhost/app/KEY1/img/icon.png'))

    expect(response.status).toBe(200)
    expect(response.headers.get('content-type')).toBe('image/png')
    const buffer = await response.arrayBuffer()
    expect(new Uint8Array(buffer)).toEqual(binaryData)
  })

  test('returns binary response for instance root fallback route', async () => {
    ProxyService.isInstanceAvailable = () => true
    const binaryData = new Uint8Array([1, 2, 3, 4])
    ProxyService.forwardRequest = async () => ({
      status: 200,
      headers: { 'content-type': 'application/pdf' },
      body: binaryData.buffer,
      isBinary: true,
    })
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(new Request('http://localhost/app/KEY1'))

    expect(response.status).toBe(200)
    expect(response.headers.get('content-type')).toBe('application/pdf')
    const buffer = await response.arrayBuffer()
    expect(new Uint8Array(buffer)).toEqual(binaryData)
  })

  test('forwards non-binary response and applies headers/status via set', async () => {
    ProxyService.isInstanceAvailable = () => true
    ProxyService.forwardRequest = async () => ({
      status: 204,
      headers: { 'x-custom': 'hello' },
      body: '',
      isBinary: false,
    })
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(new Request('http://localhost/app/KEY1/api/no-content'))

    expect(response.status).toBe(204)
    expect(response.headers.get('x-custom')).toBe('hello')
  })

  test('returns 502 and sanitized error when forwardRequest throws', async () => {
    const consoleError = spyOn(console, 'error').mockImplementation(() => {})
    ProxyService.isInstanceAvailable = () => true
    ProxyService.forwardRequest = async () => {
      throw new Error('upstream exploded with secret details')
    }
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(new Request('http://localhost/app/KEY1/api/fail'))

    expect(response.status).toBe(502)
    expect(await response.json()).toEqual({ error: 'Proxy request failed', success: false })
    consoleError.mockRestore()
  })

  test('GET request does not attempt to read body', async () => {
    ProxyService.isInstanceAvailable = () => true
    let receivedBody: any = 'unset'
    ProxyService.forwardRequest = async (_instanceKey, _path, _method, _headers, body) => {
      receivedBody = body
      return {
        status: 200,
        headers: { 'content-type': 'application/json' },
        body: { ok: true },
        isBinary: false,
      }
    }
    const app = new Elysia().use(proxyModule)

    const response = await app.handle(new Request('http://localhost/app/KEY1/api/list'))

    expect(response.status).toBe(200)
    // For GET requests, body should remain undefined (not read)
    expect(receivedBody).toBeUndefined()
  })
})

describe('proxy index — WebSocket handlers', () => {
  let consoleError: ReturnType<typeof spyOn>
  let consoleLog: ReturnType<typeof spyOn>

  beforeEach(() => {
    consoleError = spyOn(console, 'error').mockImplementation(() => {})
    consoleLog = spyOn(console, 'log').mockImplementation(() => {})
  })

  afterEach(() => {
    consoleError.mockRestore()
    consoleLog.mockRestore()
    ProxyService.isInstanceAvailable = originalIsInstanceAvailable
    WebSocketProxyService.createWebSocketConnection = originalCreateWebSocketConnection
    WebSocketProxyService.getWebSocketConnection = originalGetWebSocketConnection
    WebSocketProxyService.closeWebSocketConnection = originalCloseWebSocketConnection
  })

  function createMockWs(data: Record<string, any> = {}) {
    const ws = {
      data: {
        params: { instanceKey: 'KEY1' },
        ...data,
      },
      closed: false,
      sent: [] as string[],
      close() {
        ws.closed = true
      },
      send(payload: string) {
        ws.sent.push(payload)
      },
    }
    return ws
  }

  test('open handler closes ws when instance is not available', async () => {
    const { open } = getWsHandlers()
    ProxyService.isInstanceAvailable = () => false
    const ws = createMockWs()

    await open(ws)

    expect(ws.closed).toBe(true)
  })

  test('open handler closes ws when createWebSocketConnection returns null', async () => {
    const { open } = getWsHandlers()
    ProxyService.isInstanceAvailable = () => true
    WebSocketProxyService.createWebSocketConnection = (async () => null) as any
    const ws = createMockWs()

    await open(ws)

    expect(ws.closed).toBe(true)
  })

  test('open handler creates connection and wires proxy message/close/error events', async () => {
    const { open } = getWsHandlers()
    ProxyService.isInstanceAvailable = () => true

    // Capture listeners per-created proxy ws so we can simulate upstream events.
    const created: { listeners: Record<string, ((...args: any[]) => void)[]> }[] = []
    const makeProxyWs = () => {
      const listeners: Record<string, ((...args: any[]) => void)[]> = {}
      const proxyWs = {
        OPEN: 1,
        readyState: 1,
        on(event: string, cb: (...args: any[]) => void) {
          (listeners[event] ||= []).push(cb)
        },
        send: (_data: any) => {},
        close: () => {},
      }
      created.push({ listeners })
      return proxyWs
    }
    WebSocketProxyService.createWebSocketConnection = (async () => makeProxyWs()) as any

    const ws = createMockWs({ query: { foo: 'bar' }, headers: { authorization: 'Basic abc', origin: 'http://localhost' } })

    await open(ws)

    expect((ws.data as any).proxyConnectionId).toBeDefined()
    expect(ws.closed).toBe(false)

    // Simulate a message from the upstream proxy -> should forward to client ws
    created[0].listeners['message'][0]('hello from upstream')
    expect(ws.sent).toEqual(['hello from upstream'])

    // Simulate proxy close -> should close client ws
    created[0].listeners['close'][0]()
    expect(ws.closed).toBe(true)
  })

  test('open handler closes client ws when upstream proxy emits an error', async () => {
    const { open } = getWsHandlers()
    ProxyService.isInstanceAvailable = () => true

    const listeners: Record<string, ((...args: any[]) => void)[]> = {}
    const proxyWs = {
      OPEN: 1,
      readyState: 1,
      on(event: string, cb: (...args: any[]) => void) {
        (listeners[event] ||= []).push(cb)
      },
      send: (_data: any) => {},
      close: () => {},
    }
    WebSocketProxyService.createWebSocketConnection = (async () => proxyWs) as any

    const ws = createMockWs({ headers: {} })
    await open(ws)

    // Simulate proxy error -> should close client ws
    listeners['error'][0](new Error('upstream ws error'))
    expect(ws.closed).toBe(true)
  })

  test('open handler swallows errors thrown inside proxy message forwarding', async () => {
    const { open } = getWsHandlers()
    ProxyService.isInstanceAvailable = () => true

    const listeners: Record<string, ((...args: any[]) => void)[]> = {}
    const proxyWs = {
      OPEN: 1,
      readyState: 1,
      on(event: string, cb: (...args: any[]) => void) {
        (listeners[event] ||= []).push(cb)
      },
      send: (_data: any) => {},
      close: () => {},
    }
    WebSocketProxyService.createWebSocketConnection = (async () => proxyWs) as any

    const ws = createMockWs({ headers: {} })
    ws.send = () => {
      throw new Error('client send failed')
    }
    await open(ws)

    // Should not throw; error is caught and logged internally
    expect(() => listeners['message'][0]('data')).not.toThrow()
  })

  test('open handler forwards array header values as joined strings', async () => {
    const { open } = getWsHandlers()
    ProxyService.isInstanceAvailable = () => true

    let receivedHeaders: Record<string, string> | undefined
    const proxyWs = { OPEN: 1, readyState: 1, on: () => {}, send: () => {}, close: () => {} }
    WebSocketProxyService.createWebSocketConnection = (async (_id: string, _key: string, _path: string, headers?: Record<string, string>) => {
      receivedHeaders = headers
      return proxyWs as any
    }) as any

    const ws = createMockWs({ headers: { 'set-cookie': ['a=1', 'b=2'], origin: 'http://localhost' } })

    await open(ws)

    expect(receivedHeaders).toBeDefined()
    expect(receivedHeaders!['set-cookie']).toBe('a=1, b=2')
    expect(receivedHeaders!['origin']).toBe('http://localhost')
  })

  test('open handler closes ws when createWebSocketConnection throws', async () => {
    const { open } = getWsHandlers()
    ProxyService.isInstanceAvailable = () => true
    WebSocketProxyService.createWebSocketConnection = (async () => {
      throw new Error('boom')
    }) as any
    const ws = createMockWs()

    await open(ws)

    expect(ws.closed).toBe(true)
  })

  test('message handler closes ws when no active proxy connection exists', () => {
    const { message } = getWsHandlers()
    WebSocketProxyService.getWebSocketConnection = (() => null) as any
    const ws = createMockWs({ proxyConnectionId: 'conn-1' })

    message(ws, 'hello')

    expect(ws.closed).toBe(true)
  })

  test('message handler closes ws when proxy ws is not in OPEN state', () => {
    const { message } = getWsHandlers()
    const proxyWs = { OPEN: 1, readyState: 0 /* CONNECTING */, send: () => {} }
    WebSocketProxyService.getWebSocketConnection = (() => proxyWs) as any
    const ws = createMockWs({ proxyConnectionId: 'conn-1' })

    message(ws, 'hello')

    expect(ws.closed).toBe(true)
  })

  test('message handler forwards string message to proxy ws', () => {
    const { message } = getWsHandlers()
    const sent: any[] = []
    const proxyWs = { OPEN: 1, readyState: 1, send: (data: any) => sent.push(data) }
    WebSocketProxyService.getWebSocketConnection = (() => proxyWs) as any
    const ws = createMockWs({ proxyConnectionId: 'conn-1' })

    message(ws, 'hello-to-upstream')

    expect(sent).toEqual(['hello-to-upstream'])
    expect(ws.closed).toBe(false)
  })

  test('message handler closes ws when proxy send throws', () => {
    const { message } = getWsHandlers()
    const proxyWs = {
      OPEN: 1,
      readyState: 1,
      send: () => {
        throw new Error('send failed')
      },
    }
    WebSocketProxyService.getWebSocketConnection = (() => proxyWs) as any
    const ws = createMockWs({ proxyConnectionId: 'conn-1' })

    message(ws, 'hello')

    expect(ws.closed).toBe(true)
  })

  test('message handler does nothing when proxyConnectionId is missing', () => {
    const { message } = getWsHandlers()
    let called = false
    WebSocketProxyService.getWebSocketConnection = (() => {
      called = true
      return null
    }) as any
    const ws = createMockWs({}) // no proxyConnectionId

    message(ws, 'hello')

    // getWebSocketConnection should not be called with undefined connectionId
    expect(called).toBe(false)
    expect(ws.closed).toBe(true)
  })

  test('close handler closes the proxy connection when connectionId is present', () => {
    const { close } = getWsHandlers()
    let closedId: string | undefined
    WebSocketProxyService.closeWebSocketConnection = ((id: string) => {
      closedId = id
    }) as any
    const ws = createMockWs({ proxyConnectionId: 'conn-42' })

    close(ws)

    expect(closedId).toBe('conn-42')
  })

  test('close handler does nothing when connectionId is missing', () => {
    const { close } = getWsHandlers()
    let called = false
    WebSocketProxyService.closeWebSocketConnection = (() => {
      called = true
    }) as any
    const ws = createMockWs({}) // no proxyConnectionId

    close(ws)

    expect(called).toBe(false)
  })
})
