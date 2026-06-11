import { describe, expect, test } from 'bun:test'
import { Elysia } from 'elysia'

function basicHeader(username: string, password: string) {
  return `Basic ${btoa(`${username}:${password}`)}`
}

async function json(response: Response) {
  return await response.json() as any
}

describe('auth routes', () => {
  test('login requires manager basic auth', async () => {
    const { authModule } = await import('./index')
    const app = new Elysia().use(authModule)

    const response = await app.handle(new Request('http://localhost/api/auth/login', {
      method: 'POST',
    }))

    expect(response.status).toBe(401)
  })

  test('login returns success response for valid manager credentials', async () => {
    const { authModule } = await import('./index')
    const app = new Elysia().use(authModule)

    const response = await app.handle(new Request('http://localhost/api/auth/login', {
      method: 'POST',
      headers: { authorization: basicHeader('admin', 'password') },
    }))
    const body = await json(response)

    expect(response.status).toBe(200)
    expect(body).toEqual({
      success: true,
      message: 'Login successful',
      user: 'admin',
    })
  })

  test('login rejects invalid manager credentials', async () => {
    const { authModule } = await import('./index')
    const app = new Elysia().use(authModule)

    const response = await app.handle(new Request('http://localhost/api/auth/login', {
      method: 'POST',
      headers: { authorization: basicHeader('admin', 'wrong') },
    }))

    expect(response.status).toBe(401)
  })

  test('logout returns success without manager auth', async () => {
    const { authModule } = await import('./index')
    const app = new Elysia().use(authModule)

    const response = await app.handle(new Request('http://localhost/api/auth/logout', {
      method: 'POST',
    }))
    const body = await json(response)

    expect(response.status).toBe(200)
    expect(body).toEqual({ success: true, message: 'Logout successful' })
  })
})
