import { describe, expect, test } from 'bun:test'
import { createProxyErrorResponse, createWebSocketConnectionId, createWebSocketProxyPath, normalizeProxyPath } from './utils'

describe('proxy utils', () => {
  test('normalizes proxy path with query string intact', () => {
    const path = normalizeProxyPath('http://localhost:3000/app/ABC12345/app/devices?limit=10&cursor=next')

    expect(path).toBe('/app/ABC12345/app/devices?limit=10&cursor=next')
  })

  test('normalizes proxy root path without query string', () => {
    const path = normalizeProxyPath('http://localhost:3000/app/ABC12345')

    expect(path).toBe('/app/ABC12345')
  })

  test('creates sanitized proxy error response', () => {
    const response = createProxyErrorResponse()

    expect(response).toEqual({
      error: 'Proxy request failed',
      success: false,
    })
    expect(JSON.stringify(response)).not.toContain('localhost')
    expect(JSON.stringify(response)).not.toContain('3001')
  })

  test('creates websocket proxy path with query string', () => {
    const path = createWebSocketProxyPath('ABC12345', {
      token: 'secret',
      mode: 'stream',
    })

    expect(path).toBe('/app/ABC12345/ws?token=secret&mode=stream')
  })

  test('creates websocket proxy path without empty query string', () => {
    const path = createWebSocketProxyPath('ABC12345')

    expect(path).toBe('/app/ABC12345/ws')
  })

  test('creates unique websocket connection ids per client', () => {
    const first = createWebSocketConnectionId('ABC12345')
    const second = createWebSocketConnectionId('ABC12345')

    expect(first).toStartWith('ABC12345:')
    expect(second).toStartWith('ABC12345:')
    expect(first).not.toBe(second)
  })
})
