import { mkdir, exists, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { promisify } from 'node:util'

const execAsync = promisify(spawn)

// GitHub repository information
const REPO_URL = 'https://api.github.com/repos/aldinokemal/go-whatsapp-web-multidevice/releases/latest'
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

export async function downloadGowaBinary(): Promise<void> {
  try {
    console.log('🚀 Checking for GOWA binary auto-download...')
    
    const dataDir = join(process.cwd(), 'data')
    const binDir = join(dataDir, 'bin')
    const binaryPath = join(binDir, BINARY_NAME)
    
    // Create directories if they don't exist
    await mkdir(binDir, { recursive: true })
    
    // Check if binary already exists
    const binaryExists = await exists(binaryPath)
    if (binaryExists) {
      console.log(`✅ GOWA binary already exists: ${binaryPath}`)
      return
    }
    
    console.log('📡 Fetching latest release information...')
    
    // Get latest release info
    const response = await fetch(REPO_URL)
    if (!response.ok) {
      throw new Error(`Failed to fetch release info: ${response.statusText}`)
    }
    
    const release: GitHubRelease = await response.json()
    console.log(`🏷️  Latest version: ${release.tag_name}`)
    
    // Find the correct asset for current platform
    const platformName = getPlatformAssetName()
    const asset = release.assets.find(asset => 
      asset.name.includes(platformName) && asset.name.endsWith('.zip')
    )
    
    if (!asset) {
      console.warn(`⚠️  No binary found for platform: ${platformName}`)
      console.log('Available assets:', release.assets.map(a => a.name))
      return
    }
    
    console.log(`📦 Found asset: ${asset.name}`)
    
    // Download the zip file
    const tempDir = join(dataDir, 'temp')
    const zipPath = join(tempDir, asset.name)
    const extractDir = join(tempDir, 'extract')
    
    await mkdir(tempDir, { recursive: true })
    
    try {
      await downloadFile(asset.browser_download_url, zipPath)
      await extractBinary(zipPath, extractDir, binaryPath)
      
      console.log(`🎉 GOWA binary successfully installed: ${binaryPath}`)
    } finally {
      // Cleanup temp files
      try {
        await rm(tempDir, { recursive: true, force: true })
      } catch (cleanupError) {
        console.warn('⚠️  Failed to cleanup temp files:', cleanupError)
      }
    }
    
  } catch (error) {
    console.error('❌ Failed to download GOWA binary:', error)
    console.log('ℹ️  You can manually download and place the binary at data/bin/gowa')
  }
}
