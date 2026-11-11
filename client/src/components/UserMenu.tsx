import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { LogOut, Moon, Rows3, Sun, UserCircle, Users } from 'lucide-react'
import { cn } from '../lib/utils'
import { useTheme } from '../lib/theme'
import { usePreferences } from '../lib/preferences'
import type { User } from '../types'

function Segmented<T extends string>({ value, options, onChange, label }: { value: T; options: Array<{ value: T; label: string }>; onChange: (value: T) => void; label: string }) {
  return (
    <div className="inline-flex rounded-md bg-gray-100 p-0.5 text-xs dark:bg-gray-800" role="group" aria-label={label}>
      {options.map(option => (
        <button
          key={option.value}
          type="button"
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
          className={cn('rounded px-2 py-1', value === option.value ? 'bg-white font-medium text-gray-900 shadow dark:bg-gray-950 dark:text-white' : 'text-gray-600 dark:text-gray-400')}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

export function UserMenu({ user, onLogout }: { user: User | null; onLogout: () => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const { theme, setTheme } = useTheme()
  const { density, setDensity } = usePreferences()
  const isSuperadmin = user?.role === 'superadmin'
  const initials = (user?.username ?? '?').slice(0, 2).toUpperCase()

  useEffect(() => {
    if (!open) return
    const onPointer = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const itemClass = 'flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-gray-800'

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(value => !value)}
        className="inline-flex items-center gap-2 rounded-full border border-gray-300 bg-white py-1 pl-1 pr-2.5 text-sm text-gray-800 transition-colors hover:border-indigo-400 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200"
      >
        <span className="flex h-7 w-7 items-center justify-center rounded-full bg-indigo-600 text-xs font-semibold text-white">{initials}</span>
        <span className="hidden max-w-32 truncate sm:inline">{user?.username}</span>
      </button>
      {open && (
        <div role="menu" className="absolute right-0 z-50 mt-2 w-64 overflow-hidden rounded-lg border border-gray-200 bg-white py-1 shadow-xl dark:border-gray-700 dark:bg-gray-900">
          <div className="border-b border-gray-100 px-3 py-2 dark:border-gray-800">
            <div className="truncate font-medium text-gray-900 dark:text-white">{user?.username}</div>
            <div className="text-xs capitalize text-gray-500 dark:text-gray-400">{user?.role}</div>
          </div>
          <Link role="menuitem" to="/account" onClick={() => setOpen(false)} className={itemClass}>
            <UserCircle className="h-4 w-4" />My account
          </Link>
          {isSuperadmin && (
            <Link role="menuitem" to="/users" onClick={() => setOpen(false)} className={itemClass}>
              <Users className="h-4 w-4" />Users
            </Link>
          )}
          <div className="flex items-center justify-between gap-2 border-t border-gray-100 px-3 py-2 text-sm text-gray-700 dark:border-gray-800 dark:text-gray-200">
            <span className="flex items-center gap-2">{theme === 'dark' ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}Theme</span>
            <Segmented label="Theme" value={theme} onChange={setTheme} options={[{ value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }]} />
          </div>
          <div className="flex items-center justify-between gap-2 px-3 py-2 text-sm text-gray-700 dark:text-gray-200">
            <span className="flex items-center gap-2"><Rows3 className="h-4 w-4" />Density</span>
            <Segmented label="Density" value={density} onChange={setDensity} options={[{ value: 'compact', label: 'Compact' }, { value: 'comfortable', label: 'Comfortable' }]} />
          </div>
          <button role="menuitem" type="button" onClick={() => { setOpen(false); onLogout() }} className={cn(itemClass, 'border-t border-gray-100 dark:border-gray-800')}>
            <LogOut className="h-4 w-4" />Log out
          </button>
        </div>
      )}
    </div>
  )
}
