import { afterEach, describe, expect, mock, spyOn, test } from 'bun:test'
import { queries } from '../../db'
import { WebSocketProxyService } from './service.websocket'

// --- Fake WebSocket --------------------------------------------------------
// Records constructor args and collects close/error handlers so tests can
// simulate socket lifecycle events without a real WS server.

class FakeWebSocket {
  static lastInstance: FakeWebSocket | null = null
  static instances: FakeWebSocket[] = []
  static lastUrl: string | null = null
  static lastSubprotocols: string[] | undefined = undefined
  static lastOptions: { headers: Record<string, string> } | undefined = undefined

  closeCount = 0
  closeHandlers: Array<() => void> = []
  errorHandlers: Array<(err: unknown) => void> = []
  closed = false

  constructor(
    url: string,
    subprotocols?: string[],
    options?: { headers: Record<string, string> },
  ) {
    FakeWebSocket.lastInstance = this
    FakeWebSocket.instances.push(this)
    FakeWebSocket.lastUrl = url
    FakeWebSocket.lastSubprotocols = subprotocols
    FakeWebSocket.lastOptions = options
  }

  on(event: string, handler: (...args: unknown[]) => void): void {
    if (event === 'close') this.closeHandlers.push(handler as () => void)
    if (event === 'error') this.errorHandlers.push(handler as (err: unknown) => void)
  }

  close(): void {
    this.closeCount += 1
    this.closed = true
  }
}

mock.module('ws', () => ({
  WebSocket: FakeWebSocket,
}))

// --- Spies -----------------------------------------------------------------

const consoleLogSpy = spyOn(console, 'log').mockImplementation(() => {})
const consoleErrorSpy = spyOn(console, 'error').mockImplementation(() => {})

// Spy on the real prepared statement's `.get` method so we control what
// `queries.getInstanceByKey.get(key)` returns without replacing the entire
// `queries` object (which would break other test files that rely on it).
const getInstanceByKeyGetSpy = spyOn(queries.getInstanceByKey, 'get')

// --- Helpers ---------------------------------------------------------------

function setInstance(overrides: Partial<{
  key: string
  status: string
  port: number | null
  config: string | null
}>) {
  const instance = {
    id: 1,
    key: overrides.key ?? 'RUNNING01',
    name: 'test-instance',
    port: overrides.port === undefined ? 18000 : overrides.port,
    status: overrides.status ?? 'running',
    config: overrides.config ?? null,
    gowa_version: 'latest',
    error_message: null,
    created_at: 0,
  }
  getInstanceByKeyGetSpy.mockImplementation((key: string) =>
    key === instance.key ? instance : null,
  )
  return instance
}

// Register multiple instances so the spy can resolve several keys in one test.
function setInstances(...instances: Array<ReturnType<typeof setInstance>>) {
  getInstanceByKeyGetSpy.mockImplementation((key: string) =>
    instances.find((i) => i.key === key) ?? null,
  )
}

function resetState() {
  FakeWebSocket.lastInstance = null
  FakeWebSocket.instances = []
  FakeWebSocket.lastUrl = null
  FakeWebSocket.lastSubprotocols = undefined
  FakeWebSocket.lastOptions = undefined
  getInstanceByKeyGetSpy.mockReset()
  getInstanceByKeyGetSpy.mockImplementation(() => null)
  // Close any leftover connections so registry state doesn't leak between tests.
  WebSocketProxyService.closeAllWebSocketConnections()
}

// --- Tests -----------------------------------------------------------------

