import type { InstanceModel } from '../model'

export type DeviceSource = 'live' | 'cache' | 'not-running'

export type DevicesResponse = {
  count: number
  connected: boolean
  stale: boolean
  devices: Array<Record<string, unknown>>
  fetchedAt?: string
  source: DeviceSource
  error?: string
}

export type DeviceSummary = {
  count: number
  connected: boolean
  stale: boolean
  fetchedAt?: string
  error?: string
}

type CachedDevices = DevicesResponse & {
  cachedAt: number
}

const DEFAULT_CACHE_TTL_MS = 15_000
const DEFAULT_FETCH_TIMEOUT_MS = 3_000

function getCacheTtlMs(): number {
  const ttl = Number(process.env.INSTANCE_DEVICES_CACHE_TTL_MS)
  return Number.isFinite(ttl) && ttl >= 0 ? ttl : DEFAULT_CACHE_TTL_MS
}

function getFetchTimeoutMs(): number {
  const timeout = Number(process.env.INSTANCE_DEVICES_FETCH_TIMEOUT_MS)
  return Number.isFinite(timeout) && timeout > 0 ? timeout : DEFAULT_FETCH_TIMEOUT_MS
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function normalizeDevice(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : { value }
}

function normalizeDevicesBody(body: unknown): { devices: Array<Record<string, unknown>>; error?: string } {
  if (Array.isArray(body)) {
    return { devices: body.map(normalizeDevice) }
  }

  if (isRecord(body)) {
    for (const key of ['devices', 'data', 'results', 'sessions', 'accounts']) {
      const value = body[key]
      if (Array.isArray(value)) {
        return { devices: value.map(normalizeDevice) }
      }
      if (isRecord(value)) {
        const nested = normalizeDevicesBody(value)
        if (!nested.error) return nested
      }
    }

    const metadataKeys = new Set(['count', 'connected', 'success', 'status', 'message', 'error'])
    const collectionValues = Object.entries(body)
      .filter(([key]) => !metadataKeys.has(key))
      .map(([, value]) => value)

    if (collectionValues.length > 0 && collectionValues.every(isRecord)) {
      return { devices: collectionValues.map(normalizeDevice) }
    }
  }

  return { devices: [], error: 'Unexpected devices response shape' }
}

function extractBasicAuthHeader(configJson: string): string | undefined {
  try {
    const config = JSON.parse(configJson || '{}')
    const firstAuth = config.flags?.basicAuth?.[0]
    if (firstAuth?.username && firstAuth?.password) {
      return `Basic ${btoa(`${firstAuth.username}:${firstAuth.password}`)}`
    }
  } catch {
    return undefined
  }

  return undefined
}

function toSafeError(error: unknown): string {
  if (error instanceof Error && error.name === 'AbortError') {
    return 'GOWA devices request timed out'
  }
  return error instanceof Error ? error.message : String(error)
}

function withoutCacheMetadata(cached: CachedDevices, stale: boolean, error?: string): DevicesResponse {
  const { cachedAt: _cachedAt, ...response } = cached
  return {
    ...response,
    source: 'cache',
    stale,
    ...(error ? { error } : response.error ? { error: response.error } : {}),
  }
}

function toSummary(response: DevicesResponse): DeviceSummary {
  return {
    count: response.count,
    connected: response.connected,
    stale: response.stale,
    ...(response.fetchedAt ? { fetchedAt: response.fetchedAt } : {}),
    ...(response.error ? { error: response.error } : {}),
  }
}

export abstract class DeviceClient {
  private static cache = new Map<number, CachedDevices>()

  static async getDevices(instance: InstanceModel.instanceResponse): Promise<DevicesResponse> {
    if (instance.status.toLowerCase() !== 'running' || !instance.port) {
      return {
        count: 0,
        connected: false,
        stale: false,
        devices: [],
        source: 'not-running',
      }
    }

    const cached = this.cache.get(instance.id)
    const now = Date.now()
    if (cached && now - cached.cachedAt < getCacheTtlMs()) {
      return withoutCacheMetadata(cached, false)
    }

    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), getFetchTimeoutMs())

    try {
      const authHeader = extractBasicAuthHeader(instance.config)
      const response = await fetch(`http://localhost:${instance.port}/app/${instance.key}/devices`, {
        headers: {
          ...(authHeader ? { Authorization: authHeader } : {}),
          Accept: 'application/json',
        },
        signal: controller.signal,
      })

      if (!response.ok) {
        throw new Error(`GOWA devices request failed with status ${response.status}`)
      }

      const body = await response.json()
      const normalized = normalizeDevicesBody(body)
      const liveResponse: DevicesResponse = {
        count: normalized.devices.length,
        connected: normalized.devices.length > 0,
        stale: false,
        devices: normalized.devices,
        fetchedAt: new Date().toISOString(),
        source: 'live',
        ...(normalized.error ? { error: normalized.error } : {}),
      }

      if (!normalized.error) {
        this.cache.set(instance.id, { ...liveResponse, cachedAt: now })
      }

      return liveResponse
    } catch (error) {
      const message = toSafeError(error)
      if (cached) {
        return withoutCacheMetadata(cached, true, message)
      }

      return {
        count: 0,
        connected: false,
        stale: false,
        devices: [],
        source: 'live',
        error: message,
      }
    } finally {
      clearTimeout(timeout)
    }
  }

  static async getDevicesSummary(instance: InstanceModel.instanceResponse): Promise<DeviceSummary> {
    const response = await this.getDevices(instance)
    return toSummary(response)
  }

  static clearCache(instanceId: number): void {
    this.cache.delete(instanceId)
  }

  static clearAllCache(): void {
    this.cache.clear()
  }
}
