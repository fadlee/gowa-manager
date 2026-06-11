import { describe, expect, test } from 'bun:test'
import { WebSocketRegistry } from './websocket-registry'

class FakeSocket {
  closeCount = 0

  close() {
    this.closeCount += 1
  }
}

describe('WebSocketRegistry', () => {
  test('stores multiple client connections for the same instance prefix independently', () => {
    const registry = new WebSocketRegistry<FakeSocket>()
    const first = new FakeSocket()
    const second = new FakeSocket()

    registry.set('ABC12345:first-client', first)
    registry.set('ABC12345:second-client', second)

    expect(registry.count()).toBe(2)
    expect(registry.get('ABC12345:first-client')).toBe(first)
    expect(registry.get('ABC12345:second-client')).toBe(second)
  })

  test('closing one client connection does not close another for the same instance', () => {
    const registry = new WebSocketRegistry<FakeSocket>()
    const first = new FakeSocket()
    const second = new FakeSocket()

    registry.set('ABC12345:first-client', first)
    registry.set('ABC12345:second-client', second)

    registry.close('ABC12345:first-client')

    expect(first.closeCount).toBe(1)
    expect(second.closeCount).toBe(0)
    expect(registry.get('ABC12345:first-client')).toBeNull()
    expect(registry.get('ABC12345:second-client')).toBe(second)
    expect(registry.count()).toBe(1)
  })

  test('delete removes a connection without closing the socket', () => {
    const registry = new WebSocketRegistry<FakeSocket>()
    const socket = new FakeSocket()

    registry.set('ABC12345:client', socket)
    registry.delete('ABC12345:client')

    expect(socket.closeCount).toBe(0)
    expect(registry.get('ABC12345:client')).toBeNull()
    expect(registry.count()).toBe(0)
  })

  test('closeAll closes every connection and clears the registry', () => {
    const registry = new WebSocketRegistry<FakeSocket>()
    const first = new FakeSocket()
    const second = new FakeSocket()

    registry.set('ABC12345:first-client', first)
    registry.set('XYZ98765:second-client', second)

    registry.closeAll()

    expect(first.closeCount).toBe(1)
    expect(second.closeCount).toBe(1)
    expect(registry.count()).toBe(0)
  })
})
