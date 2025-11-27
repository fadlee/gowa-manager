import { useState, useEffect } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import { Button } from '../components/ui/button'
import { Badge } from '../components/ui/badge'
import {
  ArrowLeft,
  ExternalLink,
  Loader2,
  Eye,
  Settings,
  AlertTriangle,
  RefreshCw
} from 'lucide-react'
import { cn } from '../lib/utils'
import { OverviewSection } from '../components/instance-detail/OverviewSection'
import { SettingsSection } from '../components/instance-detail/SettingsSection'
import { DangerZoneSection } from '../components/instance-detail/DangerZoneSection'

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
    },
    onError: (error) => setLastError(error.message)
  })

  const stopMutation = useMutation({
    mutationFn: () => apiClient.stopInstance(Number(id)),
    onSuccess: () => {
      setLastError(null)
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', Number(id)] })
    },
    onError: (error) => setLastError(error.message)
  })

  const restartMutation = useMutation({
    mutationFn: () => apiClient.restartInstance(Number(id)),
    onSuccess: () => {
      setLastError(null)
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', Number(id)] })
    },
    onError: (error) => setLastError(error.message)
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
    <div className="flex">
      {/* Instance header bar - below topbar */}
      <div className="w-full">
        <div className="bg-gray-800 border-b border-gray-700 px-4 py-4">
          <div className="mx-auto max-w-7xl sm:px-6 lg:px-8">
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-4">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => navigate('/')}
                  className="text-gray-400 hover:text-white"
                >
                  <ArrowLeft className="w-4 h-4" />
                </Button>
                <div>
                  <h1 className="text-2xl font-bold text-white">{instance.name}</h1>
                  <div className="flex gap-2 items-center mt-1">
                    <Badge variant="secondary" className="text-xs">
                      {instance.gowa_version || 'latest'}
                    </Badge>
                  </div>
                </div>
              </div>

              {/* Power toggle */}
              <div className="flex items-center gap-3">
                <span className="text-sm text-gray-400">
                  {isRunning ? 'ON' : 'OFF'}
                </span>
                <button
                  onClick={() => isRunning ? stopMutation.mutate() : startMutation.mutate()}
                  disabled={isLoading}
                  className={cn(
                    'w-14 h-7 rounded-full flex items-center px-1 transition-colors cursor-pointer',
                    isRunning ? 'bg-green-500' : 'bg-gray-600',
                    isLoading && 'opacity-50 cursor-not-allowed'
                  )}
                >
                  {isLoading ? (
                    <Loader2 className="w-5 h-5 text-white animate-spin mx-auto" />
                  ) : (
                    <div className={cn(
                      'w-5 h-5 rounded-full bg-white shadow transition-transform',
                      isRunning ? 'translate-x-7' : 'translate-x-0'
                    )} />
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>

        <div className="flex mx-auto max-w-7xl">
        {/* Sidebar */}
        <aside className="w-56 min-h-[calc(100vh-80px)] bg-gray-800 border-r border-gray-700">
          <nav className="p-4 space-y-1">
            {sidebarItems.map((item) => (
              <button
                key={item.id}
                onClick={() => setActiveTab(item.id)}
                className={cn(
                  'w-full flex items-center gap-3 px-3 py-2 text-sm rounded-md transition-colors text-left',
                  activeTab === item.id
                    ? item.danger
                      ? 'bg-red-900/30 text-red-400'
                      : 'bg-gray-700 text-white'
                    : item.danger
                      ? 'text-red-400 hover:bg-red-900/20'
                      : 'text-gray-400 hover:bg-gray-700 hover:text-white'
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
                className="flex gap-3 items-center px-3 py-2 text-sm text-gray-400 rounded-md transition-colors hover:bg-gray-700 hover:text-white"
              >
                <ExternalLink className="w-4 h-4" />
                Admin
              </a>
            )}
          </nav>
        </aside>

        {/* Main content */}
        <main className="flex-1 p-6 bg-gray-900">
          {/* Error banner */}
          {(isError || lastError || status?.error_message || instance.error_message) && (
            <div className="flex items-center gap-3 p-4 mb-6 bg-yellow-900/30 rounded-lg border border-yellow-700">
              <AlertTriangle className="w-5 h-5 text-yellow-500" />
              <span className="text-yellow-200">
                {status?.error_message || instance.error_message || lastError || 'Instance encountered an error'}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => startMutation.mutate()}
                className="ml-auto text-yellow-200 border-yellow-700 hover:bg-yellow-900/50"
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
