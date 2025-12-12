import { writeFile, access } from 'fs/promises'
import { join } from 'path'
import { execSync } from 'child_process'

const versionFilePath = join(process.cwd(), 'src', 'version.ts')

async function fileExists(path: string): Promise<boolean> {
  try {
    await access(path)
    return true
  } catch {
    return false
  }
}

async function generateVersionFile() {
  let version: string

  try {
    // Get the current git describe output (e.g., "v1.5.0" or "v1.5.0-7-g85614e6")
    const gitDescribe = execSync('git describe --tags --always', {
      encoding: 'utf-8',
      cwd: process.cwd()
    }).trim()

    // Extract version and strip 'v' prefix
    version = gitDescribe.replace(/^v/, '').split('-')[0]
    console.log(`✓ Generated version.ts with MANAGER_VERSION = '${version}' (from: ${gitDescribe})`)
  } catch (error) {
    // Git not available - check if version.ts already exists (pre-generated)
    if (await fileExists(versionFilePath)) {
      console.log('✓ Using existing version.ts (git not available)')
      return
    }

    // Fallback to VERSION env var or default
    version = process.env.VERSION?.replace(/^v/, '') || '0.0.0'
    console.log(`✓ Generated version.ts with MANAGER_VERSION = '${version}' (fallback)`)
  }

  const versionContent = `// Auto-generated from git tags. Do not edit manually.\nexport const MANAGER_VERSION = '${version}'\n`
  await writeFile(versionFilePath, versionContent)
}

generateVersionFile()
