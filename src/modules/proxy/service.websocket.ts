import { queries } from '../../db'
import { WebSocket } from 'ws'
import { WebSocketRegistry } from './websocket-registry'
import { applyInstanceWebSocketAuthHeader } from './auth-utils'

const wsConnections = new WebSocketRegistry<WebSocket>()

export abstract class WebSocketProxyService {
  // WebSocket connection management
  static async createWebSocketConnection(
    connectionId: string,
    instanceKey: string,
    path: string,
    incomingHeaders?: Record<string, string>
  ): Promise<WebSocket | null> {
    try {
      const instance = queries.getInstanceByKey.get(instanceKey) as any
      if (!instance || instance.status !== 'running' || !instance.port) {
        return null
      }

      const targetUrl = `ws://localhost:${instance.port}${path}`

      // Forward only essential headers for WS auth/handshake
      const forwardHeaders: Record<string, string> = {}
      let subprotocols: string[] | undefined
      if (incomingHeaders) {
        const allowList = [
          'authorization',
          'cookie',
          'origin',
          'user-agent',
          'accept-language',
        ]
        for (const [k, v] of Object.entries(incomingHeaders)) {
          const key = k.toLowerCase()
          if (key === 'sec-websocket-protocol') {
            subprotocols = v.split(',').map((s) => s.trim()).filter(Boolean)
            continue
          }
          if (allowList.includes(key)) forwardHeaders[key] = v
        }
      }

      const upstreamHeaders = applyInstanceWebSocketAuthHeader(forwardHeaders, instance)
      const proxyWs = new WebSocket(targetUrl, subprotocols, { headers: upstreamHeaders })

      console.log('WS targetUrl: ' + targetUrl)

      wsConnections.set(connectionId, proxyWs)

      proxyWs.on('close', () => {
        wsConnections.delete(connectionId)
      })

      proxyWs.on('error', (error) => {
        console.error(`WebSocket error for instance ${instanceKey}:`, error)
        wsConnections.delete(connectionId)
      })

      // Note: 'unexpected-response' event is not implemented in Bun's ws shim; avoid adding a listener to prevent warnings.

      return proxyWs
    } catch (error) {
      console.error(`Failed to create WebSocket connection for instance ${instanceKey}:`, error)
      return null
    }
  }

  static getWebSocketConnection(connectionId: string): WebSocket | null {
    return wsConnections.get(connectionId)
  }

  static closeWebSocketConnection(connectionId: string): void {
    wsConnections.close(connectionId)
  }

  static getConnectionCount(): number {
    return wsConnections.count()
  }

  static closeAllWebSocketConnections(): void {
    wsConnections.closeAll()
  }
}
