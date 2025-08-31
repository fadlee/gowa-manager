import { Elysia } from 'elysia'
import { ProxyService } from './service'
import { ProxyModel } from './model'
import { WebSocketProxyService } from './service.websocket'

// Create a shared handler function
const handleProxyRequest = async (
  instanceId: string,
  path: string,
  request: Request,
  set: any,
  headers: any
) => {
  try {
    // Check if instance is available
    if (!ProxyService.isInstanceAvailable(instanceId)) {
      const status = ProxyService.getProxyStatus(instanceId)
      if (!status) {
        set.status = 404
        return { error: 'Instance not found', success: false }
      }
      set.status = 503
      return { error: 'Instance is not running', success: false, instanceId }
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
      instanceId,
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
    console.error(`Proxy error for instance ${instanceId}:`, error)
    set.status = 502
    return {
      error: error instanceof Error ? error.message : 'Proxy request failed',
      success: false
    }
  }
}

export const proxyModule = new Elysia({ prefix: `/${ProxyModel.prefix}` })
  // Get all available proxy targets
  .get('/', () => {
    return ProxyService.getAvailableProxyTargets()
  }, {
    response: {
      200: ProxyModel.proxyStatusList
    }
  })

  // Dynamic proxy route - forwards all requests to the target instance
  .all('/:instanceId/*', async ({ params: { instanceId }, request, set, headers }) => {
    const url = new URL(request.url)
    const pathSegments = url.pathname.split('/')
    const proxyPath = '/' + pathSegments.slice(3).join('/') + url.search
    return handleProxyRequest(instanceId, proxyPath, request, set, headers)
  })

  // Fallback route for instance root
  .all('/:instanceId', async ({ params: { instanceId }, request, set, headers }) => {
    return handleProxyRequest(instanceId, '/', request, set, headers)
  })

  // Get proxy status for specific instance
  .get('/:instanceId/status', ({ params: { instanceId }, set }) => {
    const status = ProxyService.getProxyStatus(instanceId)
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
  .get('/:instanceId/health', async ({ params: { instanceId }, set }) => {
    const status = ProxyService.getProxyStatus(instanceId)
    if (!status) {
      set.status = 404
      return { error: 'Instance not found', success: false }
    }
    const isHealthy = await ProxyService.healthCheck(instanceId)
    return { instanceId, healthy: isHealthy, status: status.status }
  }, {
    response: {
      200: ProxyModel.healthResponse,
      404: ProxyModel.healthErrorResponse
    }
  })

  // WebSocket proxy route
  .ws('/:instanceId/ws', {
    async open(ws) {
      const instanceId = ws.data.params.instanceId
      console.log(`WebSocket opened for instance: ${instanceId}`)

      try {
        // Check if instance is available
        if (!ProxyService.isInstanceAvailable(instanceId)) {
          console.log(`Instance ${instanceId} not available for WebSocket connection`)
          ws.close()
          return
        }

        // Create proxy WebSocket connection
        const proxyWs = await WebSocketProxyService.createWebSocketConnection(instanceId)
        if (!proxyWs) {
          console.log(`Failed to create proxy WebSocket for instance ${instanceId}`)
          ws.close()
          return
        }

        // Set up message forwarding from proxy to client
        proxyWs.on('message', (data) => {
          try {
            ws.send(data.toString())
          } catch (error) {
            console.error(`Error forwarding message from proxy to client for instance ${instanceId}:`, error)
          }
        })

        // Handle proxy WebSocket close
        proxyWs.on('close', () => {
          console.log(`Proxy WebSocket closed for instance ${instanceId}`)
          ws.close()
        })

        // Handle proxy WebSocket error
        proxyWs.on('error', (error) => {
          console.error(`Proxy WebSocket error for instance ${instanceId}:`, error)
          ws.close()
        })

      } catch (error) {
        console.error(`Error setting up WebSocket proxy for instance ${instanceId}:`, error)
        ws.close()
      }
    },

    message(ws, message) {
      const instanceId = ws.data.params.instanceId
      const proxyWs = WebSocketProxyService.getWebSocketConnection(instanceId)

      if (!proxyWs || proxyWs.readyState !== proxyWs.OPEN) {
        console.error(`No active proxy WebSocket connection for instance ${instanceId}`)
        ws.close()
        return
      }

      try {
        // Forward message to proxy WebSocket
        proxyWs.send(JSON.stringify(message))
      } catch (error) {
        console.error(`Error forwarding message to proxy for instance ${instanceId}:`, error)
        ws.close()
      }
    },

    close(ws) {
      const instanceId = ws.data.params.instanceId
      console.log(`Client WebSocket closed for instance ${instanceId}`)

      // Close the proxy WebSocket connection
      WebSocketProxyService.closeWebSocketConnection(instanceId)
    }
  })
