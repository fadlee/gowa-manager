import { Elysia, t } from 'elysia'
import { VersionManager, type VersionInfo } from './version-manager'

// Response models
const VersionInfoModel = t.Object({
  version: t.String(),
  path: t.String(),
  installed: t.Boolean(),
  isLatest: t.Boolean(),
  size: t.Optional(t.Number()),
  installedAt: t.Optional(t.Date())
})

const VersionListModel = t.Array(VersionInfoModel)

const ErrorModel = t.Object({
  error: t.String(),
  success: t.Literal(false)
})

const SuccessModel = t.Object({
  success: t.Literal(true),
  message: t.String()
})

const InstallRequestModel = t.Object({
  version: t.String()
})

const VersionSizesModel = t.Record(t.String(), t.Number())

export const versionsModule = new Elysia({ prefix: '/versions' })
  // Get all installed versions
  .get('/installed', async () => {
    try {
      const versions = await VersionManager.getInstalledVersions()
      return versions
    } catch (error) {
      console.error('Failed to get installed versions:', error)
      return { error: 'Failed to get installed versions', success: false }
    }
  }, {
    response: {
      200: VersionListModel,
      500: ErrorModel
    }
  })

  // Get available versions from GitHub
  .get('/available', async ({ query }) => {
    try {
      const limit = query.limit ? parseInt(query.limit as string) : 10
      const versions = await VersionManager.getAvailableVersions(limit)
      return versions
    } catch (error) {
      console.error('Failed to get available versions:', error)
      return { error: 'Failed to get available versions', success: false }
    }
  }, {
    query: t.Object({
      limit: t.Optional(t.String())
    }),
    response: {
      200: VersionListModel,
      500: ErrorModel
    }
  })

  // Install a specific version
  .post('/install', async ({ body, set }) => {
    try {
      await VersionManager.installVersion(body.version)
      return { 
        success: true, 
        message: `Successfully installed GOWA version ${body.version}` 
      }
    } catch (error) {
      set.status = 500
      return { 
        error: error instanceof Error ? error.message : 'Failed to install version', 
        success: false 
      }
    }
  }, {
    body: InstallRequestModel,
    response: {
      200: SuccessModel,
      500: ErrorModel
    }
  })

  // Remove a specific version
  .delete('/:version', async ({ params: { version }, set }) => {
    try {
      if (version === 'latest') {
        set.status = 400
        return { 
          error: 'Cannot remove the latest version alias', 
          success: false 
        }
      }

      await VersionManager.removeVersion(version)
      return { 
        success: true, 
        message: `Successfully removed GOWA version ${version}` 
      }
    } catch (error) {
      set.status = 500
      return { 
        error: error instanceof Error ? error.message : 'Failed to remove version', 
        success: false 
      }
    }
  }, {
    response: {
      200: SuccessModel,
      400: ErrorModel,
      500: ErrorModel
    }
  })

  // Check if a version is available/installed
  .get('/:version/available', async ({ params: { version } }) => {
    try {
      const available = await VersionManager.isVersionAvailable(version)
      return { 
        version, 
        available,
        path: VersionManager.getVersionBinaryPath(version)
      }
    } catch (error) {
      return { 
        error: error instanceof Error ? error.message : 'Failed to check version availability', 
        success: false 
      }
    }
  }, {
    response: {
      200: t.Object({
        version: t.String(),
        available: t.Boolean(),
        path: t.String()
      }),
      500: ErrorModel
    }
  })

  // Get disk usage for all versions
  .get('/usage', async () => {
    try {
      const sizes = await VersionManager.getVersionsSize()
      return sizes
    } catch (error) {
      return { 
        error: error instanceof Error ? error.message : 'Failed to get version sizes', 
        success: false 
      }
    }
  }, {
    response: {
      200: VersionSizesModel,
      500: ErrorModel
    }
  })

  // Cleanup old versions
  .post('/cleanup', async ({ body, set }) => {
    try {
      const keepCount = body?.keepCount ?? 3
      const removed = await VersionManager.cleanup(keepCount)
      return { 
        success: true, 
        message: `Cleaned up ${removed.length} old versions: ${removed.join(', ')}`,
        removed
      }
    } catch (error) {
      set.status = 500
      return { 
        error: error instanceof Error ? error.message : 'Failed to cleanup versions', 
        success: false 
      }
    }
  }, {
    body: t.Optional(t.Object({
      keepCount: t.Optional(t.Number({ minimum: 1, maximum: 10 }))
    })),
    response: {
      200: t.Object({
        success: t.Literal(true),
        message: t.String(),
        removed: t.Array(t.String())
      }),
      500: ErrorModel
    }
  })
