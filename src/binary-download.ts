import { mkdir, exists, rm, readdir } from 'node:fs/promises'
import { join, resolve } from 'node:path'
import { spawn } from 'node:child_process'
import { promisify } from 'node:util'

const execAsync = promisify(spawn)

// GitHub repository information
const REPO_BASE_URL = 'https://api.github.com/repos/aldinokemal/go-whatsapp-web-multidevice/releases'
const REPO_LATEST_URL = `${REPO_BASE_URL}/latest`
const BINARY_NAME = process.platform === 'win32' ? 'gowa.exe' : 'gowa'

interface GitHubRelease {
  tag_name: string
  assets: {
    name: string
    browser_download_url: string
  }[]
}

function getPlatformAssetName(): string {
  const platform = process.platform
  const arch = process.arch

  // Map Node.js platform/arch to release asset naming convention
  let osName: string
  let archName: string

  switch (platform) {
    case 'darwin':
      osName = 'darwin'
      break
    case 'linux':
      osName = 'linux'
      break
    case 'win32':
      osName = 'windows'
      break
    default:
      throw new Error(`Unsupported platform: ${platform}`)
  }

  switch (arch) {
    case 'x64':
      archName = 'amd64'
      break
    case 'arm64':
      archName = 'arm64'
      break
    case 'arm':
      archName = 'arm'
      break
    default:
      throw new Error(`Unsupported architecture: ${arch}`)
  }

  return `${osName}_${archName}`
}

async function downloadFile(url: string, outputPath: string): Promise<void> {
  console.log(`📥 Downloading: ${url}`)

  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`Failed to download: ${response.statusText}`)
  }

  const arrayBuffer = await response.arrayBuffer()
  await Bun.write(outputPath, arrayBuffer)
  console.log(`✅ Downloaded to: ${outputPath}`)
}

async function extractBinary(zipPath: string, extractDir: string, targetBinaryPath: string): Promise<void> {
  console.log(`📦 Extracting: ${zipPath}`)

  // Create extraction directory
  await mkdir(extractDir, { recursive: true })

  // Extract zip file using unzip command
  return new Promise((resolve, reject) => {
    const unzipProcess = spawn('unzip', ['-o', zipPath, '-d', extractDir], {
      stdio: 'pipe'
    })

    unzipProcess.on('close', async (code) => {
      if (code !== 0) {
        reject(new Error(`Unzip failed with code: ${code}`))
        return
      }

      try {
        // Find the main binary file (should be the largest file or follow naming pattern)
        const fs = require('node:fs')
        const files = fs.readdirSync(extractDir)

        // Look for files that might be the binary (exclude readme, etc.)
        const binaryFile = files.find((file: string) =>
          file.toLowerCase() !== 'readme.md' &&
          file.toLowerCase() !== 'license' &&
          !file.includes('.')
        ) || files.find((file: string) =>
          file.includes('whatsapp') ||
          file.includes('main') ||
          file.includes('app')
        )

        if (!binaryFile) {
          throw new Error('Could not find binary file in extracted archive')
        }

        const sourceBinaryPath = join(extractDir, binaryFile)

        // Copy and rename the binary to target location
        await Bun.write(targetBinaryPath, Bun.file(sourceBinaryPath))

        // Make binary executable on Unix systems
        if (process.platform !== 'win32') {
          const chmodProcess = spawn('chmod', ['+x', targetBinaryPath])
          await new Promise((resolve) => chmodProcess.on('close', resolve))
        }

        console.log(`✅ Binary extracted and installed: ${targetBinaryPath}`)
        resolve()
      } catch (error) {
        reject(error)
      }
    })

    unzipProcess.on('error', reject)
  })
}

