import { useEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, Pencil } from 'lucide-react'
import { apiClient } from '../../lib/api'
import type { Instance } from '../../types'
import { toast } from '../ui/use-toast'

const MAX_NAME_LENGTH = 100

// InstanceNameEditor shows the instance name as the panel heading and lets
// the user rename it in place: click the name or the pencil, then Enter or
// click away to save, Escape to cancel.
export function InstanceNameEditor({ instance }: { instance: Instance }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState(instance.name)
  const inputRef = useRef<HTMLInputElement>(null)
  // Escape cancels, and the blur that follows must not save.
  const cancelled = useRef(false)

  const rename = useMutation({
    mutationFn: (name: string) => apiClient.updateInstance(instance.id, { name }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-statuses'] })
    },
    onError: (error) => {
      setValue(instance.name)
      toast({ title: 'Could not rename the instance', description: error instanceof Error ? error.message : undefined, variant: 'error' })
    },
  })

  useEffect(() => {
    if (!editing) setValue(instance.name)
  }, [instance.name, editing])

  useEffect(() => {
    if (editing) inputRef.current?.select()
  }, [editing])

  const startEditing = () => {
    cancelled.current = false
    setValue(instance.name)
    setEditing(true)
  }

  const finish = () => {
    setEditing(false)
    if (cancelled.current) return
    const name = value.trim()
    if (!name || name === instance.name) {
      setValue(instance.name)
      return
    }
    rename.mutate(name)
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        value={value}
        maxLength={MAX_NAME_LENGTH}
        aria-label="Instance name"
        onChange={(event) => setValue(event.target.value)}
        onBlur={finish}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            event.preventDefault()
            inputRef.current?.blur()
          } else if (event.key === 'Escape') {
            event.preventDefault()
            cancelled.current = true
            inputRef.current?.blur()
          }
        }}
        className="-mx-1.5 h-8 min-w-0 max-w-full flex-1 rounded-md border border-indigo-400 bg-white px-1.5 text-lg font-semibold text-gray-900 outline-none ring-2 ring-indigo-500/20 dark:bg-gray-900 dark:text-white sm:max-w-sm"
      />
    )
  }

  // While a rename is saving, show the new name.
  const shown = rename.isPending && rename.variables ? rename.variables : instance.name

  return (
    <div className="group/name flex min-w-0 items-center gap-1">
      <h1
        className="mb-0 cursor-text truncate text-lg font-semibold text-gray-900 dark:text-white"
        title="Click to rename"
        onClick={startEditing}
      >
        {shown}
      </h1>
      {rename.isPending ? (
        <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-gray-400" />
      ) : (
        <button
          type="button"
          onClick={startEditing}
          aria-label="Rename instance"
          className="shrink-0 rounded p-1 text-gray-400 transition-opacity hover:bg-gray-100 hover:text-gray-700 focus-visible:opacity-100 dark:hover:bg-gray-800 dark:hover:text-gray-200 sm:opacity-0 sm:group-hover/name:opacity-100"
        >
          <Pencil className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  )
}
