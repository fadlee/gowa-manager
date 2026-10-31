import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ShieldAlert } from 'lucide-react'
import { apiClient } from '../lib/api'
import { BASIC_AUTH_REQUIRED_SINCE, flagsFromConfig, hasBasicAuth, requiresBasicAuth, resolveGowaVersion } from '../lib/basicAuth'
import { cn } from '../lib/utils'
import type { CliFlags, Instance } from '../types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'

// useBasicAuthRequirement reports whether a GOWA version needs Basic Auth to
// start, resolving "latest" to the newest installed version like the manager.
export function useBasicAuthRequirement(version: string | null | undefined) {
  const { data: installed = [] } = useQuery({
    queryKey: ['versions', 'installed'],
    queryFn: () => apiClient.getInstalledVersions(),
    staleTime: 60 * 1000,
  })
  const latestInstalled = installed.find((info) => info.isLatest)?.version ?? null
  return {
    required: requiresBasicAuth(version, latestInstalled),
    resolvedVersion: resolveGowaVersion(version, latestInstalled),
  }
}

// useMissingBasicAuth is true when the instance's saved config lacks the
// Basic Auth its GOWA version needs, so the instance cannot start.
export function useMissingBasicAuth(instance: Pick<Instance, 'gowa_version' | 'config'>): boolean {
  const { required } = useBasicAuthRequirement(instance.gowa_version)
  return required && !hasBasicAuth(flagsFromConfig(instance.config))
}

function versionLabel(resolvedVersion: string | null): string {
  return resolvedVersion ? `GOWA ${resolvedVersion}` : `GOWA ${BASIC_AUTH_REQUIRED_SINCE} and newer`
}

interface BasicAuthWarningProps {
  version: string | null | undefined
  flags?: CliFlags | null
  // When set, the warning only shows while flags have no Basic Auth pair.
  checkFlags?: boolean
  children?: ReactNode
  className?: string
}

// BasicAuthWarning explains that the selected GOWA version will not start
// without Basic Auth. children add context-specific guidance.
export function BasicAuthWarning({ version, flags, checkFlags = false, children, className }: BasicAuthWarningProps) {
  const { required, resolvedVersion } = useBasicAuthRequirement(version)
  if (!required || (checkFlags && hasBasicAuth(flags))) return null

  return (
    <div
      role="alert"
      className={cn(
        'flex gap-2 items-start p-3 rounded-lg border bg-amber-50 border-amber-300 text-amber-900 dark:bg-amber-950/30 dark:border-amber-800 dark:text-amber-100',
        className,
      )}
    >
      <ShieldAlert className="mt-0.5 w-4 h-4 shrink-0 text-amber-600 dark:text-amber-400" />
      <div className="space-y-1 text-sm">
        <p className="font-medium">Basic Auth is required</p>
        <p>{versionLabel(resolvedVersion)} refuses to start without a Basic Auth username and password.</p>
        {children}
      </div>
    </div>
  )
}

interface BasicAuthMissingIndicatorProps {
  instance: Pick<Instance, 'gowa_version' | 'config'>
  className?: string
}

// BasicAuthMissingIndicator is a compact icon for instance lists and headers.
export function BasicAuthMissingIndicator({ instance, className }: BasicAuthMissingIndicatorProps) {
  const missing = useMissingBasicAuth(instance)
  const { resolvedVersion } = useBasicAuthRequirement(instance.gowa_version)
  if (!missing) return null

  return (
    <TooltipProvider delayDuration={150}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            role="img"
            tabIndex={0}
            aria-label="Basic Auth required"
            onClick={(event) => event.stopPropagation()}
            className={cn(
              'inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-amber-600 transition-colors hover:bg-amber-50 focus:outline-none focus:ring-2 focus:ring-amber-400 focus:ring-offset-2 dark:text-amber-400 dark:hover:bg-amber-950/40',
              className,
            )}
          >
            <ShieldAlert className="h-3.5 w-3.5" />
          </span>
        </TooltipTrigger>
        <TooltipContent className="max-w-64 text-xs leading-relaxed">
          <p className="font-medium">Basic Auth required</p>
          <p className="mt-1 text-gray-600 dark:text-gray-300">
            {versionLabel(resolvedVersion)} will not start until you add Basic Auth in the instance settings.
          </p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
