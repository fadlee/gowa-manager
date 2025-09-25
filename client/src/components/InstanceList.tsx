import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Input, Select } from 'antd'
import { apiClient } from '../lib/api'
import { InstanceCard } from './InstanceCard'
import { CreateInstanceDialog } from './CreateInstanceDialog'
import { EditInstanceDialog } from './EditInstanceDialog'
import { Button } from './ui/button'
import { Plus, RefreshCw, Search } from 'lucide-react'
import { useState } from 'react'
import { Segmented } from 'antd'
import type { Instance } from '../types'
import { InstanceTable } from './InstanceTable'

export function InstanceList() {
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editingInstance, setEditingInstance] = useState<Instance | null>(null)
  const [viewMode, setViewMode] = useState<'cards' | 'table'>('cards')
  const [searchTerm, setSearchTerm] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [versionFilter, setVersionFilter] = useState('all')
  const queryClient = useQueryClient()

  const { data: instances, isLoading, error, refetch } = useQuery({
    queryKey: ['instances'],
    queryFn: () => apiClient.getInstances(),
  })

  // Compute unique versions
  const uniqueVersions = instances ? [...new Set(instances.map(inst => inst.gowa_version || 'latest'))].sort() : []

  // Filtered instances
  const filteredInstances = instances
    ? instances
        .filter(inst => statusFilter === 'all' || statusFilter === undefined || inst.status?.toLowerCase() === statusFilter.toLowerCase())
        .filter(inst => versionFilter === 'all' || versionFilter === undefined || inst.gowa_version === versionFilter)
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
    <div className="space-y-6">
      {/* Filters and Search */}
      <div className="mb-6 space-y-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 w-4 h-4 text-gray-400 transform -translate-y-1/2" />
            <Input
              placeholder="Search by name or key..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              prefix=""
              className="pl-10"
            />
          </div>
          <Select
            placeholder="Filter by status"
            value={statusFilter}
            onChange={setStatusFilter}
            allowClear
            style={{ width: 150 }}
          >
            <Select.Option value="all">All Status</Select.Option>
            <Select.Option value="running">Running</Select.Option>
            <Select.Option value="stopped">Stopped</Select.Option>
            <Select.Option value="error">Error</Select.Option>
          </Select>
          <Select
            placeholder="Filter by version"
            value={versionFilter}
            onChange={setVersionFilter}
            allowClear
            style={{ width: 150 }}
          >
            <Select.Option value="all">All Versions</Select.Option>
            {uniqueVersions.map(version => (
              <Select.Option key={version} value={version}>{version}</Select.Option>
            ))}
          </Select>
        </div>
      </div>

      <div className="mb-6 space-y-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center space-x-2">
            <Button onClick={() => setShowCreateDialog(true)}>
              <Plus className="mr-2 w-4 h-4" />
              Create Instance
            </Button>
            <Button
              variant="outline"
              onClick={() => refreshMutation.mutate()}
              disabled={refreshMutation.isPending}
            >
              <RefreshCw className={`h-4 w-4 mr-2 ${refreshMutation.isPending ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
          </div>
          <div className="flex gap-4 items-center">
            {filteredInstances && (
              <span className="inline-flex items-center m-0 h-8 text-sm text-gray-600 sm:text-right">
                {filteredCount} instance{filteredCount !== 1 ? 's' : ''}
              </span>
            )}
            <Segmented
              options={[{ label: 'Cards', value: 'cards' }, { label: 'Table', value: 'table' }]}
              value={viewMode}
              onChange={(val) => setViewMode(val as 'cards' | 'table')}
            />
          </div>
        </div>
      </div>

      {filteredInstances.length > 0 ? (
        viewMode === 'cards' ? (
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
            {filteredInstances.map((instance) => (
              <InstanceCard key={instance.id} instance={instance} />
            ))}
          </div>
        ) : (
          <InstanceTable instances={filteredInstances as Instance[]} onEdit={setEditingInstance} />
        )
      ) : (
        <div className="py-12 text-center bg-white rounded-lg border border-gray-200">
          <p className="mb-4 text-gray-600">
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
      {editingInstance && (
        <EditInstanceDialog
          instance={editingInstance}
          open={!!editingInstance}
          onOpenChange={(open) => {
            if (!open) setEditingInstance(null)
          }}
        />
      )}
    </div>
  )
}
