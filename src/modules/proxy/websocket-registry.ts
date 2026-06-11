type ClosableWebSocket = {
  close: () => void
}

export class WebSocketRegistry<T extends ClosableWebSocket> {
  private readonly connections = new Map<string, T>()

  set(connectionId: string, socket: T): void {
    this.connections.set(connectionId, socket)
  }

  get(connectionId: string): T | null {
    return this.connections.get(connectionId) || null
  }

  delete(connectionId: string): void {
    this.connections.delete(connectionId)
  }

  close(connectionId: string): void {
    const socket = this.connections.get(connectionId)
    if (!socket) return

    socket.close()
    this.connections.delete(connectionId)
  }

  closeAll(): void {
    for (const socket of this.connections.values()) {
      socket.close()
    }
    this.connections.clear()
  }

  count(): number {
    return this.connections.size
  }
}
