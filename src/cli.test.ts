import { afterAll, beforeAll, beforeEach, afterEach, describe, expect, mock, spyOn, test } from 'bun:test'
import { MANAGER_VERSION } from './version'

// --- Module mock for `process.exit` ----------------------------------------
//
// `cli.ts` does `import { exit } from 'process'` and calls `exit(code)`. In
// Bun the named `exit` binding is captured at import time and is NOT a live
// binding to `process.exit`, so `spyOn(process, 'exit')` cannot intercept it
// (a real `process.exit(1)` would terminate the test runner).
//
// We use `mock.module('process', ...)` to provide a `process` module whose
// `exit` export is a controllable mock, while every other property (env, argv,
// stdout, ...) is delegated to the real `process` object via a Proxy. This
// keeps env/argv reads live.
//
// `mock.module` is a runtime call (not hoisted), so we register it BEFORE
// dynamically importing `cli` in `beforeAll`. This ensures `cli.ts` resolves
// its `import { exit } from 'process'` against the mocked module.

const exitMock = mock<(code?: number) => never>((code?: number) => {
  // Throw to short-circuit the caller, mirroring real exit semantics so that
  // code after `exit(...)` in cli.ts is not reached.
  throw new Error(`__EXIT_${code ?? 0}__`)
})

mock.module('process', () => {
  return new Proxy(process, {
    get(target, prop, receiver) {
      if (prop === 'exit') return exitMock
      return Reflect.get(target, prop, receiver)
    },
  })
})

// Dynamically imported so the `process` mock is registered first.
let parseCliArgs: (args: string[]) => import('./cli').CliConfig
let getConfig: () => import('./cli').CliConfig

beforeAll(async () => {
  const cli = await import('./cli')
  parseCliArgs = cli.parseCliArgs
  getConfig = cli.getConfig
})

// --- Spies -----------------------------------------------------------------

let consoleLogSpy: ReturnType<typeof spyOn>
let consoleErrorSpy: ReturnType<typeof spyOn>

// Snapshot of env/argv so each test starts from a clean baseline.
const envSnapshot: Record<string, string | undefined> = {}
let argvSnapshot: string[]

beforeEach(() => {
  consoleLogSpy = spyOn(console, 'log').mockImplementation(() => {})
  consoleErrorSpy = spyOn(console, 'error').mockImplementation(() => {})
  exitMock.mockReset()
  exitMock.mockImplementation((code?: number) => {
    throw new Error(`__EXIT_${code ?? 0}__`)
  })

  // Capture current env values we care about, then clear them so defaults apply.
  for (const key of ['PORT', 'ADMIN_USERNAME', 'ADMIN_PASSWORD', 'DATA_DIR']) {
    envSnapshot[key] = process.env[key]
    delete process.env[key]
  }
  argvSnapshot = process.argv.slice()
  process.argv = ['bun', 'src/index.ts']
})

afterEach(() => {
  consoleLogSpy.mockRestore()
  consoleErrorSpy.mockRestore()

  for (const key of Object.keys(envSnapshot)) {
    if (envSnapshot[key] === undefined) {
      delete process.env[key]
    } else {
      process.env[key] = envSnapshot[key]
    }
  }
  process.argv = argvSnapshot
})

// Helper: run parseCliArgs expecting it to call exit (which throws).
function expectExit(args: string[], code?: number): void {
  expect(() => parseCliArgs(args)).toThrow(`__EXIT_${code ?? ''}`)
  expect(exitMock).toHaveBeenCalled()
}

// --- Tests -----------------------------------------------------------------

