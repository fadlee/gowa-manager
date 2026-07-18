import { Elysia } from 'elysia'
import { InstanceService } from './service'
import { InstanceModel } from './model'
import { DeviceClient } from './utils/device-client'
import { createMagicAdminToken } from '../proxy/magic-auth'

export const instancesModule = new Elysia({ prefix: '/api/instances' })
  // Get all instances
  .get('/', () => {
    return InstanceService.getAllInstances()
  }, {
    response: {
      200: InstanceModel.instanceListResponse
    }
  })

  // Get instance devices
  .get('/:id/devices', async ({ params: { id }, set }) => {
    const instance = InstanceService.getInstanceById(Number(id))
    if (!instance) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }

    return await DeviceClient.getDevices(instance)
  }, {
    response: {
      200: InstanceModel.devicesResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Get instance by ID
  .get('/:id', ({ params: { id }, set }) => {
    const instance = InstanceService.getInstanceById(Number(id))
    if (!instance) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    return instance
  }, {
    response: {
      200: InstanceModel.instanceResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Create new instance
  .post('/', async ({ body, set }) => {
    try {
      // Handle case where body might be undefined or empty
      const requestBody = body || {}
      const instance = await InstanceService.createInstance(requestBody)
      set.status = 201
      return instance
    } catch (error) {
      set.status = 400
      return {
        error: error instanceof Error ? error.message : 'Failed to create instance',
        success: false
      }
    }
  }, {
    body: InstanceModel.createBody,
    response: {
      201: InstanceModel.instanceResponse,
      400: InstanceModel.validationError
    }
  })

  // Update instance
  .put('/:id', ({ params: { id }, body, set }) => {
    const updated = InstanceService.updateInstance(Number(id), body)
    if (!updated) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    return updated
  }, {
    body: InstanceModel.updateBody,
    response: {
      200: InstanceModel.instanceResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Delete instance
  .delete('/:id', ({ params: { id }, set }) => {
    const deleted = InstanceService.deleteInstance(Number(id))
    if (!deleted) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    return { success: true, message: 'Instance deleted successfully' }
  }, {
    response: {
      200: InstanceModel.successResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Reset generated data without changing the instance ID/key
  .post('/:id/reset-data', ({ params: { id }, set }) => {
    const reset = InstanceService.resetInstanceData(Number(id))
    if (!reset) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    return { success: true, message: 'Instance data reset successfully' }
  }, {
    response: {
      200: InstanceModel.successResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Start instance
  .post('/:id/start', async ({ params: { id }, set }) => {
    try {
      const status = await InstanceService.startInstance(Number(id))
      if (!status) {
        set.status = 404
        return { error: 'Instance not found', success: false }
      }
      return status
    } catch (error) {
      set.status = 500
      return {
        error: error instanceof Error ? error.message : 'Failed to start instance',
        success: false
      }
    }
  }, {
    response: {
      200: InstanceModel.statusResponse,
      404: InstanceModel.notFoundError,
      500: InstanceModel.validationError
    }
  })

  // Stop instance (graceful)
  .post('/:id/stop', async ({ params: { id }, set }) => {
    const status = await InstanceService.stopInstance(Number(id))
    if (!status) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    return status
  }, {
    response: {
      200: InstanceModel.statusResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Kill instance (forceful)
  .post('/:id/kill', async ({ params: { id }, set }) => {
    try {
      const status = await InstanceService.killInstance(Number(id))
      if (!status) {
        set.status = 404
        return { error: 'Instance not found', success: false }
      }
      return status
    } catch (error) {
      set.status = 500
      return {
        error: error instanceof Error ? error.message : 'Failed to kill instance',
        success: false
      }
    }
  }, {
    response: {
      200: InstanceModel.statusResponse,
      404: InstanceModel.notFoundError,
      500: InstanceModel.validationError
    }
  })

  // Restart instance
  .post('/:id/restart', async ({ params: { id }, set }) => {
    try {
      const status = await InstanceService.restartInstance(Number(id))
      if (!status) {
        set.status = 404
        return { error: 'Instance not found', success: false }
      }
      return status
    } catch (error) {
      set.status = 500
      return {
        error: error instanceof Error ? error.message : 'Failed to restart instance',
        success: false
      }
    }
  }, {
    response: {
      200: InstanceModel.statusResponse,
      404: InstanceModel.notFoundError,
      500: InstanceModel.validationError
    }
  })

  // Get instance status
  .get('/:id/status', async ({ params: { id }, set }) => {
    const status = await InstanceService.getInstanceStatus(Number(id))
    if (!status) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    return status
  }, {
    response: {
      200: InstanceModel.statusResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Create a short-lived magic admin link for opening proxied GOWA admin without browser auth prompts
  .post('/:id/admin-link', ({ params: { id }, set }) => {
    const instance = InstanceService.getInstanceById(Number(id)) as any
    if (!instance) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }

    let hasBasicAuth = false
    try {
      const config = JSON.parse(instance.config || '{}')
      const firstAuth = config.flags?.basicAuth?.[0]
      hasBasicAuth = Boolean(firstAuth?.username && firstAuth?.password)
    } catch {
      hasBasicAuth = false
    }

    if (!hasBasicAuth) {
      return { url: `/app/${instance.key}/` }
    }

    const { token, expiresAt } = createMagicAdminToken(instance.key)
    return {
      url: `/app/${instance.key}/?autologin=${encodeURIComponent(token)}`,
      expiresAt: expiresAt.toISOString(),
    }
  }, {
    response: {
      200: InstanceModel.adminLinkResponse,
      404: InstanceModel.notFoundError
    }
  })

  // Test proxied GOWA API connection without triggering browser basic-auth prompts
  .post('/:id/test-connection', async ({ params: { id }, set }) => {
    const instance = InstanceService.getInstanceById(Number(id)) as any
    if (!instance) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }

    if (instance.status !== 'running' || !instance.port) {
      return {
        ok: false,
        message: 'Instance is not running. Start it before testing the GOWA API connection.',
      }
    }

    let authHeader: string | undefined
    try {
      const config = JSON.parse(instance.config || '{}')
      const firstAuth = config.flags?.basicAuth?.[0]
      if (firstAuth?.username && firstAuth?.password) {
        authHeader = `Basic ${btoa(`${firstAuth.username}:${firstAuth.password}`)}`
      }
    } catch {
      // Invalid config should not block testing unauthenticated instances.
    }

    try {
      const response = await fetch(`http://localhost:${instance.port}/app/${instance.key}/devices`, {
        headers: {
          ...(authHeader ? { Authorization: authHeader } : {}),
          Accept: 'application/json',
        },
      })
      const text = await response.text()
      const body = text.length > 600 ? `${text.slice(0, 600)}...` : text

      return {
        ok: response.ok,
        status: response.status,
        message: response.ok
          ? 'Connection successful. The instance responded to GET /devices.'
          : 'Connection failed. Check instance status, applied settings, or credentials.',
        body: body || 'No response body.',
      }
    } catch (error) {
      return {
        ok: false,
        message: error instanceof Error ? error.message : 'Connection failed before receiving a response.',
      }
    }
  }, {
    response: {
      200: InstanceModel.connectionTestResponse,
      404: InstanceModel.notFoundError
    }
  })
