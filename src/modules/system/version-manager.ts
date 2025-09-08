import { join, resolve } from 'node:path'
import { mkdir, exists, readdir, rm, stat } from 'node:fs/promises'
import { getConfig } from '../../cli'

export interface VersionInfo {
  version: string
  path: string
  installed: boolean
  isLatest: boolean
  size?: number
  installedAt?: Date
}

export interface GitHubRelease {
  tag_name: string
  published_at: string
  assets: {
    name: string
    browser_download_url: string
    size: number
  }[]
}

export class VersionManager {
  private static getDataDir(): string {
    const config = getConfig()
    return resolve(config.dataDir)
  }

  private static getVersionsDir(): string {
    return join(this.getDataDir(), 'bin', 'versions')
  }

  private static getBinaryName(): string {
    return process.platform === 'win32' ? 'gowa.exe' : 'gowa'
  }

  // Get path to a specific version's binary
  static getVersionBinaryPath(version: string): string {
    if (version === 'latest') {
      // For 'latest', resolve to the actual latest version
      const latestVersion = this.resolveLatestVersion()
      if (latestVersion) {
        version = latestVersion
      } else {
        // Fallback to old path if no versions installed
        return join(this.getDataDir(), 'bin', this.getBinaryName())
      }
    }
    
    return join(this.getVersionsDir(), version, this.getBinaryName())
  }

  // Resolve 'latest' to actual version string
  private static resolveLatestVersion(): string | null {
    try {
      const versions = this.getInstalledVersionsSync()
      if (versions.length === 0) return null
      
      // Sort versions by semver (simple string sort should work for most cases)
      versions.sort((a, b) => b.localeCompare(a))
      return versions[0]
    } catch {
      return null
    }
  }

  private static getInstalledVersionsSync(): string[] {
    try {
      const fs = require('node:fs')
      const versionsDir = this.getVersionsDir()
      if (!fs.existsSync(versionsDir)) return []
      
      return fs.readdirSync(versionsDir, { withFileTypes: true })
        .filter((dirent: any) => dirent.isDirectory())
        .map((dirent: any) => dirent.name)
        .filter((name: string) => name !== 'latest') // Exclude symlink
    } catch {
      return []
    }
  }

  // List all locally installed versions
  static async getInstalledVersions(): Promise<VersionInfo[]> {
    try {
      const versionsDir = this.getVersionsDir()
      if (!(await exists(versionsDir))) {
        return []
      }

      const entries = await readdir(versionsDir, { withFileTypes: true })
      const versions: VersionInfo[] = []
      const latestVersion = this.resolveLatestVersion()

      for (const entry of entries) {
        if (entry.isDirectory() && entry.name !== 'latest') {
          const versionPath = join(versionsDir, entry.name)
          const binaryPath = join(versionPath, this.getBinaryName())
          
          let size: number | undefined
          let installedAt: Date | undefined
          
          try {
            const binaryStats = await stat(binaryPath)
            size = binaryStats.size
            installedAt = binaryStats.birthtime
          } catch {
            // Binary doesn't exist, skip this version
            continue
          }

          versions.push({
            version: entry.name,
            path: binaryPath,
            installed: true,
            isLatest: entry.name === latestVersion,
            size,
            installedAt
          })
        }
      }

      // Sort by version (descending)
      versions.sort((a, b) => b.version.localeCompare(a.version))
      return versions
    } catch (error) {
      console.error('Failed to get installed versions:', error)
      return []
    }
  }

  // Get available versions from GitHub (latest releases)
  static async getAvailableVersions(limit: number = 10): Promise<VersionInfo[]> {
    try {
      const response = await fetch(
        `https://api.github.com/repos/aldinokemal/go-whatsapp-web-multidevice/releases?per_page=${limit}`
      )
      
      if (!response.ok) {
        throw new Error(`GitHub API error: ${response.statusText}`)
      }

      const releases = await response.json() as GitHubRelease[]
      const installedVersions = await this.getInstalledVersions()
      const installedVersionMap = new Map(installedVersions.map(v => [v.version, v]))

      const result: VersionInfo[] = []
      
      // Add 'latest' as the first option if we have releases
      if (releases.length > 0) {
        const latestRelease = releases[0]
        const latestInstalled = installedVersionMap.has(latestRelease.tag_name)
        const latestInfo = installedVersionMap.get(latestRelease.tag_name)
        
        result.push({
          version: 'latest',
          path: this.getVersionBinaryPath('latest'),
          installed: latestInstalled,
          isLatest: true,
          size: latestInfo?.size,
          installedAt: latestInfo?.installedAt
        })
      }
      
      // Add specific versions
      releases.forEach((release, index) => {
        const installed = installedVersionMap.has(release.tag_name)
        const installedInfo = installedVersionMap.get(release.tag_name)

        result.push({
          version: release.tag_name,
          path: this.getVersionBinaryPath(release.tag_name),
          installed,
          isLatest: index === 0, // First release is latest
          size: installedInfo?.size,
          installedAt: installedInfo?.installedAt
        })
      })
      
      return result
    } catch (error) {
      console.error('Failed to fetch available versions:', error)
      return []
    }
  }

  // Install a specific version
  static async installVersion(version: string): Promise<void> {
    // We'll use the enhanced binary-download functionality
    const { downloadSpecificVersion } = await import('../../binary-download')
    await downloadSpecificVersion(version)
  }

  // Remove a specific version
  static async removeVersion(version: string): Promise<void> {
    if (version === 'latest') {
      throw new Error('Cannot remove the latest version alias')
    }

    const versionDir = join(this.getVersionsDir(), version)
    if (await exists(versionDir)) {
      await rm(versionDir, { recursive: true, force: true })
      console.log(`Removed version ${version}`)
    }
  }

  // Check if a version is installed and binary exists
  static async isVersionAvailable(version: string): Promise<boolean> {
    if (version === 'latest') {
      // For 'latest', check if any version that could be considered 'latest' is installed
      try {
        const availableVersions = await this.getAvailableVersions(1)
        if (availableVersions.length === 0) return false
        
        // Check if the actual latest version is installed
        const latestActualVersion = availableVersions.find(v => v.isLatest && v.version !== 'latest')
        if (latestActualVersion) {
          const latestBinaryPath = this.getVersionBinaryPath(latestActualVersion.version)
          return await exists(latestBinaryPath)
        }
      } catch {
        // Fallback to old behavior if API fails
      }
    }
    
    const binaryPath = this.getVersionBinaryPath(version)
    return await exists(binaryPath)
  }

  // Get disk usage for all versions
  static async getVersionsSize(): Promise<{ [version: string]: number }> {
    const versions = await this.getInstalledVersions()
    const sizes: { [version: string]: number } = {}
    
    for (const version of versions) {
      if (version.size) {
        sizes[version.version] = version.size
      }
    }
    
    return sizes
  }

  // Cleanup old versions (keep only the latest N versions)
  static async cleanup(keepCount: number = 3): Promise<string[]> {
    const versions = await this.getInstalledVersions()
    if (versions.length <= keepCount) {
      return []
    }

    // Sort by installation date, keep the newest ones
    versions.sort((a, b) => {
      if (!a.installedAt || !b.installedAt) return 0
      return b.installedAt.getTime() - a.installedAt.getTime()
    })

    const toRemove = versions.slice(keepCount)
    const removed: string[] = []

    for (const version of toRemove) {
      try {
        await this.removeVersion(version.version)
        removed.push(version.version)
      } catch (error) {
        console.warn(`Failed to remove version ${version.version}:`, error)
      }
    }

    return removed
  }
}
