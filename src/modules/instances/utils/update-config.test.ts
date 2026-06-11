import { describe, expect, test } from 'bun:test'
import { normalizeUpdateConfig } from './update-config'

describe('normalizeUpdateConfig', () => {
  test('forces basePath for updated config', () => {
    const config = normalizeUpdateConfig(
      JSON.stringify({ flags: { basePath: '/app/OLDKEY', debug: true } }),
      JSON.stringify({ flags: { basePath: '/custom/path', debug: false }, env: { FOO: 'bar' } }),
      'ABC12345'
    )

    expect(JSON.parse(config)).toEqual({
      flags: {
        basePath: '/app/ABC12345',
        debug: false,
      },
      env: {
        FOO: 'bar',
      },
    })
  })

  test('uses existing config when next config is undefined', () => {
    const config = normalizeUpdateConfig(
      JSON.stringify({ flags: { basePath: '/app/OLDKEY', webhook: 'https://example.com' } }),
      undefined,
      'XYZ98765'
    )

    expect(JSON.parse(config)).toEqual({
      flags: {
        basePath: '/app/XYZ98765',
        webhook: 'https://example.com',
      },
    })
  })

  test('falls back to existing config when next config is invalid JSON', () => {
    const config = normalizeUpdateConfig(
      JSON.stringify({ flags: { basePath: '/app/OLDKEY' }, command: 'rest' }),
      '{bad-json',
      'GOODKEY1'
    )

    expect(JSON.parse(config)).toEqual({
      flags: {
        basePath: '/app/GOODKEY1',
      },
      command: 'rest',
    })
  })

  test('creates flags object when updated config has no flags', () => {
    const config = normalizeUpdateConfig(
      '{}',
      JSON.stringify({ command: 'rest' }),
      'NOFLAGS1'
    )

    expect(JSON.parse(config)).toEqual({
      command: 'rest',
      flags: {
        basePath: '/app/NOFLAGS1',
      },
    })
  })

  test('replaces non-object config with flags-only object', () => {
    const config = normalizeUpdateConfig('{}', JSON.stringify(['invalid']), 'ARRAYKEY')

    expect(JSON.parse(config)).toEqual({
      flags: {
        basePath: '/app/ARRAYKEY',
      },
    })
  })
})
