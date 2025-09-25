import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import { InstanceCard } from './InstanceCard'
import { CreateInstanceDialog } from './CreateInstanceDialog'
import { EditInstanceDialog } from './EditInstanceDialog'
import { Button } from './ui/button'
import { Plus, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { Segmented } from 'antd'
import type { Instance } from '../types'
import { InstanceTable } from './InstanceTable'

export function InstanceList() {
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editingInstance, setEditingInstance] = useState<Instance | null>(null)
  const [viewMode, setViewMode] = useState<'cards' | 'table'>('cards')
  const queryClient = useQueryClient()

  const { data: instances, isLoading, error, refetch } = useQuery({
    queryKey: ['instances'],
    queryFn: () => apiClient.getInstances(),
  })

  const refreshMutation = useMutation({
    mutationFn: () => apiClient.getInstances(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-900"></div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="text-center py-12">
        <p className="text-red-600 mb-4">Failed to load instances</p>
        <Button onClick={() => refetch()} variant="outline">
          <RefreshCw className="h-4 w-4 mr-2" />
          Retry
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3 mb-6">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center space-x-2">
            <Button onClick={() => setShowCreateDialog(true)}>
              <Plus className="h-4 w-4 mr-2" />
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
          <div className="flex items-center gap-4">
            {instances && (
              <span className="inline-flex items-center h-8 text-sm text-gray-600 sm:text-right m-0">
                {instances.length} instance{instances.length !== 1 ? 's' : ''}
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

      {instances && instances.length > 0 ? (
        viewMode === 'cards' ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {instances.map((instance) => (
              <InstanceCard key={instance.id} instance={instance} />
            ))}
          </div>
        ) : (
          <InstanceTable instances={instances as Instance[]} onEdit={setEditingInstance} />
        )
      ) : (
        <div className="text-center py-12 bg-white rounded-lg border border-gray-200">
          <p className="text-gray-600 mb-4">No instances found</p>
          <Button onClick={() => setShowCreateDialog(true)}>
            <Plus className="h-4 w-4 mr-2" />
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