describe('WebSocketProxyService.createWebSocketConnection', () => {
  afterEach(() => {
    resetState()
  })

  test('returns null when instance is missing', async () => {
    getInstanceByKeyGetSpy.mockImplementation(() => null)

    const result = await WebSocketProxyService.createWebSocketConnection(
      'conn-1',
      'MISSING',
      '/ws',
    )

    expect(result).toBeNull()
    expect(FakeWebSocket.lastUrl).toBeNull()
  })

  test('returns null when instance status is not running', async () => {
    setInstance({ key: 'STOPPED01', status: 'stopped', port: 18001 })

    const result = await WebSocketProxyService.createWebSocketConnection(
      'conn-2',
      'STOPPED01',
      '/ws',
    )

    expect(result).toBeNull()
    expect(FakeWebSocket.lastUrl).toBeNull()
  })

  test('returns null when instance has no port', async () => {
    setInstance({ key: 'NOPORT001', status: 'running', port: null })

    const result = await WebSocketProxyService.createWebSocketConnection(
      'conn-3',
      'NOPORT001',
      '/ws',
    )

    expect(result).toBeNull()
    expect(FakeWebSocket.lastUrl).toBeNull()
  })

  test('builds ws://localhost:{port}{path} URL for a running instance', async () => {
    setInstance({ key: 'RUNNING01', status: 'running', port: 18080 })

    await WebSocketProxyService.createWebSocketConnection(
      'conn-4',
      'RUNNING01',
      '/ws?token=abc',
    )

    expect(FakeWebSocket.lastUrl).toBe('ws://localhost:18080/ws?token=abc')
  })

  test('forwards allowlisted headers and subprotocols from incoming headers', async () => {
    setInstance({ key: 'RUNNING02', status: 'running', port: 18081 })

    await WebSocketProxyService.createWebSocketConnection(
      'conn-5',
      'RUNNING02',
      '/ws',
      {
        authorization: 'Basic dXNlcjpwYXNz',
        cookie: 'session=1',
        origin: 'http://localhost',
        'sec-websocket-protocol': 'chat, superchat',
        'x-internal': 'should-not-forward',
      },
    )

    expect(FakeWebSocket.lastSubprotocols).toEqual(['chat', 'superchat'])
    expect(FakeWebSocket.lastOptions?.headers).toMatchObject({
      authorization: 'Basic dXNlcjpwYXNz',
      cookie: 'session=1',
      origin: 'http://localhost',
    })
    expect(FakeWebSocket.lastOptions?.headers).not.toHaveProperty('x-internal')
  })

  test('injects instance basic auth header when incoming headers lack authorization', async () => {
    setInstance({
      key: 'AUTHED001',
      status: 'running',
      port: 18082,
      config: JSON.stringify({
        flags: { basicAuth: [{ username: 'admin', password: 'secret' }] },
      }),
    })

    await WebSocketProxyService.createWebSocketConnection(
      'conn-6',
      'AUTHED001',
      '/ws',
      { origin: 'http://localhost' },
    )

    expect(FakeWebSocket.lastOptions?.headers.authorization).toBe(
      `Basic ${btoa('admin:secret')}`,
    )
  })

  test('returns the created WebSocket and registers it', async () => {
    setInstance({ key: 'RUNNING03', status: 'running', port: 18083 })

    const ws = await WebSocketProxyService.createWebSocketConnection(
      'conn-7',
      'RUNNING03',
      '/ws',
    )

    expect(ws).toBeInstanceOf(FakeWebSocket)
    expect(WebSocketProxyService.getWebSocketConnection('conn-7')).toBe(ws)
    expect(WebSocketProxyService.getConnectionCount()).toBe(1)
  })

  test('close handler removes the connection from the registry', async () => {
    setInstance({ key: 'RUNNING04', status: 'running', port: 18084 })

    const ws = (await WebSocketProxyService.createWebSocketConnection(
      'conn-8',
      'RUNNING04',
      '/ws',
    )) as FakeWebSocket

    expect(WebSocketProxyService.getWebSocketConnection('conn-8')).toBe(ws)
    ws.closeHandlers.forEach((h) => h())
    expect(WebSocketProxyService.getWebSocketConnection('conn-8')).toBeNull()
  })

  test('error handler removes the connection and logs the error', async () => {
    setInstance({ key: 'RUNNING05', status: 'running', port: 18085 })

    const ws = (await WebSocketProxyService.createWebSocketConnection(
      'conn-9',
      'RUNNING05',
      '/ws',
    )) as FakeWebSocket

    const errorCallsBefore = consoleErrorSpy.mock.calls.length
    ws.errorHandlers.forEach((h) => h(new Error('socket died')))

    expect(WebSocketProxyService.getWebSocketConnection('conn-9')).toBeNull()
    expect(consoleErrorSpy.mock.calls.length).toBeGreaterThan(errorCallsBefore)
  })

  test('returns null and logs when the WebSocket constructor throws', async () => {
    setInstance({ key: 'THROWING1', status: 'running', port: 18086 })

    class ThrowingWebSocket extends FakeWebSocket {
      constructor() {
        super('', undefined, undefined)
        throw new Error('connect refused')
      }
    }
    mock.module('ws', () => ({ WebSocket: ThrowingWebSocket }))

    const result = await WebSocketProxyService.createWebSocketConnection(
      'conn-10',
      'THROWING1',
      '/ws',
    )

    expect(result).toBeNull()
    expect(consoleErrorSpy).toHaveBeenCalled()

    // Restore the standard FakeWebSocket for subsequent tests.
    mock.module('ws', () => ({ WebSocket: FakeWebSocket }))
  })
})

describe('WebSocketProxyService connection registry', () => {
  afterEach(() => {
    resetState()
  })

  test('getWebSocketConnection returns null for unknown id', () => {
    expect(WebSocketProxyService.getWebSocketConnection('nope')).toBeNull()
  })

  test('closeWebSocketConnection removes the entry', async () => {
    setInstance({ key: 'RUNNING06', status: 'running', port: 18090 })

    const ws = (await WebSocketProxyService.createWebSocketConnection(
      'conn-11',
      'RUNNING06',
      '/ws',
    )) as FakeWebSocket

    WebSocketProxyService.closeWebSocketConnection('conn-11')

    expect(ws.closeCount).toBe(1)
    expect(WebSocketProxyService.getWebSocketConnection('conn-11')).toBeNull()
  })

  test('closeAllWebSocketConnections closes every connection and clears the registry', async () => {
    const instA = setInstance({ key: 'RUNNING07', status: 'running', port: 18091 })
    const instB = setInstance({ key: 'RUNNING08', status: 'running', port: 18092 })
    setInstances(instA, instB)

    const a = (await WebSocketProxyService.createWebSocketConnection(
      'conn-12',
      'RUNNING07',
      '/ws',
    )) as FakeWebSocket

    const b = (await WebSocketProxyService.createWebSocketConnection(
      'conn-13',
      'RUNNING08',
      '/ws',
    )) as FakeWebSocket

    expect(WebSocketProxyService.getConnectionCount()).toBe(2)

    WebSocketProxyService.closeAllWebSocketConnections()

    expect(a.closeCount).toBe(1)
    expect(b.closeCount).toBe(1)
    expect(WebSocketProxyService.getConnectionCount()).toBe(0)
  })
})
