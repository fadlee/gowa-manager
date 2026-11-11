import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from './api'
import { hasNewerVersion } from './version'
import { toast } from '../components/ui/use-toast'
import type { Instance, InstanceStatus } from '../types'

export const STATUSES_QUERY_KEY = ['instance-statuses'] as const

// useInstanceStatuses polls the batch status endpoint once for the whole list
// and returns the statuses keyed by instance ID.
export function useInstanceStatuses(enabled = true) {
  const query = useQuery({
    queryKey: STATUSES_QUERY_KEY,
    queryFn: () => apiClient.getInstanceStatuses(),
    refetchInterval: 5000,
    staleTime: 2000,
    enabled,
  })
  const byId = new Map<number, InstanceStatus>()
  for (const status of query.data ?? []) byId.set(status.id, status)
  return { ...query, byId }
}

export function useLatestGowaVersion(): string | undefined {
  const { data = [] } = useQuery({
    queryKey: ['versions', 'available'],
    queryFn: () => apiClient.getAvailableVersions(5),
    staleTime: 5 * 60 * 1000,
  })
  return data.find(version => version.version !== 'latest' && version.isLatest)?.version
}

export type HealthState = 'running' | 'stopped' | 'error' | 'starting' | 'unknown'

export interface InstanceHealth {
  state: HealthState
  // needsLogin is true when the instance runs but no WhatsApp device is paired.
  needsLogin: boolean
  needsAttention: boolean
  outdated: boolean
  devices: number | undefined
  errorMessage: string | null
}

export function getInstanceHealth(instance: Instance, status: InstanceStatus | undefined, latestVersion?: string): InstanceHealth {
  const raw = (status?.status || instance.status || 'unknown').toLowerCase()
  const state: HealthState = raw === 'running' || raw === 'stopped' || raw === 'error' || raw === 'starting' ? raw : 'unknown'
  const devices = status?.devices?.count
  const needsLogin = state === 'running' && devices === 0
  return {
    state,
    needsLogin,
    needsAttention: state === 'error' || needsLogin,
    outdated: hasNewerVersion(instance.gowa_version, latestVersion),
    devices,
    errorMessage: status?.error_message || instance.error_message || null,
  }
}

export type InstanceAction = 'start' | 'stop' | 'restart'

const actionLabels: Record<InstanceAction, { done: string; fail: string }> = {
  start: { done: 'started', fail: 'start' },
  stop: { done: 'stopped', fail: 'stop' },
  restart: { done: 'restarted', fail: 'restart' },
}

function runAction(action: InstanceAction, id: number) {
  if (action === 'start') return apiClient.startInstance(id)
  if (action === 'stop') return apiClient.stopInstance(id)
  return apiClient.restartInstance(id)
}

// useInstanceActions runs start/stop/restart for one or many instances and
// refreshes every status query afterwards.
export function useInstanceActions() {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: async ({ action, instances }: { action: InstanceAction; instances: Instance[] }) => {
      const results = await Promise.allSettled(instances.map(instance => runAction(action, instance.id)))
      return { action, instances, results }
    },
    onSuccess: ({ action, instances, results }) => {
      const failed = results.filter(result => result.status === 'rejected') as PromiseRejectedResult[]
      if (instances.length === 1) {
        const [instance] = instances
        if (failed.length) {
          toast({ title: `Failed to ${actionLabels[action].fail} ${instance.name}`, description: String(failed[0].reason?.message ?? failed[0].reason), variant: 'error' })
        } else {
          toast({ title: `${instance.name} ${actionLabels[action].done}`, variant: action === 'stop' ? 'default' : 'success' })
        }
      } else if (failed.length) {
        toast({ title: `${instances.length - failed.length} of ${instances.length} instances ${actionLabels[action].done}`, description: `${failed.length} failed. Check their logs for details.`, variant: 'error' })
      } else {
        toast({ title: `${instances.length} instances ${actionLabels[action].done}`, variant: action === 'stop' ? 'default' : 'success' })
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: STATUSES_QUERY_KEY })
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status'] })
    },
  })
  return {
    run: (action: InstanceAction, instances: Instance[]) => mutation.mutate({ action, instances }),
    isPending: mutation.isPending,
    pendingIds: mutation.isPending ? new Set(mutation.variables?.instances.map(instance => instance.id)) : new Set<number>(),
  }
}
