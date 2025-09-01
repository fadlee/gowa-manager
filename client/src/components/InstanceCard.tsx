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
  Hash
} from 'lucide-react'
import { EditInstanceDialog } from './EditInstanceDialog'

interface InstanceCardProps {
  instance: Instance
}

export function InstanceCard({ instance }: InstanceCardProps) {
  const [showEditDialog, setShowEditDialog] = useState(false)
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
  const isLoading = startMutation.isPending || stopMutation.isPending || restartMutation.isPending

  return (
    <>
      <Card className="hover:shadow-md transition-shadow">
        <CardHeader className="pb-3">
          <div className="flex items-start justify-between">
            <CardTitle className="text-lg font-medium truncate pr-2">
              {instance.name}
            </CardTitle>
            <Badge variant={getStatusVariant(status?.status || 'unknown')}>
              {status?.status || 'unknown'}
            </Badge>
          </div>
          <div className="flex items-center space-x-4 text-sm text-gray-600">
            <div className="flex items-center">
              <Hash className="h-3 w-3 mr-1" />
              {instance.id}
            </div>
            {status?.port && (
              <div className="flex items-center">
                <span>Port: {status.port}</span>
              </div>
            )}
          </div>
        </CardHeader>

        <CardContent className="pb-3">
          <div className="space-y-2 text-sm">
            {status?.pid && (
              <div className="flex items-center text-gray-600">
                <span className="font-medium mr-2">PID:</span>
                <span>{status.pid}</span>
              </div>
            )}
            {status?.uptime !== null && (
              <div className="flex items-center text-gray-600">
                <Clock className="h-3 w-3 mr-2" />
                <span>Uptime: {formatUptime(status.uptime)}</span>
              </div>
            )}
            {instance.config && instance.config !== '{}' && (
              <div className="text-gray-600">
                <span className="font-medium">Config:</span>
                <pre className="text-xs mt-1 p-2 bg-gray-50 rounded border overflow-x-auto">
                  {JSON.stringify(JSON.parse(instance.config), null, 2)}
                </pre>
              </div>
            )}
          </div>
        </CardContent>

        <CardFooter className="pt-0">
          <div className="flex flex-wrap gap-2 w-full">
            {isLoading ? (
              <Button disabled className="flex-1">
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Processing...
              </Button>
            ) : (
              <>
                {isStopped && (
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() => startMutation.mutate()}
                    className="flex-1"
                  >
                    <Play className="h-4 w-4 mr-1" />
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
                      <Square className="h-4 w-4 mr-1" />
                      Stop
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => restartMutation.mutate()}
                      className="flex-1"
                    >
                      <RotateCcw className="h-4 w-4 mr-1" />
                      Restart
                    </Button>
                  </>
                )}
                
                {isRunning && status?.port && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleOpenProxy}
                    className="flex-1"
                  >
                    <ExternalLink className="h-4 w-4 mr-1" />
                    Open
                  </Button>
                )}
                
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowEditDialog(true)}
                  className="flex-1"
                >
                  <Edit className="h-4 w-4 mr-1" />
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
        onOpenChange={setShowEditDialog}
      />
    </>
  )
}