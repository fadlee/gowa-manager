import { queries } from '../../db'
import ws, { WebSocket } from 'ws'

const wsConnections = new Map<string, WebSocket>()

export abstract class WebSocketProxyService {
  // WebSocket connection management
  static async createWebSocketConnection(instanceId: string): Promise<WebSocket | null> {
    try {
      const instance = queries.getInstanceById.get(Number(instanceId)) as any
      if (!instance || instance.status !== 'running' || !instance.port) {
        return null
      }

      const existingWs = wsConnections.get(instanceId)
      if (existingWs && existingWs.readyState === WebSocket.OPEN) {
        return existingWs
      }

      const targetUrl = `ws://localhost:${instance.port}/ws`
      const proxyWs = new WebSocket(targetUrl)

      wsConnections.set(instanceId, proxyWs)

      proxyWs.on('close', () => {
        wsConnections.delete(instanceId)
      })

      proxyWs.on('error', (error) => {
        console.error(`WebSocket error for instance ${instanceId}:`, error)
        wsConnections.delete(instanceId)
      })

      return proxyWs
    } catch (error) {
      console.error(`Failed to create WebSocket connection for instance ${instanceId}:`, error)
      return null
    }
  }

  static getWebSocketConnection(instanceId: string): WebSocket | null {
    return wsConnections.get(instanceId) || null
  }

  static closeWebSocketConnection(instanceId: string): void {
    const ws = wsConnections.get(instanceId)
    if (ws) {
      ws.close()
      wsConnections.delete(instanceId)
    }
  }

  static closeAllWebSocketConnections(): void {
    for (const [instanceId, ws] of wsConnections) {
      ws.close()
    }
    wsConnections.clear()
  }
}
