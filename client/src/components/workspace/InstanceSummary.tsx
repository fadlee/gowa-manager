import { ArrowUpCircle, CheckCircle2, Play, QrCode, RotateCcw, ScrollText } from 'lucide-react'
import type { Instance } from '../../types'
import type { InstanceAction } from '../../lib/instanceHealth'
import { Button } from '../ui/button'
import { QuickAction } from './InstanceSidebar'
import { StatusDot, type InstanceFilter } from './primitives'
import { rowSubtitle, versionLabel, type InstanceRowData } from './model'

interface InstanceSummaryProps {
  rows: InstanceRowData[]
  latestVersion?: string
  pendingIds: Set<number>
  onFilter: (filter: InstanceFilter) => void
  onOpenTab: (id: number, tab?: string) => void
  onAction: (action: InstanceAction, instance: Instance) => void
}

export function InstanceSummary({ rows, latestVersion, pendingIds, onFilter, onOpenTab, onAction }: InstanceSummaryProps) {
  const attention = rows.filter(row => row.health.needsAttention).sort((a, b) => a.instance.name.localeCompare(b.instance.name))
  const outdated = rows.filter(row => row.health.outdated)
  const running = rows.filter(row => row.health.state === 'running').length
  const errors = rows.filter(row => row.health.state === 'error').length
  const devices = rows.reduce((total, row) => total + (row.health.devices ?? 0), 0)

  const tiles: Array<{ value: number; label: string; tone: string; filter: InstanceFilter }> = [
    { value: running, label: 'Running', tone: 'text-green-600 dark:text-green-400', filter: 'running' },
    { value: attention.length, label: 'Needs attention', tone: 'text-amber-600 dark:text-amber-400', filter: 'attention' },
    { value: errors, label: 'Error', tone: 'text-red-600 dark:text-red-400', filter: 'error' },
    { value: devices, label: 'Devices connected', tone: 'text-indigo-700 dark:text-indigo-300', filter: 'all' },
  ]

  return (
    <div className="min-h-0 flex-1 space-y-5 overflow-y-auto bg-gray-50 p-4 dark:bg-gray-950/40 sm:p-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Summary</h2>
        <p className="text-sm text-gray-500 dark:text-gray-400">Pick an instance on the left, or handle the ones that need attention below.</p>
      </div>

      <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        {tiles.map(tile => (
          <button
            key={tile.label}
            type="button"
            onClick={() => onFilter(tile.filter)}
            className="rounded-lg border border-gray-200 bg-white p-3 text-left transition-colors hover:border-indigo-400 dark:border-gray-800 dark:bg-gray-900"
          >
            <div className={`text-2xl font-semibold tabular-nums ${tile.tone}`}>{tile.value}</div>
            <div className="text-xs text-gray-500 dark:text-gray-400">{tile.label}</div>
          </button>
        ))}
      </div>

      <section className="overflow-hidden rounded-lg border border-amber-200 bg-white dark:border-amber-900/60 dark:bg-gray-900">
        <h3 className="border-b border-amber-200 bg-amber-50 px-4 py-2.5 text-sm font-semibold text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
          Needs attention <span className="tabular-nums">{attention.length}</span>
        </h3>
        {attention.length === 0 ? (
          <p className="flex items-center gap-2 px-4 py-4 text-sm text-gray-600 dark:text-gray-300">
            <CheckCircle2 className="h-4 w-4 text-green-600" />All instances are healthy.
          </p>
        ) : (
          attention.map(row => {
            const { instance, health } = row
            const pending = pendingIds.has(instance.id)
            return (
              <div key={instance.id} className="flex items-center gap-3 border-t border-gray-100 px-4 py-2.5 first:border-t-0 dark:border-gray-800">
                <StatusDot state={health.state} />
                <button type="button" onClick={() => onOpenTab(instance.id)} className="min-w-0 flex-1 text-left">
                  <div className="truncate text-sm font-medium text-gray-900 hover:text-indigo-700 dark:text-gray-100 dark:hover:text-indigo-300">{instance.name}</div>
                  <div className={`truncate text-xs ${health.state === 'error' ? 'font-mono text-red-600 dark:text-red-400' : 'text-amber-700 dark:text-amber-400'}`}>{rowSubtitle(row)}</div>
                </button>
                {health.needsLogin ? (
                  <Button size="sm" className="h-8" onClick={() => onOpenTab(instance.id, 'login')}>
                    <QrCode className="mr-1.5 h-4 w-4" />Log in
                  </Button>
                ) : health.state === 'running' ? (
                  <Button size="sm" variant="outline" className="h-8" disabled={pending} onClick={() => onAction('restart', instance)}>
                    <RotateCcw className="mr-1.5 h-4 w-4" />Restart
                  </Button>
                ) : (
                  <Button size="sm" variant="outline" className="h-8" disabled={pending} onClick={() => onAction('start', instance)}>
                    <Play className="mr-1.5 h-4 w-4" />Start
                  </Button>
                )}
                <QuickAction label={`Logs for ${instance.name}`} icon={ScrollText} onClick={() => onOpenTab(instance.id, 'logs')} />
              </div>
            )
          })
        )}
      </section>

      {outdated.length > 0 && latestVersion && (
        <section className="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
            <ArrowUpCircle className="h-4 w-4 text-indigo-600 dark:text-indigo-400" />
            GOWA {latestVersion} is available for {outdated.length} instance{outdated.length === 1 ? '' : 's'}
          </h3>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {outdated.map(({ instance, health }) => (
              <button
                key={instance.id}
                type="button"
                onClick={() => onOpenTab(instance.id, 'settings')}
                className="rounded-full border border-gray-300 px-2.5 py-0.5 text-xs text-gray-700 hover:border-indigo-400 hover:text-indigo-700 dark:border-gray-700 dark:text-gray-300 dark:hover:text-indigo-300"
              >
                {instance.name} · {versionLabel(health)}
              </button>
            ))}
          </div>
          <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">Change the version in each instance's Settings tab.</p>
        </section>
      )}
    </div>
  )
}
