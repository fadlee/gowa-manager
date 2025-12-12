import { writeFile } from 'fs/promises'
import { join } from 'path'
import { execSync } from 'child_process'

async function generateVersionFile() {
  try {
    // Get the current git describe output (e.g., "v1.5.0" or "v1.5.0-7-g85614e6")
    const gitDescribe = execSync('git describe --tags --always', {
      encoding: 'utf-8',
      cwd: process.cwd()
    }).trim()

    // Extract version and strip 'v' prefix
    const version = gitDescribe.replace(/^v/, '').split('-')[0]

    const versionContent = `// Auto-generated from git tags. Do not edit manually.\nexport const MANAGER_VERSION = '${version}'\n`

    const versionFilePath = join(process.cwd(), 'src', 'version.ts')
    await writeFile(versionFilePath, versionContent)

    console.log(`✓ Generated version.ts with MANAGER_VERSION = '${version}' (from: ${gitDescribe})`)
  } catch (error) {
    console.error('Error generating version file:', error)
    process.exit(1)
  }
}

generateVersionFile()
