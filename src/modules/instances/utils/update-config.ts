import { Proxy } from '../../../types'

export function normalizeUpdateConfig(existingConfig: string, nextConfig: string | undefined, instanceKey: string): string {
  const sourceConfig = nextConfig ?? existingConfig
  let parsedConfig: any

  try {
    parsedConfig = JSON.parse(sourceConfig || '{}')
  } catch {
    parsedConfig = JSON.parse(existingConfig || '{}')
  }

  if (!parsedConfig || typeof parsedConfig !== 'object' || Array.isArray(parsedConfig)) {
    parsedConfig = {}
  }

  if (!parsedConfig.flags || typeof parsedConfig.flags !== 'object' || Array.isArray(parsedConfig.flags)) {
    parsedConfig.flags = {}
  }

  parsedConfig.flags.basePath = `/${Proxy.PREFIX}/${instanceKey}`

  return JSON.stringify(parsedConfig)
}
