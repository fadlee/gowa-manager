import { t } from 'elysia'
import { Proxy } from '../../types'

export namespace ProxyModel {
  export const prefix = Proxy.PREFIX

  // Proxy request info
  export const proxyRequest = t.Object({
    instanceId: t.String(),
    path: t.String(),
    method: t.String(),
    headers: t.Record(t.String(), t.String()),
    body: t.Optional(t.Any())
  })
  export type proxyRequest = Proxy.Request

  // Proxy response
  export const proxyResponse = t.Object({
    status: t.Number(),
    headers: t.Record(t.String(), t.String()),
    body: t.Any()
  })
  export type proxyResponse = Proxy.Response

  // Proxy error responses
  export const instanceNotFoundError = t.Object({
    error: t.Literal('Instance not found'),
    success: t.Literal(false)
  })
  export type instanceNotFoundError = Proxy.InstanceNotFoundError

  export const instanceOfflineError = t.Object({
    error: t.String(),
    success: t.Literal(false),
    instanceId: t.Optional(t.String())
  })
  export type instanceOfflineError = Proxy.InstanceOfflineError

  export const proxyError = t.Object({
    error: t.String(),
    success: t.Literal(false)
  })
  export type proxyError = Proxy.ProxyError

  // Proxy status response
  export const proxyStatus = t.Object({
    instanceId: t.String(),
    instanceName: t.String(),
    status: t.String(),
    port: t.Union([t.Number(), t.Null()]),
    targetPort: t.Union([t.Number(), t.Null()]),
    proxyPath: t.String()
  })
  export type proxyStatus = Proxy.Status

  export const proxyStatusList = t.Array(proxyStatus)
  export type proxyStatusList = Proxy.StatusList

  export const healthResponse = t.Object({
    instanceId: t.String(),
    healthy: t.Boolean(),
    status: t.String()
  })

  export const healthErrorResponse = t.Object({
    error: t.String(),
    success: t.Literal(false)
  })
  export type healthResponse = Proxy.HealthResponse
  export type healthErrorResponse = Proxy.HealthErrorResponse

  // WebSocket connection status
  export const wsConnectionStatus = t.Object({
    instanceId: t.String(),
    connected: t.Boolean(),
    targetUrl: t.Optional(t.String())
  })
  export type wsConnectionStatus = Proxy.WSConnectionStatus

  // WebSocket message
  export const wsMessage = t.Object({
    type: t.Optional(t.String()),
    data: t.Any()
  })
  export type wsMessage = Proxy.WSMessage

  // WebSocket error
  export const wsError = t.Object({
    error: t.String(),
    instanceId: t.String()
  })
  export type wsError = Proxy.WSError
}
