import { useState, useEffect } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import { Button } from '../components/ui/button'
import { Badge } from '../components/ui/badge'
import {
  ArrowLeft,
  ExternalLink,
  Eye,
  Clock,
  Settings,
  AlertTriangle,
  RefreshCw,
  Braces,
  Play,
  Square
} from 'lucide-react'
import { cn } from '../lib/utils'
import { OverviewSection } from '../components/instance-detail/OverviewSection'
import { ApiSection } from '../components/instance-detail/ApiSection'
import { SettingsSection } from '../components/instance-detail/SettingsSection'
import { DangerZoneSection } from '../components/instance-detail/DangerZoneSection'
import { toast } from '../components/ui/use-toast'

type TabType = 'overview' | 'api' | 'settings' | 'danger'

export function InstanceDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const queryClient = useQueryClient()
  const [lastError, setLastError] = useState<string | null>(null)

  const activeTab = (searchParams.get('tab') as TabType) || 'overview'

  const setActiveTab = (tab: TabType) => {
    setSearchParams({ tab })
  }

  // Fetch instance data
  const { data: instances } = useQuery({
    queryKey: ['instances'],
    queryFn: () => apiClient.getInstances(),
  })

  const instance = instances?.find(i => i.id === Number(id))

  // Fetch real-time status
  const { data: status } = useQuery({
    queryKey: ['instance-status', Number(id)],
    queryFn: () => apiClient.getInstanceStatus(Number(id)),
    refetchInterval: 5000,
    enabled: !!id,
  })

  // Clear error when status changes away from error
  useEffect(() => {
    if (status?.status && status.status.toLowerCase() !== 'error' && !status.error_message) {
      setLastError(null)
    }
  }, [status?.status, status?.error_message])

  // Mutations
  const startMutation = useMutation({
    mutationFn: () => apiClient.startInstance(Number(id)),
    onSuccess: () => {
      setLastError(null)
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', Number(id)] })
      toast({ title: 'Instance started', description: 'The instance is now running.', variant: 'success' })
    },
    onError: (error) => {
      setLastError(error.message)
      toast({ title: 'Failed to start instance', description: error.message, variant: 'error' })
    }
  })

  const stopMutation = useMutation({
    mutationFn: () => apiClient.stopInstance(Number(id)),
    onSuccess: () => {
      setLastError(null)
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', Number(id)] })
      toast({ title: 'Instance stopped', description: 'The instance has been stopped.', variant: 'default' })
    },
    onError: (error) => {
      setLastError(error.message)
      toast({ title: 'Failed to stop instance', description: error.message, variant: 'error' })
    }
  })

  const restartMutation = useMutation({
    mutationFn: () => apiClient.restartInstance(Number(id)),
    onSuccess: () => {
      setLastError(null)
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', Number(id)] })
      toast({ title: 'Instance restarted', description: 'The instance is now running.', variant: 'success' })
    },
    onError: (error) => {
      setLastError(error.message)
      toast({ title: 'Failed to restart instance', description: error.message, variant: 'error' })
    }
  })

  if (!instance) {
    return (
      <div className="flex flex-col justify-center items-center py-12">
        <p className="mb-4 text-gray-600">Instance not found</p>
        <Button onClick={() => navigate('/')} variant="outline">
          <ArrowLeft className="mr-2 w-4 h-4" />
          Back to Dashboard
        </Button>
      </div>
    )
  }

  const currentStatus = status?.status || instance.status || 'unknown'
  const normalizedStatus = currentStatus.toLowerCase()
  const isRunning = normalizedStatus === 'running'
  const isError = normalizedStatus === 'error'
  const isStopped = normalizedStatus === 'stopped'
  const isLoading = startMutation.isPending || stopMutation.isPending || restartMutation.isPending
  const statusLabel = isLoading
    ? startMutation.isPending
      ? 'Starting'
      : stopMutation.isPending
        ? 'Stopping'
        : 'Restarting'
    : currentStatus.charAt(0).toUpperCase() + currentStatus.slice(1)
  const statusBadgeClass = isError
    ? 'bg-red-100 text-red-700 border-red-200 dark:bg-red-900/30 dark:text-red-300 dark:border-red-800'
    : isRunning
      ? 'bg-green-100 text-green-700 border-green-200 dark:bg-green-900/30 dark:text-green-300 dark:border-green-800'
      : isStopped
        ? 'bg-gray-200 text-gray-700 border-gray-300 dark:bg-gray-700 dark:text-gray-200 dark:border-gray-600'
        : 'bg-yellow-100 text-yellow-700 border-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-300 dark:border-yellow-800'

  const formatUptime = (uptime: number | null | undefined) => {
    if (!uptime) return null
    const seconds = Math.floor(uptime / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    const days = Math.floor(hours / 24)

    if (days > 0) return `${days}d ${hours % 24}h`
    if (hours > 0) return `${hours}h ${minutes % 60}m`
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`
    return `${seconds}s`
  }

  const uptimeLabel = formatUptime(status?.uptime)

  const handleOpenProxy = () => {
    if (status?.port) {
      window.open(apiClient.getProxyUrl(instance.key), '_blank')
    }
  }

  const sidebarItems = [
    { id: 'overview' as const, label: 'Overview', icon: Eye },
    { id: 'api' as const, label: 'API', icon: Braces },
    { id: 'settings' as const, label: 'Settings', icon: Settings },
    { id: 'danger' as const, label: 'Danger Zone', icon: AlertTriangle, danger: true },
  ]

  return (
    <div className="min-h-[calc(100vh-64px)]">
      <div className="mx-auto max-w-7xl sm:px-6 lg:px-8">
      {/* Instance header bar */}
      <div className="bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 sm:border-x sm:border-gray-200 dark:sm:border-gray-700">
        <div className="flex justify-between items-center gap-3 px-4 py-3 min-h-16">
          {/* Left: Back + Name + Version */}
          <div className="flex gap-3 items-center min-w-0">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => navigate('/')}
                className="text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white shrink-0"
              >
                <ArrowLeft className="w-4 h-4" />
              </Button>
              <h1 className="mb-0 text-lg font-semibold text-gray-900 dark:text-white truncate sm:text-xl">{instance.name}</h1>
              <Badge variant="secondary" className="hidden text-xs shrink-0 sm:inline-flex">
                {instance.gowa_version || 'latest'}
              </Badge>
            </div>

            {/* Right: Status and lifecycle actions */}
            <div className="flex gap-2 items-center sm:gap-3 shrink-0">
              <Badge variant="outline" className={cn('hidden sm:inline-flex text-xs border', statusBadgeClass)}>
                {statusLabel}
              </Badge>
              {status?.port && (
                <span className="hidden text-xs text-gray-500 dark:text-gray-400 lg:inline">
                  :{status.port}
                </span>
              )}
              {uptimeLabel && isRunning && (
                <span className="hidden items-center gap-1 text-xs text-gray-500 dark:text-gray-400 xl:inline-flex">
                  <Clock className="h-3.5 w-3.5" />
                  {uptimeLabel}
                </span>
              )}
              <Button
                variant="outline"
                size="sm"
                onClick={handleOpenProxy}
                disabled={!isRunning}
                className="hidden border-blue-500 text-blue-600 hover:bg-blue-50 dark:border-blue-700 dark:text-blue-300 dark:hover:bg-blue-950/40 md:inline-flex"
              >
                <ExternalLink className="w-4 h-4 lg:mr-2" />
                <span className="hidden lg:inline">Open Admin</span>
              </Button>
              <div className="flex overflow-hidden items-center rounded-md border border-gray-300 dark:border-gray-600">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => startMutation.mutate()}
                  disabled={isLoading || isRunning}
                  className="rounded-none border-r border-gray-300 dark:border-gray-600 text-green-700 dark:text-green-300 hover:bg-green-50 dark:hover:bg-green-900/20"
                >
                  <Play className="w-4 h-4 sm:mr-2" />
                  <span className="hidden sm:inline">Start</span>
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => restartMutation.mutate()}
                  disabled={isLoading || !isRunning}
                  className="rounded-none border-r border-gray-300 dark:border-gray-600"
                >
                  <RefreshCw className={cn('w-4 h-4 sm:mr-2', restartMutation.isPending && 'animate-spin')} />
                  <span className="hidden sm:inline">Restart</span>
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => stopMutation.mutate()}
                  disabled={isLoading || !isRunning}
                  className="rounded-none text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-900/20"
                >
                  <Square className="w-4 h-4 sm:mr-2" />
                  <span className="hidden sm:inline">Stop</span>
                </Button>
              </div>
            </div>
        </div>

        {/* Mobile tabs */}
        <div className="flex overflow-x-auto border-t border-gray-200 dark:border-gray-700 sm:hidden">
          {sidebarItems.map((item) => (
            <button
              key={item.id}
              onClick={() => setActiveTab(item.id)}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 px-3 py-2.5 text-xs font-medium whitespace-nowrap transition-colors',
                activeTab === item.id
                  ? item.danger
                    ? 'text-red-500 dark:text-red-400 border-b-2 border-red-500 dark:border-red-400'
                    : 'text-gray-900 dark:text-white border-b-2 border-gray-900 dark:border-white'
                  : item.danger
                    ? 'text-red-400/70'
                    : 'text-gray-500 dark:text-gray-400'
              )}
            >
              <item.icon className="w-4 h-4" />
              {item.label}
            </button>
          ))}
        </div>
      </div>

      <div className="flex">
        {/* Sidebar - hidden on mobile */}
        <aside className="hidden sm:block w-48 lg:w-56 shrink-0 bg-gray-100 dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 min-h-[calc(100vh-120px)]">
          <nav className="p-3 space-y-1">
            {sidebarItems.map((item) => (
              <button
                key={item.id}
                onClick={() => setActiveTab(item.id)}
                className={cn(
                  'w-full flex items-center gap-3 px-3 py-2 text-sm rounded-md transition-colors text-left',
                  activeTab === item.id
                    ? item.danger
                      ? 'bg-red-100 dark:bg-red-900/30 text-red-600 dark:text-red-400'
                      : 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-white'
                    : item.danger
                      ? 'text-red-500 dark:text-red-400 hover:bg-red-100 dark:hover:bg-red-900/20'
                      : 'text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-white'
                )}
              >
                <item.icon className="w-4 h-4" />
                {item.label}
              </button>
            ))}

          </nav>
        </aside>

        {/* Main content */}
        <main className="flex-1 p-4 sm:p-6 bg-white dark:bg-gray-900 min-h-[calc(100vh-120px)] overflow-hidden">
          {/* Error banner */}
          {(isError || lastError || status?.error_message || instance.error_message) && (
            <div className="flex gap-3 items-center p-4 mb-6 rounded-lg border border-yellow-300 dark:border-yellow-700 bg-yellow-100 dark:bg-yellow-900/30">
              <AlertTriangle className="w-5 h-5 text-yellow-600 dark:text-yellow-500" />
              <span className="text-yellow-800 dark:text-yellow-200">
                {status?.error_message || instance.error_message || lastError || 'Instance encountered an error'}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => startMutation.mutate()}
                className="ml-auto text-yellow-800 dark:text-yellow-200 border-yellow-400 dark:border-yellow-700 hover:bg-yellow-200 dark:hover:bg-yellow-900/50"
              >
                <RefreshCw className="mr-2 w-4 h-4" />
                Retry
              </Button>
            </div>
          )}

          {/* Tab content */}
          {activeTab === 'overview' && (
            <OverviewSection
              instance={instance}
              status={status}
              isRunning={isRunning}
            />
          )}
          {activeTab === 'api' && (
            <ApiSection
              instance={instance}
            />
          )}
          {activeTab === 'settings' && (
            <SettingsSection
              instance={instance}
            />
          )}
          {activeTab === 'danger' && (
            <DangerZoneSection
              instance={instance}
              onDeleted={() => navigate('/')}
            />
          )}
        </main>
      </div>
      </div>
    </div>
  )
}
