import { describe, expect, test } from 'bun:test'
import { createCorsConfig } from './cors-config'

describe('createCorsConfig', () => {
  test('allows all origins by default outside production', () => {
    const config = createCorsConfig({ NODE_ENV: 'development' })

    expect(config.origin).toBe(true)
    expect(config.credentials).toBe(true)
    expect(config.allowedHeaders).toContain('Authorization')
  })

  test('uses explicit allowlist outside production when provided', () => {
    const config = createCorsConfig({
      NODE_ENV: 'development',
      CORS_ALLOWED_ORIGINS: 'http://localhost:3001, https://manager.example.com ',
    })

    expect(config.origin).toEqual(['http://localhost:3001', 'https://manager.example.com'])
  })

  test('denies browser origins by default in production', () => {
    const config = createCorsConfig({ NODE_ENV: 'production' })

    expect(config.origin).toEqual([])
    expect(config.credentials).toBe(true)
  })

  test('uses production allowlist from CORS_ALLOWED_ORIGINS', () => {
    const config = createCorsConfig({
      NODE_ENV: 'production',
      CORS_ALLOWED_ORIGINS: 'https://manager.example.com,https://admin.example.com',
    })

    expect(config.origin).toEqual(['https://manager.example.com', 'https://admin.example.com'])
  })

  test('ignores blank origins in allowlist', () => {
    const config = createCorsConfig({
      NODE_ENV: 'production',
      CORS_ALLOWED_ORIGINS: ' https://manager.example.com, , ',
    })

    expect(config.origin).toEqual(['https://manager.example.com'])
  })
})
