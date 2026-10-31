import type { BasicAuthPair, InstanceConfig } from '../types'

// buildCreateInstanceConfig returns the config sent when creating an
// instance from the New Instance dialog, optionally with one Basic Auth pair.
export function buildCreateInstanceConfig(basicAuth?: BasicAuthPair | null): InstanceConfig {
  return {
    args: ['rest', '--port=PORT'],
    flags: {
      accountValidation: true,
      os: 'GowaManager',
      ...(basicAuth ? { basicAuth: [basicAuth] } : {}),
    },
  }
}
