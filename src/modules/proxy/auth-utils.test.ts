import { afterEach, describe, expect, test } from 'bun:test'
import {
  applyInstanceHttpAuthHeader,
  applyInstanceWebSocketAuthHeader,
  getFirstInstanceBasicAuthHeader,
  shouldInjectInstanceWebSocketAuth,
} from './auth-utils'
import { createMagicAdminCookie, createMagicAdminToken } from './magic-auth'

describe('proxy auth utils', () => {
  afterEach(() => {
    delete process.env.PROXY_WS_INJECT_INSTANCE_AUTH
  })

  test('injects instance websocket auth by default', () => {
    expect(shouldInjectInstanceWebSocketAuth()).toBe(true)
  })

  test('allows websocket auth injection opt-out', () => {
    process.env.PROXY_WS_INJECT_INSTANCE_AUTH = 'false'

    expect(shouldInjectInstanceWebSocketAuth()).toBe(false)
  })

  test('extracts first basic auth pair from instance config', () => {
    const header = getFirstInstanceBasicAuthHeader({
      config: JSON.stringify({
        flags: {
          basicAuth: [
            { username: 'admin', password: 'admin123' },
            { username: 'other', password: 'secret' },
          ],
        },
      }),
    })

    expect(header).toBe(`Basic ${btoa('admin:admin123')}`)
  })

  test('returns null when config has no basic auth', () => {
    const header = getFirstInstanceBasicAuthHeader({
      config: JSON.stringify({ flags: {} }),
    })

    expect(header).toBeNull()
  })

  test('returns null for invalid config JSON', () => {
    const header = getFirstInstanceBasicAuthHeader({ config: '{bad-json' })

    expect(header).toBeNull()
  })

  test('adds authorization header when missing', () => {
    const headers = applyInstanceWebSocketAuthHeader(
      { origin: 'http://localhost:3001' },
      {
        config: JSON.stringify({
          flags: {
            basicAuth: [{ username: 'admin', password: 'admin123' }],
          },
        }),
      }
    )

    expect(headers).toEqual({
      origin: 'http://localhost:3001',
      authorization: `Basic ${btoa('admin:admin123')}`,
    })
  })

  test('does not override incoming authorization header', () => {
    const headers = applyInstanceWebSocketAuthHeader(
      { authorization: 'Basic existing' },
      {
        config: JSON.stringify({
          flags: {
            basicAuth: [{ username: 'admin', password: 'admin123' }],
          },
        }),
      }
    )

    expect(headers).toEqual({ authorization: 'Basic existing' })
  })

  test('does not add authorization when injection is disabled', () => {
    process.env.PROXY_WS_INJECT_INSTANCE_AUTH = 'false'

    const headers = applyInstanceWebSocketAuthHeader(
      {},
      {
        config: JSON.stringify({
          flags: {
            basicAuth: [{ username: 'admin', password: 'admin123' }],
          },
        }),
      }
    )

    expect(headers).toEqual({})
  })

  test('adds HTTP authorization when magic admin cookie is valid', () => {
    const { token } = createMagicAdminToken('ABC12345')
    const cookie = createMagicAdminCookie('ABC12345', token, 'http://localhost/app/ABC12345/')

    const headers = applyInstanceHttpAuthHeader(
      { cookie },
      {
        key: 'ABC12345',
        config: JSON.stringify({
          flags: {
            basicAuth: [{ username: 'admin', password: 'admin123' }],
          },
        }),
      }
    )

    expect(headers.authorization).toBe(`Basic ${btoa('admin:admin123')}`)
  })

  test('does not override incoming HTTP authorization header', () => {
    const { token } = createMagicAdminToken('ABC12345')
    const cookie = createMagicAdminCookie('ABC12345', token, 'http://localhost/app/ABC12345/')

    const headers = applyInstanceHttpAuthHeader(
      { cookie, authorization: 'Basic existing' },
      {
        key: 'ABC12345',
        config: JSON.stringify({
          flags: {
            basicAuth: [{ username: 'admin', password: 'admin123' }],
          },
        }),
      }
    )

    expect(headers.authorization).toBe('Basic existing')
  })

  test('does not add HTTP authorization when magic cookie is invalid', () => {
    const headers = applyInstanceHttpAuthHeader(
      { cookie: 'gowa_admin_auth_ABC12345=invalid' },
      {
        key: 'ABC12345',
        config: JSON.stringify({
          flags: {
            basicAuth: [{ username: 'admin', password: 'admin123' }],
          },
        }),
      }
    )

    expect(headers.authorization).toBeUndefined()
  })
})
