import { queries, db } from '../../db'
import { join } from 'path'
import type { SystemModel } from './model'
import { createServer } from 'net'

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
    return {
      port_range: {
        min: 8000,
        max: 9000
      },
      data_directory: join(process.cwd(), 'data'),
      binaries_directory: join(process.cwd(), 'data', 'binaries')
    }
  }

  // Check if port is available by checking instances table and actual network port
  static isPortAvailable(port: number): Promise<boolean> {
    // Special case: port 3000 is used by the server itself
    if (port === 3000) {
      return Promise.resolve(false)
    }
    
    // Check reserved system ports (0-1023)
    if (port < 1024) {
      return Promise.resolve(false)
    }
    
    // Only check database for ports assigned to OTHER instances (not during restart)
    // Skip database check during restart scenarios - rely on actual network test
    
    // Check actual network port availability
    return new Promise((resolve) => {
      const server = createServer()
      
      server.once('error', (err: any) => {
        // Port is in use if we get EADDRINUSE error
        if (err.code === 'EADDRINUSE') {
          resolve(false)
        } else {
          // For any other error, we'll assume the port is available
          resolve(true)
        }
        server.close()
      })
      
      server.once('listening', () => {
        // Port is available
        server.close()
        resolve(true)
      })
      
      server.listen(port, '127.0.0.1')
    })
  }
}
