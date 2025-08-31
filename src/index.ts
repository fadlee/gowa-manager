import { Elysia } from 'elysia'
import { cors } from '@elysiajs/cors'
import './db' // Import to initialize database
import { instancesModule } from './modules/instances'
import { systemModule } from './modules/system'
import { proxyModule } from './modules/proxy'
import type { ApiResponse } from './types'

const app = new Elysia()
  .use(cors({
    origin: true, // Allow all origins in development
    methods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
    allowedHeaders: ['Content-Type', 'Authorization'],
    credentials: true
  }))

  // Global error handler
  .onError(({ code, error, set }) => {
    console.error('Server error:', error)

    if (code === 'VALIDATION') {
      set.status = 400
      return { error: 'Validation failed', success: false }
    }

    if (code === 'NOT_FOUND') {
      set.status = 404
      return { error: 'Route not found', success: false }
    }

    set.status = 500
    return { error: 'Internal server error', success: false }
  })

  // Health check endpoint
  .get('/', () => {
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

  // Register feature modules
  .use(proxyModule)
  .use(instancesModule)
  .use(systemModule)

  .listen(3000)

console.log(`🚀 GOWA Manager server is running on ${app.server?.hostname}:${app.server?.port}`)
