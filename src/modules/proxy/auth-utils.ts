import { hasValidMagicAdminCookie } from './magic-auth'

type InstanceWithConfig = {
  key?: string
  config?: string | null
}

export function shouldInjectInstanceWebSocketAuth(): boolean {
  return process.env.PROXY_WS_INJECT_INSTANCE_AUTH !== 'false'
}

export function getFirstInstanceBasicAuthHeader(instance: InstanceWithConfig): string | null {
  if (!instance.config) return null

  try {
    const config = JSON.parse(instance.config)
    const firstAuth = config?.flags?.basicAuth?.[0]
    if (!firstAuth?.username || !firstAuth?.password) return null

    return `Basic ${btoa(`${firstAuth.username}:${firstAuth.password}`)}`
  } catch {
    return null
  }
}

export function applyInstanceWebSocketAuthHeader(
  headers: Record<string, string>,
  instance: InstanceWithConfig
): Record<string, string> {
  if (headers.authorization || !shouldInjectInstanceWebSocketAuth()) {
    return headers
  }

  const authHeader = getFirstInstanceBasicAuthHeader(instance)
  if (!authHeader) return headers

  return {
    ...headers,
    authorization: authHeader,
  }
}

export function applyInstanceHttpAuthHeader(
  headers: Record<string, string>,
  instance: InstanceWithConfig
): Record<string, string> {
  if (headers.authorization || !instance.key || !hasValidMagicAdminCookie(headers.cookie, instance.key)) {
    return headers
  }

  const authHeader = getFirstInstanceBasicAuthHeader(instance)
  if (!authHeader) return headers

  return {
    ...headers,
    authorization: authHeader,
  }
}