describe('parseCliArgs — defaults & env fallbacks', () => {
  test('uses default values when no env or args are provided', () => {
    const config = parseCliArgs([])

    expect(config).toEqual({
      port: 3000,
      adminUsername: 'admin',
      adminPassword: 'password',
      dataDir: './data',
      help: false,
      version: false,
    })
  })

  test('reads port/username/password/dataDir from environment variables', () => {
    process.env.PORT = '8080'
    process.env.ADMIN_USERNAME = 'envuser'
    process.env.ADMIN_PASSWORD = 'envpass'
    process.env.DATA_DIR = '/env/data'

    const config = parseCliArgs([])

    expect(config.port).toBe(8080)
    expect(config.adminUsername).toBe('envuser')
    expect(config.adminPassword).toBe('envpass')
    expect(config.dataDir).toBe('/env/data')
  })

  test('command-line arguments take precedence over environment variables', () => {
    process.env.PORT = '8080'
    process.env.ADMIN_USERNAME = 'envuser'
    process.env.ADMIN_PASSWORD = 'envpass'
    process.env.DATA_DIR = '/env/data'

    const config = parseCliArgs([
      '--port', '9090',
      '--admin-username', 'cliuser',
      '--admin-password', 'clipass',
      '--data-dir', '/cli/data',
    ])

    expect(config.port).toBe(9090)
    expect(config.adminUsername).toBe('cliuser')
    expect(config.adminPassword).toBe('clipass')
    expect(config.dataDir).toBe('/cli/data')
  })

  test('accepts short-form flags (-p, -u, -P, -d)', () => {
    const config = parseCliArgs(['-p', '7070', '-u', 'short', '-P', 'pwd', '-d', '/short'])

    expect(config.port).toBe(7070)
    expect(config.adminUsername).toBe('short')
    expect(config.adminPassword).toBe('pwd')
    expect(config.dataDir).toBe('/short')
  })
})

