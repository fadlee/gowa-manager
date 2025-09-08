import { t } from 'elysia'
import type { Instance } from '../../types'

export namespace InstanceModel {
  // Create instance request
  export const createBody = t.Object({
    name: t.Optional(t.String({ minLength: 1, maxLength: 100 })),
    config: t.Optional(t.String()),
    gowa_version: t.Optional(t.String({ default: 'latest' }))
  })
  export type createBody = Instance.CreateRequest

  // Update instance request
  export const updateBody = t.Object({
    name: t.Optional(t.String({ minLength: 1, maxLength: 100 })),
    config: t.Optional(t.String()),
    gowa_version: t.Optional(t.String())
  })
  export type updateBody = Instance.UpdateRequest

  // Instance response
  export const instanceResponse = t.Object({
    id: t.Number(),
    key: t.String(),
    name: t.String(),
    port: t.Union([t.Number(), t.Null()]),
    status: t.String(),
    config: t.String(),
    gowa_version: t.String(),
    created_at: t.String(),
    updated_at: t.String()
  })
  export type instanceResponse = Instance.Response

  // Instance list response
  export const instanceListResponse = t.Array(instanceResponse)
  export type instanceListResponse = Instance.ListResponse

  // Control action request
  export const controlAction = t.Object({
    action: t.Union([t.Literal('start'), t.Literal('stop'), t.Literal('restart')])
  })
  export type controlAction = Instance.ControlAction

  // Status response
  export const statusResponse = t.Object({
    id: t.Number(),
    name: t.String(),
    status: t.String(),
    port: t.Union([t.Number(), t.Null()]),
    pid: t.Union([t.Number(), t.Null()]),
    uptime: t.Union([t.Number(), t.Null()]),
    resources: t.Optional(t.Object({
      cpuPercent: t.Number(),
      memoryMB: t.Number(),
      memoryPercent: t.Number(),
      avgCpu: t.Optional(t.Number()),
      avgMemory: t.Optional(t.Number())
    }))
  })
  export type statusResponse = Instance.StatusResponse

  // Error responses
  export const notFoundError = t.Object({
    error: t.Literal('Instance not found'),
    success: t.Literal(false)
  })
  export type notFoundError = Instance.NotFoundError

  export const validationError = t.Object({
    error: t.String(),
    success: t.Literal(false)
  })
  export type validationError = typeof validationError.static

  export const successResponse = t.Object({
    success: t.Literal(true),
    message: t.String()
  })
  export type successResponse = Instance.SuccessResponse
}
