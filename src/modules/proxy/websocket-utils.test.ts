import { describe, expect, test } from 'bun:test'
import { createWebSocketForwardingOptions, serializeWebSocketMessage } from './websocket-utils'

describe('websocket utils', () => {
  test('forwards allowlisted headers and parses subprotocols', () => {
    const options = createWebSocketForwardingOptions({
      Authorization: 'Basic token',
      Cookie: 'session=abc',
      Origin: 'http://localhost:3001',
      'User-Agent': 'test-agent',
      'Accept-Language': 'en-US',
      Host: 'localhost:3001',
      Connection: 'upgrade',
      'Sec-WebSocket-Protocol': 'chat, superchat , ',
    })

    expect(options.headers).toEqual({
      authorization: 'Basic token',
      cookie: 'session=abc',
      origin: 'http://localhost:3001',
      'user-agent': 'test-agent',
      'accept-language': 'en-US',
    })
    expect(options.subprotocols).toEqual(['chat', 'superchat'])
  })

  test('returns empty headers when no incoming headers exist', () => {
    expect(createWebSocketForwardingOptions()).toEqual({ headers: {} })
  })

  test('preserves string messages', () => {
    expect(serializeWebSocketMessage('hello')).toBe('hello')
  })

  test('preserves array buffer messages', () => {
    const buffer = new ArrayBuffer(2)

    expect(serializeWebSocketMessage(buffer)).toBe(buffer)
  })

  test('preserves typed array messages', () => {
    const bytes = new Uint8Array([1, 2, 3])

    expect(serializeWebSocketMessage(bytes)).toBe(bytes)
  })

  test('preserves buffer messages', () => {
    const buffer = Buffer.from('hello')

    expect(serializeWebSocketMessage(buffer)).toBe(buffer)
  })

  test('serializes object messages as JSON', () => {
    expect(serializeWebSocketMessage({ event: 'ping' })).toBe('{"event":"ping"}')
  })
})
