import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Plus, RefreshCw, Smartphone } from 'lucide-react'
import { apiClient } from '../lib/api'
import { cn } from '../lib/utils'
import { useInstanceActions, useInstanceStatuses, useLatestGowaVersion, type InstanceAction } from '../lib/instanceHealth'
import type { Instance } from '../types'
import { Button } from '../components/ui/button'
import { CreateInstanceDialog } from '../components/CreateInstanceDialog'
import { InstanceSidebar, type InstanceListControls } from '../components/workspace/InstanceSidebar'
import { InstanceTable } from '../components/workspace/InstanceTable'
import { InstanceSummary } from '../components/workspace/InstanceSummary'
import { InstanceDetailPanel, normalizeTab, type DetailTab } from '../components/workspace/InstanceDetailPanel'
import { buildRows, countFilters, matchesFilter, matchesSearch, sortRows } from '../components/workspace/model'
import type { InstanceFilter } from '../components/workspace/primitives'
import type { WorkspaceView } from '../components/workspace/ViewToggle'

function isTyping(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null
  return !!element && (element.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(element.tagName))
}

export function WorkspacePage() {
  const { id } = useParams<{ id: string }>()
  const selectedId = id ? Number(id) : undefined
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const view: WorkspaceView = searchParams.get('view') === 'table' ? 'table' : 'list'
  const tab = normalizeTab(searchParams.get('tab'))

  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState<InstanceFilter>('all')
  const [selection, setSelection] = useState<Set<number>>(() => new Set())
  const [showCreate, setShowCreate] = useState(false)
  const searchRef = useRef<HTMLInputElement>(null)

  const { data: instances = [], isLoading, error, refetch } = useQuery({
    queryKey: ['instances'],
    queryFn: () => apiClient.getInstances(),
  })
  const statuses = useInstanceStatuses()
  const latestVersion = useLatestGowaVersion()
  const actions = useInstanceActions()

  const allRows = useMemo(
    () => buildRows(instances, statuses.byId, latestVersion),
    // statuses.byId is rebuilt every render; statuses.data is the stable source.
    [instances, statuses.data, latestVersion],
  )
  const searchedRows = allRows.filter(row => matchesSearch(row, search))
  const visibleRows = sortRows(searchedRows.filter(row => matchesFilter(row, filter)))
  const counts = countFilters(searchedRows)
  const selectedRow = allRows.find(row => row.instance.id === selectedId)

  // The command palette opens the create dialog through ?create=1.
  useEffect(() => {
    if (searchParams.get('create') === '1') {
      setShowCreate(true)
      const next = new URLSearchParams(searchParams)
      next.delete('create')
      setSearchParams(next, { replace: true })
    }
  }, [searchParams, setSearchParams])

  // Switching instances keeps the open tab, so comparing logs (or any other
  // tab) across instances takes one click each. The last tab is remembered
  // while the summary or table is shown, too.
  const lastTab = useRef<DetailTab>(tab)
  useEffect(() => {
    if (selectedId !== undefined) lastTab.current = tab
  }, [selectedId, tab])

  const openInstance = (instanceId: number, nextTab: string = lastTab.current) => {
    navigate(nextTab !== 'overview' ? `/instances/${instanceId}?tab=${nextTab}` : `/instances/${instanceId}`)
  }

  // Keyboard: "/" focuses search, J/K move through the list.
  const visibleIds = visibleRows.map(row => row.instance.id)
  const visibleIdsKey = visibleIds.join(',')
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) return
      if (event.key === '/') {
        event.preventDefault()
        searchRef.current?.focus()
        return
      }
      if (view !== 'list' || (event.key !== 'j' && event.key !== 'k')) return
      const ids = visibleIdsKey ? visibleIdsKey.split(',').map(Number) : []
      if (!ids.length) return
      const index = selectedId === undefined ? -1 : ids.indexOf(selectedId)
      const next = event.key === 'j' ? Math.min(ids.length - 1, index + 1) : Math.max(0, index - 1)
      navigate(`/instances/${ids[next]}${tab !== 'overview' ? `?tab=${tab}` : ''}`)
      requestAnimationFrame(() => document.querySelector(`[data-instance-row="${ids[next]}"]`)?.scrollIntoView({ block: 'nearest' }))
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [navigate, selectedId, tab, view, visibleIdsKey])

  const runAction = (action: InstanceAction, instance: Instance) => actions.run(action, [instance])

  const controls: InstanceListControls = {
    rows: visibleRows,
    counts,
    filter,
    onFilterChange: setFilter,
    search,
    onSearchChange: setSearch,
    searchRef,
    selection,
    onToggleSelect: (instanceId) => setSelection(previous => {
      const next = new Set(previous)
      if (next.has(instanceId)) next.delete(instanceId)
      else next.add(instanceId)
      return next
    }),
    onToggleAll: () => setSelection(previous => {
      const allSelected = visibleRows.length > 0 && visibleRows.every(row => previous.has(row.instance.id))
      const next = new Set(previous)
      for (const row of visibleRows) {
        if (allSelected) next.delete(row.instance.id)
        else next.add(row.instance.id)
      }
      return next
    }),
    onClearSelection: () => setSelection(new Set()),
    onBulkAction: (action) => {
      const targets = allRows
        .filter(row => selection.has(row.instance.id))
        .filter(row => action === 'start' ? row.health.state !== 'running' : row.health.state === 'running')
        .map(row => row.instance)
      if (targets.length) actions.run(action, targets)
      setSelection(new Set())
    },
    onAction: runAction,
    pendingIds: actions.pendingIds,
    busy: actions.isPending,
    onOpen: (instanceId) => openInstance(instanceId),
    onCreate: () => setShowCreate(true),
    view,
    onViewChange: (nextView) => {
      if (nextView === 'table') navigate('/?view=table')
      else navigate(selectedId ? `/instances/${selectedId}` : '/')
    },
  }

  const createDialog = (
    <CreateInstanceDialog open={showCreate} onOpenChange={setShowCreate} onCreated={(instance) => openInstance(instance.id, 'login')} />
  )

  if (isLoading) {
    return (
      <div className="flex h-[calc(100dvh-3.5rem)]" aria-busy="true">
        <div className="hidden w-80 space-y-2 border-r border-gray-200 p-3 dark:border-gray-800 sm:block">
          {Array.from({ length: 8 }, (_, index) => <div key={index} className="h-8 animate-pulse rounded bg-gray-100 dark:bg-gray-800" />)}
        </div>
        <div className="flex-1 bg-gray-50 dark:bg-gray-950/40" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-[calc(100dvh-3.5rem)] flex-col items-center justify-center gap-3 p-6 text-center">
        <AlertTriangle className="h-10 w-10 text-red-500" />
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Failed to load instances</h2>
        <p className="max-w-md text-sm text-gray-500 dark:text-gray-400">{error instanceof Error ? error.message : 'Check that the Gowa Manager server is running.'}</p>
        <Button variant="outline" onClick={() => refetch()}><RefreshCw className="mr-2 h-4 w-4" />Try again</Button>
      </div>
    )
  }

  if (instances.length === 0) {
    return (
      <div className="flex h-[calc(100dvh-3.5rem)] items-center justify-center bg-gray-50 p-6 dark:bg-gray-950/40">
        <div className="max-w-md space-y-4 rounded-2xl border border-gray-200 bg-white p-8 text-center shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <span className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-indigo-100 text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300">
            <Smartphone className="h-7 w-7" />
          </span>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Create your first instance</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">Each instance runs its own GOWA process and links to one WhatsApp number. After creating it, you will scan a QR code to log in.</p>
          <Button onClick={() => setShowCreate(true)}><Plus className="mr-2 h-4 w-4" />New instance</Button>
        </div>
        {createDialog}
      </div>
    )
  }

  if (view === 'table') {
    return (
      <div className="flex h-[calc(100dvh-3.5rem)] min-h-0">
        <InstanceTable {...controls} onOpenTab={(instanceId, nextTab) => openInstance(instanceId, nextTab)} />
        {createDialog}
      </div>
    )
  }

  return (
    <div className="flex h-[calc(100dvh-3.5rem)] min-h-0">
      <InstanceSidebar
        {...controls}
        selectedId={selectedId}
        onShowSummary={() => navigate('/')}
        className={cn(selectedId !== undefined ? 'hidden sm:flex' : 'flex', 'w-full shrink-0 sm:w-80 lg:w-[22rem]')}
      />
      <section className={cn(selectedId !== undefined ? 'flex' : 'hidden sm:flex', 'min-w-0 flex-1 flex-col')}>
        {selectedRow ? (
          <InstanceDetailPanel
            key={selectedRow.instance.id}
            row={selectedRow}
            tab={tab}
            pending={actions.pendingIds.has(selectedRow.instance.id)}
            onTabChange={(nextTab: DetailTab) => setSearchParams(nextTab === 'overview' ? {} : { tab: nextTab })}
            onAction={runAction}
            onBack={() => navigate('/')}
            onDeleted={() => navigate('/')}
          />
        ) : selectedId !== undefined ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
            <p className="text-sm text-gray-600 dark:text-gray-300">This instance no longer exists.</p>
            <Button variant="outline" onClick={() => navigate('/')}>Back to summary</Button>
          </div>
        ) : (
          <InstanceSummary
            rows={allRows}
            latestVersion={latestVersion}
            pendingIds={actions.pendingIds}
            onFilter={setFilter}
            onOpenTab={(instanceId, nextTab) => openInstance(instanceId, nextTab)}
            onAction={runAction}
          />
        )}
      </section>
      {createDialog}
    </div>
  )
}
