import type { HTTPMethod } from '@elysiajs/cors'

type CorsOrigin = true | string[]

export type CorsConfig = {
  origin: CorsOrigin
  methods: HTTPMethod[]
  allowedHeaders: string[]
  credentials: boolean
}

function parseAllowedOrigins(value?: string): string[] {
  if (!value) return []

  return value
    .split(',')
    .map((origin) => origin.trim())
    .filter(Boolean)
}

export function createCorsConfig(env: NodeJS.ProcessEnv = process.env): CorsConfig {
  const allowedOrigins = parseAllowedOrigins(env.CORS_ALLOWED_ORIGINS)
  const isProduction = env.NODE_ENV === 'production'

  return {
    origin: isProduction ? allowedOrigins : allowedOrigins.length > 0 ? allowedOrigins : true,
    methods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
    allowedHeaders: ['Content-Type', 'Authorization'],
    credentials: true,
  }
}
