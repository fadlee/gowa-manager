import { Check, Minus } from 'lucide-react'
import { cn } from '../../lib/utils'
import type { HealthState } from '../../lib/instanceHealth'
import { Button } from '../ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '../ui/dialog'

const dotClass: Record<HealthState, string> = {
  running: 'bg-green-500',
  stopped: 'bg-gray-400 dark:bg-gray-500',
  error: 'bg-red-500',
  starting: 'bg-amber-400 animate-pulse',
  unknown: 'bg-amber-400',
}

const pillClass: Record<HealthState, string> = {
  running: 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300',
  stopped: 'bg-gray-200 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
  error: 'bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300',
  starting: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300',
  unknown: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300',
}

const stateLabel: Record<HealthState, string> = {
  running: 'Running',
  stopped: 'Stopped',
  error: 'Error',
  starting: 'Starting',
  unknown: 'Unknown',
}

export function StatusDot({ state, className }: { state: HealthState; className?: string }) {
  return <span aria-hidden="true" className={cn('inline-block h-2.5 w-2.5 shrink-0 rounded-full', dotClass[state], className)} />
}

export function StatusPill({ state, label }: { state: HealthState; label?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium', pillClass[state])}>
      <StatusDot state={state} className="h-2 w-2" />
      {label ?? stateLabel[state]}
    </span>
  )
}

interface CheckboxProps {
  checked: boolean
  indeterminate?: boolean
  label: string
  onToggle: () => void
}

// A compact checkbox that does not trigger the row it sits in.
export function RowCheckbox({ checked, indeterminate, label, onToggle }: CheckboxProps) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={indeterminate ? 'mixed' : checked}
      aria-label={label}
      onClick={(event) => {
        event.stopPropagation()
        onToggle()
      }}
      className={cn(
        'flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500',
        checked || indeterminate
          ? 'border-indigo-600 bg-indigo-600 text-white'
          : 'border-gray-300 bg-white hover:border-indigo-400 dark:border-gray-600 dark:bg-gray-900',
      )}
    >
      {checked ? <Check className="h-3 w-3" /> : indeterminate ? <Minus className="h-3 w-3" /> : null}
    </button>
  )
}

export type InstanceFilter = 'all' | 'attention' | 'running' | 'stopped' | 'error' | 'update'

export const FILTER_LABELS: Record<InstanceFilter, string> = {
  all: 'All',
  attention: 'Needs attention',
  running: 'Running',
  stopped: 'Stopped',
  error: 'Error',
  update: 'Update',
}

export function FilterChips({ value, counts, onChange }: { value: InstanceFilter; counts: Record<InstanceFilter, number>; onChange: (filter: InstanceFilter) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label="Filter instances">
      {(Object.keys(FILTER_LABELS) as InstanceFilter[]).map(key => {
        const active = value === key
        const alert = (key === 'attention' || key === 'error') && counts[key] > 0
        return (
          <button
            key={key}
            type="button"
            aria-pressed={active}
            onClick={() => onChange(key)}
            className={cn(
              'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium transition-colors',
              active
                ? 'border-indigo-600 bg-indigo-600 text-white'
                : 'border-gray-300 bg-white text-gray-700 hover:bg-gray-100 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
            )}
          >
            {FILTER_LABELS[key]}
            <span className={cn('tabular-nums', active ? 'text-indigo-100' : alert ? 'font-semibold text-red-600 dark:text-red-400' : 'text-gray-400')}>
              {counts[key]}
            </span>
          </button>
        )
      })}
    </div>
  )
}

interface ConfirmDialogProps {
  open: boolean
  title: string
  description: string
  confirmLabel: string
  onConfirm: () => void
  onOpenChange: (open: boolean) => void
}

export function ConfirmDialog({ open, title, description, confirmLabel, onConfirm, onOpenChange }: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="destructive"
            onClick={() => {
              onConfirm()
              onOpenChange(false)
            }}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
