import { Elysia } from 'elysia'
import { InstanceService } from './service'
import { InstanceModel } from './model'

export const instancesModule = new Elysia({ prefix: '/api/instances' })
  // Get all instances
  .get('/', () => {
    return InstanceService.getAllInstances()
  }, {
    response: {
      200: InstanceModel.instanceListResponse
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
