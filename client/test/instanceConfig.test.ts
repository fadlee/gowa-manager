import { describe, expect, test } from 'bun:test'
import { buildCreateInstanceConfig } from '../src/lib/instanceConfig'

describe('buildCreateInstanceConfig', () => {
  test('default config has no Basic Auth', () => {
    expect(buildCreateInstanceConfig()).toEqual({
      args: ['rest', '--port=PORT'],
      flags: { accountValidation: true, os: 'GowaManager' },
    })
    expect(buildCreateInstanceConfig(null).flags).not.toHaveProperty('basicAuth')
  })

  test('adds the generated Basic Auth pair', () => {
    const config = buildCreateInstanceConfig({ username: 'user_abc', password: 'secret' })
    expect(config.flags?.basicAuth).toEqual([{ username: 'user_abc', password: 'secret' }])
    expect(config.flags?.os).toBe('GowaManager')
  })
})
