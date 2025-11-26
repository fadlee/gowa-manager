// Process management utilities
import treeKill from 'tree-kill'

interface ProcessInfo {
  process: Bun.Subprocess;
  pid: number;
  startTime: number;
  cleanup?: () => void;
}

const runningProcesses = new Map<number, ProcessInfo>()
let isShuttingDown = false

export class ProcessManager {
  // Get running processes map
  static getRunningProcesses(): Map<number, ProcessInfo> {
    return runningProcesses
  }

  // Check if instance is really running
  static isReallyRunning(instanceId: number): boolean {
    return runningProcesses.has(instanceId)
  }

  // Add process to tracking
  static addProcess(instanceId: number, processInfo: ProcessInfo): void {
    runningProcesses.set(instanceId, processInfo)
  }

  // Remove process from tracking
  static removeProcess(instanceId: number): void {
    runningProcesses.delete(instanceId)
  }

  // Get process info
  static getProcessInfo(instanceId: number): ProcessInfo | undefined {
    return runningProcesses.get(instanceId)
  }

  // Cleanup all running instances (kill processes only, no database updates)
  static async cleanupAllInstances(): Promise<void> {
    if (isShuttingDown) return
    isShuttingDown = true

    console.log('Cleaning up all running instances...')
    const cleanupPromises: Promise<void>[] = []

    for (const [instanceId, processInfo] of runningProcesses) {
      cleanupPromises.push(
        new Promise<void>((resolve) => {
          console.log(`Tree-killing instance ${instanceId} (PID: ${processInfo.pid})`)

          // Use tree-kill to kill the process and all its children
          treeKill(processInfo.pid, 'SIGKILL', (err) => {
            if (err) {
              console.warn(`Failed to tree-kill instance ${instanceId}:`, err)
            } else {
              console.log(`Tree-killed instance ${instanceId}`)
            }
            resolve()
          })
        })
      )
    }

    await Promise.all(cleanupPromises)
    runningProcesses.clear()
    console.log('All instances cleaned up')
  }

  // Stop a specific process gracefully (async due to tree-kill)
  static stopProcess(instanceId: number): boolean {
    const processInfo = runningProcesses.get(instanceId)
    if (processInfo) {
      // Use tree-kill with SIGTERM for graceful shutdown of process tree
      treeKill(processInfo.pid, 'SIGTERM', (err) => {
        if (err) {
          console.error(`Failed to tree-kill (SIGTERM) process ${processInfo.pid}:`, err)
        } else {
          console.log(`Gracefully tree-killed instance ${instanceId} with PID ${processInfo.pid}`)
        }
      })
      runningProcesses.delete(instanceId)
      return true
    }
    return false
  }

  // Kill a specific process forcefully (async due to tree-kill)
  static killProcess(instanceId: number): boolean {
    const processInfo = runningProcesses.get(instanceId)
    if (processInfo) {
      // Use tree-kill with SIGKILL to forcefully kill process tree
      treeKill(processInfo.pid, 'SIGKILL', (err) => {
        if (err) {
          // ESRCH means process doesn't exist anymore, that's fine
          if ((err as any).code !== 'ESRCH') {
            console.error(`Failed to tree-kill (SIGKILL) process ${processInfo.pid}:`, err)
          }
        } else {
          console.log(`Forcefully tree-killed instance ${instanceId} with PID ${processInfo.pid}`)
        }
      })
      runningProcesses.delete(instanceId)
      return true
    }
    return false
  }

  // Setup process exit handlers
  static setupExitHandlers(): void {
    // Register process exit handlers
    process.on('SIGTERM', async () => {
      console.log('Received SIGTERM, cleaning up...')
      await ProcessManager.cleanupAllInstances()
      process.exit(0)
    })

    process.on('SIGINT', async () => {
      console.log('Received SIGINT, cleaning up...')
      await ProcessManager.cleanupAllInstances()
      process.exit(0)
    })

    process.on('beforeExit', async () => {
      await ProcessManager.cleanupAllInstances()
    })

    // Handle uncaught exceptions
    process.on('uncaughtException', async (error) => {
      console.error('Uncaught exception:', error)
      await ProcessManager.cleanupAllInstances()
      process.exit(1)
    })

    process.on('unhandledRejection', async (reason, promise) => {
      console.error('Unhandled rejection at:', promise, 'reason:', reason)
      await ProcessManager.cleanupAllInstances()
      process.exit(1)
    })
  }
}

export type { ProcessInfo }
