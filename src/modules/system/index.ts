import { Elysia, t } from 'elysia'
import { SystemService } from './service'
import { SystemModel } from './model'
import { versionsModule } from './versions'

export const systemModule = new Elysia({ prefix: '/api/system' })
  .use(versionsModule)
  // Get system status
  .get('/status', () => {
    return SystemService.getSystemStatus()
  }, {
    response: {
      200: SystemModel.statusResponse
    }
  })

  // Get next available port
  .get('/ports/next', async () => {
    const port = await SystemService.getNextAvailablePort()
    return { port }
  }, {
    response: {
      200: t.Object({ port: t.Number() })
    }
  })

  // Get system configuration
  .get('/config', () => {
    return SystemService.getSystemConfig()
  }, {
    response: {
      200: SystemModel.configResponse
    }
  })


  // Check if specific port is available
  .get('/ports/:port/available', async ({ params: { port } }) => {
    const isAvailable = await SystemService.isPortAvailable(Number(port))
    return { port: Number(port), available: isAvailable }
  }, {
    response: {
      200: SystemModel.portCheckResponse
    }
  })
