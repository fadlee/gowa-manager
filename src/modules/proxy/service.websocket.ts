import { queries } from '../../db'
import { WebSocket } from 'ws'

const wsConnections = new Map<string, WebSocket>()

export abstract class WebSocketProxyService {
  // WebSocket connection management
  static async createWebSocketConnection(instanceKey: string): Promise<WebSocket | null> {
    try {
      const instance = queries.getInstanceByKey.get(instanceKey) as any
      if (!instance || instance.status !== 'running' || !instance.port) {
        return null
      }

      const existingWs = wsConnections.get(instanceKey)
      if (existingWs && existingWs.readyState === WebSocket.OPEN) {
        return existingWs
      }

      const targetUrl = `ws://localhost:${instance.port}/ws`
      const proxyWs = new WebSocket(targetUrl)

      wsConnections.set(instanceKey, proxyWs)

      proxyWs.on('close', () => {
        wsConnections.delete(instanceKey)
      })

      proxyWs.on('error', (error) => {
        console.error(`WebSocket error for instance ${instanceKey}:`, error)
        wsConnections.delete(instanceKey)
      })

      return proxyWs
    } catch (error) {
      console.error(`Failed to create WebSocket connection for instance ${instanceKey}:`, error)
      return null
    }
  }

  static getWebSocketConnection(instanceKey: string): WebSocket | null {
    return wsConnections.get(instanceKey) || null
  }

  static closeWebSocketConnection(instanceKey: string): void {
    const ws = wsConnections.get(instanceKey)
    if (ws) {
      ws.close()
      wsConnections.delete(instanceKey)
    }
  }

  static closeAllWebSocketConnections(): void {
    for (const [instanceKey, ws] of wsConnections) {
      ws.close()
    }
    wsConnections.clear()
  }
}
