function parseVersionParts(version: string): [number, number, number] | null {
  const match = version.trim().replace(/^v/i, '').match(/^(\d+)(?:\.(\d+))?(?:\.(\d+))?/)
  if (!match) return null

  return [
    Number(match[1]),
    Number(match[2] ?? 0),
    Number(match[3] ?? 0),
  ]
}

// compareVersions compares two "vX.Y.Z" versions. It returns null when either
// one cannot be parsed (e.g. "latest").
export function compareVersions(a: string, b: string): number | null {
  const left = parseVersionParts(a)
  const right = parseVersionParts(b)
  if (!left || !right) return null
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) return left[index] > right[index] ? 1 : -1
  }
  return 0
}

export function hasNewerVersion(currentVersion: string | null | undefined, latestVersion: string | null | undefined): boolean {
  const current = currentVersion ? currentVersion.trim().replace(/^v/i, '') : ''
  const latest = latestVersion ? latestVersion.trim().replace(/^v/i, '') : ''

  if (!current || !latest || current === 'latest' || current === latest) return false

  return compareVersions(latest, current) === 1
}
