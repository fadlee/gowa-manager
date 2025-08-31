import { existsSync, mkdirSync } from 'node:fs'
import type { InstanceModel } from './model'
import { join } from 'node:path'
import { queries, generateInstanceKey } from '../../db'
import { SystemService } from '../system/service'

// Process management utilities
const runningProcesses = new Map<number, { pid: number; startTime: number }>()

// GOWA binary path
const GOWA_BINARY_PATH = join(process.cwd(), 'data', 'bin', 'gowa')

// Generate random instance name
function generateRandomName(): string {
  const adjectives = ['fast', 'swift', 'bright', 'cool', 'smart', 'quick', 'active', 'dynamic']
  const nouns = ['app', 'service', 'worker', 'server', 'instance', 'process', 'handler', 'engine']
  const adjective = adjectives[Math.floor(Math.random() * adjectives.length)]
  const noun = nouns[Math.floor(Math.random() * nouns.length)]
  const randomNum = Math.floor(Math.random() * 1000)
  return `${adjective}-${noun}-${randomNum}`
}

export abstract class InstanceService {
  // Get all instances
  static getAllInstances(): InstanceModel.instanceListResponse {
    return queries.getAllInstances.all() as InstanceModel.instanceListResponse
  }

  // Get instance by ID
  static getInstanceById(id: number): InstanceModel.instanceResponse | null {
    const instance = queries.getInstanceById.get(id) as InstanceModel.instanceResponse | undefined
    return instance || null
  }

  // Create new instance
  static async createInstance(data: InstanceModel.createBody): Promise<InstanceModel.instanceResponse> {
    // Get next available port dynamically
    const port = await SystemService.getNextAvailablePort()

    // Generate name if not provided
    const name = data.name || generateRandomName()

    // Set default config with gowa rest command
    const defaultConfig = {
      args: ['rest', '--port=PORT']
    }

    let config = defaultConfig
    if (data.config) {
      try {
        const parsedConfig = JSON.parse(data.config)
        config = { ...defaultConfig, ...parsedConfig }
      } catch {
        // If config is invalid JSON, use default
        config = defaultConfig
      }
    }

    // Generate a unique key for the instance
    const key = generateInstanceKey()

    const instance = queries.createInstance.get(
      key,
      name,
      port,
      JSON.stringify(config)
    ) as InstanceModel.instanceResponse

    // Create instance directory
    this.createInstanceDirectory(instance.id)

    return instance
  }

  // Create instance-specific directory
  private static createInstanceDirectory(instanceId: number): string {
    const instanceDir = join(process.cwd(), 'data', 'instances', instanceId.toString())
    if (!existsSync(instanceDir)) {
      mkdirSync(instanceDir, { recursive: true })
    }
    return instanceDir
  }

  // Get instance directory path
  private static getInstanceDirectory(instanceId: number): string {
    return join(process.cwd(), 'data', 'instances', instanceId.toString())
  }

  // Update instance
  static updateInstance(id: number, data: InstanceModel.updateBody): InstanceModel.instanceResponse | null {
    const existing = this.getInstanceById(id)
    if (!existing) return null

    const updated = queries.updateInstance.get(
      existing.key,
      data.name || existing.name,
      existing.port,
      data.config || existing.config,
      id
    ) as InstanceModel.instanceResponse

    return updated
  }

  // Delete instance
  static deleteInstance(id: number): boolean {
    const instance = this.getInstanceById(id)
    if (!instance) return false

    // Stop process if running
    if (instance.status === 'running') {
      this.stopInstance(id)
    }

    // Clean up instance directory
    this.cleanupInstanceDirectory(id)

    const result = queries.deleteInstance.run(id)
    return result.changes > 0
  }

  // Clean up instance directory
  private static cleanupInstanceDirectory(instanceId: number): void {
    const instanceDir = this.getInstanceDirectory(instanceId)
    if (existsSync(instanceDir)) {
      try {
        const fs = require('fs')
        fs.rmSync(instanceDir, { recursive: true, force: true })
      } catch (error) {
        console.warn(`Failed to cleanup instance directory ${instanceDir}:`, error)
      }
    }
  }

  private static isReallyRunning(id: number): boolean {
    const instance = this.getInstanceById(id)
    if (!instance) return false

    return runningProcesses.has(id)
  }

