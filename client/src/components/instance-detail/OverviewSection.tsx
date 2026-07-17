import { useState } from 'react'
import { Clock, Cpu, MemoryStick, HardDrive, Eye, EyeOff, Globe, KeyRound, Radio, Webhook } from 'lucide-react'
import { Button } from '../ui/button'
import { CopyButton } from '../ui/shadcn-io/copy-button'
import type { Instance, InstanceStatus, InstanceConfig, BasicAuthPair } from '../../types'

interface OverviewSectionProps {
  instance: Instance
  status: InstanceStatus | undefined
  isRunning: boolean
}

export function OverviewSection({ instance, status, isRunning }: OverviewSectionProps) {
  const proxyUrl = `${window.location.origin}/app/${instance.key}`
  const [revealedAuth, setRevealedAuth] = useState<Record<number, boolean>>({})

  // Parse config for integration details
  let basicAuthPairs: BasicAuthPair[] = []
  let webhooks: string[] = []
  let disabledWebhooks: string[] = []
  try {
    const config: InstanceConfig = JSON.parse(instance.config || '{}')
    basicAuthPairs = config.flags?.basicAuth || []
    webhooks = config.flags?.webhooks || []
    disabledWebhooks = config.flags?.disabledWebhooks || []
  } catch {
    // Invalid config
  }

  const disabledWebhookSet = new Set(disabledWebhooks)

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

  const maskValue = (value: string) => value ? '*'.repeat(Math.min(value.length, 12)) : 'Not set'

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Overview</h2>
        <p className="text-sm text-gray-500 dark:text-gray-400">Connection details and live resource usage for this instance.</p>
      </div>

      {/* Connection / Integration */}
      <div className="space-y-4 rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900 sm:p-5">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 className="text-base font-semibold text-gray-900 dark:text-white">Connection / Integration</h3>
            <p className="text-sm text-gray-500 dark:text-gray-400">Everything needed to connect your app to this instance.</p>
          </div>
          <div className="inline-flex items-center gap-2 rounded-full bg-gray-100 px-3 py-1 text-xs font-medium text-gray-700 dark:bg-gray-800 dark:text-gray-300">
            <Radio className={isRunning ? 'h-3.5 w-3.5 text-green-500' : 'h-3.5 w-3.5 text-gray-400'} />
            {isRunning ? 'Instance running' : 'Instance stopped'}
          </div>
        </div>

        <div className="grid gap-3 lg:grid-cols-2">
          <div className="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-gray-800/70">
            <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
              <Globe className="h-4 w-4" />
              Base URL
            </div>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded-md bg-white px-3 py-2 font-mono text-sm text-gray-900 dark:bg-gray-950 dark:text-white">
                {proxyUrl}
              </code>
              <CopyButton content={proxyUrl} variant="ghost" className="text-gray-600 dark:text-gray-400" />
            </div>
          </div>

          <div className="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-gray-800/70">
            <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
              <Webhook className="h-4 w-4" />
              Webhook URL{webhooks.length > 1 ? 's' : ''}
            </div>
            {webhooks.length > 0 ? (
              <div className="space-y-2">
                {webhooks.map((webhook, index) => {
                  const disabled = disabledWebhookSet.has(webhook)
                  return (
                    <div key={`${webhook}-${index}`} className="flex items-center gap-2">
                      <code className={`min-w-0 flex-1 truncate rounded-md bg-white px-3 py-2 font-mono text-sm dark:bg-gray-950 ${disabled ? 'text-gray-500 line-through dark:text-gray-500' : 'text-gray-900 dark:text-white'}`}>
                        {webhook}
                      </code>
                      {disabled && (
                        <span className="rounded-full bg-gray-200 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-400">
                          Disabled
                        </span>
                      )}
                      <CopyButton content={webhook} variant="ghost" className="text-gray-600 dark:text-gray-400" />
                    </div>
                  )
                })}
              </div>
            ) : (
              <p className="rounded-md bg-white px-3 py-2 text-sm text-gray-500 dark:bg-gray-950 dark:text-gray-400">No webhook configured.</p>
            )}
          </div>
        </div>

        <div className="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-gray-800/70">
          <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
            <KeyRound className="h-4 w-4" />
            Basic Auth Credentials
          </div>
          {basicAuthPairs.length > 0 ? (
            <div className="space-y-2">
              {basicAuthPairs.map((pair, index) => {
                const isRevealed = revealedAuth[index]
                const username = isRevealed ? pair.username : maskValue(pair.username)
                const password = isRevealed ? pair.password : maskValue(pair.password)
                return (
                  <div key={`${pair.username}-${index}`} className="grid gap-2 rounded-md bg-white p-3 dark:bg-gray-950 sm:grid-cols-[1fr_1fr_auto] sm:items-center">
                    <div className="min-w-0">
                      <p className="mb-1 text-xs text-gray-500 dark:text-gray-400">Username</p>
                      <div className="flex items-center gap-2">
                        <code className="min-w-0 flex-1 truncate font-mono text-sm text-gray-900 dark:text-white">{username}</code>
                        <CopyButton content={pair.username} variant="ghost" size="sm" className="text-gray-600 dark:text-gray-400" />
                      </div>
                    </div>
                    <div className="min-w-0">
                      <p className="mb-1 text-xs text-gray-500 dark:text-gray-400">Password</p>
                      <div className="flex items-center gap-2">
                        <code className="min-w-0 flex-1 truncate font-mono text-sm text-gray-900 dark:text-white">{password}</code>
                        <CopyButton content={pair.password} variant="ghost" size="sm" className="text-gray-600 dark:text-gray-400" />
                      </div>
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => setRevealedAuth((current) => ({ ...current, [index]: !current[index] }))}
                    >
                      {isRevealed ? <EyeOff className="mr-2 h-4 w-4" /> : <Eye className="mr-2 h-4 w-4" />}
                      {isRevealed ? 'Hide' : 'Reveal'}
                    </Button>
                  </div>
                )
              })}
            </div>
          ) : (
            <p className="rounded-md bg-white px-3 py-2 text-sm text-gray-500 dark:bg-gray-950 dark:text-gray-400">No basic auth configured.</p>
          )}
        </div>
      </div>

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
              <div className="mt-2 w-full h-1.5 bg-gray-300 dark:bg-gray-700 rounded-full overflow-hidden">
                <div
                  className="h-full bg-gradient-to-r from-blue-500 to-cyan-400 rounded-full transition-all duration-500 ease-out"
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
              <div className="mt-2 w-full h-1.5 bg-gray-300 dark:bg-gray-700 rounded-full overflow-hidden">
                <div
                  className="h-full bg-gradient-to-r from-purple-500 to-pink-400 rounded-full transition-all duration-500 ease-out"
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
              <div className="mt-2 w-full h-1.5 bg-gray-300 dark:bg-gray-700 rounded-full overflow-hidden">
                <div className="h-full w-full bg-gradient-to-r from-amber-500 to-orange-400 rounded-full opacity-20" />
              </div>
            </div>
          </div>
        </div>
      )}

    </div>
  )
}
