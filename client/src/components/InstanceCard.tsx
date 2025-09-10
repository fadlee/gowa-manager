import { useState } from 'react'
import { useMutation, useQueryClient, useQuery } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import type { Instance } from '../types'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from './ui/card'
import { Button } from './ui/button'
import { Badge } from './ui/badge'
import {
  Play,
  Square,
  RotateCcw,
  ExternalLink,
  Edit,
  Loader2,
  Clock,
  Hash,
  Cpu,
  MemoryStick,
  Trash2,
  AlertTriangle
} from 'lucide-react'
import { EditInstanceDialog } from './EditInstanceDialog'

interface InstanceCardProps {
  instance: Instance
}

export function InstanceCard({ instance }: InstanceCardProps) {
  const [showEditDialog, setShowEditDialog] = useState(false)
  const [openWithJsonView, setOpenWithJsonView] = useState(false)
  const queryClient = useQueryClient()

  // Get real-time status
  const { data: status } = useQuery({
    queryKey: ['instance-status', instance.id],
    queryFn: () => apiClient.getInstanceStatus(instance.id),
    refetchInterval: 5000, // Refresh every 5 seconds
    initialData: {
      id: instance.id,
      name: instance.name,
      status: instance.status,
      port: instance.port,
      pid: null,
      uptime: null,
    }
  })

  const startMutation = useMutation({
    mutationFn: () => apiClient.startInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
    },
  })

  const stopMutation = useMutation({
    mutationFn: () => apiClient.stopInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
    },
  })

  const restartMutation = useMutation({
    mutationFn: () => apiClient.restartInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
  })

  const getStatusColor = (status: string) => {
    switch (status.toLowerCase()) {
      case 'running':
        return 'bg-green-500'
      case 'stopped':
        return 'bg-gray-400'
      case 'starting':
      case 'stopping':
        return 'bg-yellow-500'
      case 'error':
        return 'bg-red-500'
      default:
        return 'bg-gray-300'
    }
  }

  const getCardBorderColor = (status: string) => {
    switch (status.toLowerCase()) {
      case 'error':
        return 'border-orange-200 bg-orange-50'
      default:
        return 'border-gray-200'
    }
  }

  const formatUptime = (uptime: number | null) => {
    if (!uptime) return 'N/A'
    const seconds = Math.floor(uptime / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)

    if (hours > 0) {
      return `${hours}h ${minutes % 60}m`
    } else if (minutes > 0) {
      return `${minutes}m ${seconds % 60}s`
    } else {
      return `${seconds}s`
    }
  }

  const formatMemory = (memoryMB: number) => {
    if (memoryMB >= 1024) {
      return `${(memoryMB / 1024).toFixed(1)} GB`
    }
    return `${memoryMB.toFixed(1)} MB`
  }

  const getCpuColor = (cpuPercent: number) => {
    if (cpuPercent >= 80) return 'text-red-500'
    if (cpuPercent >= 60) return 'text-orange-500'
    if (cpuPercent >= 40) return 'text-yellow-500'
    return 'text-green-500'
  }

  const getMemoryColor = (memoryPercent: number) => {
    if (memoryPercent >= 80) return 'text-red-500'
    if (memoryPercent >= 60) return 'text-orange-500'
    if (memoryPercent >= 40) return 'text-yellow-500'
    return 'text-blue-500'
  }

  const handleOpenProxy = () => {
    if (status?.port) {
      window.open(apiClient.getProxyUrl(instance.key), '_blank')
    }
  }

  const handleDelete = () => {
    if (window.confirm(`Are you sure you want to delete instance "${instance.name}"? This action cannot be undone.`)) {
      deleteMutation.mutate()
    }
  }

  const isRunning = status?.status?.toLowerCase() === 'running'
  const isStopped = status?.status?.toLowerCase() === 'stopped'
  const isError = status?.status?.toLowerCase() === 'error'
  const isLoading = startMutation.isPending || stopMutation.isPending || restartMutation.isPending || deleteMutation.isPending

  return (
    <>
      <Card className={`transition-shadow hover:shadow-md ${getCardBorderColor(status?.status || 'unknown')}`}>
        <CardHeader className="pb-4">
          {/* Header with status dot */}
          <div className="flex justify-between items-start">
            <CardTitle className="text-xl font-semibold text-gray-900 truncate">
              {instance.name}
            </CardTitle>
            <div className={`w-3 h-3 rounded-full ${getStatusColor(status?.status || 'unknown')}`} />
          </div>

          {/* Instance details */}
          <div className="space-y-1 text-sm text-gray-600">
            <div className="flex items-center space-x-4">
              <span>#{instance.id}</span>
              <span>Port: {status?.port || '--'}</span>
              <span>PID: {status?.pid || '--'}</span>
            </div>
            <div className="flex items-center space-x-2">
              <span className="px-2 py-1 text-xs bg-gray-100 rounded">
                {instance.gowa_version || 'latest'}
              </span>
              {instance.gowa_version === 'latest' && (
                <Badge variant="secondary" className="text-xs">
                  Latest
                </Badge>
              )}
            </div>
          </div>
        </CardHeader>

        <CardContent className="pb-4">
          {/* Status messages */}
          {isStopped && (
            <div className="flex items-center mb-4 text-sm text-gray-600 bg-gray-100">
              <Square className="mr-2 w-4 h-4 text-gray-500" />
              <span className="text-sm text-gray-600">
                Stopped {status?.uptime ? formatUptime(Date.now() - status.uptime) + ' ago' : ''}
              </span>
            </div>
          )}

          {isError && (
            <div className="flex items-center p-3 mb-4 bg-yellow-100 rounded-lg">
              <AlertTriangle className="mr-2 w-4 h-4 text-yellow-600" />
              <span className="text-sm text-yellow-800">
                Failed to start - Port already in use
              </span>
            </div>
          )}

          {/* Uptime for running instances */}
          {isRunning && (
            <div className="flex items-center mb-4 text-sm text-gray-600">
              <Clock className="mr-2 w-4 h-4" />
              <span>Uptime: {status?.uptime ? formatUptime(status.uptime) : '--'}</span>
            </div>
          )}

          {/* Resource monitoring */}
          <div className="grid grid-cols-2 gap-6">
            <div>
              <div className="mb-1 text-xs font-medium tracking-wide text-gray-500 uppercase">
                CPU USAGE
              </div>
              <div className="text-lg font-semibold text-gray-900">
                {isRunning && status?.resources ? `${status.resources.cpuPercent.toFixed(1)}%` : '--'}
              </div>
              <div className="mt-2 w-full h-1 bg-gray-200 rounded-full">
                <div
                  className="h-1 bg-blue-500 rounded-full transition-all duration-300"
                  style={{ width: `${Math.min(status?.resources?.cpuPercent || 0, 100)}%` }}
                />
              </div>
            </div>

            <div>
              <div className="mb-1 text-xs font-medium tracking-wide text-gray-500 uppercase">
                MEMORY
              </div>
              <div className="text-lg font-semibold text-gray-900">
                {isRunning && status?.resources ? formatMemory(status.resources.memoryMB) : '--'}
            </div>
              <div className="mt-2 w-full h-1 bg-gray-200 rounded-full">
                <div
                  className="h-1 bg-blue-500 rounded-full transition-all duration-300"
                  style={{ width: `${Math.min(status?.resources?.memoryPercent || 0, 100)}%` }}
                />
              </div>
            </div>
          </div>
        </CardContent>

        <CardFooter className="pt-0">
          <div className="flex gap-1.5 w-full">
            {isLoading ? (
              <Button disabled className="flex-1 h-8">
                <Loader2 className="mr-2 w-3 h-3 animate-spin" />
                Processing...
              </Button>
            ) : (
              <>
                {/* Running state buttons */}
                {isRunning && (
                  <>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleOpenProxy}
                      className="px-3 h-8 text-xs text-blue-600 bg-blue-50 border-blue-200 hover:bg-blue-100"
                    >
                      <ExternalLink className="mr-1 w-3 h-3" />
                      Open
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => restartMutation.mutate()}
                      className="p-0 w-8 h-8"
                    >
                      <RotateCcw className="w-3 h-3" />
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setOpenWithJsonView(false)
                        setShowEditDialog(true)
                      }}
                      className="p-0 w-8 h-8"
                    >
                      <Edit className="w-3 h-3" />
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => stopMutation.mutate()}
                      className="p-0 w-8 h-8 text-red-600 bg-red-50 border-red-200 hover:bg-red-100"
                    >
                      <Square className="w-3 h-3" />
                    </Button>
                  </>
                )}

                {/* Stopped state buttons */}
                {isStopped && (
                  <>
                    <Button
                      variant="default"
                      size="sm"
                      onClick={() => startMutation.mutate()}
                      className="px-3 h-8 text-xs bg-green-600 hover:bg-green-700"
                    >
                      <Play className="mr-1 w-3 h-3" />
                      Start
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setOpenWithJsonView(false)
                        setShowEditDialog(true)
                      }}
                      className="p-0 w-8 h-8"
                    >
                      <Edit className="w-3 h-3" />
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleDelete}
                      className="p-0 w-8 h-8 text-gray-500"
                    >
                      <Trash2 className="w-3 h-3" />
                    </Button>
                  </>
                )}

                {/* Error state buttons */}
                {isError && (
                  <>
                    <Button
                      variant="default"
                      size="sm"
                      onClick={() => startMutation.mutate()}
                      className="px-3 h-8 text-xs bg-green-600 hover:bg-green-700"
                    >
                      <Play className="mr-1 w-3 h-3" />
                      Retry
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setOpenWithJsonView(false)
                        setShowEditDialog(true)
                      }}
                      className="p-0 w-8 h-8"
                    >
                      <Edit className="w-3 h-3" />
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleDelete}
                      className="p-0 w-8 h-8 text-gray-500"
                    >
                      <Trash2 className="w-3 h-3" />
                    </Button>
                  </>
                )}
              </>
            )}
          </div>
        </CardFooter>
      </Card>

      <EditInstanceDialog
        instance={instance}
        open={showEditDialog}
        onOpenChange={(open) => {
          setShowEditDialog(open)
          if (!open) setOpenWithJsonView(false)
        }}
        showJsonViewInitial={openWithJsonView}
      />
    </>
  )
}
