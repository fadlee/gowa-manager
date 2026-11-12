import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { LayoutDashboard, LayoutPanelTop, Moon, Play, Plus, QrCode, RotateCcw, ScrollText, Search, Square, Table2, UserCircle, Users } from 'lucide-react'
import { apiClient } from '../lib/api'
import { cn } from '../lib/utils'
import { useTheme } from '../lib/theme'
import { getInstanceHealth, useInstanceActions, useInstanceStatuses, type HealthState } from '../lib/instanceHealth'
import { StatusDot } from './workspace/primitives'

interface PaletteItem {
  id: string
  label: string
  hint: string
  icon?: typeof Play
  state?: HealthState
  keywords?: string
  run: () => void
}

interface CommandPaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  isSuperadmin: boolean
}

export function CommandPalette({ open, onOpenChange, isSuperadmin }: CommandPaletteProps) {
  const navigate = useNavigate()
  const { toggleTheme } = useTheme()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const { data: instances = [] } = useQuery({ queryKey: ['instances'], queryFn: () => apiClient.getInstances(), enabled: open })
  const statuses = useInstanceStatuses(open)
  const actions = useInstanceActions()

  useEffect(() => {
    if (open) {
      setQuery('')
      setActive(0)
    }
  }, [open])

  const items = useMemo<PaletteItem[]>(() => {
    const list: PaletteItem[] = []
    for (const instance of instances) {
      const health = getInstanceHealth(instance, statuses.byId.get(instance.id))
      list.push({ id: `open-${instance.id}`, label: instance.name, hint: health.needsAttention ? 'Needs attention' : 'Open instance', state: health.state, keywords: `${instance.key} ${instance.port ?? ''}`, run: () => navigate(`/instances/${instance.id}`) })
      if (health.state === 'running') {
        list.push({ id: `restart-${instance.id}`, label: `Restart ${instance.name}`, hint: 'Action', icon: RotateCcw, run: () => actions.run('restart', [instance]) })
        list.push({ id: `stop-${instance.id}`, label: `Stop ${instance.name}`, hint: 'Action', icon: Square, run: () => actions.run('stop', [instance]) })
      } else {
        list.push({ id: `start-${instance.id}`, label: `Start ${instance.name}`, hint: 'Action', icon: Play, run: () => actions.run('start', [instance]) })
      }
      list.push({ id: `logs-${instance.id}`, label: `Logs ${instance.name}`, hint: 'Open logs', icon: ScrollText, run: () => navigate(`/instances/${instance.id}?tab=logs`) })
      list.push({ id: `login-${instance.id}`, label: `WhatsApp login ${instance.name}`, hint: 'Scan QR', icon: QrCode, run: () => navigate(`/instances/${instance.id}?tab=login`) })
      list.push({ id: `admin-${instance.id}`, label: `GOWA admin ${instance.name}`, hint: 'Open admin page', icon: LayoutPanelTop, run: () => navigate(`/instances/${instance.id}?tab=admin`) })
    }
    list.push({ id: 'create', label: 'New instance', hint: 'Create', icon: Plus, run: () => navigate('/?create=1') })
    list.push({ id: 'summary', label: 'Summary', hint: 'Go to', icon: LayoutDashboard, run: () => navigate('/') })
    list.push({ id: 'table', label: 'Table view', hint: 'Go to', icon: Table2, run: () => navigate('/?view=table') })
    list.push({ id: 'account', label: 'My account', hint: 'Go to', icon: UserCircle, run: () => navigate('/account') })
    if (isSuperadmin) list.push({ id: 'users', label: 'Users', hint: 'Go to', icon: Users, run: () => navigate('/users') })
    list.push({ id: 'theme', label: 'Toggle dark mode', hint: 'Appearance', icon: Moon, run: toggleTheme })
    return list
  // statuses.byId is rebuilt every render; statuses.data is the stable source.
  }, [instances, statuses.data, isSuperadmin, navigate, toggleTheme])

  const results = useMemo(() => {
    const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
    if (!words.length) return items.filter(item => item.id.startsWith('open-') || ['create', 'summary', 'table'].includes(item.id)).slice(0, 12)
    const matches = items.filter(item => words.every(word => `${item.label} ${item.keywords ?? ''}`.toLowerCase().includes(word)))
    // Rank items whose words start with every typed word first ("restart s" → "Restart Sales…").
    const startsWithAll = (item: PaletteItem) => words.every(word => item.label.toLowerCase().split(/\s+/).some(token => token.startsWith(word)))
    return [...matches.filter(startsWithAll), ...matches.filter(item => !startsWithAll(item))].slice(0, 12)
  }, [items, query])

  const choose = (item: PaletteItem | undefined) => {
    if (!item) return
    onOpenChange(false)
    item.run()
  }

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/50 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          className="fixed left-1/2 top-[12vh] z-50 w-[calc(100%-2rem)] max-w-xl -translate-x-1/2 overflow-hidden rounded-xl border border-gray-200 bg-white shadow-2xl dark:border-gray-700 dark:bg-gray-900"
        >
          <DialogPrimitive.Title className="sr-only">Search instances and actions</DialogPrimitive.Title>
          <div className="flex items-center gap-2 border-b border-gray-200 px-4 dark:border-gray-800">
            <Search className="h-4 w-4 text-gray-400" />
            <input
              autoFocus
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setActive(0)
              }}
              onKeyDown={(event) => {
                if (event.key === 'ArrowDown') {
                  event.preventDefault()
                  setActive(index => Math.min(results.length - 1, index + 1))
                } else if (event.key === 'ArrowUp') {
                  event.preventDefault()
                  setActive(index => Math.max(0, index - 1))
                } else if (event.key === 'Enter') {
                  event.preventDefault()
                  choose(results[active])
                }
              }}
              placeholder="Type an instance or action, e.g. “restart sales”"
              aria-label="Search instances and actions"
              className="h-12 flex-1 bg-transparent text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none dark:text-white"
            />
          </div>
          <ul role="listbox" className="max-h-[50vh] overflow-y-auto py-1">
            {results.map((item, index) => (
              <li
                key={item.id}
                role="option"
                aria-selected={index === active}
                onMouseMove={() => setActive(index)}
                onClick={() => choose(item)}
                className={cn('flex cursor-pointer items-center gap-3 px-4 py-2 text-sm', index === active && 'bg-indigo-50 dark:bg-indigo-950/50')}
              >
                <span className="flex w-4 justify-center text-gray-400">
                  {item.state ? <StatusDot state={item.state} /> : item.icon ? <item.icon className="h-4 w-4" /> : null}
                </span>
                <span className="flex-1 truncate text-gray-900 dark:text-gray-100">{item.label}</span>
                <span className="truncate text-xs text-gray-400">{item.hint}</span>
              </li>
            ))}
            {results.length === 0 && <li className="px-4 py-6 text-center text-sm text-gray-500">No results for “{query}”</li>}
          </ul>
          <div className="flex gap-4 border-t border-gray-200 px-4 py-2 text-xs text-gray-500 dark:border-gray-800 dark:text-gray-400">
            <span>↑↓ select</span><span>Enter run</span><span>Esc close</span>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
