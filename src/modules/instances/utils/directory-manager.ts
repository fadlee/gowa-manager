import { existsSync, mkdirSync } from 'node:fs'
import { join, resolve } from 'node:path'

export class DirectoryManager {
  // Get the base data directory (respects DATA_DIR env var or CLI config)
  private static getDataDirectory(): string {
    const dataDir = process.env.DATA_DIR || join(process.cwd(), 'data')
    // Resolve relative paths to absolute paths
    return resolve(dataDir)
  }

  // Create instance-specific directory
  static createInstanceDirectory(instanceId: number): string {
    const dataDir = this.getDataDirectory()
    const instanceDir = join(dataDir, 'instances', instanceId.toString())
    if (!existsSync(instanceDir)) {
      mkdirSync(instanceDir, { recursive: true })
    }
    return instanceDir
  }

  // Get instance directory path
  static getInstanceDirectory(instanceId: number): string {
    const dataDir = this.getDataDirectory()
    return join(dataDir, 'instances', instanceId.toString())
  }

  // Clean up instance directory
  static cleanupInstanceDirectory(instanceId: number): void {
    const instanceDir = this.getInstanceDirectory(instanceId)
    if (existsSync(instanceDir)) {
      try {
        const fs = require('fs')
        fs.rmSync(instanceDir, { recursive: true, force: true })
        console.log(`🗑️  Cleaned up instance directory: ${instanceDir}`)
      } catch (error) {
        console.warn(`Failed to cleanup instance directory ${instanceDir}:`, error)
      }
    }
  }

  // Get instances root directory
  static getInstancesRootDirectory(): string {
    const dataDir = this.getDataDirectory()
    return join(dataDir, 'instances')
  }

  // Ensure instances root directory exists
  static ensureInstancesRootDirectory(): string {
    const instancesDir = this.getInstancesRootDirectory()
    if (!existsSync(instancesDir)) {
      mkdirSync(instancesDir, { recursive: true })
    }
    return instancesDir
  }
}
