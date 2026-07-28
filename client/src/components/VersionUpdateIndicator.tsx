import { useQuery } from '@tanstack/react-query'
import { AlertCircle } from 'lucide-react'
import { apiClient } from '../lib/api'
import { cn } from '../lib/utils'
import type { VersionInfo } from '../types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'

interface VersionUpdateIndicatorProps {
  currentVersion?: string | null
  className?: string
}

function parseVersionParts(version: string): [number, number, number] | null {
  const match = version.trim().replace(/^v/i, '').match(/^(\d+)(?:\.(\d+))?(?:\.(\d+))?/)
  if (!match) return null

  return [
    Number(match[1]),
    Number(match[2] ?? 0),
    Number(match[3] ?? 0),
  ]
}


export function hasNewerRelease(currentVersion: string | null | undefined, latestVersion: string | undefined): boolean {
  const current = currentVersion || 'latest'
  if (!latestVersion || current === 'latest' || current === latestVersion) return false

  const currentParts = parseVersionParts(current)
  const latestParts = parseVersionParts(latestVersion)
  if (!currentParts || !latestParts) return false

  for (let index = 0; index < latestParts.length; index += 1) {
    if (latestParts[index] > currentParts[index]) return true
    if (latestParts[index] < currentParts[index]) return false
  }

  return false
}

export function VersionUpdateIndicator({ currentVersion, className }: VersionUpdateIndicatorProps) {
  const { data: availableVersions = [] } = useQuery({
    queryKey: ['versions', 'available'],
    queryFn: () => apiClient.getAvailableVersions(5),
    staleTime: 5 * 60 * 1000,
  })

  const latestVersion = availableVersions.find(version => version.version !== 'latest' && version.isLatest)?.version
  if (!hasNewerRelease(currentVersion, latestVersion)) return null

  const displayedCurrent = currentVersion || 'latest'

  return (
    <TooltipProvider delayDuration={150}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            role="img"
            tabIndex={0}
            aria-label={`New GOWA version ${latestVersion} available`}
            onClick={(event) => event.stopPropagation()}
            className={cn(
              'inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-amber-500 transition-colors hover:bg-amber-50 hover:text-amber-600 focus:outline-none focus:ring-2 focus:ring-amber-400 focus:ring-offset-2 dark:hover:bg-amber-950/40 dark:hover:text-amber-300',
              className,
            )}
          >
            <AlertCircle className="h-3.5 w-3.5" />
          </span>
        </TooltipTrigger>
        <TooltipContent className="max-w-64 text-xs leading-relaxed">
          <p className="font-medium">New GOWA version available</p>
          <p className="mt-1 text-gray-600 dark:text-gray-300">
            This instance uses {displayedCurrent}. Latest release is {latestVersion}.
          </p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
