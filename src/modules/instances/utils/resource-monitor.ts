import pidusage from 'pidusage'

export interface ResourceUsage {
  cpuPercent: number
  memoryMB: number
  memoryPercent: number
  avgCpu?: number
  avgMemory?: number
}

interface ResourceHistory {
  cpuHistory: number[]
  memoryHistory: number[]
  maxHistorySize: number
}

// Track resource history for each instance (for averages)
const resourceHistory = new Map<number, ResourceHistory>()

export class ResourceMonitor {
  
  /**
   * Get current resource usage for a process PID
   */
  static async getResourceUsage(pid: number, instanceId?: number): Promise<ResourceUsage | null> {
    try {
      const stats = await pidusage(pid)
      
      const resourceUsage: ResourceUsage = {
        cpuPercent: stats.cpu || 0,
        memoryMB: (stats.memory || 0) / (1024 * 1024), // Convert bytes to MB
        memoryPercent: stats.memory ? (stats.memory / (stats.memory + (process.memoryUsage().heapTotal || 1))) * 100 : 0
      }

      // Calculate system memory percentage more accurately
      const systemMemoryGB = 16 // Assume 16GB system memory (could be made dynamic)
      const systemMemoryBytes = systemMemoryGB * 1024 * 1024 * 1024
      resourceUsage.memoryPercent = (stats.memory / systemMemoryBytes) * 100

      // If we have an instance ID, track history for averages
      if (instanceId !== undefined) {
        const history = this.getOrCreateHistory(instanceId)
        
        // Add current values to history
        history.cpuHistory.push(resourceUsage.cpuPercent)
        history.memoryHistory.push(resourceUsage.memoryMB)
        
        // Trim history if it gets too long
        if (history.cpuHistory.length > history.maxHistorySize) {
          history.cpuHistory.shift()
          history.memoryHistory.shift()
        }
        
        // Calculate averages
        resourceUsage.avgCpu = this.calculateAverage(history.cpuHistory)
        resourceUsage.avgMemory = this.calculateAverage(history.memoryHistory)
      }

      return resourceUsage
      
    } catch (error) {
      // Process might not exist anymore or we don't have permission
      if (error && typeof error === 'object' && 'code' in error) {
        const nodeError = error as { code: string }
        if (nodeError.code === 'ESRCH') {
          // Process not found - this is normal when process stops
          return null
        }
      }
      
      console.warn(`Failed to get resource usage for PID ${pid}:`, error)
      return null
    }
  }

  /**
   * Get or create resource history for an instance
   */
  private static getOrCreateHistory(instanceId: number): ResourceHistory {
    let history = resourceHistory.get(instanceId)
    if (!history) {
      history = {
        cpuHistory: [],
        memoryHistory: [],
        maxHistorySize: 10 // Keep last 10 measurements for rolling average
      }
      resourceHistory.set(instanceId, history)
    }
    return history
  }

  /**
   * Calculate average from array of numbers
   */
  private static calculateAverage(values: number[]): number {
    if (values.length === 0) return 0
    return values.reduce((sum, val) => sum + val, 0) / values.length
  }

  /**
   * Clear resource history for an instance (call when instance stops)
   */
  static clearHistory(instanceId: number): void {
    resourceHistory.delete(instanceId)
  }

  /**
   * Clear all resource history (call on app shutdown)
   */
  static clearAllHistory(): void {
    resourceHistory.clear()
  }

  /**
   * Get resource usage for multiple PIDs at once
   */
  static async getMultipleResourceUsage(pids: number[]): Promise<Map<number, ResourceUsage | null>> {
    const results = new Map<number, ResourceUsage | null>()
    
    const promises = pids.map(async (pid) => {
      const usage = await this.getResourceUsage(pid)
      results.set(pid, usage)
    })
    
    await Promise.allSettled(promises) // Use allSettled to handle individual failures gracefully
    
    return results
  }

  /**
   * Validate that pidusage is working correctly
   */
  static async testPidUsage(): Promise<boolean> {
    try {
      // Test with current process PID
      const currentPid = process.pid
      const stats = await pidusage(currentPid)
      
      return typeof stats.cpu === 'number' && typeof stats.memory === 'number'
    } catch (error) {
      console.error('pidusage test failed:', error)
      return false
    }
  }
}
