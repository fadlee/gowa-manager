import { ExternalLink, Clock, Cpu, MemoryStick, HardDrive } from 'lucide-react'
import { Button } from '../ui/button'
import { CopyButton } from '../ui/shadcn-io/copy-button'
import type { Instance, InstanceStatus, InstanceConfig, BasicAuthPair } from '../../types'

interface OverviewSectionProps {
  instance: Instance
  status: InstanceStatus | undefined
  onOpenProxy: () => void
  isRunning: boolean
}

export function OverviewSection({ instance, status, onOpenProxy, isRunning }: OverviewSectionProps) {
  const proxyUrl = `${window.location.origin}/app/${instance.key}`

  // Parse config for basic auth
  let basicAuthPairs: BasicAuthPair[] = []
  try {
    const config: InstanceConfig = JSON.parse(instance.config || '{}')
    basicAuthPairs = config.flags?.basicAuth || []
  } catch {
    // Invalid config
  }

  const generateToken = (pair: BasicAuthPair): string => {
    return `Basic ${btoa(`${pair.username}:${pair.password}`)}`
  }

  const formatUptime = (uptime: number | null) => {
    if (!uptime) return 'N/A'
    const seconds = Math.floor(uptime / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)

    if (hours > 0) {
      return `${hours}h ${minutes % 60}m`
    } else if (minutes > 0) {
      return `${minutes}m ${seconds % 60}s`
    } else {
      return `${seconds}s`
    }
  }

  const formatMemory = (memoryMB: number) => {
    if (memoryMB >= 1024) {
      return `${(memoryMB / 1024).toFixed(1)} GB`
    }
    return `${memoryMB.toFixed(1)} MB`
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-3">
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Overview</h2>
        {isRunning && (
          <Button
            onClick={onOpenProxy}
            variant="outline"
            className="text-blue-600 dark:text-blue-400 border-blue-500 dark:border-blue-600 hover:bg-blue-100 dark:hover:bg-blue-900/30 w-full sm:w-auto"
          >
            <ExternalLink className="mr-2 w-4 h-4" />
            Open Admin Panel
          </Button>
        )}
      </div>

      {/* API URL */}
      <div className="space-y-3">
        <h3 className="text-sm font-medium text-gray-600 dark:text-gray-400">Your Gowa URL is</h3>
        <div className="flex items-center gap-2">
          <code className="flex-1 px-4 py-3 font-mono text-sm text-gray-900 dark:text-white bg-gray-100 dark:bg-gray-800 rounded-lg">
            {proxyUrl}
          </code>
          <CopyButton
            content={proxyUrl}
            variant="ghost"
            className="text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white"
          />
        </div>
      </div>

      {/* Connection info */}
      <div className="space-y-3">
        <h3 className="text-sm font-medium text-gray-600 dark:text-gray-400">Connecting to Your Instance</h3>
        <div className="p-4 font-mono text-sm text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-800 rounded-lg">
          <pre className="whitespace-pre-wrap">
{`// Using fetch
const response = await fetch('${proxyUrl}/app/devices', {
  headers: {${basicAuthPairs.length > 0 ? `
    'Authorization': '${generateToken(basicAuthPairs[0])}'` : ''}
  }
});`}
          </pre>
        </div>
      </div>

      {/* Auth tokens */}
      {basicAuthPairs.length > 0 && (
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-gray-600 dark:text-gray-400">Authentication Token</h3>
          {basicAuthPairs.map((pair, index) => (
            <div key={index} className="flex items-center gap-2">
              <code className="flex-1 px-4 py-3 font-mono text-sm text-gray-900 dark:text-white bg-gray-100 dark:bg-gray-800 rounded-lg truncate">
                {generateToken(pair)}
              </code>
              <CopyButton
                content={generateToken(pair)}
                variant="ghost"
                className="text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white"
              />
            </div>
          ))}
        </div>
      )}

      {/* Resource monitoring - only when running */}
      {isRunning && (
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-gray-600 dark:text-gray-400">Resource Usage</h3>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
            {/* Uptime */}
            <div className="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg">
              <div className="flex gap-2 items-center mb-2 text-xs text-gray-500 dark:text-gray-400 uppercase">
                <Clock className="w-4 h-4" />
                Uptime
              </div>
              <div className="text-lg font-semibold text-gray-900 dark:text-white">
                {status?.uptime ? formatUptime(status.uptime) : '--'}
              </div>
            </div>

            {/* CPU */}
            <div className="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg">
              <div className="flex gap-2 items-center mb-2 text-xs text-gray-500 dark:text-gray-400 uppercase">
                <Cpu className="w-4 h-4" />
                CPU
              </div>
              <div className="text-lg font-semibold text-gray-900 dark:text-white">
                {status?.resources ? `${status.resources.cpuPercent.toFixed(1)}%` : '--'}
              </div>
              <div className="mt-2 w-full h-1 bg-gray-300 dark:bg-gray-700 rounded-full">
                <div
                  className="h-1 bg-blue-500 rounded-full transition-all duration-300"
                  style={{ width: `${Math.min(status?.resources?.cpuPercent || 0, 100)}%` }}
                />
              </div>
            </div>

            {/* Memory */}
            <div className="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg">
              <div className="flex gap-2 items-center mb-2 text-xs text-gray-500 dark:text-gray-400 uppercase">
                <MemoryStick className="w-4 h-4" />
                Memory
              </div>
              <div className="text-lg font-semibold text-gray-900 dark:text-white">
                {status?.resources ? formatMemory(status.resources.memoryMB) : '--'}
              </div>
              <div className="mt-2 w-full h-1 bg-gray-300 dark:bg-gray-700 rounded-full">
                <div
                  className="h-1 bg-blue-500 rounded-full transition-all duration-300"
                  style={{ width: `${Math.min(status?.resources?.memoryPercent || 0, 100)}%` }}
                />
              </div>
            </div>

            {/* Disk */}
            <div className="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg">
              <div className="flex gap-2 items-center mb-2 text-xs text-gray-500 dark:text-gray-400 uppercase">
                <HardDrive className="w-4 h-4" />
                Disk
              </div>
              <div className="text-lg font-semibold text-gray-900 dark:text-white">
                {status?.resources?.diskMB !== undefined ? formatMemory(status.resources.diskMB) : '--'}
              </div>
              <div className="mt-2 w-full h-1 bg-gray-300 dark:bg-gray-700 rounded-full" />
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