  // Start instance
  static async startInstance(id: number): Promise<InstanceModel.statusResponse | null> {
    const instance = this.getInstanceById(id)
    if (!instance) return null

    if (instance.status === 'running' && this.isReallyRunning(id)) {
      return this.getInstanceStatus(id)
    }

    try {
      // Ensure instance directory exists
      const instanceDir = this.createInstanceDirectory(id)

      // Parse configuration to get command arguments and environment
      let config: any = {}
      try {
        config = JSON.parse(instance.config || '{}')
      } catch {
        config = {}
      }

      // Prepare command arguments, replacing PORT placeholder
      let args: string[] = []
      if (config.args) {
        if (Array.isArray(config.args)) {
          // If args is already an array, use it directly
          args = config.args
        } else if (typeof config.args === 'string') {
          // If args is a string, split it by spaces (handling quoted arguments)
          args = config.args.trim() ? config.args.trim().split(/\s+/) : []
        }
      }

      console.log(`Debug - config.args type: ${typeof config.args}, value:`, config.args)
      console.log(`Debug - processed args:`, args)

      const processedArgs = args.map((arg: string) =>
        arg.replace(/PORT/g, instance.port?.toString() || '8080')
      )

      // Prepare environment variables
      let envVars: Record<string, string> = {}
      if (config.env) {
        if (typeof config.env === 'object') {
          envVars = config.env
        } else if (typeof config.env === 'string' || typeof config.envVars === 'string') {
          // Parse environment variables from string format "KEY=value KEY2=value2"
          const envString = config.env || config.envVars || ''
          envString.split(/\s+/).forEach((pair: string) => {
            const [key, ...valueParts] = pair.split('=')
            if (key && valueParts.length > 0) {
              envVars[key] = valueParts.join('=')
            }
          })
        }
      } else if (config.envVars && typeof config.envVars === 'string') {
        // Handle legacy envVars field
        config.envVars.split(/\s+/).forEach((pair: string) => {
          const [key, ...valueParts] = pair.split('=')
          if (key && valueParts.length > 0) {
            envVars[key] = valueParts.join('=')
          }
        })
      }

      const env = {
        ...process.env,
        PORT: instance.port?.toString() || '8080',
        ...envVars
      }

      console.log(`Starting instance ${id}:`, {
        binary: GOWA_BINARY_PATH,
        args: processedArgs,
        workingDir: instanceDir,
        envKeys: Object.keys(env).filter(k => k !== 'PORT' && !k.startsWith('SYSTEM_')),
        port: instance.port
      })

      // Spawn process using Bun.spawn
      const spawnedProcess = Bun.spawn({
        cmd: [GOWA_BINARY_PATH, ...processedArgs],
        cwd: instanceDir, // Run in instance-specific directory
        env
      })

      // Store process info
      runningProcesses.set(id, {
        pid: spawnedProcess.pid,
        startTime: Date.now()
      })

      // Update status in database
      queries.updateInstanceStatus.run('running', id)

      return this.getInstanceStatus(id)
    } catch (error) {
      console.error(`Failed to start instance ${id}:`, error)
      queries.updateInstanceStatus.run('error', id)
      throw new Error(`Failed to start instance: ${error}`)
    }
  }

  // Stop instance (graceful)
  static stopInstance(id: number): InstanceModel.statusResponse | null {
    const instance = this.getInstanceById(id)
    if (!instance) return null

    const processInfo = runningProcesses.get(id)
    if (processInfo) {
      try {
        // Graceful shutdown with SIGTERM
        process.kill(processInfo.pid, 'SIGTERM')
        runningProcesses.delete(id)
      } catch (error) {
        console.error(`Failed to stop process ${processInfo.pid}:`, error)
      }
    }

    queries.updateInstanceStatus.run('stopped', id)
    return this.getInstanceStatus(id)
  }

  // Kill instance (forceful)
  static killInstance(id: number): InstanceModel.statusResponse | null {
    const instance = this.getInstanceById(id)
    if (!instance) return null

    const processInfo = runningProcesses.get(id)
    if (processInfo) {
      try {
        // Forceful kill with SIGKILL
        process.kill(processInfo.pid, 'SIGKILL')
        runningProcesses.delete(id)
        console.log(`Forcefully killed instance ${id} with PID ${processInfo.pid}`)
      } catch (error) {
        console.error(`Failed to kill process ${processInfo.pid}:`, error)
        // If process doesn't exist anymore, that's fine
        if (error instanceof Error && (error as any).code !== 'ESRCH') {
          throw error
        }
      }
    }

    queries.updateInstanceStatus.run('stopped', id)
    return this.getInstanceStatus(id)
  }

  // Restart instance
  static async restartInstance(id: number): Promise<InstanceModel.statusResponse | null> {
    this.stopInstance(id)
    // Wait a bit before starting
    await new Promise(resolve => setTimeout(resolve, 1000))
    return this.startInstance(id)
  }

  // Get instance status
  static getInstanceStatus(id: number): InstanceModel.statusResponse | null {
    const instance = this.getInstanceById(id)
    if (!instance) return null

    const processInfo = runningProcesses.get(id)
    const uptime = processInfo ? Date.now() - processInfo.startTime : null

    return {
      id: instance.id,
      name: instance.name,
      status: instance.status,
      port: instance.port,
      pid: processInfo?.pid || null,
      uptime
    }
  }
}
