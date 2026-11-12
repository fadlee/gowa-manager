import { useState, type RefObject } from 'react'
import { ArrowUpCircle, LayoutDashboard, Play, Plus, RotateCcw, Search, Square } from 'lucide-react'
import { cn } from '../../lib/utils'
import { usePreferences } from '../../lib/preferences'
import type { InstanceAction } from '../../lib/instanceHealth'
import type { Instance } from '../../types'
import { Button } from '../ui/button'
import { BulkActionBar } from './BulkActionBar'
import { ConfirmDialog, FilterChips, RowCheckbox, StatusDot, type InstanceFilter } from './primitives'
import { rowSubtitle, versionLabel, versionTitle, type InstanceRowData } from './model'
import { ViewToggle, type WorkspaceView } from './ViewToggle'

export interface InstanceListControls {
  rows: InstanceRowData[]
  counts: Record<InstanceFilter, number>
  filter: InstanceFilter
  onFilterChange: (filter: InstanceFilter) => void
  search: string
  onSearchChange: (search: string) => void
  searchRef: RefObject<HTMLInputElement>
  selection: Set<number>
  onToggleSelect: (id: number) => void
  onToggleAll: () => void
  onClearSelection: () => void
  onBulkAction: (action: InstanceAction) => void
  onAction: (action: InstanceAction, instance: Instance) => void
  pendingIds: Set<number>
  busy: boolean
  onOpen: (id: number) => void
  onCreate: () => void
  view: WorkspaceView
  onViewChange: (view: WorkspaceView) => void
}

export function SearchField({ value, onChange, inputRef, placeholder = 'Search instances…' }: { value: string; onChange: (value: string) => void; inputRef: RefObject<HTMLInputElement>; placeholder?: string }) {
  return (
    <div className="relative min-w-0 flex-1">
      <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
      <input
        ref={inputRef}
        id="instance-search"
        type="search"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        aria-label="Search instances"
        className="h-9 w-full rounded-md border border-gray-300 bg-white pl-8 pr-8 text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:border-gray-700 dark:bg-gray-950 dark:text-white"
      />
      <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded border border-gray-300 px-1.5 text-[10px] text-gray-400 dark:border-gray-700">/</kbd>
    </div>
  )
}

interface SidebarProps extends InstanceListControls {
  selectedId?: number
  onShowSummary: () => void
  className?: string
}

