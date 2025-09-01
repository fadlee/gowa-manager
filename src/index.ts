import { Elysia } from 'elysia'
import { cors } from '@elysiajs/cors'
import { staticPlugin } from '@elysiajs/static'
import './db' // Import to initialize database
import './restart' // Import to auto-restart instances
import { instancesModule } from './modules/instances'
import { systemModule } from './modules/system'
import { proxyModule } from './modules/proxy'
import { authModule } from './modules/auth'
import { basicAuth } from './middlewares/auth'
import type { ApiResponse } from './types'

// Get credentials from environment variables
const ADMIN_USERNAME = process.env.ADMIN_USERNAME || 'admin'
const ADMIN_PASSWORD = process.env.ADMIN_PASSWORD || 'password'

const app = new Elysia()
  .use(cors({
    origin: true, // Allow all origins in development
    methods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
    allowedHeaders: ['Content-Type', 'Authorization'],
    credentials: true
  }))

  // Global error handler
  .onError(({ code, error, set }: any) => {
    console.error('Server error:', error)

    if (code === 'VALIDATION') {
      set.status = 400
      return { error: 'Validation failed', success: false }
    }

    if (code === 'UNAUTHORIZED') {
      set.status = 401
      set.headers['WWW-Authenticate'] = 'Basic realm="GOWA Manager"'
      return { error: 'Unauthorized', success: false }
    }

    if (code === 'NOT_FOUND') {
      set.status = 404
      return { error: 'Route not found', success: false }
    }

    set.status = 500
    return { error: 'Internal server error', success: false }
  })

  // Health check endpoint (no auth required)
  .get('/api/health', () => {
    const data: ApiResponse = {
      message: "GOWA Manager API is running",
      success: true
    }
    return data
  })

  // Legacy hello endpoint
  .get('/hello', () => {
    const data: ApiResponse = {
      message: "Hello VERB!",
      success: true
    }
    return data
  })

  // Register auth module
  .use(authModule)
  .use(proxyModule)
  .use(instancesModule)
  .use(systemModule)

  // // Protected API routes (when auth is enabled)
  .guard(
    {
      beforeHandle: basicAuth(ADMIN_USERNAME, ADMIN_PASSWORD),
    },
    (app) => app
      .use(instancesModule)
      .use(systemModule)
  )

  // Serve static files (client build) with optional auth
  .use(staticPlugin({
    assets: 'public/assets',
    prefix: '/assets',
    indexHTML: false
  }))

  .get('/', ({ headers, set }: any) => {
    return Bun.file('public/index.html')
  })

  .listen(3000)

console.log(`🚀 GOWA Manager server is running on ${app.server?.hostname}:${app.server?.port}`)
