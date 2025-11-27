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
      <div className="flex justify-center items-center py-12">
        <div className="w-8 h-8 rounded-full border-b-2 border-gray-900 animate-spin"></div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="py-12 text-center">
        <p className="mb-4 text-red-600">Failed to load instances</p>
        <Button onClick={() => refetch()} variant="outline">
          <RefreshCw className="mr-2 w-4 h-4" />
          Retry
        </Button>
      </div>
    )
  }

  return (
    <div className="px-4 py-8 mx-auto space-y-6 max-w-7xl sm:px-6 lg:px-8">
      {/* Header */}
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-bold text-white">Dashboard</h1>
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
            className="pl-10 text-white bg-gray-800 border-gray-700 placeholder:text-gray-400"
          />
        </div>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="px-3 py-2 w-[150px] rounded-md border border-gray-700 bg-gray-800 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        >
          <option value="all">All Status</option>
          <option value="running">Running</option>
          <option value="stopped">Stopped</option>
          <option value="error">Error</option>
        </select>
        <select
          value={versionFilter}
          onChange={(e) => setVersionFilter(e.target.value)}
          className="px-3 py-2 w-[150px] rounded-md border border-gray-700 bg-gray-800 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
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
      <div className="text-sm text-gray-400">
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
        <div className="py-12 text-center bg-gray-800 rounded-lg border border-gray-700">
          <p className="mb-4 text-gray-400">
            {searchTerm || statusFilter !== 'all' || versionFilter !== 'all'
              ? 'No instances match the current filters'
              : 'No instances found'
            }
          </p>
          <Button onClick={() => setShowCreateDialog(true)}>
            <Plus className="mr-2 w-4 h-4" />
            Create your first instance
          </Button>
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

  return (
    <Card
      className="bg-gray-800 border-gray-700 transition-all cursor-pointer hover:shadow-md hover:border-gray-600"
      onClick={onClick}
    >
      <CardContent className="p-4">
        <div className="flex justify-between items-start">
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-white truncate">{instance.name}</h3>
            <div className="flex gap-2 items-center mt-1">
              <Badge variant="secondary" className="text-xs text-gray-300 bg-gray-700 hover:bg-gray-600">
                {instance.gowa_version || 'latest'}
              </Badge>
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
