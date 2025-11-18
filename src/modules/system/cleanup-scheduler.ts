import { existsSync, readdirSync, rmSync, statSync } from 'node:fs'
import { join, resolve } from 'node:path'
import cron from 'node-cron'
import { getConfig } from '../../cli'
import { queries } from '../../db'
import type { Instance } from '../../types'

export class CleanupScheduler {
  private static task: cron.ScheduledTask | null = null
  private static isRunning = false

  static start(): void {
    if (this.isRunning) {
      console.warn('⚠️ Cleanup scheduler is already running')
      return
    }

    console.log('🧹 Starting daily cleanup scheduler (midnight daily)')

    // Schedule cleanup every day at midnight (00:00)
    this.task = cron.schedule('0 0 * * *', () => {
      this.runCleanup()
    })

    this.isRunning = true
    console.log('✅ Cleanup scheduler started')
  }

  static stop(): void {
    if (this.task) {
      this.task.stop()
      this.isRunning = false
      console.log('⏹️ Cleanup scheduler stopped')
    }
  }

  private static getDataDirectory(): string {
    const dataDir = process.env.DATA_DIR || join(process.cwd(), 'data')
    return resolve(dataDir)
  }

  private static async runCleanup(): Promise<void> {
    const startTime = Date.now()
    console.log(`🧹 Running cleanup task at ${new Date().toISOString()}`)

    try {
      const allInstances = queries.getAllInstances.all() as Instance.Response[]
      let totalFilesDeleted = 0
      let errorCount = 0

      for (const instance of allInstances) {
        try {
          const jpegCount = this.cleanupInstanceStorageJpegs(instance.id)
          const mediaCount = this.cleanupInstanceMediaFiles(instance.id)
          const instanceTotal = jpegCount + mediaCount

          if (instanceTotal > 0) {
            console.log(
              `  📦 Instance ${instance.name} (ID: ${instance.id}): ` +
              `${jpegCount} JPEG(s) + ${mediaCount} media file(s) deleted`
            )
          }

          totalFilesDeleted += instanceTotal
        } catch (error) {
          errorCount++
          console.error(`  ❌ Error cleaning instance ${instance.name} (ID: ${instance.id}):`, error)
        }
      }

      const duration = Date.now() - startTime
      console.log(`✅ Cleanup completed in ${duration}ms: ${totalFilesDeleted} file(s) deleted${errorCount > 0 ? `, ${errorCount} error(s)` : ''}`)
    } catch (error) {
      console.error('❌ Cleanup task failed:', error)
    }
  }

  private static cleanupInstanceStorageJpegs(instanceId: number): number {
    const dataDir = this.getDataDirectory()
    const storageDir = join(dataDir, 'instances', instanceId.toString(), 'storages')

    if (!existsSync(storageDir)) {
      return 0
    }

    let deletedCount = 0

    try {
      const entries = readdirSync(storageDir)

      for (const entry of entries) {
        if (entry.toLowerCase().endsWith('.jpeg') || entry.toLowerCase().endsWith('.jpg')) {
          const filePath = join(storageDir, entry)
          try {
            if (statSync(filePath).isFile()) {
              rmSync(filePath, { force: true })
              deletedCount++
            }
          } catch (error) {
            console.warn(`    ⚠️ Failed to delete JPEG: ${filePath}`, error)
          }
        }
      }
    } catch (error) {
      console.warn(`    ⚠️ Failed to read storage directory: ${storageDir}`, error)
    }

    return deletedCount
  }

  private static cleanupInstanceMediaFiles(instanceId: number): number {
    const dataDir = this.getDataDirectory()
    const mediaDir = join(dataDir, 'instances', instanceId.toString(), 'statics', 'media')

    if (!existsSync(mediaDir)) {
      return 0
    }

    let deletedCount = 0

    try {
      const entries = readdirSync(mediaDir)

      for (const entry of entries) {
        const filePath = join(mediaDir, entry)
        try {
          const stat = statSync(filePath)
          if (stat.isFile()) {
            rmSync(filePath, { force: true })
            deletedCount++
          } else if (stat.isDirectory()) {
            // Recursively delete subdirectories
            rmSync(filePath, { recursive: true, force: true })
            deletedCount++ // Count as one deletion unit
          }
        } catch (error) {
          console.warn(`    ⚠️ Failed to delete media item: ${filePath}`, error)
        }
      }
    } catch (error) {
      console.warn(`    ⚠️ Failed to read media directory: ${mediaDir}`, error)
    }

    return deletedCount
  }
}
