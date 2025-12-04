import { Elysia, t } from 'elysia'
import { SystemService } from './service'
import { SystemModel } from './model'
import { versionsModule } from './versions'
import { AutoUpdater } from './auto-updater'

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

  // Get auto-update status
  .get('/auto-update/status', () => {
    return AutoUpdater.getStatus()
  }, {
    response: {
      200: t.Object({
        lastCheck: t.Union([t.Date(), t.Null()]),
        lastUpdate: t.Union([t.Date(), t.Null()]),
        latestVersion: t.Union([t.String(), t.Null()]),
        isChecking: t.Boolean(),
        nextCheck: t.Union([t.Date(), t.Null()])
      })
    }
  })

  // Trigger manual update check
  .post('/auto-update/check', async () => {
    const result = await AutoUpdater.checkAndUpdate()
    return {
      success: true,
      updated: result.updated,
      version: result.version || null,
      restartedInstances: result.restartedInstances || 0
    }
  }, {
    response: {
      200: t.Object({
        success: t.Boolean(),
        updated: t.Boolean(),
        version: t.Union([t.String(), t.Null()]),
        restartedInstances: t.Number()
      })
    }
  })

  // Get instances using 'latest' version
  .get('/auto-update/instances', () => {
    return AutoUpdater.getLatestInstances()
  }, {
    response: {
      200: t.Array(t.Object({
        id: t.Number(),
        name: t.String(),
        status: t.String()
      }))
    }
  })
