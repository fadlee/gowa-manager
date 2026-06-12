import { afterEach, describe, expect, mock, test } from 'bun:test'
import { DeviceClient } from './device-client'
import type { InstanceModel } from '../model'

const originalFetch = globalThis.fetch
const originalTtl = process.env.INSTANCE_DEVICES_CACHE_TTL_MS
const originalTimeout = process.env.INSTANCE_DEVICES_FETCH_TIMEOUT_MS

function instance(overrides: Partial<InstanceModel.instanceResponse> = {}): InstanceModel.instanceResponse {
  return {
    id: 501,
    key: 'DEVTEST1',
    name: 'device-test',
    port: 19991,
    status: 'running',
    config: JSON.stringify({ flags: { basicAuth: [{ username: 'admin', password: 'secret' }] } }),
    gowa_version: 'v8.7.0',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  }
}

function jsonResponse(body: unknown, init: ResponseInit = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'content-type': 'application/json' },
    ...init,
  })
}

describe('DeviceClient', () => {
  afterEach(() => {
    globalThis.fetch = originalFetch
    if (originalTtl === undefined) delete process.env.INSTANCE_DEVICES_CACHE_TTL_MS
    else process.env.INSTANCE_DEVICES_CACHE_TTL_MS = originalTtl
    if (originalTimeout === undefined) delete process.env.INSTANCE_DEVICES_FETCH_TIMEOUT_MS
    else process.env.INSTANCE_DEVICES_FETCH_TIMEOUT_MS = originalTimeout
    DeviceClient.clearAllCache()
  })

  test('returns not-running response without fetching', async () => {
    const fetchMock = mock(async () => jsonResponse([]))
    globalThis.fetch = fetchMock as any

    const result = await DeviceClient.getDevices(instance({ status: 'stopped' }))

    expect(result).toEqual({
      count: 0,
      connected: false,
      stale: false,
      devices: [],
      source: 'not-running',
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  test('normalizes array response and sends basic auth header', async () => {
    const fetchMock = mock(async () => jsonResponse([{ id: 'phone-a' }, { id: 'phone-b' }]))
    globalThis.fetch = fetchMock as any

    const result = await DeviceClient.getDevices(instance())

    expect(result.count).toBe(2)
    expect(result.connected).toBe(true)
    expect(result.stale).toBe(false)
    expect(result.source).toBe('live')
    expect(result.devices).toEqual([{ id: 'phone-a' }, { id: 'phone-b' }])
    expect(result.fetchedAt).toEqual(expect.any(String))
    const [url, init] = fetchMock.mock.calls[0] as any[]
    expect(url).toBe('http://localhost:19991/app/DEVTEST1/devices')
    expect(init.headers.Authorization).toBe(`Basic ${btoa('admin:secret')}`)
    expect(init.headers.Accept).toBe('application/json')
  })

  test('normalizes devices and data wrapper responses', async () => {
    const fetchMock = mock(async () => jsonResponse({ devices: [{ jid: 'one' }] }))
    globalThis.fetch = fetchMock as any

    const devicesResult = await DeviceClient.getDevices(instance({ id: 502 }))
    DeviceClient.clearCache(502)
    fetchMock.mockImplementationOnce(async () => jsonResponse({ data: [{ jid: 'two' }, { jid: 'three' }] }))

    const dataResult = await DeviceClient.getDevices(instance({ id: 502 }))

    expect(devicesResult.devices).toEqual([{ jid: 'one' }])
    expect(dataResult.count).toBe(2)
    expect(dataResult.devices).toEqual([{ jid: 'two' }, { jid: 'three' }])
  })

  test('uses fresh cache within ttl', async () => {
    process.env.INSTANCE_DEVICES_CACHE_TTL_MS = '60000'
    const fetchMock = mock(async () => jsonResponse([{ id: 'cached' }]))
    globalThis.fetch = fetchMock as any

    const first = await DeviceClient.getDevices(instance({ id: 503 }))
    const second = await DeviceClient.getDevices(instance({ id: 503 }))

    expect(first.source).toBe('live')
    expect(second.source).toBe('cache')
    expect(second.devices).toEqual([{ id: 'cached' }])
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  test('returns stale cache when refresh fails', async () => {
    process.env.INSTANCE_DEVICES_CACHE_TTL_MS = '0'
    const fetchMock = mock(async () => jsonResponse([{ id: 'old' }]))
    globalThis.fetch = fetchMock as any

    await DeviceClient.getDevices(instance({ id: 504 }))
    fetchMock.mockImplementationOnce(async () => { throw new Error('network down') })

    const stale = await DeviceClient.getDevices(instance({ id: 504 }))

    expect(stale.source).toBe('cache')
    expect(stale.stale).toBe(true)
    expect(stale.count).toBe(1)
    expect(stale.error).toBe('network down')
  })

  test('returns safe live error without cache', async () => {
    const fetchMock = mock(async () => jsonResponse({ nope: true }))
    globalThis.fetch = fetchMock as any

    const result = await DeviceClient.getDevices(instance({ id: 505 }))

    expect(result).toMatchObject({
      count: 0,
      connected: false,
      stale: false,
      devices: [],
      source: 'live',
      error: 'Unexpected devices response shape',
    })
  })

  test('summarizes full response without devices list', async () => {
    const fetchMock = mock(async () => jsonResponse([{ id: 'phone' }]))
    globalThis.fetch = fetchMock as any

    const summary = await DeviceClient.getDevicesSummary(instance({ id: 506 }))

    expect(summary).toEqual({
      count: 1,
      connected: true,
      stale: false,
      fetchedAt: expect.any(String),
    })
  })
})
