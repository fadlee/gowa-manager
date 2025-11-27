import { Elysia } from 'elysia'
import { cors } from '@elysiajs/cors'
import { staticPlugin } from '@elysiajs/static'
import { getConfig } from './cli' // Import CLI configuration
import './db' // Import to initialize database
import './restart' // Import to auto-restart instances
import { downloadGowaBinary } from './binary-download' // Import binary auto-download
import { CleanupScheduler } from './modules/system/cleanup-scheduler' // Import cleanup scheduler
import { getStaticFile } from './static-handler' // Import embedded static handler
import { instancesModule } from './modules/instances'
import { systemModule } from './modules/system'
import { proxyModule } from './modules/proxy'
import { authModule } from './modules/auth'
import { basicAuth } from './middlewares/auth'
import { SystemService } from './modules/system/service'
import type { ApiResponse } from './types'

// Parse CLI configuration
const config = getConfig()

// Find the first available HTTP port starting from the desired port
async function findAvailableHttpPort(startPort: number, maxAttempts: number = 100): Promise<number> {
  let port = startPort

  for (let i = 0; i < maxAttempts; i++) {
    const available = await SystemService.isHttpPortAvailable(port)
    if (available) {
      return port
    }
    port++
  }

  throw new Error(`No available HTTP port found starting from ${startPort} (checked ${maxAttempts} ports)`)
}

if (process.env.NODE_ENV === 'production') {
  console.log('Running in PRODUCTION mode')
}

// Set environment variables from CLI config
if (config.dataDir) {
  process.env.DATA_DIR = config.dataDir
}
if (config.adminUsername) {
  process.env.ADMIN_USERNAME = config.adminUsername
}
if (config.adminPassword) {
  process.env.ADMIN_PASSWORD = config.adminPassword
}

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

  // Start cleanup scheduler
  CleanupScheduler.start()

  // Graceful shutdown
  process.on('SIGINT', () => {
    console.log('\n🛑 Shutting down...')
    CleanupScheduler.stop()
    process.exit(0)
  })
})()

// Configuration from CLI args (takes precedence over env vars)
const ADMIN_USERNAME = config.adminUsername
const ADMIN_PASSWORD = config.adminPassword
const DESIRED_PORT = config.port
const PORT = await findAvailableHttpPort(DESIRED_PORT)

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
  .get('/favicon.ico', () => {
    const file = getStaticFile('/favicon.ico')

    if (!file) {
      throw new Error('Favicon not found')
    }

    const body = Buffer.isBuffer(file.content) ? new Uint8Array(file.content) : file.content
    return new Response(body, {
      headers: {
        'Content-Type': file.contentType,
        'Cache-Control': 'public, max-age=31536000' // 1 year cache for favicon
      }
    })
  })

  .get('/assets/*', ({ params }: any) => {
    const path = '/assets/' + params['*']
    const file = getStaticFile(path)

    if (!file) {
      throw new Error('File not found')
    }

    const body = Buffer.isBuffer(file.content) ? new Uint8Array(file.content) : file.content
    return new Response(body, {
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

    const body = Buffer.isBuffer(file.content) ? new Uint8Array(file.content) : file.content
    return new Response(body, {
      headers: {
        'Content-Type': file.contentType,
        'Cache-Control': 'no-cache' // No cache for index.html
      }
    })
  })

  // SPA catch-all route - serve index.html for client-side routing
  // This must be after all API routes but handles paths like /instances/:id
  .get('/instances/*', () => {
    const file = getStaticFile('/index.html')

    if (!file) {
      return new Response('Web UI not found', { status: 404 })
    }

    const body = Buffer.isBuffer(file.content) ? new Uint8Array(file.content) : file.content
    return new Response(body, {
      headers: {
        'Content-Type': file.contentType,
        'Cache-Control': 'no-cache'
      }
    })
  })

  .listen(PORT)

if (PORT !== DESIRED_PORT) {
  console.warn(`⚠️  Port ${DESIRED_PORT} is already in use. Using fallback port ${PORT} instead.`)
}

console.log(`🚀 GOWA Manager server is running on ${app.server?.hostname}:${app.server?.port}`)
console.log(`👤 Admin credentials: ${ADMIN_USERNAME}/${ADMIN_PASSWORD}`)
console.log(`📂 Data directory: ${config.dataDir || './data'}`)
console.log(`🌐 Open: http://localhost:${PORT}`)
