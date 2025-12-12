import { queries, db } from '../../db'
import { join, resolve } from 'path'
import type { SystemModel } from './model'
import { createConnection } from 'net'
import { MANAGER_VERSION } from '../../version'

export abstract class SystemService {
  // Get system status
  static getSystemStatus(): SystemModel.statusResponse {
    // Get instance counts
    const allInstances = queries.getAllInstances.all()
    const runningInstances = allInstances.filter((i: any) => i.status === 'running')
    const stoppedInstances = allInstances.filter((i: any) => i.status === 'stopped')

    // Get port allocation info from instances table
    const allocatedPorts = allInstances.filter((i: any) => i.port !== null).map((i: any) => i.port)
    const highestPort = allocatedPorts.length > 0 ? Math.max(...allocatedPorts) : 7999
    // Note: We're using a simple calculation for next port in status
    // The actual port allocation will use the async isPortAvailable check
    const nextAvailablePort = Math.max(8000, highestPort + 1)

    return {
      status: 'running',
      uptime: process.uptime() * 1000, // Convert to milliseconds
      managerVersion: MANAGER_VERSION,
      instances: {
        total: allInstances.length,
        running: runningInstances.length,
        stopped: stoppedInstances.length
      },
      ports: {
        allocated: allocatedPorts.length,
        next_available: nextAvailablePort
      }
    }
  }

  // Get next available port by checking instances table and network availability
  static async getNextAvailablePort(): Promise<number> {
    const instances = queries.getAllInstances.all() as any[]
    const usedPorts = instances
      .filter(i => i.port !== null)
      .map(i => i.port)
      .sort((a, b) => a - b)

    // Start from 8000 and find first available port
    let port = 8000
    let isAvailable = false

    while (!isAvailable) {
      // Skip ports that are already in the database
      if (usedPorts.includes(port)) {
        port++
        continue
      }

      // Check if the port is actually available on the network
      isAvailable = await this.isPortAvailable(port)
      if (!isAvailable) {
        port++
      }
    }

    return port
  }

  // Get system configuration
  static getSystemConfig(): SystemModel.configResponse {
    const dataDir = process.env.DATA_DIR || join(process.cwd(), 'data')
    // Resolve relative paths to absolute paths
    const absoluteDataDir = resolve(dataDir)
    return {
      port_range: {
        min: 8000,
        max: 9000
      },
      data_directory: absoluteDataDir,
      binaries_directory: join(absoluteDataDir, 'binaries')
    }
  }

  // Check if port is available by trying to connect to it
  // If connection succeeds, something is listening = port NOT available
  // If connection fails (ECONNREFUSED), nothing listening = port available
  private static checkPortAvailability(port: number): Promise<boolean> {
    return new Promise((resolve) => {
      const socket = createConnection({ port, host: '127.0.0.1' })

      // Set a short timeout for the connection attempt
      socket.setTimeout(1000)

      socket.once('connect', () => {
        // Connection succeeded = something is listening = port NOT available
        socket.destroy()
        resolve(false)
      })

      socket.once('error', (err: any) => {
        // ECONNREFUSED = nothing listening = port available
        // Other errors (like ETIMEDOUT) = treat as available
        socket.destroy()
        resolve(true)
      })

      socket.once('timeout', () => {
        // Timeout = probably nothing listening = port available
        socket.destroy()
        resolve(true)
      })
    })
  }

  // Check if port is available for instances (respects reserved ranges)
  static isPortAvailable(port: number): Promise<boolean> {
    // Special case: port 3000 is used by the server itself
    if (port === 3000) {
      return Promise.resolve(false)
    }

    // Check reserved system ports (0-1023)
    if (port < 1024) {
      return Promise.resolve(false)
    }

    return this.checkPortAvailability(port)
  }

  // Check if port is available for the HTTP server (no special-case for 3000)
  static isHttpPortAvailable(port: number): Promise<boolean> {
    return this.checkPortAvailability(port)
  }
}
