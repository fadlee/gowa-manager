import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { handleGlobalError } from './error-handler'

function createContext(code: string, error: unknown = new Error('sensitive details')) {
  return {
    code,
    error,
    set: {
      status: undefined as number | string | undefined,
      headers: {} as Record<string, string>,
    },
  }
}

describe('handleGlobalError', () => {
  afterEach(() => {
    // Restore any console.error spy created by a test.
    try {
      ;(console.error as any).mockRestore?.()
    } catch {
      // ignore when console.error was not mocked
    }
  })

  test('maps validation errors to sanitized 400 response', () => {
    spyOn(console, 'error').mockImplementation(() => {})
    const context = createContext('VALIDATION')

    const response = handleGlobalError(context)

    expect(context.set.status).toBe(400)
    expect(response).toEqual({ error: 'Validation failed', success: false })
  })

  test('maps unauthorized errors to 401 with basic auth challenge', () => {
    spyOn(console, 'error').mockImplementation(() => {})
    const context = createContext('UNAUTHORIZED')

    const response = handleGlobalError(context)

    expect(context.set.status).toBe(401)
    expect(context.set.headers['WWW-Authenticate']).toBe('Basic realm="GOWA Manager"')
    expect(response).toEqual({ error: 'Unauthorized', success: false })
  })

  test('maps not found errors to sanitized 404 response', () => {
    spyOn(console, 'error').mockImplementation(() => {})
    const context = createContext('NOT_FOUND')

    const response = handleGlobalError(context)

    expect(context.set.status).toBe(404)
    expect(response).toEqual({ error: 'Route not found', success: false })
  })

  test('maps generic errors to sanitized 500 response', () => {
    spyOn(console, 'error').mockImplementation(() => {})
    const context = createContext('UNKNOWN', new Error('database password leaked'))

    const response = handleGlobalError(context)

    expect(context.set.status).toBe(500)
    expect(response).toEqual({ error: 'Internal server error', success: false })
  })
})
