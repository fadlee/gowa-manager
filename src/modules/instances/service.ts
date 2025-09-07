import type { InstanceModel } from './model'
import { queries, generateInstanceKey } from '../../db'
import { SystemService } from '../system/service'
import { ProcessManager, type ProcessInfo } from './utils/process-manager'
import { DirectoryManager } from './utils/directory-manager'
import { ConfigParser } from './utils/config-parser'
import { NameGenerator } from './utils/name-generator'
import { ResourceMonitor } from './utils/resource-monitor'
import { join } from 'node:path'

const GOWA_BINARY_PATH = join(process.cwd(), 'data', 'bin', 'gowa')

// Initialize process exit handlers
ProcessManager.setupExitHandlers()

export abstract class InstanceService {
  // Expose cleanup function for external use
  static async cleanupAllInstances(): Promise<void> {
    return ProcessManager.cleanupAllInstances()
  }

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
    const name = data.name || NameGenerator.generateRandomName()

    // Set default config with gowa rest command
    const defaultConfig = ConfigParser.getDefaultConfig()

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
    DirectoryManager.createInstanceDirectory(instance.id)

    return instance
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
    DirectoryManager.cleanupInstanceDirectory(id)
    
    // Clear resource history
    ResourceMonitor.clearHistory(id)

    const result = queries.deleteInstance.run(id)
    return result.changes > 0
  }

  private static isReallyRunning(id: number): boolean {
    const instance = this.getInstanceById(id)
    if (!instance) return false

    return ProcessManager.isReallyRunning(id)
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
      const instanceDir = DirectoryManager.createInstanceDirectory(id)

      // Parse configuration
      const config = ConfigParser.parseConfig(instance.config)
      const processedArgs = ConfigParser.processArgs(config, instance.port || 8080)
      const env = ConfigParser.parseEnvironmentVars(config, instance.port || 8080)

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
        env,
        onExit: (subprocess, exitCode, signalCode, error) => {
          console.log(`Instance ${id} process exited with code ${exitCode}`)
          ProcessManager.removeProcess(id)
        }
      })

      // Store process info with monitoring
      const processInfo: ProcessInfo = {
        process: spawnedProcess,
        pid: spawnedProcess.pid,
        startTime: Date.now(),
        cleanup: () => {
          try {
            spawnedProcess.kill()
          } catch (error) {
            console.warn(`Failed to cleanup process ${spawnedProcess.pid}:`, error)
          }
        }
      }

      ProcessManager.addProcess(id, processInfo)

      // Monitor process exit (no database updates)
      spawnedProcess.exited.then(() => {
        console.log(`Instance ${id} process exited`)
        ProcessManager.removeProcess(id)
      }).catch((error) => {
        console.error(`Instance ${id} process error:`, error)
        ProcessManager.removeProcess(id)
      })

      // Update status in database
      queries.updateInstanceStatus.run('running', id)

      return await this.getInstanceStatus(id)
    } catch (error) {
      console.error(`Failed to start instance ${id}:`, error)
      queries.updateInstanceStatus.run('error', id)
      throw new Error(`Failed to start instance: ${error}`)
    }
  }

  // Stop instance (graceful)
  static async stopInstance(id: number): Promise<InstanceModel.statusResponse | null> {
    const instance = this.getInstanceById(id)
    if (!instance) return null

    ProcessManager.stopProcess(id)
    // Clear resource history when stopping
    ResourceMonitor.clearHistory(id)
    queries.updateInstanceStatus.run('stopped', id)
    return await this.getInstanceStatus(id)
  }

  // Kill instance (forceful)
  static async killInstance(id: number): Promise<InstanceModel.statusResponse | null> {
    const instance = this.getInstanceById(id)
    if (!instance) return null

    ProcessManager.killProcess(id)
    // Clear resource history when killing
    ResourceMonitor.clearHistory(id)
    queries.updateInstanceStatus.run('stopped', id)
    return await this.getInstanceStatus(id)
  }

  // Restart instance
  static async restartInstance(id: number): Promise<InstanceModel.statusResponse | null> {
    this.stopInstance(id)
    // Wait a bit before starting
    await new Promise(resolve => setTimeout(resolve, 1000))
    return await this.startInstance(id)
  }

  // Get instance status
  static async getInstanceStatus(id: number): Promise<InstanceModel.statusResponse | null> {
    const instance = this.getInstanceById(id)
    if (!instance) return null

    const processInfo = ProcessManager.getProcessInfo(id)
    const uptime = processInfo ? Date.now() - processInfo.startTime : null

    let resources = undefined
    
    // Get resource usage if process is running
    if (processInfo?.pid && instance.status === 'running') {
      try {
        resources = await ResourceMonitor.getResourceUsage(processInfo.pid, id)
      } catch (error) {
        console.warn(`Failed to get resource usage for instance ${id}:`, error)
      }
    }

    return {
      id: instance.id,
      name: instance.name,
      status: instance.status,
      port: instance.port,
      pid: processInfo?.pid || null,
      uptime,
      resources: resources || undefined
    }
  }
}
