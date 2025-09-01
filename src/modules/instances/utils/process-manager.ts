// Process management utilities
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
          try {
            console.log(`Killing instance ${instanceId} (PID: ${processInfo.pid})`)

            // Force kill immediately - no graceful shutdown needed for crash/restart scenarios
            processInfo.process.kill('SIGKILL')
            console.log(`Force killed instance ${instanceId}`)
            resolve()

          } catch (error) {
            console.warn(`Failed to kill instance ${instanceId}:`, error)
            resolve()
          }
        })
      )
    }

    await Promise.all(cleanupPromises)
    runningProcesses.clear()
    console.log('All instances cleaned up')
  }

  // Stop a specific process gracefully
  static stopProcess(instanceId: number): boolean {
    const processInfo = runningProcesses.get(instanceId)
    if (processInfo) {
      try {
        // Graceful shutdown with SIGTERM
        processInfo.process.kill('SIGTERM')
        runningProcesses.delete(instanceId)
        return true
      } catch (error) {
        console.error(`Failed to stop process ${processInfo.pid}:`, error)
        return false
      }
    }
    return false
  }

  // Kill a specific process forcefully
  static killProcess(instanceId: number): boolean {
    const processInfo = runningProcesses.get(instanceId)
    if (processInfo) {
      try {
        // Forceful kill with SIGKILL
        processInfo.process.kill('SIGKILL')
        runningProcesses.delete(instanceId)
        console.log(`Forcefully killed instance ${instanceId} with PID ${processInfo.pid}`)
        return true
      } catch (error) {
        console.error(`Failed to kill process ${processInfo.pid}:`, error)
        // If process doesn't exist anymore, that's fine
        if (error instanceof Error && (error as any).code !== 'ESRCH') {
          throw error
        }
        return false
      }
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
