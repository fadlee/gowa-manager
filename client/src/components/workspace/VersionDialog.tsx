import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUpCircle, Loader2, RotateCcw } from 'lucide-react'
import { apiClient } from '../../lib/api'
import { useLatestGowaVersion, useNewestInstalledGowaVersion, type InstanceHealth } from '../../lib/instanceHealth'
import type { Instance } from '../../types'
import { Button } from '../ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '../ui/dialog'
import { toast } from '../ui/use-toast'
import { VersionSelector } from '../VersionSelector'
import { versionLabel } from './model'

interface VersionDialogProps {
  instance: Instance
  health: InstanceHealth
  open: boolean
  onOpenChange: (open: boolean) => void
}

// VersionDialog changes which GOWA version an instance runs. It opens from
// the version badge in the instance header and from the update notice in
// the summary.
export function VersionDialog({ instance, health, open, onOpenChange }: VersionDialogProps) {
  const queryClient = useQueryClient()
  const current = instance.gowa_version || 'latest'
  const [selected, setSelected] = useState(current)
  const newestInstalled = useNewestInstalledGowaVersion()
  const latestRelease = useLatestGowaVersion()
  const { data: installed = [] } = useQuery({
    queryKey: ['versions', 'installed'],
    queryFn: () => apiClient.getInstalledVersions(),
    enabled: open,
  })

  useEffect(() => {
    if (open) setSelected(current)
  }, [open, current])

  const running = health.state === 'running'
  const changed = selected !== current
  const selectedInstalled = selected === 'latest' || installed.some(version => version.version === selected && version.installed)
  // An instance set to latest keeps running the version it started with
  // until it restarts, even after a newer one is installed.
  const restartPicksUpNewer = !changed && running && health.followsLatest && !!newestInstalled && !!health.version && health.version !== newestInstalled

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['instances'] })
    queryClient.invalidateQueries({ queryKey: ['instance-statuses'] })
  }

  const save = useMutation({
    mutationFn: async ({ restart }: { restart: boolean }) => {
      if (changed) await apiClient.updateInstance(instance.id, { gowa_version: selected })
      if (restart) await apiClient.restartInstance(instance.id)
      return restart
    },
    onSuccess: (restarted) => {
      refresh()
      onOpenChange(false)
      const target = selected === 'latest' ? `latest${newestInstalled ? ` (${newestInstalled})` : ''}` : selected
      toast({
        title: changed ? `Version set to ${target}` : 'Instance restarted',
        description: restarted
          ? `${instance.name} restarted with the new version.`
          : running
            ? `${instance.name} uses it after its next restart.`
            : `${instance.name} uses it the next time it starts.`,
        variant: 'success',
      })
    },
    onError: (error) => {
      refresh()
      toast({ title: 'Could not change the version', description: error instanceof Error ? error.message : undefined, variant: 'error' })
    },
  })

  return (
    <Dialog open={open} onOpenChange={(next) => !save.isPending && onOpenChange(next)}>
      <DialogContent className="sm:max-w-[460px]">
        <DialogHeader>
          <DialogTitle>GOWA version</DialogTitle>
          <DialogDescription>
            {instance.name} {running ? 'runs' : 'uses'} <span className="font-mono font-medium text-gray-900 dark:text-gray-100">{versionLabel(health)}</span>
            {health.followsLatest ? ', set to follow the latest installed version.' : ', a fixed version.'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          {health.outdated && latestRelease && (
            <p className="flex items-start gap-2 rounded-md bg-indigo-50 px-3 py-2 text-sm text-indigo-800 dark:bg-indigo-950/40 dark:text-indigo-200">
              <ArrowUpCircle className="mt-0.5 h-4 w-4 shrink-0" />
              GOWA {latestRelease} is available.
            </p>
          )}
          <VersionSelector value={selected} onChange={setSelected} disabled={save.isPending} />
          {selected === 'latest' && newestInstalled && (
            <p className="text-xs text-gray-500 dark:text-gray-400">
              Starts with the newest installed version, now {newestInstalled}.
            </p>
          )}
          {restartPicksUpNewer && (
            <p className="text-xs text-amber-700 dark:text-amber-400">
              {newestInstalled} is installed. Restart to run it.
            </p>
          )}
        </div>

        <DialogFooter className="gap-2 sm:gap-0">
          <Button type="button" variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={save.isPending}>
            Cancel
          </Button>
          {restartPicksUpNewer ? (
            <Button type="button" size="sm" onClick={() => save.mutate({ restart: true })} disabled={save.isPending}>
              {save.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RotateCcw className="mr-2 h-4 w-4" />}
              Restart now
            </Button>
          ) : running ? (
            <>
              <Button type="button" variant="outline" size="sm" onClick={() => save.mutate({ restart: false })} disabled={!changed || !selectedInstalled || save.isPending}>
                Save, restart later
              </Button>
              <Button type="button" size="sm" onClick={() => save.mutate({ restart: true })} disabled={!changed || !selectedInstalled || save.isPending}>
                {save.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RotateCcw className="mr-2 h-4 w-4" />}
                Save & restart
              </Button>
            </>
          ) : (
            <Button type="button" size="sm" onClick={() => save.mutate({ restart: false })} disabled={!changed || !selectedInstalled || save.isPending}>
              {save.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Save
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
