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
  ChevronDown,
  ChevronRight,
  Shield,
  Link,
  Settings
} from 'lucide-react'
import { EditInstanceDialog } from './EditInstanceDialog'

interface InstanceCardProps {
  instance: Instance
}

export function InstanceCard({ instance }: InstanceCardProps) {
  const [showEditDialog, setShowEditDialog] = useState(false)
  const [openWithJsonView, setOpenWithJsonView] = useState(false)
  const [showConfig, setShowConfig] = useState(false)
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

  const getStatusVariant = (status: string) => {
    switch (status.toLowerCase()) {
      case 'running':
        return 'success'
      case 'stopped':
        return 'secondary'
      case 'starting':
      case 'stopping':
        return 'warning'
      case 'error':
        return 'destructive'
      default:
        return 'outline'
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

  const handleOpenProxy = () => {
    if (status?.port) {
      window.open(apiClient.getProxyUrl(instance.key), '_blank')
    }
  }

  const isRunning = status?.status?.toLowerCase() === 'running'
  const isStopped = status?.status?.toLowerCase() === 'stopped'
  const isError = status?.status?.toLowerCase() === 'error'
  const isLoading = startMutation.isPending || stopMutation.isPending || restartMutation.isPending

  const renderConfigSummary = (configStr: string) => {
    try {
      const config = JSON.parse(configStr)
      const flags = config.flags || {}

      // Extract key configuration details
      const hasBasicAuth = flags.basicAuth && flags.basicAuth.length > 0
      const hasWebhooks = flags.webhooks && flags.webhooks.length > 0

      return (
        <div className="space-y-2 text-xs">
          {/* Basic Auth Summary */}
          {hasBasicAuth && (
            <div className="flex items-center">
              <Shield className="mr-1 w-3 h-3 text-blue-500" />
              <span className="font-medium">Basic Auth:</span>
              <span className="ml-1 text-gray-600">
                {flags.basicAuth.length} credential{flags.basicAuth.length !== 1 ? 's' : ''}
              </span>
            </div>
          )}

          {/* Webhooks Summary */}
          {hasWebhooks && (
            <div className="flex items-center">
              <Link className="mr-1 w-3 h-3 text-purple-500" />
              <span className="font-medium">Webhooks:</span>
              <span className="ml-1 text-gray-600">
                {flags.webhooks.length} endpoint{flags.webhooks.length !== 1 ? 's' : ''}
              </span>
            </div>
          )}

          {/* Other Important Settings */}
          <div className="flex items-center">
            <Settings className="mr-1 w-3 h-3 text-gray-500" />
            <span className="font-medium">Advanced Settings:</span>
            <span className="ml-1 text-gray-600">
              {Object.keys(flags).filter(key => key !== 'basicAuth' && key !== 'webhooks').length} configured
            </span>
          </div>

          {/* Show JSON button */}
          <Button
            variant="ghost"
            size="sm"
            className="mt-1 w-full h-6 text-xs"
            onClick={() => {
              // Open edit dialog with JSON view enabled
              setOpenWithJsonView(true)
              setShowEditDialog(true)
            }}
          >
            View Full Configuration
          </Button>
        </div>
      )
    } catch (error) {
      return <span className="text-xs text-red-500">Invalid configuration format</span>
    }
  }

  return (
    <>
      <Card className="transition-shadow hover:shadow-md">
        <CardHeader className="pb-3">
          <div className="flex justify-between items-start">
            <CardTitle className="pr-2 text-lg font-medium truncate">
              {instance.name}
            </CardTitle>
            <div className="flex gap-2 items-center">
              {isRunning && status?.port && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleOpenProxy}
                  className="h-6 text-xs"
                >
                  <ExternalLink className="mr-1 w-4 h-4" />
                  Open
                </Button>
              )}
              <Badge variant={getStatusVariant(status?.status || 'unknown')}>
                {status?.status || 'unknown'}
              </Badge>
            </div>
          </div>
          <div className="flex items-center justify-between w-full text-sm text-gray-600">
            <div className="flex items-center space-x-4">
              <div className="flex items-center">
                <Hash className="mr-1 w-3 h-3" />
                {instance.id}
              </div>
              {status?.port && (
                <div className="flex items-center">
                  <span>Port: {status.port}</span>
                </div>
              )}
            </div>
            {status?.pid && (
              <div className="ml-4">
                <span>PID: {status.pid}</span>
              </div>
            )}
          </div>
        </CardHeader>

        <CardContent className="pb-3">
          <div className="space-y-2 text-sm">
            {status?.uptime !== null && (
              <div className="flex items-center text-gray-600">
                <Clock className="mr-2 w-3 h-3" />
                <span>Uptime: {formatUptime(status.uptime)}</span>
              </div>
            )}
            {instance.config && instance.config !== '{}' && (
              <div className="text-gray-600">
                <button
                  onClick={() => setShowConfig(!showConfig)}
                  className="flex items-center text-sm font-medium text-gray-700 hover:text-gray-900 focus:outline-none"
                >
                  {showConfig ? (
                    <ChevronDown className="mr-1 w-4 h-4" />
                  ) : (
                    <ChevronRight className="mr-1 w-4 h-4" />
                  )}
                  Configuration
                </button>

                {showConfig && (
                  <div className="mt-2 space-y-2">
                    {renderConfigSummary(instance.config)}
                  </div>
                )}
              </div>
            )}
          </div>
        </CardContent>

        <CardFooter className="pt-0">
          <div className="flex flex-wrap gap-2 w-full">
            {isLoading ? (
              <Button disabled className="flex-1">
                <Loader2 className="mr-2 w-4 h-4 animate-spin" />
                Processing...
              </Button>
            ) : (
              <>
                {(isStopped || isError) && (
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() => startMutation.mutate()}
                    className="flex-1"
                  >
                    <Play className="mr-1 w-4 h-4" />
                    Start
                  </Button>
                )}

                {isRunning && (
                  <>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => stopMutation.mutate()}
                      className="flex-1"
                    >
                      <Square className="mr-1 w-4 h-4" />
                      Stop
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => restartMutation.mutate()}
                      className="flex-1"
                    >
                      <RotateCcw className="mr-1 w-4 h-4" />
                      Restart
                    </Button>
                  </>
                )}


                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setOpenWithJsonView(false)
                    setShowEditDialog(true)
                  }}
                  className="flex-1"
                >
                  <Edit className="mr-1 w-4 h-4" />
                  Edit
                </Button>
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
