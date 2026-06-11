import { describe, expect, test } from 'bun:test'
import { basicAuth } from './auth'

function createContext(authorization?: string) {
  let statusCode: number | undefined

  return {
    request: new Request('http://localhost/protected', {
      headers: authorization ? { authorization } : undefined,
    }),
    status(code: number) {
      statusCode = code
      return new Response(null, { status: code })
    },
    get statusCode() {
      return statusCode
    },
  }
}

describe('basicAuth', () => {
  test('returns 401 when Authorization header is missing', () => {
    const context = createContext()

    const result = basicAuth('admin', 'password')(context)

    expect(context.statusCode).toBe(401)
    expect(result).toBeInstanceOf(Response)
    expect((result as Response).status).toBe(401)
  })

  test('returns 401 when auth type is not Basic', () => {
    const context = createContext('Bearer token')

    const result = basicAuth('admin', 'password')(context)

    expect(context.statusCode).toBe(401)
    expect(result).toBeInstanceOf(Response)
  })

  test('returns 401 when credentials are wrong', () => {
    const credentials = btoa('admin:wrong')
    const context = createContext(`Basic ${credentials}`)

    const result = basicAuth('admin', 'password')(context)

    expect(context.statusCode).toBe(401)
    expect(result).toBeInstanceOf(Response)
  })

  test('allows request when credentials are correct', () => {
    const credentials = btoa('admin:password')
    const context = createContext(`Basic ${credentials}`)

    const result = basicAuth('admin', 'password')(context)

    expect(context.statusCode).toBeUndefined()
    expect(result).toBeUndefined()
  })

  test('returns 401 for invalid base64 credentials', () => {
    const context = createContext('Basic invalid-base64')

    expect(() => basicAuth('admin', 'password')(context)).not.toThrow()
    expect(context.statusCode).toBe(401)
  })

  test('returns 401 when decoded credentials do not contain a colon', () => {
    const credentials = btoa('admin')
    const context = createContext(`Basic ${credentials}`)

    const result = basicAuth('admin', 'password')(context)

    expect(context.statusCode).toBe(401)
    expect(result).toBeInstanceOf(Response)
  })

  test('supports passwords containing colons', () => {
    const credentials = btoa('admin:pass:word')
    const context = createContext(`Basic ${credentials}`)

    const result = basicAuth('admin', 'pass:word')(context)

    expect(context.statusCode).toBeUndefined()
    expect(result).toBeUndefined()
  })

  test('returns 401 for malformed Basic header with missing credentials', () => {
    const context = createContext('Basic')

    const result = basicAuth('admin', 'password')(context)

    expect(context.statusCode).toBe(401)
    expect(result).toBeInstanceOf(Response)
  })

  test('returns 401 for lowercase basic scheme', () => {
    const credentials = btoa('admin:password')
    const context = createContext(`basic ${credentials}`)

    const result = basicAuth('admin', 'password')(context)

    expect(context.statusCode).toBe(401)
    expect(result).toBeInstanceOf(Response)
  })
})
