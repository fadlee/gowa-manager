import { useState } from 'react'
import { ArrowUpCircle, Play, Plus, QrCode, RotateCcw, ScrollText, Square } from 'lucide-react'
import { cn } from '../../lib/utils'
import { usePreferences } from '../../lib/preferences'
import type { Instance } from '../../types'
import { Button } from '../ui/button'
import { BulkActionBar } from './BulkActionBar'
import { ConfirmDialog, FilterChips, RowCheckbox, StatusPill } from './primitives'
import { formatMemory, formatUptime, rowSubtitle } from './model'
import { QuickAction, SearchField, type InstanceListControls } from './InstanceSidebar'
import { ViewToggle } from './ViewToggle'

interface InstanceTableProps extends InstanceListControls {
  onOpenTab: (id: number, tab: string) => void
}

export function InstanceTable(props: InstanceTableProps) {
  const { rows, selection } = props
  const { density } = usePreferences()
  const [stopTarget, setStopTarget] = useState<Instance | null>(null)
  const allSelected = rows.length > 0 && rows.every(row => selection.has(row.instance.id))
  const someSelected = rows.some(row => selection.has(row.instance.id))
  const cell = density === 'compact' ? 'px-3 py-1.5' : 'px-3 py-2.5'
  const th = 'px-3 py-2 text-left text-xs font-semibold text-gray-500 dark:text-gray-400 whitespace-nowrap'

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-white dark:bg-gray-900">
      <div className="space-y-2 border-b border-gray-200 p-3 dark:border-gray-800">
        <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
          <ViewToggle value={props.view} onChange={props.onViewChange} />
          <SearchField value={props.search} onChange={props.onSearchChange} inputRef={props.searchRef} />
          <FilterChips value={props.filter} counts={props.counts} onChange={props.onFilterChange} />
          <Button size="sm" className="h-9 shrink-0" onClick={props.onCreate}>
            <Plus className="mr-1 h-4 w-4" />New instance
          </Button>
        </div>
        <BulkActionBar count={selection.size} busy={props.busy} onAction={props.onBulkAction} onClear={props.onClearSelection} />
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <table className="w-full">
          <thead className="sticky top-0 z-10 bg-gray-100 dark:bg-gray-800">
            <tr>
              <th className={th}>
                <RowCheckbox checked={allSelected} indeterminate={!allSelected && someSelected} label="Select all instances" onToggle={props.onToggleAll} />
              </th>
              <th className={th}>Name</th>
              <th className={th}>Status</th>
              <th className={th}>WhatsApp</th>
              <th className={th}>Version</th>
              <th className={cn(th, 'hidden lg:table-cell')}>Port</th>
              <th className={cn(th, 'hidden lg:table-cell')}>CPU · RAM</th>
              <th className={cn(th, 'hidden xl:table-cell')}>Uptime</th>
              <th className={th}><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {rows.map(row => {
              const { instance, health, status } = row
              const pending = props.pendingIds.has(instance.id)
              return (
                <tr
                  key={instance.id}
                  onClick={() => props.onOpen(instance.id)}
                  className="cursor-pointer border-t border-gray-100 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/60"
                >
                  <td className={cell}>
                    <RowCheckbox checked={selection.has(instance.id)} label={`Select ${instance.name}`} onToggle={() => props.onToggleSelect(instance.id)} />
                  </td>
                  <td className={cn(cell, 'min-w-[10rem]')}>
                    <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{instance.name}</div>
                    {density === 'comfortable' && <div className="font-mono text-xs text-gray-400">{instance.key}</div>}
                  </td>
                  <td className={cell}><StatusPill state={pending ? 'starting' : health.state} /></td>
                  <td className={cn(cell, 'max-w-[16rem] truncate text-sm', health.needsAttention ? 'text-amber-700 dark:text-amber-400' : 'text-gray-600 dark:text-gray-300')}>
                    {health.state === 'running' && !health.needsLogin ? `${health.devices ?? '—'} device${health.devices === 1 ? '' : 's'}` : rowSubtitle(row)}
                  </td>
                  <td className={cn(cell, 'whitespace-nowrap text-sm text-gray-700 dark:text-gray-300')}>
                    {instance.gowa_version || 'latest'}
                    {health.outdated && <ArrowUpCircle aria-label="Update available" className="ml-1 inline h-3.5 w-3.5 align-[-2px] text-indigo-600 dark:text-indigo-400" />}
                  </td>
                  <td className={cn(cell, 'hidden font-mono text-sm tabular-nums text-gray-600 dark:text-gray-400 lg:table-cell')}>{instance.port ? `:${instance.port}` : '—'}</td>
                  <td className={cn(cell, 'hidden whitespace-nowrap text-sm tabular-nums text-gray-600 dark:text-gray-400 lg:table-cell')}>
                    {status?.resources ? `${status.resources.cpuPercent.toFixed(1)}% · ${formatMemory(status.resources.memoryMB)}` : '—'}
                  </td>
                  <td className={cn(cell, 'hidden text-sm tabular-nums text-gray-600 dark:text-gray-400 xl:table-cell')}>{formatUptime(status?.uptime)}</td>
                  <td className="px-2 py-1">
                    <div className="flex justify-end">
                      {health.state === 'running' ? (
                        <>
                          <QuickAction label={`Stop ${instance.name}`} icon={Square} disabled={pending} onClick={() => setStopTarget(instance)} />
                          <QuickAction label={`Restart ${instance.name}`} icon={RotateCcw} disabled={pending} onClick={() => props.onAction('restart', instance)} />
                        </>
                      ) : (
                        <QuickAction label={`Start ${instance.name}`} icon={Play} disabled={pending} onClick={() => props.onAction('start', instance)} />
                      )}
                      <QuickAction label={`WhatsApp login for ${instance.name}`} icon={QrCode} onClick={() => props.onOpenTab(instance.id, 'login')} className={health.needsAttention ? 'text-amber-600 dark:text-amber-400' : undefined} />
                      <QuickAction label={`Logs for ${instance.name}`} icon={ScrollText} onClick={() => props.onOpenTab(instance.id, 'logs')} />
                    </div>
                  </td>
                </tr>
              )
            })}
            {rows.length === 0 && (
              <tr><td colSpan={9} className="p-8 text-center text-sm text-gray-500 dark:text-gray-400">No instances match your search or filter.</td></tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="border-t border-gray-200 px-3 py-1.5 text-xs text-gray-500 dark:border-gray-800 dark:text-gray-400">
        Click a row to open the instance in List view.
      </p>
      <ConfirmDialog
        open={stopTarget !== null}
        onOpenChange={(open) => !open && setStopTarget(null)}
        title={`Stop ${stopTarget?.name ?? 'instance'}?`}
        description="The WhatsApp session stays paired, but the instance stops sending and receiving messages until it is started again."
        confirmLabel="Stop instance"
        onConfirm={() => stopTarget && props.onAction('stop', stopTarget)}
      />
    </div>
  )
}
