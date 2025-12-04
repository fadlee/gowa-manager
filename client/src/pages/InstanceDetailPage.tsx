import { useState, useEffect } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import { Button } from '../components/ui/button'
import { Badge } from '../components/ui/badge'
import { Switch } from '../components/ui/switch'
import {
  ArrowLeft,
  ExternalLink,
  Eye,
  Settings,
  AlertTriangle,
  RefreshCw
} from 'lucide-react'
import { cn } from '../lib/utils'
import { OverviewSection } from '../components/instance-detail/OverviewSection'
import { SettingsSection } from '../components/instance-detail/SettingsSection'
import { DangerZoneSection } from '../components/instance-detail/DangerZoneSection'
import { toast } from '../components/ui/use-toast'

type TabType = 'overview' | 'settings' | 'danger'

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

  const isRunning = status?.status?.toLowerCase() === 'running'
  const isError = status?.status?.toLowerCase() === 'error'
  const isLoading = startMutation.isPending || stopMutation.isPending || restartMutation.isPending

  const handleOpenProxy = () => {
    if (status?.port) {
      window.open(apiClient.getProxyUrl(instance.key), '_blank')
    }
  }

  const sidebarItems = [
    { id: 'overview' as const, label: 'Overview', icon: Eye },
    { id: 'settings' as const, label: 'Settings', icon: Settings },
    { id: 'danger' as const, label: 'Danger Zone', icon: AlertTriangle, danger: true },
  ]

  return (
    <div className="min-h-[calc(100vh-64px)]">
      <div className="mx-auto max-w-7xl sm:px-6 lg:px-8">
      {/* Instance header bar */}
      <div className="bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 sm:border-x sm:border-gray-200 dark:sm:border-gray-700">
        <div className="flex justify-between items-center px-4 h-16">
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

            {/* Right: Power toggle */}
            <div className="flex gap-2 items-center sm:gap-3 shrink-0">
              <span className="hidden text-xs text-gray-600 dark:text-gray-400 sm:text-sm sm:inline">
                {isRunning ? 'ON' : 'OFF'}
              </span>
              <Switch
                checked={isRunning}
                onCheckedChange={(checked) => {
                  if (checked) {
                    startMutation.mutate()
                  } else {
                    stopMutation.mutate()
                  }
                }}
                disabled={isLoading}
                className="data-[state=checked]:bg-green-600 data-[state=checked]:shadow-[0_0_12px_rgba(34,197,94,0.4)]"
              />
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

            {/* Admin link - only when running */}
            {isRunning && (
              <a
                href={apiClient.getProxyUrl(instance.key)}
                target="_blank"
                rel="noopener noreferrer"
                className="flex gap-3 items-center px-3 py-2 text-sm text-gray-600 dark:text-gray-400 rounded-md transition-colors hover:bg-gray-200 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-white"
              >
                <ExternalLink className="w-4 h-4" />
                Admin
              </a>
            )}
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
              onOpenProxy={handleOpenProxy}
              isRunning={isRunning}
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
