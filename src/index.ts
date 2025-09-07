import { Elysia } from 'elysia'
import { cors } from '@elysiajs/cors'
import { staticPlugin } from '@elysiajs/static'
import './db' // Import to initialize database
import './restart' // Import to auto-restart instances
import { downloadGowaBinary } from './binary-download' // Import binary auto-download
import { getStaticFile } from './static-handler' // Import embedded static handler
import { instancesModule } from './modules/instances'
import { systemModule } from './modules/system'
import { proxyModule } from './modules/proxy'
import { authModule } from './modules/auth'
import { basicAuth } from './middlewares/auth'
import type { ApiResponse } from './types'

// Auto-download GOWA binary if not present
;(async () => {
  await downloadGowaBinary()

  // Test pidusage functionality
  const { ResourceMonitor } = await import('./modules/instances/utils/resource-monitor')
  const pidusageWorks = await ResourceMonitor.testPidUsage()
  if (pidusageWorks) {
    console.log('✅ Resource monitoring (pidusage) is working')
  } else {
    console.warn('⚠️  Resource monitoring (pidusage) test failed - CPU/memory stats may not be available')
  }
})()

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

  // Protected API routes (when auth is enabled)
  .guard(
    {
      beforeHandle: basicAuth(ADMIN_USERNAME, ADMIN_PASSWORD),
    },
    (app) => app
      .use(instancesModule)
      .use(systemModule)
  )

  // Serve static files (client build) with embedded support
  .get('/assets/*', ({ params }: any) => {
    const path = '/assets/' + params['*']
    const file = getStaticFile(path)

    if (!file) {
      throw new Error('File not found')
    }

    return new Response(file.content, {
      headers: {
        'Content-Type': file.contentType,
        'Cache-Control': 'public, max-age=31536000' // 1 year cache for assets
      }
    })
  })

  .get('/', () => {
    const file = getStaticFile('/index.html')

    if (!file) {
      return new Response('Web UI not found', { status: 404 })
    }

    return new Response(file.content, {
      headers: {
        'Content-Type': file.contentType,
        'Cache-Control': 'no-cache' // No cache for index.html
      }
    })
  })

  .listen(3000)

console.log(`🚀 GOWA Manager server is running on ${app.server?.hostname}:${app.server?.port}`)
