import { readFile } from 'node:fs/promises'
import { describe, expect, test } from 'bun:test'

const DOCKERFILES = ['Dockerfile', 'Dockerfile.prebuilt']

describe('Docker runtime ownership repair', () => {
  test('runtime images start through root entrypoint that fixes legacy volume ownership', async () => {
    const entrypoint = await readFile('scripts/docker-entrypoint.sh', 'utf8')
    expect(entrypoint).toContain('chown -R app:app "$DATA_DIR"')
    expect(entrypoint).toContain('exec su-exec app "$@"')

    for (const dockerfilePath of DOCKERFILES) {
      const dockerfile = await readFile(dockerfilePath, 'utf8')
      expect(dockerfile).toContain('su-exec')
      expect(dockerfile).toContain('COPY scripts/docker-entrypoint.sh /usr/local/bin/gowa-manager-entrypoint')
      expect(dockerfile).toContain('ENTRYPOINT ["gowa-manager-entrypoint"]')
      expect(dockerfile).toContain('CMD ["./gowa-manager"]')
      expect(dockerfile).not.toContain('USER app')
    }
  })
})
