export type WebSocketForwardingOptions = {
  headers: Record<string, string>
  subprotocols?: string[]
}

export function createWebSocketForwardingOptions(
  incomingHeaders?: Record<string, string>
): WebSocketForwardingOptions {
  const headers: Record<string, string> = {}
  let subprotocols: string[] | undefined

  if (!incomingHeaders) return { headers }

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
    if (allowList.includes(key)) headers[key] = v
  }

  return { headers, subprotocols }
}

export function serializeWebSocketMessage(message: unknown): string | Buffer | ArrayBuffer | Uint8Array {
  if (typeof message === 'string') return message
  if (message instanceof ArrayBuffer) return message
  if (ArrayBuffer.isView(message)) return message as Uint8Array
  if (typeof Buffer !== 'undefined' && Buffer.isBuffer(message)) return message

  return JSON.stringify(message)
}
