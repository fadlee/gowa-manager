import { Elysia } from 'elysia'
import { ProxyService } from './service'
import { ProxyModel } from './model'
import { WebSocketProxyService } from './service.websocket'
import { createProxyErrorResponse, createWebSocketConnectionId, createWebSocketProxyPath, normalizeProxyPath } from './utils'
import { serializeWebSocketMessage } from './websocket-utils'

// Create a shared handler function
const handleProxyRequest = async (
  instanceKey: string,
  path: string,
  request: Request,
  set: any,
  headers: any
) => {
  try {
    // Check if instance is available
    if (!ProxyService.isInstanceAvailable(instanceKey)) {
      const status = ProxyService.getProxyStatus(instanceKey)
      if (!status) {
        set.status = 404
        return { error: 'Instance not found', success: false }
      }
      set.status = 503
      return { error: 'Instance is not running', success: false, instanceKey }
    }

    // Get request headers
    const requestHeaders: Record<string, string> = {}
    request.headers.forEach((value, key) => {
      requestHeaders[key] = value
    })

    // Get request body if present
    let body: any
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      try {
        body = await request.arrayBuffer()
      } catch {
        try {
          body = await request.text()
        } catch {
          body = undefined
        }
      }
    }

    // Forward the request
    const response = await ProxyService.forwardRequest(
      instanceKey,
      path,
      request.method,
      requestHeaders,
      body,
      headers.host
    )

    // Handle binary responses
    if (response.isBinary) {
      return new Response(response.body, {
        status: response.status,
        headers: response.headers
      })
    }

    // For non-binary responses
    set.status = response.status
    Object.entries(response.headers).forEach(([key, value]) => {
      set.headers[key] = value
    })
    return response.body
  } catch (error) {
    console.error(`Proxy error for instance ${instanceKey}:`, error)
    set.status = 502
    return createProxyErrorResponse()
  }
}

export const proxyModule = new Elysia({ prefix: `/${ProxyModel.prefix}`, detail: { hide: true } })
  // Get all available proxy targets
  // .get('/', () => {
  //   return ProxyService.getAvailableProxyTargets()
  // }, {
  //   response: {
  //     200: ProxyModel.proxyStatusList
  //   }
  // })

  // Get proxy status for specific instance
  .get('/:instanceKey/status', ({ params: { instanceKey }, set }) => {
    const status = ProxyService.getProxyStatus(instanceKey)
    if (!status) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    return status
  }, {
    response: {
      200: ProxyModel.proxyStatus,
      404: ProxyModel.instanceNotFoundError
    }
  })

  // Health check for proxied instance
  .get('/:instanceKey/health', async ({ params: { instanceKey }, set }) => {
    const status = ProxyService.getProxyStatus(instanceKey)
    if (!status) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    const isHealthy = await ProxyService.healthCheck(instanceKey)
    return { instanceKey, healthy: isHealthy, status: status.status }
  }, {
    response: {
      200: ProxyModel.healthResponse,
      404: ProxyModel.healthErrorResponse
    }
  })

  // WebSocket proxy route
  .ws('/:instanceKey/ws', {
    async open(ws) {
      const instanceKey = (ws.data.params as { instanceKey: string }).instanceKey
      console.log(`WebSocket opened for instance: ${instanceKey}`)

      try {
        // Check if instance is available
        if (!ProxyService.isInstanceAvailable(instanceKey)) {
          console.log(`Instance ${instanceKey} not available for WebSocket connection`)
          ws.close()
          return
        }

        // Create proxy WebSocket connection using the full proxied path including query string
        const query = (ws.data as any)?.query as Record<string, string> | undefined
        const connectionId = createWebSocketConnectionId(instanceKey)
        ;(ws.data as any).proxyConnectionId = connectionId
        const wsPath = createWebSocketProxyPath(instanceKey, query)

        // Forward incoming headers (auth, cookies, protocols) if available
        const incomingHeaders: Record<string, string> = {}
        const hdrs = (ws.data as any)?.headers as Record<string, string | string[]> | undefined
        if (hdrs) {
          for (const [k, v] of Object.entries(hdrs)) {
            if (Array.isArray(v)) incomingHeaders[k] = v.join(', ')
            else if (typeof v === 'string') incomingHeaders[k] = v
          }
        }

        const proxyWs = await WebSocketProxyService.createWebSocketConnection(
          connectionId,
          instanceKey,
          wsPath,
          incomingHeaders
        )
        if (!proxyWs) {
          console.log(`Failed to create proxy WebSocket for instance ${instanceKey}`)
          ws.close()
          return
        }

        // Set up message forwarding from proxy to client
        proxyWs.on('message', (data) => {
          try {
            ws.send(data.toString())
          } catch (error) {
            console.error(`Error forwarding message from proxy to client for instance ${instanceKey}:`, error)
          }
        })

        // Handle proxy WebSocket close
        proxyWs.on('close', () => {
          console.log(`Proxy WebSocket closed for instance ${instanceKey}`)
          ws.close()
        })

        // Handle proxy WebSocket error
        proxyWs.on('error', (error) => {
          console.error(`Proxy WebSocket error for instance ${instanceKey}:`, error)
          ws.close()
        })

      } catch (error) {
        console.error(`Error setting up WebSocket proxy for instance ${instanceKey}:`, error)
        ws.close()
      }
    },

    message(ws, message) {
      const instanceKey = (ws.data.params as { instanceKey: string }).instanceKey
      const connectionId = (ws.data as any).proxyConnectionId as string | undefined
      const proxyWs = connectionId ? WebSocketProxyService.getWebSocketConnection(connectionId) : null

      if (!proxyWs || proxyWs.readyState !== proxyWs.OPEN) {
        console.error(`No active proxy WebSocket connection for instance ${instanceKey}`)
        ws.close()
        return
      }

      try {
        // Forward payload without forcing strings/binary frames into JSON strings.
        proxyWs.send(serializeWebSocketMessage(message))
      } catch (error) {
        console.error(`Error forwarding message to proxy for instance ${instanceKey}:`, error)
        ws.close()
      }
    },

    close(ws) {
      const instanceKey = (ws.data.params as { instanceKey: string }).instanceKey
      const connectionId = (ws.data as any).proxyConnectionId as string | undefined
      console.log(`Client WebSocket closed for instance ${instanceKey}`)

      // Close only this client's proxy WebSocket connection.
      if (connectionId) {
        WebSocketProxyService.closeWebSocketConnection(connectionId)
      }
    }
  })

  // Dynamic proxy route - forwards all requests to the target instance
  .all('/:instanceKey/*', async ({ params: { instanceKey }, request, set, headers }) => {
    return handleProxyRequest(instanceKey, normalizeProxyPath(request.url), request, set, headers)
  })

  // Fallback route for instance root
  .all('/:instanceKey', async ({ params: { instanceKey }, request, set, headers }) => {
    return handleProxyRequest(instanceKey, normalizeProxyPath(request.url), request, set, headers)
  })
