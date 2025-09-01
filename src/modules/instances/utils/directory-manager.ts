import { existsSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'

export class DirectoryManager {
  // Create instance-specific directory
  static createInstanceDirectory(instanceId: number): string {
    const instanceDir = join(process.cwd(), 'data', 'instances', instanceId.toString())
    if (!existsSync(instanceDir)) {
      mkdirSync(instanceDir, { recursive: true })
    }
    return instanceDir
  }

  // Get instance directory path
  static getInstanceDirectory(instanceId: number): string {
    return join(process.cwd(), 'data', 'instances', instanceId.toString())
  }

  // Clean up instance directory
  static cleanupInstanceDirectory(instanceId: number): void {
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
}
