import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Input } from '../components/ui/input'
import { apiClient } from '../lib/api'
import { Button } from '../components/ui/button'
import { Badge } from '../components/ui/badge'
import { Card, CardContent } from '../components/ui/card'
import { CreateInstanceDialog } from '../components/CreateInstanceDialog'
import { Plus, RefreshCw, Search } from 'lucide-react'
import type { Instance } from '../types'
import { cn } from '../lib/utils'

export function DashboardPage() {
  const navigate = useNavigate()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [versionFilter, setVersionFilter] = useState('all')
  const queryClient = useQueryClient()

  const { data: instances, isLoading, error, refetch } = useQuery({
    queryKey: ['instances'],
    queryFn: () => apiClient.getInstances(),
  })

  // Compute unique versions
  const uniqueVersions = instances
    ? [...new Set(instances.map(inst => inst.gowa_version || 'latest'))].sort()
    : []

  // Filtered instances
  const filteredInstances = instances
    ? instances
        .filter(inst => statusFilter === 'all' || inst.status?.toLowerCase() === statusFilter.toLowerCase())
        .filter(inst => versionFilter === 'all' || inst.gowa_version === versionFilter)
        .filter(inst =>
          searchTerm === ''
            ? true
            : inst.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
              inst.key.toLowerCase().includes(searchTerm.toLowerCase())
        )
    : []

  const filteredCount = filteredInstances.length

  const refreshMutation = useMutation({
    mutationFn: () => apiClient.getInstances(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
  })


  if (isLoading) {
    return (
      <div className="px-4 py-8 mx-auto space-y-6 max-w-7xl sm:px-6 lg:px-8">
        <div className="flex justify-between items-center">
          <div className="h-9 w-40 bg-gray-200 dark:bg-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-36 bg-gray-200 dark:bg-gray-800 rounded-md animate-pulse" />
        </div>
        <div className="flex gap-3">
          <div className="flex-1 h-10 bg-gray-200 dark:bg-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-[150px] bg-gray-200 dark:bg-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-[150px] bg-gray-200 dark:bg-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-10 bg-gray-200 dark:bg-gray-800 rounded-md animate-pulse" />
        </div>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="h-24 bg-gray-200 dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-700 animate-pulse" />
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="px-4 py-12 mx-auto max-w-7xl sm:px-6 lg:px-8">
        <div className="flex flex-col items-center justify-center py-16 bg-gray-100 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
          <div className="w-16 h-16 mb-4 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <RefreshCw className="w-8 h-8 text-red-500 dark:text-red-400" />
          </div>
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">Failed to load instances</h3>
          <p className="text-gray-600 dark:text-gray-400 mb-6 text-center max-w-md">
            There was an error loading your instances. Please check your connection and try again.
          </p>
          <Button onClick={() => refetch()} variant="outline" className="border-gray-300 dark:border-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700">
            <RefreshCw className="mr-2 w-4 h-4" />
            Try Again
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="px-4 py-8 mx-auto space-y-6 max-w-7xl sm:px-6 lg:px-8">
      {/* Header */}
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Dashboard</h1>
        <Button onClick={() => setShowCreateDialog(true)}>
          <Plus className="mr-2 w-4 h-4" />
          New Instance
        </Button>
      </div>

      {/* Filters */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 w-4 h-4 text-gray-400 transform -translate-y-1/2" />
          <Input
            placeholder="Search by name or key..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-10"
          />
        </div>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="px-3 py-2 w-[150px] rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        >
          <option value="all">All Status</option>
          <option value="running">Running</option>
          <option value="stopped">Stopped</option>
          <option value="error">Error</option>
        </select>
        <select
          value={versionFilter}
          onChange={(e) => setVersionFilter(e.target.value)}
          className="px-3 py-2 w-[150px] rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        >
          <option value="all">All Versions</option>
          {uniqueVersions.map(version => (
            <option key={version} value={version}>{version}</option>
          ))}
        </select>
        <Button
          variant="outline"
          onClick={() => refreshMutation.mutate()}
          disabled={refreshMutation.isPending}
        >
          <RefreshCw className={cn('h-4 w-4', refreshMutation.isPending && 'animate-spin')} />
        </Button>
      </div>

      {/* Instance count */}
      <div className="text-sm text-gray-600 dark:text-gray-400">
        {filteredCount} instance{filteredCount !== 1 ? 's' : ''}
      </div>

      {/* Instance Grid */}
      {filteredInstances.length > 0 ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {filteredInstances.map((instance) => (
            <InstanceCardSimple
              key={instance.id}
              instance={instance}
              onClick={() => navigate(`/instances/${instance.id}`)}
            />
          ))}
        </div>
      ) : (
        <div className="flex flex-col items-center justify-center py-16 bg-gray-100 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
          {searchTerm || statusFilter !== 'all' || versionFilter !== 'all' ? (
            <>
              <div className="w-16 h-16 mb-4 rounded-full bg-gray-200 dark:bg-gray-700 flex items-center justify-center">
                <Search className="w-8 h-8 text-gray-400 dark:text-gray-500" />
              </div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">No matches found</h3>
              <p className="text-gray-600 dark:text-gray-400 mb-6 text-center max-w-md">
                No instances match your current filters. Try adjusting your search or filter criteria.
              </p>
              <Button
                variant="outline"
                className="border-gray-300 dark:border-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700"
                onClick={() => {
                  setSearchTerm('')
                  setStatusFilter('all')
                  setVersionFilter('all')
                }}
              >
                Clear Filters
              </Button>
            </>
          ) : (
            <>
              <div className="w-16 h-16 mb-4 rounded-full bg-indigo-100 dark:bg-indigo-900/30 flex items-center justify-center">
                <Plus className="w-8 h-8 text-indigo-500 dark:text-indigo-400" />
              </div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">No instances yet</h3>
              <p className="text-gray-600 dark:text-gray-400 mb-6 text-center max-w-md">
                Get started by creating your first GOWA instance. It only takes a few seconds.
              </p>
              <Button onClick={() => setShowCreateDialog(true)} className="bg-indigo-600 hover:bg-indigo-500">
                <Plus className="mr-2 w-4 h-4" />
                Create your first instance
              </Button>
            </>
          )}
        </div>
      )}

      <CreateInstanceDialog
        open={showCreateDialog}
        onOpenChange={setShowCreateDialog}
      />
    </div>
  )
}

// Simplified instance card for dashboard - just shows name, status, version
interface InstanceCardSimpleProps {
  instance: Instance
  onClick: () => void
}

function InstanceCardSimple({ instance, onClick }: InstanceCardSimpleProps) {
  const { data: status } = useQuery({
    queryKey: ['instance-status', instance.id],
    queryFn: () => apiClient.getInstanceStatus(instance.id),
    refetchInterval: 5000,
    initialData: {
      id: instance.id,
      name: instance.name,
      status: instance.status,
      port: instance.port,
      pid: null,
      uptime: null,
    }
  })

  const isRunning = status?.status?.toLowerCase() === 'running'
  const isError = status?.status?.toLowerCase() === 'error'
  const isStopped = status?.status?.toLowerCase() === 'stopped'

  const getStatusConfig = () => {
    if (isRunning) return { label: 'Running', color: 'text-green-400', bg: 'bg-green-500', dotBg: 'bg-green-400' }
    if (isError) return { label: 'Error', color: 'text-red-400', bg: 'bg-red-500', dotBg: 'bg-red-400' }
    if (isStopped) return { label: 'Stopped', color: 'text-gray-400', bg: 'bg-gray-500', dotBg: 'bg-gray-400' }
    return { label: status?.status || 'Unknown', color: 'text-yellow-400', bg: 'bg-yellow-500', dotBg: 'bg-yellow-400' }
  }

  const statusConfig = getStatusConfig()

  return (
    <Card
      className={cn(
        'bg-white dark:bg-gray-800 border-gray-200 dark:border-gray-700 transition-all cursor-pointer hover:shadow-lg hover:border-gray-300 dark:hover:border-gray-500 hover:scale-[1.02]',
        isError && 'border-red-300 dark:border-red-800 hover:border-red-400 dark:hover:border-red-700'
      )}
      onClick={onClick}
    >
      <CardContent className="p-4">
        <div className="flex justify-between items-start">
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-gray-900 dark:text-white truncate">{instance.name}</h3>
            <div className="flex gap-2 items-center mt-2">
              <Badge variant="secondary" className="text-xs text-gray-700 dark:text-gray-300 bg-gray-200 dark:bg-gray-700 hover:bg-gray-300 dark:hover:bg-gray-600">
                {instance.gowa_version || 'latest'}
              </Badge>
              {/* Status indicator with label */}
              <div className="flex items-center gap-1.5">
                <span className={cn('w-2 h-2 rounded-full animate-pulse', statusConfig.dotBg)} />
                <span className={cn('text-xs font-medium', statusConfig.color)}>{statusConfig.label}</span>
              </div>
            </div>
          </div>
          <div className="flex gap-2 items-center">
            {/* Power toggle indicator */}
            <div className={cn(
              'flex items-center px-1 w-12 h-6 rounded-full transition-colors',
              isRunning ? 'bg-green-600' : 'bg-gray-600'
            )}>
              <div className={cn(
                'w-4 h-4 bg-white rounded-full shadow transition-transform',
                isRunning ? 'translate-x-6' : 'translate-x-0'
              )} />
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
