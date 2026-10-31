import { describe, expect, test } from 'bun:test'
import { flagsFromConfig, hasBasicAuth, requiresBasicAuth, resolveGowaVersion } from '../src/lib/basicAuth'

describe('requiresBasicAuth', () => {
  test('versions from v9.8.0 require Basic Auth', () => {
    expect(requiresBasicAuth('v9.8.0')).toBe(true)
    expect(requiresBasicAuth('v9.8.1')).toBe(true)
    expect(requiresBasicAuth('v10.0.0')).toBe(true)
    expect(requiresBasicAuth('9.8.0')).toBe(true)
  })

  test('older versions do not', () => {
    expect(requiresBasicAuth('v9.7.0')).toBe(false)
    expect(requiresBasicAuth('v7.5.1')).toBe(false)
  })

  test('latest resolves to the newest installed version', () => {
    expect(requiresBasicAuth('latest', 'v9.7.0')).toBe(false)
    expect(requiresBasicAuth('latest', 'v9.8.0')).toBe(true)
    expect(requiresBasicAuth('', 'v9.7.0')).toBe(false)
  })

  test('unresolved latest is treated as requiring Basic Auth', () => {
    expect(requiresBasicAuth('latest')).toBe(true)
    expect(requiresBasicAuth(null, null)).toBe(true)
  })

  test('unparseable versions are not flagged', () => {
    expect(requiresBasicAuth('custom-build')).toBe(false)
  })
})

describe('resolveGowaVersion', () => {
  test('keeps explicit versions and maps latest', () => {
    expect(resolveGowaVersion('v9.7.0', 'v9.8.0')).toBe('v9.7.0')
    expect(resolveGowaVersion('latest', 'v9.8.0')).toBe('v9.8.0')
    expect(resolveGowaVersion('latest', null)).toBeNull()
  })
})

describe('hasBasicAuth', () => {
  test('needs a pair with a username and a password', () => {
    expect(hasBasicAuth({ basicAuth: [{ username: 'u', password: 'p' }] })).toBe(true)
    expect(hasBasicAuth({ basicAuth: [{ username: 'u', password: ' ' }] })).toBe(false)
    expect(hasBasicAuth({ basicAuth: [] })).toBe(false)
    expect(hasBasicAuth({})).toBe(false)
    expect(hasBasicAuth(null)).toBe(false)
  })
})

describe('flagsFromConfig', () => {
  test('reads flags from the instance config JSON', () => {
    expect(flagsFromConfig('{"flags":{"basicAuth":[{"username":"u","password":"p"}]}}')).toEqual({ basicAuth: [{ username: 'u', password: 'p' }] })
    expect(flagsFromConfig('{}')).toBeNull()
    expect(flagsFromConfig('not json')).toBeNull()
    expect(flagsFromConfig(null)).toBeNull()
  })
})
