import { ProxyModel } from './model'

export function createProxyErrorResponse() {
  return {
    error: 'Proxy request failed',
    success: false,
  }
}

export function normalizeProxyPath(requestUrl: string): string {
  const url = new URL(requestUrl)
  return `${url.pathname}${url.search}`
}

export function createWebSocketProxyPath(instanceKey: string, query?: Record<string, string>): string {
  const qs = query ? new URLSearchParams(query) : undefined
  const queryStr = qs && qs.toString().length > 0 ? `?${qs.toString()}` : ''
  return `/${ProxyModel.prefix}/${instanceKey}/ws${queryStr}`
}

export function createWebSocketConnectionId(instanceKey: string): string {
  return `${instanceKey}:${crypto.randomUUID()}`
}
