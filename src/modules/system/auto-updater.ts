import { VersionManager } from './version-manager'
import { InstanceService } from '../instances/service'
import { queries } from '../../db'

export interface AutoUpdateStatus {
  lastCheck: Date | null
  lastUpdate: Date | null
  latestVersion: string | null
  isChecking: boolean
  nextCheck: Date | null
}

export class AutoUpdater {
  private static checkInterval: Timer | null = null
  private static status: AutoUpdateStatus = {
    lastCheck: null,
    lastUpdate: null,
    latestVersion: null,
    isChecking: false,
    nextCheck: null
  }
  private static intervalMs: number = 60 * 60 * 1000 // 1 hour default

  // Start periodic update check
  static start(intervalMs: number = 60 * 60 * 1000): void {
    this.intervalMs = intervalMs
    
    if (this.checkInterval) {
      clearInterval(this.checkInterval)
    }

    console.log(`✅ Starting auto updater with ${intervalMs / 1000 / 60} minute interval...`)
    
    // Run first check after 1 minute to let server stabilize
    setTimeout(() => {
      this.checkAndUpdate()
    }, 60 * 1000)

    // Then run periodically
    this.checkInterval = setInterval(() => {
      this.checkAndUpdate()
    }, intervalMs)

    this.updateNextCheck()
  }

  // Stop periodic checks
  static stop(): void {
    if (this.checkInterval) {
      clearInterval(this.checkInterval)
      this.checkInterval = null
      this.status.nextCheck = null
      console.log('[AutoUpdater] Stopped')
    }
  }

  // Get current status
  static getStatus(): AutoUpdateStatus {
    return { ...this.status }
  }

  private static updateNextCheck(): void {
    if (this.checkInterval) {
      this.status.nextCheck = new Date(Date.now() + this.intervalMs)
    }
  }

  // Check for updates and apply if available
  static async checkAndUpdate(): Promise<{ updated: boolean; version?: string; restartedInstances?: number }> {
    if (this.status.isChecking) {
      console.log('[AutoUpdater] Check already in progress, skipping')
      return { updated: false }
    }

    this.status.isChecking = true
    this.status.lastCheck = new Date()

    try {
      console.log('[AutoUpdater] Checking for updates...')

      // Get latest version from GitHub
      const availableVersions = await VersionManager.getAvailableVersions(1)
      if (availableVersions.length === 0) {
        console.log('[AutoUpdater] No versions available from GitHub')
        return { updated: false }
      }

      // Find the actual latest version (not the 'latest' alias)
      const latestRelease = availableVersions.find(v => v.version !== 'latest' && v.isLatest)
      if (!latestRelease) {
        console.log('[AutoUpdater] Could not determine latest version')
        return { updated: false }
      }

      this.status.latestVersion = latestRelease.version
      console.log(`[AutoUpdater] Latest version: ${latestRelease.version}`)

      // Check if already installed
      if (latestRelease.installed) {
        console.log(`[AutoUpdater] Version ${latestRelease.version} already installed`)
        return { updated: false }
      }

      // Download new version
      console.log(`[AutoUpdater] Downloading version ${latestRelease.version}...`)
      await VersionManager.installVersion(latestRelease.version)
      console.log(`[AutoUpdater] Version ${latestRelease.version} installed successfully`)

      this.status.lastUpdate = new Date()

      // Find and restart instances using 'latest'
      const restartedCount = await this.restartLatestInstances()

      console.log(`[AutoUpdater] Update complete. Restarted ${restartedCount} instances.`)
      
      return { 
        updated: true, 
        version: latestRelease.version,
        restartedInstances: restartedCount
      }
    } catch (error) {
      console.error('[AutoUpdater] Error during update check:', error)
      return { updated: false }
    } finally {
      this.status.isChecking = false
      this.updateNextCheck()
    }
  }

  // Get all instances using 'latest' version
  static getLatestInstances(): { id: number; name: string; status: string }[] {
    const allInstances = queries.getAllInstances.all() as any[]
    return allInstances
      .filter(inst => inst.gowa_version === 'latest' || !inst.gowa_version)
      .map(inst => ({
        id: inst.id,
        name: inst.name,
        status: inst.status
      }))
  }

  // Restart all running instances that use 'latest' version
  private static async restartLatestInstances(): Promise<number> {
    const latestInstances = this.getLatestInstances()
    const runningInstances = latestInstances.filter(inst => inst.status === 'running')

    if (runningInstances.length === 0) {
      console.log('[AutoUpdater] No running instances using "latest" version')
      return 0
    }

    console.log(`[AutoUpdater] Restarting ${runningInstances.length} instances using "latest" version...`)

    let restartedCount = 0
    for (const instance of runningInstances) {
      try {
        console.log(`[AutoUpdater] Restarting instance ${instance.id} (${instance.name})...`)
        await InstanceService.restartInstance(instance.id)
        restartedCount++
        console.log(`[AutoUpdater] Instance ${instance.id} restarted successfully`)
      } catch (error) {
        console.error(`[AutoUpdater] Failed to restart instance ${instance.id}:`, error)
      }
    }

    return restartedCount
  }
}
