import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Lock, LockOpen, ShieldAlert } from 'lucide-react'
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

interface BasicAuthBadgeProps {
  instance: Pick<Instance, 'gowa_version' | 'config'>
  className?: string
}

// BasicAuthBadge shows at a glance whether an instance has Basic Auth:
// green "Auth" when it does, grey "No auth" when its GOWA version runs
// without it, and amber "No auth" when its version will not start.
export function BasicAuthBadge({ instance, className }: BasicAuthBadgeProps) {
  const { required, resolvedVersion } = useBasicAuthRequirement(instance.gowa_version)
  const secured = hasBasicAuth(flagsFromConfig(instance.config))
  const blocking = required && !secured

  const Icon = secured ? Lock : blocking ? ShieldAlert : LockOpen
  const label = secured ? 'Auth' : 'No auth'
  const title = secured ? 'Basic Auth enabled' : blocking ? 'Basic Auth required' : 'No Basic Auth'
  const detail = secured
    ? "The instance's GOWA API and UI require a username and password."
    : blocking
      ? `${versionLabel(resolvedVersion)} will not start until you add Basic Auth in the instance settings.`
      : "Anyone who can reach the manager can use this instance's GOWA API and UI. Add Basic Auth in the instance settings."

  return (
    <TooltipProvider delayDuration={150}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            tabIndex={0}
            aria-label={title}
            onClick={(event) => event.stopPropagation()}
            className={cn(
              'inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium focus:outline-none focus:ring-2 focus:ring-offset-2',
              secured && 'bg-green-50 text-green-700 focus:ring-green-400 dark:bg-green-950/40 dark:text-green-300',
              blocking && 'bg-amber-50 text-amber-700 focus:ring-amber-400 dark:bg-amber-950/40 dark:text-amber-300',
              !secured && !blocking && 'bg-gray-100 text-gray-500 focus:ring-gray-400 dark:bg-gray-800 dark:text-gray-400',
              className,
            )}
          >
            <Icon className="h-3 w-3" />
            {label}
          </span>
        </TooltipTrigger>
        <TooltipContent className="max-w-64 text-xs leading-relaxed">
          <p className="font-medium">{title}</p>
          <p className="mt-1 text-gray-600 dark:text-gray-300">{detail}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
