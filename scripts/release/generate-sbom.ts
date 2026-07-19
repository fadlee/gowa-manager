/**
 * SBOM generator for the Go backend release.
 *
 * Produces a simple JSON SBOM (not CycloneDX/SPDX) covering:
 *   - Go module dependencies (parsed from go.mod)
 *   - Frontend npm dependencies (parsed from client/package.json)
 *
 * The SBOM never embeds build-host paths or credentials. It records only
 * component name, version, type, and (when declared) license.
 *
 * Output: dist-go/sbom.json
 */

import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { existsSync } from 'node:fs'
import packageJson from '../../package.json'
import clientPackageJson from '../../client/package.json'

const DIST_DIR = 'dist-go'
const GO_MOD = 'go.mod'
const SBOM_OUT = join(DIST_DIR, 'sbom.json')

interface Component {
  name: string
  version: string
  type: 'go-module' | 'npm-package'
  license?: string
  indirect?: boolean
}

interface Sbom {
  name: string
  version: string
  specVersion: string
  generatedAt: string
  components: Component[]
}

/** Parse go.mod require blocks into components. */
function parseGoMod(content: string): Component[] {
  const components: Component[] = []
  const requireRe = /^\s*(?:\/\/\s*)?(\S+)\s+(v\S+)\s+(\/\/\s*indirect)?$/m
  let inRequire = false
  for (const line of content.split('\n')) {
    const trimmed = line.trim()
    if (trimmed.startsWith('require ')) {
      inRequire = true
      // single-line require: require foo v1.0.0
      const single = trimmed.slice('require '.length)
      if (!single.startsWith('(')) {
        const m = single.match(/^(\S+)\s+(v\S+)/)
        if (m) components.push({ name: m[1], version: m[2], type: 'go-module' })
        inRequire = false
      }
      continue
    }
    if (inRequire) {
      if (trimmed === ')') { inRequire = false; continue }
      const m = trimmed.match(/^(\S+)\s+(v\S+)(?:\s+\/\/\s*indirect)?$/)
      if (m) {
        const indirect = /\/\/\s*indirect/.test(trimmed)
        components.push({ name: m[1], version: m[2], type: 'go-module', indirect })
      }
    }
  }
  return components
}

function npmComponents(deps: Record<string, string>, type: Component['type']): Component[] {
  return Object.entries(deps).map(([name, version]) => ({
    name,
    version,
    type,
  }))
}

async function main() {
  if (!existsSync(GO_MOD)) {
    console.error('go.mod not found; run from repo root.')
    process.exit(1)
  }
  const goMod = await readFile(GO_MOD, 'utf8')
  const goComponents = parseGoMod(goMod)

  const clientPkg = clientPackageJson as { dependencies?: Record<string, string>; devDependencies?: Record<string, string> }
  const npmDeps = npmComponents(clientPkg.dependencies ?? {}, 'npm-package')
  const npmDevDeps = npmComponents(clientPkg.devDependencies ?? {}, 'npm-package')

  const sbom: Sbom = {
    name: 'gowa-manager',
    version: packageJson.version || 'dev',
    specVersion: 'gowa-sbom-v1',
    // Use a date-only timestamp to avoid embedding build-host wall-clock
    // details; this is deterministic per release day.
    generatedAt: new Date().toISOString().slice(0, 10),
    components: [...goComponents, ...npmDeps, ...npmDevDeps],
  }

  if (!existsSync(DIST_DIR)) {
    await mkdir(DIST_DIR, { recursive: true })
  }
  await writeFile(SBOM_OUT, JSON.stringify(sbom, null, 2) + '\n')
  console.log(`✔ SBOM written to ${SBOM_OUT} (${sbom.components.length} components)`)
}

await main()
