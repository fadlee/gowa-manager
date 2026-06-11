import { describe, expect, test } from 'bun:test'
import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { systemModule } from './index'

function createTestApp() {
  return new Elysia().guard(
    {
      beforeHandle: basicAuth('manager', 'secret'),
    },
    (app) => app.use(systemModule)
  )
}

function basicHeader(username: string, password: string) {
  return `Basic ${btoa(`${username}:${password}`)}`
}

async function json(response: Response) {
  return await response.json() as any
}

describe('system routes', () => {
  test('requires manager basic auth', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/status'))

    expect(response.status).toBe(401)
  })

  test('returns system status with manager basic auth', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/status', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))
    const body = await json(response)

    expect(response.status).toBe(200)
    expect(body.status).toBe('running')
    expect(body.instances).toMatchObject({
      total: expect.any(Number),
      running: expect.any(Number),
      stopped: expect.any(Number),
    })
    expect(body.ports).toMatchObject({
      allocated: expect.any(Number),
      next_available: expect.any(Number),
    })
  })

  test('returns system config with isolated test data directory', async () => {
    const app = createTestApp()

    const response = await app.handle(new Request('http://localhost/api/system/config', {
      headers: { authorization: basicHeader('manager', 'secret') },
    }))
    const body = await json(response)

    expect(response.status).toBe(200)
    expect(body.data_directory).toBe(process.env.DATA_DIR)
    expect(body.port_range).toEqual({ min: 8000, max: 9000 })
    expect(body.binaries_directory).toContain(process.env.DATA_DIR!)
  })
})