describe('parseCliArgs — port validation', () => {
  test('rejects non-numeric port', () => {
    expectExit(['--port', 'abc'], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith(
      expect.stringContaining('Invalid port: abc'),
    )
  })

  test('rejects port below 1', () => {
    expectExit(['--port', '0'], 1)
  })

  test('rejects port above 65535', () => {
    expectExit(['--port', '70000'], 1)
  })

  test('accepts boundary ports 1 and 65535', () => {
    expect(parseCliArgs(['--port', '1']).port).toBe(1)
    expect(parseCliArgs(['--port', '65535']).port).toBe(65535)
  })
})

describe('parseCliArgs — username validation', () => {
  test('rejects empty username', () => {
    expectExit(['--admin-username', ''], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith('❌ Username cannot be empty')
  })

  test('rejects username longer than 50 characters', () => {
    const longName = 'a'.repeat(51)
    expectExit(['--admin-username', longName], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith(
      '❌ Username cannot be longer than 50 characters',
    )
  })

  test('accepts username of exactly 50 characters', () => {
    const name = 'a'.repeat(50)
    expect(parseCliArgs(['--admin-username', name]).adminUsername).toBe(name)
  })
})

describe('parseCliArgs — password validation', () => {
  test('rejects empty password', () => {
    expectExit(['--admin-password', ''], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith('❌ Password cannot be empty')
  })

  test('rejects password longer than 100 characters', () => {
    const longPass = 'a'.repeat(101)
    expectExit(['--admin-password', longPass], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith(
      '❌ Password cannot be longer than 100 characters',
    )
  })

  test('accepts password of exactly 100 characters', () => {
    const pass = 'a'.repeat(100)
    expect(parseCliArgs(['--admin-password', pass]).adminPassword).toBe(pass)
  })
})

describe('parseCliArgs — missing values', () => {
  test('errors when --port has no value (end of args)', () => {
    expectExit(['--port'], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith('❌ Missing value for --port')
  })

  test('errors when -p has no value', () => {
    expectExit(['-p'], 1)
  })

  test('errors when --admin-username has no value', () => {
    expectExit(['--admin-username'], 1)
  })

  test('errors when -u has no value', () => {
    expectExit(['-u'], 1)
  })

  test('errors when --admin-password has no value', () => {
    expectExit(['--admin-password'], 1)
  })

  test('errors when -P has no value', () => {
    expectExit(['-P'], 1)
  })

  test('errors when --data-dir has no value', () => {
    expectExit(['--data-dir'], 1)
  })

  test('errors when -d has no value', () => {
    expectExit(['-d'], 1)
  })
})

describe('parseCliArgs — unknown & unexpected arguments', () => {
  test('errors on unknown option (starts with -)', () => {
    expectExit(['--bogus'], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith('❌ Unknown option: --bogus')
    expect(consoleErrorSpy).toHaveBeenCalledWith('Use --help to see available options')
  })

  test('errors on unknown short option', () => {
    expectExit(['-x'], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith('❌ Unknown option: -x')
  })

  test('errors on unexpected positional argument', () => {
    expectExit(['positional'], 1)
    expect(consoleErrorSpy).toHaveBeenCalledWith('❌ Unexpected argument: positional')
    expect(consoleErrorSpy).toHaveBeenCalledWith('Use --help to see usage information')
  })
})

describe('parseCliArgs — help & version', () => {
  test('--help prints help text and exits with code 0', () => {
    expectExit(['--help'], 0)
    expect(consoleLogSpy).toHaveBeenCalled()
    const helpOutput = consoleLogSpy.mock.calls.map((c) => String(c[0])).join('\n')
    expect(helpOutput).toContain('GOWA Manager - WhatsApp Instance Manager')
    expect(helpOutput).toContain('--port')
    expect(helpOutput).toContain('--admin-username')
    expect(helpOutput).toContain('--admin-password')
    expect(helpOutput).toContain('--data-dir')
    expect(helpOutput).toContain('ENVIRONMENT VARIABLES')
  })

  test('-h short form also prints help and exits 0', () => {
    expectExit(['-h'], 0)
    expect(consoleLogSpy).toHaveBeenCalled()
  })

  test('--version prints version and exits with code 0', () => {
    expectExit(['--version'], 0)
    expect(consoleLogSpy).toHaveBeenCalledWith(`GOWA Manager v${MANAGER_VERSION}`)
    expect(consoleLogSpy).toHaveBeenCalledWith('Built with Bun and Elysia')
  })

  test('-v short form also prints version and exits 0', () => {
    expectExit(['-v'], 0)
    const output = consoleLogSpy.mock.calls.map((c) => String(c[0])).join('\n')
    expect(output).toContain(`GOWA Manager v${MANAGER_VERSION}`)
  })
})

describe('parseCliArgs — combined flags', () => {
  test('parses multiple flags in sequence', () => {
    const config = parseCliArgs([
      '--port', '4000',
      '--admin-username', 'multi',
      '--admin-password', 'multipass',
      '--data-dir', '/multi',
    ])

    expect(config).toMatchObject({
      port: 4000,
      adminUsername: 'multi',
      adminPassword: 'multipass',
      dataDir: '/multi',
      help: false,
      version: false,
    })
  })

  test('help flag is processed after earlier flags are parsed', () => {
    // --port is parsed first, then --help sets config.help=true; after the loop
    // showHelp()+exit(0) run.
    expectExit(['--port', '5000', '--help'], 0)
  })
})

describe('getConfig — argv slicing', () => {
  test('reads args from process.argv (skipping runtime & script)', () => {
    process.argv = ['bun', 'src/index.ts', '--port', '6060']

    const config = getConfig()

    expect(config.port).toBe(6060)
  })

  test('strips a leading binary path ending with "gowa-manager"', () => {
    process.argv = ['bun', 'src/index.ts', '/usr/local/bin/gowa-manager', '--port', '7070']

    const config = getConfig()

    expect(config.port).toBe(7070)
  })

  test('strips a leading .exe binary path', () => {
    process.argv = ['bun', 'src/index.ts', 'C:\\app\\gowa-manager.exe', '--port', '7171']

    const config = getConfig()

    expect(config.port).toBe(7171)
  })

  test('strips a leading compiled binary path containing /gowa-manager-', () => {
    process.argv = ['bun', 'src/index.ts', '/opt/gowa-manager-1.0.0', '--port', '7272']

    const config = getConfig()

    expect(config.port).toBe(7272)
  })

  test('strips a leading compiled binary path containing \\gowa-manager-', () => {
    process.argv = ['bun', 'src/index.ts', 'C:\\opt\\gowa-manager-1.0.0', '--port', '7373']

    const config = getConfig()

    expect(config.port).toBe(7373)
  })

  test('returns defaults when process.argv has no extra args', () => {
    process.argv = ['bun', 'src/index.ts']

    const config = getConfig()

    expect(config.port).toBe(3000)
    expect(config.adminUsername).toBe('admin')
    expect(config.adminPassword).toBe('password')
    expect(config.dataDir).toBe('./data')
  })

  test('honors environment variables when no argv flags are present', () => {
    process.env.PORT = '8181'
    process.env.ADMIN_USERNAME = 'envonly'
    process.argv = ['bun', 'src/index.ts']

    const config = getConfig()

    expect(config.port).toBe(8181)
    expect(config.adminUsername).toBe('envonly')
  })
})
