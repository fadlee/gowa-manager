function parseVersionParts(version: string): [number, number, number] | null {
  const match = version.trim().replace(/^v/i, '').match(/^(\d+)(?:\.(\d+))?(?:\.(\d+))?/)
  if (!match) return null

  return [
    Number(match[1]),
    Number(match[2] ?? 0),
    Number(match[3] ?? 0),
  ]
}

export function hasNewerVersion(currentVersion: string | null | undefined, latestVersion: string | null | undefined): boolean {
  const current = currentVersion ? currentVersion.trim().replace(/^v/i, '') : ''
  const latest = latestVersion ? latestVersion.trim().replace(/^v/i, '') : ''

  if (!current || !latest || current === 'latest' || current === latest) return false

  const currentParts = parseVersionParts(current)
  const latestParts = parseVersionParts(latest)
  if (!currentParts || !latestParts) return false

  for (let index = 0; index < latestParts.length; index += 1) {
    if (latestParts[index] > currentParts[index]) return true
    if (latestParts[index] < currentParts[index]) return false
  }

  return false
}
