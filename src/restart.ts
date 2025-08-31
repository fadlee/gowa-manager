import { queries } from './db'
import type { Instance } from './types'

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

    for (const instance of runningInstances) {
      try {
        console.log(`🔄 Auto-restarting instance: ${instance.name} (${instance.key})`)
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