export function InstanceSidebar(props: SidebarProps) {
  const { rows, selection, selectedId, className } = props
  const { density } = usePreferences()
  const [stopTarget, setStopTarget] = useState<Instance | null>(null)
  const attention = rows.filter(row => row.health.needsAttention)
  const others = rows.filter(row => !row.health.needsAttention)
  const allSelected = rows.length > 0 && rows.every(row => selection.has(row.instance.id))
  const someSelected = rows.some(row => selection.has(row.instance.id))

  const renderRow = (row: InstanceRowData) => {
    const { instance, health } = row
    const active = instance.id === selectedId
    const compact = density === 'compact'
    const pending = props.pendingIds.has(instance.id)
    const subtitle = rowSubtitle(row)
    return (
      <div
        key={instance.id}
        role="button"
        tabIndex={0}
        aria-current={active ? 'page' : undefined}
        onClick={() => props.onOpen(instance.id)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') props.onOpen(instance.id)
        }}
        data-instance-row={instance.id}
        className={cn(
          'group flex cursor-pointer items-start gap-2.5 border-l-2 px-3 outline-none focus-visible:bg-gray-100 dark:focus-visible:bg-gray-800',
          compact ? 'py-1.5' : 'py-2.5',
          active
            ? 'border-indigo-600 bg-indigo-50 dark:border-indigo-400 dark:bg-indigo-950/50'
            : 'border-transparent hover:bg-gray-100 dark:hover:bg-gray-800/60',
        )}
      >
        {/* Top-aligned so they sit on the name line when the reason wraps below it (comfortable density). The offsets center each item on that 20px line. */}
        <span className="mt-0.5 flex shrink-0">
          <RowCheckbox checked={selection.has(instance.id)} label={`Select ${instance.name}`} onToggle={() => props.onToggleSelect(instance.id)} />
        </span>
        <StatusDot state={pending ? 'starting' : health.state} className="mt-[5px]" />
        <div className={cn('min-w-0 flex-1', compact && 'flex items-baseline gap-2')}>
          <div className={cn('truncate text-sm font-medium text-gray-900 dark:text-gray-100', active && 'text-indigo-800 dark:text-indigo-200', compact && 'max-w-[60%] shrink-0')}>
            {instance.name}
          </div>
          <div className={cn('truncate text-xs', health.needsAttention ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500 dark:text-gray-400')} title={subtitle}>
            {subtitle}
          </div>
        </div>
        <span
          className={cn(
            'mt-0.5 inline-flex shrink-0 items-center gap-1 font-mono text-xs tabular-nums group-hover:hidden',
            health.outdated ? 'text-indigo-700 dark:text-indigo-300' : 'text-gray-500 dark:text-gray-400',
          )}
          title={versionTitle(health)}
        >
          {versionLabel(health)}
          {health.outdated && <ArrowUpCircle aria-label="Update available" className="h-3.5 w-3.5" />}
        </span>
        <div className="-my-1 hidden shrink-0 items-center group-hover:flex" onClick={(event) => event.stopPropagation()}>
          {health.state === 'running' ? (
            <>
              <QuickAction label={`Restart ${instance.name}`} disabled={pending} onClick={() => props.onAction('restart', instance)} icon={RotateCcw} />
              <QuickAction label={`Stop ${instance.name}`} disabled={pending} onClick={() => setStopTarget(instance)} icon={Square} />
            </>
          ) : (
            <QuickAction label={`Start ${instance.name}`} disabled={pending} onClick={() => props.onAction('start', instance)} icon={Play} />
          )}
        </div>
      </div>
    )
  }

  return (
    <aside className={cn('min-h-0 flex-col border-r border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900', className)}>
      <div className="space-y-2 border-b border-gray-200 p-3 dark:border-gray-800">
        <div className="flex items-center gap-2">
          <ViewToggle value={props.view} onChange={props.onViewChange} />
          <span className="flex-1" />
          <Button size="sm" className="h-8 px-2.5 text-xs" onClick={props.onCreate}>
            <Plus className="mr-1 h-4 w-4" />New
          </Button>
        </div>
        <SearchField value={props.search} onChange={props.onSearchChange} inputRef={props.searchRef} />
        <FilterChips value={props.filter} counts={props.counts} onChange={props.onFilterChange} />
        <div className="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
          <RowCheckbox checked={allSelected} indeterminate={!allSelected && someSelected} label="Select all instances" onToggle={props.onToggleAll} />
          <span className="tabular-nums">{rows.length} instance{rows.length === 1 ? '' : 's'}</span>
          <span className="flex-1" />
          <button type="button" onClick={props.onShowSummary} className="inline-flex items-center gap-1 font-medium hover:text-indigo-700 dark:hover:text-indigo-300">
            <LayoutDashboard className="h-3.5 w-3.5" />Summary
          </button>
        </div>
        <BulkActionBar stacked count={selection.size} busy={props.busy} onAction={props.onBulkAction} onClear={props.onClearSelection} />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto pb-2">
        {attention.length > 0 && (
          <>
            <SectionLabel label="Needs attention" count={attention.length} tone="text-amber-700 dark:text-amber-400" />
            {attention.map(renderRow)}
          </>
        )}
        {others.length > 0 && (
          <>
            <SectionLabel label={attention.length ? 'All others · A–Z' : 'A–Z'} count={others.length} tone="text-gray-500 dark:text-gray-400" />
            {others.map(renderRow)}
          </>
        )}
        {rows.length === 0 && <p className="p-4 text-sm text-gray-500 dark:text-gray-400">No instances match your search or filter.</p>}
      </div>
      <ConfirmDialog
        open={stopTarget !== null}
        onOpenChange={(open) => !open && setStopTarget(null)}
        title={`Stop ${stopTarget?.name ?? 'instance'}?`}
        description="The WhatsApp session stays paired, but the instance stops sending and receiving messages until it is started again."
        confirmLabel="Stop instance"
        onConfirm={() => stopTarget && props.onAction('stop', stopTarget)}
      />
    </aside>
  )
}

function SectionLabel({ label, count, tone }: { label: string; count: number; tone: string }) {
  return (
    <div className={cn('px-3 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-wide', tone)}>
      {label} <span className="tabular-nums text-gray-400">{count}</span>
    </div>
  )
}

export function QuickAction({ label, icon: Icon, onClick, disabled, className }: { label: string; icon: typeof Play; onClick: () => void; disabled?: boolean; className?: string }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={(event) => {
        event.stopPropagation()
        onClick()
      }}
      className={cn('inline-flex h-7 w-7 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-200 hover:text-gray-900 disabled:opacity-40 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-white', className)}
    >
      <Icon className="h-4 w-4" />
    </button>
  )
}
