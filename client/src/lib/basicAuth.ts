import type { CliFlags } from '../types'
import { compareVersions } from './version'

// GOWA v9.8.0 refuses to start when it listens on a non-loopback address
// without Basic Auth, and the manager runs instances on 0.0.0.0.
export const BASIC_AUTH_REQUIRED_SINCE = 'v9.8.0'

// resolveGowaVersion maps "latest" to the newest installed version, the same
// binary the manager starts. Returns null when "latest" is not installed yet.
export function resolveGowaVersion(version: string | null | undefined, latestInstalled: string | null | undefined): string | null {
  const selected = version?.trim() || 'latest'
  if (selected !== 'latest') return selected
  return latestInstalled?.trim() || null
}

// requiresBasicAuth reports whether the GOWA version needs Basic Auth to
// start. An unresolved "latest" counts as required: current releases do.
export function requiresBasicAuth(version: string | null | undefined, latestInstalled?: string | null): boolean {
  const resolved = resolveGowaVersion(version, latestInstalled)
  if (!resolved) return true
  const comparison = compareVersions(resolved, BASIC_AUTH_REQUIRED_SINCE)
  return comparison !== null && comparison >= 0
}

// hasBasicAuth reports whether the flags contain a usable Basic Auth pair.
// GOWA rejects entries with an empty username or password.
export function hasBasicAuth(flags: CliFlags | null | undefined): boolean {
  return (flags?.basicAuth ?? []).some((pair) => pair.username.trim() !== '' && pair.password.trim() !== '')
}

// flagsFromConfig reads the CLI flags from an instance's JSON config string.
export function flagsFromConfig(config: string | null | undefined): CliFlags | null {
  try {
    const parsed = JSON.parse(config || '{}')
    return parsed && typeof parsed === 'object' ? (parsed.flags ?? null) : null
  } catch {
    return null
  }
}