// Download a specific version to the versions directory
export async function downloadSpecificVersion(version: string): Promise<void> {
  try {
    console.log(`🚀 Downloading GOWA version ${version}...`)

    // Use custom data directory if specified
    const dataDir = process.env.DATA_DIR || join(process.cwd(), 'data')
    const absoluteDataDir = resolve(dataDir)
    const versionsDir = join(absoluteDataDir, 'bin', 'versions')
    
    // Get release info first to get the actual version tag
    console.log(`📡 Fetching release information for ${version}...`)
    const releaseUrl = version === 'latest' ? REPO_LATEST_URL : `${REPO_BASE_URL}/tags/${version}`
    const response = await fetch(releaseUrl)
    if (!response.ok) {
      throw new Error(`Failed to fetch release info for ${version}: ${response.statusText}`)
    }

    const release = await response.json() as GitHubRelease
    const actualVersion = release.tag_name
    console.log(`🏷️  Actual version: ${actualVersion}`)
    
    // Use the actual version for directory naming
    const versionDir = join(versionsDir, actualVersion)
    const binaryPath = join(versionDir, BINARY_NAME)

    // Create directories if they don't exist
    await mkdir(versionDir, { recursive: true })

    // Check if binary already exists
    const binaryExists = await exists(binaryPath)
    if (binaryExists) {
      console.log(`✅ GOWA version ${actualVersion} already exists: ${binaryPath}`)
      return
    }

    // Find the correct asset for current platform
    const platformName = getPlatformAssetName()
    const asset = release.assets.find(asset =>
      asset.name.includes(platformName) && asset.name.endsWith('.zip')
    )

    if (!asset) {
      console.warn(`⚠️  No binary found for platform: ${platformName}`)
      console.log('Available assets:', release.assets.map(a => a.name))
      throw new Error(`No compatible binary found for version ${version} on platform ${platformName}`)
    }

    console.log(`📦 Found asset: ${asset.name}`)

    // Download the zip file
    const tempDir = join(absoluteDataDir, 'temp', `${actualVersion}-${Date.now()}`)
    const zipPath = join(tempDir, asset.name)
    const extractDir = join(tempDir, 'extract')

    await mkdir(tempDir, { recursive: true })

    try {
      await downloadFile(asset.browser_download_url, zipPath)
      await extractBinary(zipPath, extractDir, binaryPath)

      console.log(`🎉 GOWA version ${actualVersion} successfully installed: ${binaryPath}`)
    } finally {
      // Cleanup temp files
      try {
        await rm(tempDir, { recursive: true, force: true })
      } catch (cleanupError) {
        console.warn('⚠️  Failed to cleanup temp files:', cleanupError)
      }
    }

  } catch (error) {
    console.error(`❌ Failed to download GOWA version ${actualVersion}:`, error)
    throw error
  }
}

export async function downloadGowaBinary(): Promise<void> {
  try {
    console.log('🚀 Checking for GOWA binary auto-download...')

    // Use custom data directory if specified
    const dataDir = process.env.DATA_DIR || join(process.cwd(), 'data')
    // Resolve relative paths to absolute paths
    const absoluteDataDir = resolve(dataDir)
    const binDir = join(absoluteDataDir, 'bin')
    const binaryPath = join(binDir, BINARY_NAME)

    // Create directories if they don't exist
    await mkdir(binDir, { recursive: true })

    // Check if binary already exists (backward compatibility)
    const binaryExists = await exists(binaryPath)
    if (binaryExists) {
      console.log(`✅ GOWA binary already exists: ${binaryPath}`)
      return
    }

    // Try to download latest version to the new versioned structure
    try {
      await downloadSpecificVersion('latest')
      
      // Create backward compatibility symlink
      const latestVersion = await getLatestInstalledVersion()
      if (latestVersion) {
        const latestBinaryPath = join(absoluteDataDir, 'bin', 'versions', latestVersion, BINARY_NAME)
        try {
          // Create symlink for backward compatibility
          const fs = require('node:fs')
          if (fs.existsSync(latestBinaryPath)) {
            fs.symlinkSync(latestBinaryPath, binaryPath)
            console.log(`🔗 Created compatibility symlink: ${binaryPath} -> ${latestBinaryPath}`)
          }
        } catch (linkError) {
          console.warn('⚠️  Failed to create compatibility symlink:', linkError)
          // Copy file instead of symlink as fallback
          await Bun.write(binaryPath, Bun.file(latestBinaryPath))
        }
      }
    } catch (error) {
      console.error('❌ Failed to download latest GOWA version:', error)
      console.log('ℹ️  You can manually download and place the binary at data/bin/gowa')
    }

  } catch (error) {
    console.error('❌ Failed to download GOWA binary:', error)
    console.log('ℹ️  You can manually download and place the binary at data/bin/gowa')
  }
}

// Helper function to get the latest installed version
async function getLatestInstalledVersion(): Promise<string | null> {
  try {
    const dataDir = process.env.DATA_DIR || join(process.cwd(), 'data')
    const absoluteDataDir = resolve(dataDir)
    const versionsDir = join(absoluteDataDir, 'bin', 'versions')
    
    if (!(await exists(versionsDir))) return null
    
    const entries = await readdir(versionsDir, { withFileTypes: true })
    const versions = entries
      .filter(entry => entry.isDirectory() && entry.name !== 'latest')
      .map(entry => entry.name)
      .sort((a, b) => b.localeCompare(a)) // Simple version sort
    
    return versions.length > 0 ? versions[0] : null
  } catch {
    return null
  }
}
