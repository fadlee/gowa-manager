import { readFile } from 'node:fs/promises'
import { describe, expect, test } from 'bun:test'

const EXPECTED_RELEASE_ARTIFACTS = [
  'gowa-manager-linux-amd64',
  'gowa-manager-linux-arm64',
  'gowa-manager-macos-amd64',
  'gowa-manager-macos-arm64',
  'gowa-manager-windows-amd64.exe',
]

describe('Go release artifacts', () => {
  test('build, verify, and GitHub release config include every supported desktop platform', async () => {
    const [buildScript, verifierScript, workflow] = await Promise.all([
      readFile('scripts/release/build-go.ts', 'utf8'),
      readFile('scripts/release/verify-artifact.ts', 'utf8'),
      readFile('.github/workflows/release.yml', 'utf8'),
    ])

    for (const artifact of EXPECTED_RELEASE_ARTIFACTS) {
      expect(buildScript).toContain(artifact)
      expect(verifierScript).toContain(artifact)
      expect(workflow).toContain(`dist-go/${artifact}`)
      expect(workflow).toContain(`| \`${artifact}\``)
    }
  })
})
