import { List, Table2 } from 'lucide-react'
import { cn } from '../../lib/utils'

export type WorkspaceView = 'list' | 'table'

export function ViewToggle({ value, onChange }: { value: WorkspaceView; onChange: (view: WorkspaceView) => void }) {
  const options: Array<{ key: WorkspaceView; label: string; icon: typeof List }> = [
    { key: 'list', label: 'List', icon: List },
    { key: 'table', label: 'Table', icon: Table2 },
  ]
  return (
    <div className="inline-flex rounded-md bg-gray-200 p-0.5 text-xs font-medium dark:bg-gray-800" role="group" aria-label="View">
      {options.map(option => (
        <button
          key={option.key}
          type="button"
          aria-pressed={value === option.key}
          onClick={() => onChange(option.key)}
          className={cn(
            'inline-flex items-center gap-1 rounded px-2 py-1 transition-colors',
            value === option.key
              ? 'bg-white text-indigo-700 shadow dark:bg-gray-950 dark:text-indigo-300'
              : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white',
          )}
        >
          <option.icon className="h-3.5 w-3.5" />
          {option.label}
        </button>
      ))}
    </div>
  )
}
