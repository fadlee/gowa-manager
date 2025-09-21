import { queries } from '../../db'
import { ProxyModel } from './model'

export abstract class ProxyService {

  // Forward request to instance with URL modification and content transformation
  static async forwardRequest(
    instanceKey: string,
    path: string,
    method: string,
    headers: Record<string, string>,
    body?: any,
    hostHeader?: string
  ): Promise<{ status: number; headers: Record<string, string>; body: any; isBinary: boolean }> {
    // Get instance info
    const instance = queries.getInstanceByKey.get(instanceKey) as any
    if (!instance) {
      throw new Error('Instance not found')
    }

    if (instance.status !== 'running' || !instance.port) {
      throw new Error('Instance is not running')
    }

    // Build target URL
    const targetUrl = `http://localhost:${instance.port}${path}`

    try {
      // Prepare headers for forwarding
      const forwardHeaders: Record<string, string> = {
        ...headers,
        'X-Forwarded-For': headers['x-forwarded-for'] || 'localhost',
        'X-Forwarded-Proto': 'http',
        'X-Forwarded-Host': headers.host || 'localhost'
      }
      // Remove host header to avoid conflicts
      delete forwardHeaders.host

      // Prepare request body
      let requestBody: string | ArrayBuffer | undefined
      if (method !== 'GET' && method !== 'HEAD' && body !== undefined) {
        if (body instanceof ArrayBuffer) {
          requestBody = body
        } else if (typeof body === 'string') {
          requestBody = body
        } else {
          requestBody = JSON.stringify(body)
          if (!forwardHeaders['content-type']) {
            forwardHeaders['content-type'] = 'application/json'
          }
        }
      }

      // Forward the request
      const response = await fetch(targetUrl, {
        method,
        headers: forwardHeaders,
        body: requestBody
      })

      // Get response headers
      const responseHeaders: Record<string, string> = {}
      response.headers.forEach((value, key) => {
        responseHeaders[key] = value
      })

      const contentType = response.headers.get('content-type') || ''
      let responseBody: any

      // Handle binary content (images, videos, etc.) - pass through without modification
      if (this.isBinaryContent(contentType)) {
        responseBody = await response.arrayBuffer()
      }
      // Handle other content types
      else {
        responseBody = await response.text()
      }

      return {
        status: response.status,
        headers: responseHeaders,
        body: responseBody,
        isBinary: this.isBinaryContent(contentType)
      }
    } catch (error) {
      throw new Error(`Proxy request failed: ${error}`)
    }
  }

  // Modify URLs in JSON responses to include proxy prefix
  private static modifyJsonUrls(obj: any, instanceKey: string, hostHeader?: string): any {
    if (typeof obj !== 'object' || obj === null) return obj

    if (Array.isArray(obj)) {
      return obj.map(item => this.modifyJsonUrls(item, instanceKey, hostHeader))
    }

    const result: any = {}
    const host = hostHeader || 'localhost'

    for (const [key, value] of Object.entries(obj)) {
      if (typeof value === 'string') {
        if (value.includes(host)) {
          result[key] = value.replace(host, `${host}/${ProxyModel.prefix}/${instanceKey}`)
        } else if (value.startsWith('/') && !value.startsWith(`/${ProxyModel.prefix}/${instanceKey}/`)) {
          result[key] = `/${ProxyModel.prefix}/${instanceKey}${value}`
        } else {
          result[key] = value
        }
      } else if (typeof value === 'object' && value !== null) {
        result[key] = this.modifyJsonUrls(value, instanceKey, hostHeader)
      } else {
        result[key] = value
      }
    }

    return result
  }

  // Check if content type is binary
  private static isBinaryContent(contentType: string): boolean {
    const binaryTypes = [
      'image/',
      'video/',
      'audio/',
      'application/pdf',
      'application/zip',
      'application/octet-stream',
      'font/',
      'application/font'
    ]

    return binaryTypes.some(type => contentType.includes(type))
  }

  // Get proxy status for instance
  static getProxyStatus(instanceKey: string): ProxyModel.proxyStatus | null {
    const instance = queries.getInstanceByKey.get(instanceKey) as any
    if (!instance) return null

    return {
      instanceKey: instanceKey,
      instanceName: instance.name,
      status: instance.status,
      port: instance.port,
      targetPort: instance.port,
      proxyPath: `${ProxyModel.prefix}/${instanceKey}`
    }
  }

  // Check if instance is available for proxying
  static isInstanceAvailable(instanceKey: string): boolean {
    const instance = queries.getInstanceByKey.get(instanceKey) as any
    return instance && instance.status === 'running' && instance.port
  }

  // Get all available proxy targets
  static getAvailableProxyTargets(): ProxyModel.proxyStatus[] {
    const allInstances = queries.getAllInstances.all() as any[]
    return allInstances
      .filter(instance => instance.status === 'running' && instance.port)
      .map(instance => ({
        instanceKey: instance.key,
        instanceName: instance.name,
        status: instance.status,
        port: instance.port,
        targetPort: instance.port,
        proxyPath: `${ProxyModel.prefix}/${instance.key}`
      }))
  }

  // Health check for proxied instance
  static async healthCheck(instanceKey: string): Promise<boolean> {
    try {
      const instance = queries.getInstanceByKey.get(instanceKey) as any
      if (!instance || instance.status !== 'running' || !instance.port) {
        return false
      }

      const response = await fetch(`http://localhost:${instance.port}/`, {
        method: 'GET',
        signal: AbortSignal.timeout(5000) // 5 second timeout
      })

      return response.ok
    } catch {
      return false
    }
  }
}
