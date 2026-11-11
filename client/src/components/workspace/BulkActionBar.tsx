import { useState } from 'react'
import { Play, RotateCcw, Square, X } from 'lucide-react'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { ConfirmDialog } from './primitives'
import type { InstanceAction } from '../../lib/instanceHealth'

interface BulkActionBarProps {
  count: number
  busy: boolean
  // stacked puts the actions on their own row, for narrow containers like the sidebar.
  stacked?: boolean
  onAction: (action: InstanceAction) => void
  onClear: () => void
}

export function BulkActionBar({ count, busy, stacked = false, onAction, onClear }: BulkActionBarProps) {
  const [confirmStop, setConfirmStop] = useState(false)
  if (count === 0) return null
  const buttons = (
    <div className="flex flex-wrap gap-1.5">
      <Button size="sm" variant="outline" className="h-7 px-2 text-xs" disabled={busy} onClick={() => onAction('start')}>
        <Play className="mr-1 h-3.5 w-3.5" />Start
      </Button>
      <Button size="sm" variant="outline" className="h-7 px-2 text-xs" disabled={busy} onClick={() => setConfirmStop(true)}>
        <Square className="mr-1 h-3.5 w-3.5" />Stop
      </Button>
      <Button size="sm" variant="outline" className="h-7 px-2 text-xs" disabled={busy} onClick={() => onAction('restart')}>
        <RotateCcw className="mr-1 h-3.5 w-3.5" />Restart
      </Button>
    </div>
  )
  const clear = (
    <button type="button" onClick={onClear} className="inline-flex items-center gap-1 rounded px-1.5 py-1 text-xs font-medium text-gray-600 hover:bg-indigo-100 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-indigo-900 dark:hover:text-white">
      <X className="h-3.5 w-3.5" />Clear
    </button>
  )
  return (
    <div className={cn('rounded-md border border-indigo-200 bg-indigo-50 px-2.5 py-1.5 text-sm dark:border-indigo-900 dark:bg-indigo-950/40', stacked ? 'space-y-1.5 py-2' : 'flex flex-wrap items-center gap-2')}>
      {stacked ? (
        <>
          <div className="flex items-center gap-2">
            <span className="font-medium tabular-nums text-indigo-900 dark:text-indigo-200">{count} selected</span>
            <span className="flex-1" />
            {clear}
          </div>
          {buttons}
        </>
      ) : (
        <>
          <span className="font-medium tabular-nums text-indigo-900 dark:text-indigo-200">{count} selected</span>
          {buttons}
          <span className="flex-1" />
          {clear}
        </>
      )}
      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title={`Stop ${count} instance${count === 1 ? '' : 's'}?`}
        description="Their WhatsApp sessions stay paired, but they stop sending and receiving messages until started again."
        confirmLabel="Stop instances"
        onConfirm={() => onAction('stop')}
      />
    </div>
  )
}
