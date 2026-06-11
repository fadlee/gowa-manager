import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../../lib/api'
import { Button } from '../ui/button'
import { Loader2, Trash2, AlertTriangle, Skull } from 'lucide-react'
import type { Instance } from '../../types'

interface DangerZoneSectionProps {
  instance: Instance
  onDeleted: () => void
}

export function DangerZoneSection({ instance, onDeleted }: DangerZoneSectionProps) {
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [showKillConfirm, setShowKillConfirm] = useState(false)
  const queryClient = useQueryClient()

  const killMutation = useMutation({
    mutationFn: () => apiClient.killInstance(instance.id),
    onSuccess: () => {
      setShowKillConfirm(false)
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
    },
    onError: (error) => {
      console.error('Failed to force kill instance:', error)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      onDeleted()
    },
    onError: (error) => {
      console.error('Failed to delete instance:', error)
    },
  })

  const handleDelete = () => {
    deleteMutation.mutate()
  }

  const handleForceKill = () => {
    killMutation.mutate()
  }

  return (
    <div className="space-y-8">
      <div className="flex items-center gap-2">
        <AlertTriangle className="w-5 h-5 text-red-500" />
        <h2 className="text-xl font-semibold text-red-400">Danger Zone</h2>
      </div>

      <p className="text-gray-600 dark:text-gray-400">
        Actions in this section are destructive and cannot be undone. Please proceed with caution.
      </p>

      {/* Force Kill Process */}
      <div className="p-6 bg-orange-100 dark:bg-orange-900/20 rounded-lg border border-orange-300 dark:border-orange-800">
        <div className="flex flex-col gap-4 sm:flex-row sm:justify-between sm:items-start">
          <div>
            <h3 className="font-medium text-gray-900 dark:text-white">Force Kill Process</h3>
            <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">
              Forcefully stop a hung process with SIGKILL. Instance data is kept, but active requests may be interrupted.
            </p>
          </div>
          {!showKillConfirm ? (
            <Button
              variant="destructive"
              onClick={() => setShowKillConfirm(true)}
              className="bg-orange-600 hover:bg-orange-700 sm:shrink-0"
              size="sm"
            >
              <Skull className="mr-2 w-4 h-4" />
              Force Kill
            </Button>
          ) : (
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:shrink-0">
              <span className="text-sm text-orange-700 dark:text-orange-300">Kill process now?</span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowKillConfirm(false)}
                disabled={killMutation.isPending}
                className="text-gray-700 dark:text-gray-300 border-gray-300 dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-gray-800"
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={handleForceKill}
                disabled={killMutation.isPending}
                className="bg-orange-600 hover:bg-orange-700"
              >
                {killMutation.isPending ? (
                  <Loader2 className="mr-2 w-4 h-4 animate-spin" />
                ) : (
                  <Skull className="mr-2 w-4 h-4" />
                )}
                Confirm Kill
              </Button>
            </div>
          )}
        </div>
      </div>

      {/* Delete Instance */}
      <div className="p-6 bg-red-100 dark:bg-red-900/20 rounded-lg border border-red-300 dark:border-red-800">
        <div className="flex justify-between items-start">
          <div>
            <h3 className="font-medium text-gray-900 dark:text-white">Delete Instance</h3>
            <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">
              Permanently delete this instance and all its data. This action cannot be undone.
            </p>
          </div>
          {!showDeleteConfirm ? (
            <Button
              variant="destructive"
              onClick={() => setShowDeleteConfirm(true)}
              className="bg-red-600 hover:bg-red-700"
              size="sm"
            >
              <Trash2 className="mr-2 w-4 h-4" />
              Delete
            </Button>
          ) : (
            <div className="flex gap-2 items-center">
              <span className="text-sm text-red-600 dark:text-red-300">Are you sure?</span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowDeleteConfirm(false)}
                className="text-gray-700 dark:text-gray-300 border-gray-300 dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-gray-800"
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={handleDelete}
                disabled={deleteMutation.isPending}
                className="bg-red-600 hover:bg-red-700"
              >
                {deleteMutation.isPending ? (
                  <Loader2 className="mr-2 w-4 h-4 animate-spin" />
                ) : (
                  <Trash2 className="mr-2 w-4 h-4" />
                )}
                Confirm Delete
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
