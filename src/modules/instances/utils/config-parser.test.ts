import { describe, expect, spyOn, test } from 'bun:test'
import { ConfigParser } from './config-parser'

describe('ConfigParser', () => {
  test('returns default config for invalid or empty JSON', () => {
    expect(ConfigParser.parseConfig(null)).toEqual({})
    expect(ConfigParser.parseConfig('{bad-json')).toEqual({})
  })

  test('parses valid config JSON', () => {
    const config = ConfigParser.parseConfig(JSON.stringify({
      args: ['rest', '--port=PORT'],
      env: { LOG_LEVEL: 'debug' },
    }))

    expect(config).toEqual({
      args: ['rest', '--port=PORT'],
      env: { LOG_LEVEL: 'debug' },
    })
  })

  test('returns default rest config', () => {
    expect(ConfigParser.getDefaultConfig()).toEqual({
      args: ['rest', '--port=PORT'],
      flags: {
        accountValidation: true,
        os: 'GowaManager',
      },
    })
  })

  test('converts flags to CLI arguments', () => {
    const args = ConfigParser.flagsToArgs({
      accountValidation: false,
      basicAuth: [
        { username: 'admin', password: 'secret' },
        { username: 'api', password: 'key:with:colon' },
      ],
      os: 'Chrome',
      webhooks: ['https://example.com/a', 'https://example.com/b'],
      autoMarkRead: true,
      autoReply: 'hello world',
      basePath: '/app/ABC12345',
      debug: true,
      webhookSecret: 'hook-secret',
    })

    expect(args).toEqual([
      '--account-validation=false',
      '--basic-auth=admin:secret',
      '--basic-auth=api:key:with:colon',
      '--os=Chrome',
      '--webhook=https://example.com/a',
      '--webhook=https://example.com/b',
      '--auto-mark-read=true',
      '--autoreply=hello world',
      '--base-path=/app/ABC12345',
      '--debug=true',
      '--webhook-secret=hook-secret',
    ])
  })

  test('processes array args, appends flags, and replaces PORT placeholders', () => {
    const log = spyOn(console, 'log').mockImplementation(() => {})

    const args = ConfigParser.processArgs({
      args: ['rest', '--port=PORT'],
      flags: { basePath: '/app/ABC12345', debug: false },
    }, 8123)

    expect(args).toEqual(['rest', '--port=8123', '--base-path=/app/ABC12345', '--debug=false'])
    log.mockRestore()
  })

  test('processes string args', () => {
    const log = spyOn(console, 'log').mockImplementation(() => {})

    const args = ConfigParser.processArgs({ args: 'rest --port=PORT --debug=true' }, 8124)

    expect(args).toEqual(['rest', '--port=8124', '--debug=true'])
    log.mockRestore()
  })

  test('returns empty args for blank string args', () => {
    const log = spyOn(console, 'log').mockImplementation(() => {})

    const args = ConfigParser.processArgs({ args: '   ' }, 8125)

    expect(args).toEqual([])
    log.mockRestore()
  })

  test('parses env object and overrides PORT', () => {
    const env = ConfigParser.parseEnvironmentVars({
      env: { PORT: '9999', LOG_LEVEL: 'debug' },
    }, 8126)

    expect(env.PORT).toBe('9999')
    expect(env.LOG_LEVEL).toBe('debug')
  })

  test('parses env string values containing equals signs', () => {
    const env = ConfigParser.parseEnvironmentVars({
      env: 'TOKEN=a=b=c LOG_LEVEL=info',
    }, 8127)

    expect(env.PORT).toBe('8127')
    expect(env.TOKEN).toBe('a=b=c')
    expect(env.LOG_LEVEL).toBe('info')
  })

  test('parses legacy envVars string', () => {
    const env = ConfigParser.parseEnvironmentVars({
      envVars: 'LEGACY=yes SECRET=x=y',
    }, 8128)

    expect(env.PORT).toBe('8128')
    expect(env.LEGACY).toBe('yes')
    expect(env.SECRET).toBe('x=y')
  })
})
