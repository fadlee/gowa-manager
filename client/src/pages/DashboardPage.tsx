import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { motion, AnimatePresence } from 'motion/react'
import { Input } from '../components/ui/input'
import { apiClient } from '../lib/api'
import { Button } from '../components/ui/button'
import { Badge } from '../components/ui/badge'
import { Card, CardContent } from '../components/ui/card'
import { Switch } from '../components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select'
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
      <div className="px-4 py-8 mx-auto space-y-6 max-w-7xl sm:px-6 lg:px-8">
        <div className="flex justify-between items-center">
          <div className="h-9 w-40 bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-800 dark:via-gray-700 dark:to-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-36 bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-800 dark:via-gray-700 dark:to-gray-800 rounded-md animate-pulse" />
        </div>
        <div className="flex gap-3">
          <div className="flex-1 h-10 bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-800 dark:via-gray-700 dark:to-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-[150px] bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-800 dark:via-gray-700 dark:to-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-[150px] bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-800 dark:via-gray-700 dark:to-gray-800 rounded-md animate-pulse" />
          <div className="h-10 w-10 bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-800 dark:via-gray-700 dark:to-gray-800 rounded-md animate-pulse" />
        </div>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {[1, 2, 3, 4].map((i) => (
            <motion.div 
              key={i} 
              className="p-4 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.1 }}
            >
              <div className="flex justify-between items-start mb-3">
                <div className="h-5 w-32 bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-700 dark:via-gray-600 dark:to-gray-700 rounded animate-pulse" />
                <div className="w-2 h-2 bg-gray-300 dark:bg-gray-600 rounded-full" />
              </div>
              <div className="flex gap-2 items-center">
                <div className="h-5 w-16 bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-700 dark:via-gray-600 dark:to-gray-700 rounded animate-pulse" />
                <div className="h-5 w-20 bg-gradient-to-r from-gray-200 via-gray-300 to-gray-200 dark:from-gray-700 dark:via-gray-600 dark:to-gray-700 rounded animate-pulse" />
              </div>
            </motion.div>
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="px-4 py-12 mx-auto max-w-7xl sm:px-6 lg:px-8">
        <div className="flex flex-col items-center justify-center py-16 bg-gray-100 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
          <div className="w-16 h-16 mb-4 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <RefreshCw className="w-8 h-8 text-red-500 dark:text-red-400" />
          </div>
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">Failed to load instances</h3>
          <p className="text-gray-600 dark:text-gray-400 mb-6 text-center max-w-md">
            There was an error loading your instances. Please check your connection and try again.
          </p>
          <Button onClick={() => refetch()} variant="outline" className="border-gray-300 dark:border-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700">
            <RefreshCw className="mr-2 w-4 h-4" />
            Try Again
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="px-4 py-8 mx-auto space-y-6 max-w-7xl sm:px-6 lg:px-8">
      {/* Header */}
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Dashboard</h1>
        <Button onClick={() => setShowCreateDialog(true)} size="sm">
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
            className="pl-10"
          />
        </div>
        <Select value={statusFilter} onValueChange={setStatusFilter}>
          <SelectTrigger className="w-[150px]">
            <SelectValue placeholder="All Status" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Status</SelectItem>
            <SelectItem value="running">Running</SelectItem>
            <SelectItem value="stopped">Stopped</SelectItem>
            <SelectItem value="error">Error</SelectItem>
          </SelectContent>
        </Select>
        <Select value={versionFilter} onValueChange={setVersionFilter}>
          <SelectTrigger className="w-[150px]">
            <SelectValue placeholder="All Versions" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Versions</SelectItem>
            {uniqueVersions.map(version => (
              <SelectItem key={version} value={version}>{version}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          variant="outline"
          onClick={() => refreshMutation.mutate()}
          disabled={refreshMutation.isPending}
        >
          <RefreshCw className={cn('h-4 w-4', refreshMutation.isPending && 'animate-spin')} />
        </Button>
      </div>

      {/* Instance count */}
      <div className="text-sm text-gray-600 dark:text-gray-400">
        {filteredCount} instance{filteredCount !== 1 ? 's' : ''}
      </div>

      {/* Instance Grid */}
      {filteredInstances.length > 0 ? (
        <motion.div 
          className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
          initial="hidden"
          animate="visible"
          variants={{
            hidden: { opacity: 0 },
            visible: {
              opacity: 1,
              transition: { staggerChildren: 0.05 }
            }
          }}
        >
          <AnimatePresence mode="popLayout">
            {filteredInstances.map((instance) => (
              <motion.div
                key={instance.id}
                variants={{
                  hidden: { opacity: 0, y: 20, scale: 0.95 },
                  visible: { opacity: 1, y: 0, scale: 1 }
                }}
                exit={{ opacity: 0, scale: 0.95, transition: { duration: 0.15 } }}
                transition={{ type: "spring", stiffness: 300, damping: 25 }}
                layout
              >
                <InstanceCardSimple
                  instance={instance}
                  onClick={() => navigate(`/instances/${instance.id}`)}
                />
              </motion.div>
            ))}
          </AnimatePresence>
        </motion.div>
      ) : (
        <div className="flex flex-col items-center justify-center py-16 bg-gray-100 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
          {searchTerm || statusFilter !== 'all' || versionFilter !== 'all' ? (
            <>
              <div className="w-16 h-16 mb-4 rounded-full bg-gray-200 dark:bg-gray-700 flex items-center justify-center">
                <Search className="w-8 h-8 text-gray-400 dark:text-gray-500" />
              </div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">No matches found</h3>
              <p className="text-gray-600 dark:text-gray-400 mb-6 text-center max-w-md">
                No instances match your current filters. Try adjusting your search or filter criteria.
              </p>
              <Button
                variant="outline"
                className="border-gray-300 dark:border-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700"
                onClick={() => {
                  setSearchTerm('')
                  setStatusFilter('all')
                  setVersionFilter('all')
                }}
              >
                Clear Filters
              </Button>
            </>
          ) : (
            <>
              <div className="w-16 h-16 mb-4 rounded-full bg-indigo-100 dark:bg-indigo-900/30 flex items-center justify-center">
                <Plus className="w-8 h-8 text-indigo-500 dark:text-indigo-400" />
              </div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">No instances yet</h3>
              <p className="text-gray-600 dark:text-gray-400 mb-6 text-center max-w-md">
                Get started by creating your first GOWA instance. It only takes a few seconds.
              </p>
              <Button onClick={() => setShowCreateDialog(true)} className="bg-indigo-600 hover:bg-indigo-500">
                <Plus className="mr-2 w-4 h-4" />
                Create your first instance
              </Button>
            </>
          )}
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
  const queryClient = useQueryClient()
  
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

  const startMutation = useMutation({
    mutationFn: () => apiClient.startInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
  })

  const stopMutation = useMutation({
    mutationFn: () => apiClient.stopInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
  })

  const isRunning = status?.status?.toLowerCase() === 'running'
  const isError = status?.status?.toLowerCase() === 'error'
  const isStopped = status?.status?.toLowerCase() === 'stopped'

  const getStatusConfig = () => {
    if (isRunning) return { 
      label: 'Running', 
      color: 'text-green-500 dark:text-green-400', 
      dotBg: 'bg-green-500',
      glow: 'shadow-[0_0_8px_rgba(34,197,94,0.6)]'
    }
    if (isError) return { 
      label: 'Error', 
      color: 'text-red-500 dark:text-red-400', 
      dotBg: 'bg-red-500',
      glow: 'shadow-[0_0_8px_rgba(239,68,68,0.6)]'
    }
    if (isStopped) return { 
      label: 'Stopped', 
      color: 'text-gray-500 dark:text-gray-400', 
      dotBg: 'bg-gray-400 dark:bg-gray-500',
      glow: ''
    }
    return { 
      label: status?.status || 'Unknown', 
      color: 'text-yellow-500 dark:text-yellow-400', 
      dotBg: 'bg-yellow-500',
      glow: 'shadow-[0_0_8px_rgba(234,179,8,0.6)]'
    }
  }

  const statusConfig = getStatusConfig()

  return (
    <Card
      className={cn(
        'bg-white dark:bg-gray-800 border-gray-200 dark:border-gray-700 transition-all cursor-pointer hover:shadow-lg hover:border-gray-300 dark:hover:border-gray-500 hover:scale-[1.02]',
        isError && 'border-red-300 dark:border-red-800 hover:border-red-400 dark:hover:border-red-700'
      )}
      onClick={onClick}
    >
      <CardContent className="p-4">
        <div className="flex justify-between items-start">
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-gray-900 dark:text-white truncate">{instance.name}</h3>
            <div className="flex gap-2 items-center mt-2">
              <Badge variant="secondary" className="text-xs text-gray-700 dark:text-gray-300 bg-gray-200 dark:bg-gray-700 hover:bg-gray-300 dark:hover:bg-gray-600">
                {instance.gowa_version || 'latest'}
              </Badge>
              {/* Status indicator with label and glow */}
              <div className="flex items-center gap-1.5">
                <span className={cn(
                  'w-2 h-2 rounded-full transition-shadow duration-300',
                  statusConfig.dotBg,
                  statusConfig.glow,
                  isRunning && 'animate-pulse'
                )} />
                <span className={cn('text-xs font-medium', statusConfig.color)}>{statusConfig.label}</span>
              </div>
            </div>
          </div>
          <div className="flex gap-2 items-center" onClick={(e) => e.stopPropagation()}>
            {/* Power toggle switch */}
            <Switch
              checked={isRunning}
              onCheckedChange={(checked) => {
                if (checked) {
                  startMutation.mutate()
                } else {
                  stopMutation.mutate()
                }
              }}
              disabled={startMutation.isPending || stopMutation.isPending}
              className="data-[state=checked]:bg-green-600 data-[state=checked]:shadow-[0_0_12px_rgba(34,197,94,0.4)]"
            />
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
