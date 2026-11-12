import { useState } from 'react'
import { AlertTriangle, ArrowLeft, Clock, ExternalLink, Play, RotateCcw, Square } from 'lucide-react'
import { cn } from '../../lib/utils'
import { flagsFromConfig } from '../../lib/basicAuth'
import type { InstanceAction } from '../../lib/instanceHealth'
import type { Instance } from '../../types'
import { Button } from '../ui/button'
import { Badge } from '../ui/badge'
import { OverviewSection } from '../instance-detail/OverviewSection'
import { ApiSection } from '../instance-detail/ApiSection'
import { SettingsSection } from '../instance-detail/SettingsSection'
import { FilesSection } from '../instance-detail/FilesSection'
import { LogsSection } from '../instance-detail/LogsSection'
import { DangerZoneSection } from '../instance-detail/DangerZoneSection'
import { VersionUpdateIndicator } from '../VersionUpdateIndicator'
import { BasicAuthBadge, BasicAuthWarning } from '../BasicAuthRequirement'
import { ConfirmDialog, StatusPill } from './primitives'
import { formatUptime, type InstanceRowData } from './model'
import { WhatsAppLoginSection, openAdmin } from './WhatsAppLoginSection'

export const DETAIL_TABS = [
  { id: 'overview', label: 'Overview' },
  { id: 'login', label: 'WhatsApp login' },
  { id: 'logs', label: 'Logs' },
  { id: 'api', label: 'API' },
  { id: 'files', label: 'Files' },
  { id: 'settings', label: 'Settings' },
] as const

export type DetailTab = (typeof DETAIL_TABS)[number]['id']

export function normalizeTab(tab: string | null): DetailTab {
  if (tab === 'danger') return 'settings'
  return DETAIL_TABS.some(item => item.id === tab) ? (tab as DetailTab) : 'overview'
}

interface InstanceDetailPanelProps {
  row: InstanceRowData
  tab: DetailTab
  pending: boolean
  onTabChange: (tab: DetailTab) => void
  onAction: (action: InstanceAction, instance: Instance) => void
  onBack: () => void
  onDeleted: () => void
}

export function InstanceDetailPanel({ row, tab, pending, onTabChange, onAction, onBack, onDeleted }: InstanceDetailPanelProps) {
  const { instance, status, health } = row
  const [confirmStop, setConfirmStop] = useState(false)
  const running = health.state === 'running'
  const uptime = running ? formatUptime(status?.uptime) : null

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="border-b border-gray-200 bg-white px-4 pt-3 dark:border-gray-800 dark:bg-gray-900">
        <div className="flex items-start gap-2">
          <button type="button" onClick={onBack} aria-label="Back to instance list" className="-ml-1 mt-0.5 rounded-md p-1 text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800 sm:hidden">
            <ArrowLeft className="h-5 w-5" />
          </button>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="mb-0 truncate text-lg font-semibold text-gray-900 dark:text-white">{instance.name}</h1>
              <StatusPill state={pending ? 'starting' : health.state} label={pending ? 'Working…' : undefined} />
              <Badge variant="secondary" className="text-xs">{instance.gowa_version || 'latest'}</Badge>
              <VersionUpdateIndicator currentVersion={instance.gowa_version} />
              <BasicAuthBadge instance={instance} className="hidden sm:inline-flex" />
            </div>
            <p className="mt-0.5 flex flex-wrap items-center gap-x-2 truncate font-mono text-xs text-gray-500 dark:text-gray-400">
              <span>{instance.key}</span>
              {instance.port && <span>:{instance.port}</span>}
              {uptime && uptime !== '—' && (
                <span className="inline-flex items-center gap-1 font-sans"><Clock className="h-3 w-3" />up {uptime}</span>
              )}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            {running ? (
              <>
                <Button variant="outline" size="sm" disabled={pending} onClick={() => setConfirmStop(true)} aria-label="Stop">
                  <Square className="h-4 w-4 md:mr-1.5" /><span className="hidden md:inline">Stop</span>
                </Button>
                <Button variant="outline" size="sm" disabled={pending} onClick={() => onAction('restart', instance)} aria-label="Restart">
                  <RotateCcw className={cn('h-4 w-4 md:mr-1.5', pending && 'animate-spin')} /><span className="hidden md:inline">Restart</span>
                </Button>
                <Button variant="outline" size="sm" className="hidden lg:inline-flex" onClick={() => openAdmin(instance)}>
                  <ExternalLink className="mr-1.5 h-4 w-4" />Open Admin
                </Button>
              </>
            ) : (
              <Button size="sm" disabled={pending} onClick={() => onAction('start', instance)}>
                <Play className="mr-1.5 h-4 w-4" />Start
              </Button>
            )}
          </div>
        </div>
        <nav className="-mb-px mt-2 flex gap-1 overflow-x-auto" aria-label="Instance sections">
          {DETAIL_TABS.map(item => (
            <button
              key={item.id}
              type="button"
              onClick={() => onTabChange(item.id)}
              aria-current={tab === item.id ? 'page' : undefined}
              className={cn(
                'whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium transition-colors',
                tab === item.id
                  ? 'border-indigo-600 text-indigo-700 dark:border-indigo-400 dark:text-indigo-300'
                  : 'border-transparent text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
              )}
            >
              {item.label}
              {item.id === 'login' && health.needsLogin && <span className="ml-1.5 inline-block h-1.5 w-1.5 rounded-full bg-amber-500 align-middle" aria-label="Needs login" />}
            </button>
          ))}
        </nav>
      </div>

      <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden bg-gray-50 p-4 dark:bg-gray-950/40 sm:p-6">
        {health.state === 'error' && (
          <div className="mb-6 flex flex-wrap items-center gap-3 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-900/60 dark:bg-red-950/30">
            <AlertTriangle className="h-5 w-5 shrink-0 text-red-600 dark:text-red-400" />
            <span className="min-w-0 flex-1 basis-56 break-words font-mono text-sm text-red-800 dark:text-red-200">{health.errorMessage || 'Instance encountered an error'}</span>
            <Button variant="outline" size="sm" disabled={pending} onClick={() => onAction('start', instance)}>
              <RotateCcw className="mr-2 h-4 w-4" />Retry
            </Button>
            {tab !== 'logs' && (
              <Button variant="ghost" size="sm" onClick={() => onTabChange('logs')}>View logs</Button>
            )}
          </div>
        )}

        {tab !== 'settings' && (
          <BasicAuthWarning version={instance.gowa_version} flags={flagsFromConfig(instance.config)} checkFlags className="mb-6">
            <Button variant="outline" size="sm" onClick={() => onTabChange('settings')} className="mt-2 border-amber-400 dark:border-amber-700">
              Add Basic Auth in Settings
            </Button>
          </BasicAuthWarning>
        )}

        {tab === 'overview' && <OverviewSection instance={instance} status={status} isRunning={running} />}
        {tab === 'login' && <WhatsAppLoginSection instance={instance} health={health} starting={pending} onStart={() => onAction('start', instance)} />}
        {tab === 'logs' && <LogsSection instance={instance} />}
        {tab === 'api' && <ApiSection instance={instance} />}
        {tab === 'files' && <FilesSection instance={instance} />}
        {tab === 'settings' && (
          <div className="space-y-10">
            <SettingsSection instance={instance} />
            <DangerZoneSection instance={instance} onDeleted={onDeleted} />
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title={`Stop ${instance.name}?`}
        description="The WhatsApp session stays paired, but the instance stops sending and receiving messages until it is started again."
        confirmLabel="Stop instance"
        onConfirm={() => onAction('stop', instance)}
      />
    </div>
  )
}
