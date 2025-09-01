import { queries } from './db'
import type { Instance } from './types'

// Kill process using a specific port
async function killProcessUsingPort(port: number): Promise<void> {
  try {
    // Use lsof to find process using the port
    const { spawn } = require('child_process')

    return new Promise((resolve) => {
      const lsof = spawn('lsof', ['-ti', `:${port}`])
      let pids = ''

      lsof.stdout.on('data', (data: Buffer) => {
        pids += data.toString()
      })

      lsof.on('close', (code: number) => {
        if (code === 0 && pids.trim()) {
          // Kill all PIDs found
          const pidList = pids.trim().split('\n').filter(pid => pid.trim())
          console.log(`🔪 Killing processes using port ${port}: ${pidList.join(', ')}`)

          pidList.forEach((pid) => {
            try {
              process.kill(parseInt(pid.trim()), 'SIGKILL')
            } catch (error) {
              console.warn(`Failed to kill PID ${pid}:`, error)
            }
          })
        } else {
          console.log(`✅ No processes found using port ${port}`)
        }
        resolve()
      })

      lsof.on('error', (error: Error) => {
        console.warn(`lsof error for port ${port}:`, error)
        resolve()
      })
    })
  } catch (error) {
    console.warn(`Error checking port ${port}:`, error)
  }
}

// Auto-restart running instances on app startup
(async () => {
  try {
    console.log('🔄 Checking for running instances to auto-restart...')

    const allInstances = queries.getAllInstances.all() as Instance.Response[]
    const runningInstances = allInstances.filter((instance: Instance.Response) => instance.status === 'running')

    if (runningInstances.length === 0) {
      console.log('ℹ️ No running instances found to restart')
      return
    }

    console.log(`🚀 Auto-restarting ${runningInstances.length} instance(s)...`)

    // Import the InstanceService dynamically to avoid circular dependencies
    const { InstanceService } = await import('./modules/instances/service')

    // First, ensure any leftover processes are cleaned up
    await InstanceService.cleanupAllInstances()

    // Kill any processes using the ports we need
    console.log('🔍 Checking for processes using required ports...')
    for (const instance of runningInstances) {
      await killProcessUsingPort(instance.port)
    }

    // Wait for ports to be released
    console.log('⏳ Waiting for ports to be released...')
    await new Promise(resolve => setTimeout(resolve, 3000))


    for (const instance of runningInstances) {
      try {
        console.log(`🔄 Auto-restarting instance: ${instance.name} (${instance.key})`)

        // Try to start the instance directly - let it fail if port is truly occupied
        // The previous cleanup should have handled any real conflicts

        await InstanceService.startInstance(instance.id)
        console.log(`✅ Successfully restarted instance: ${instance.name}`)
      } catch (error) {
        console.error(`❌ Failed to auto-restart instance ${instance.name}:`, error)
        // Mark as error status if restart failed
        queries.updateInstanceStatus.run('error', instance.id)
      }
    }

    console.log('🎉 Auto-restart process completed')
  } catch (error) {
    console.error('❌ Error during auto-restart process:', error)
  }
})()
