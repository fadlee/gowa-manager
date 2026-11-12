import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../../lib/api'
import { Button } from '../ui/button'
import { Loader2, Save, AlertCircle, RotateCcw } from 'lucide-react'
import { CliFlagsComponent } from '../CliFlags/index'
import { toast } from '../ui/use-toast'
import type { Instance, CliFlags, InstanceConfig } from '../../types'

interface SettingsSectionProps {
  instance: Instance
}

const DEFAULT_FLAGS: CliFlags = { accountValidation: true, os: 'GowaManager' }

function flagsOf(instance: Instance): CliFlags {
  try {
    const config: InstanceConfig = JSON.parse(instance.config)
    return config.flags ?? DEFAULT_FLAGS
  } catch {
    return DEFAULT_FLAGS
  }
}

// SettingsSection edits the instance's GOWA command-line configuration.
// The name is edited in the panel header and the version in the version
// dialog, which opens from the version badge.
export function SettingsSection({ instance }: SettingsSectionProps) {
  const [flags, setFlags] = useState<CliFlags>(() => flagsOf(instance))
  const [restartRequired, setRestartRequired] = useState(false)
  const queryClient = useQueryClient()

  useEffect(() => {
    setFlags(flagsOf(instance))
  }, [instance.config])

  const buildConfig = () => JSON.stringify({ args: ['rest', '--port=PORT'], flags } satisfies InstanceConfig)
  const hasChanges = JSON.stringify(flags) !== JSON.stringify(flagsOf(instance))

  const updateMutation = useMutation({
    mutationFn: (config: string) => apiClient.updateInstance(instance.id, { config }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-statuses'] })
      if (instance.status === 'running') setRestartRequired(true)
      toast({ title: 'Settings saved', description: 'Your changes have been saved successfully.', variant: 'success' })
    },
    onError: (error) => {
      console.error('Failed to update instance:', error)
      toast({ title: 'Failed to save settings', description: error.message, variant: 'error' })
    },
  })

  const restartMutation = useMutation({
    mutationFn: () => apiClient.restartInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-statuses'] })
      setRestartRequired(false)
      toast({ title: 'Instance restarted', description: 'The instance has been restarted with the latest settings.', variant: 'success' })
    },
    onError: (error) => {
      console.error('Failed to restart instance:', error)
      toast({ title: 'Failed to restart instance', description: error.message, variant: 'error' })
    },
  })

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Settings</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">Command-line options GOWA starts with. Rename the instance or change its version from the header.</p>
        </div>
        <Button onClick={() => updateMutation.mutate(buildConfig())} disabled={!hasChanges || updateMutation.isPending} size="sm" className="shrink-0">
          {updateMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
          Save Changes
        </Button>
      </div>

      {restartRequired && (
        <div className="flex flex-col gap-3 rounded-lg border border-blue-300 bg-blue-50 p-4 dark:border-blue-800 dark:bg-blue-950/40 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-start gap-3">
            <AlertCircle className="mt-0.5 h-5 w-5 text-blue-600 dark:text-blue-400" />
            <div>
              <p className="text-sm font-medium text-blue-900 dark:text-blue-100">Changes saved. Restart instance to apply.</p>
              <p className="text-xs text-blue-700 dark:text-blue-300">The new configuration is used after the process restarts.</p>
            </div>
          </div>
          <Button type="button" size="sm" onClick={() => restartMutation.mutate()} disabled={restartMutation.isPending || updateMutation.isPending} className="sm:shrink-0">
            {restartMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RotateCcw className="mr-2 h-4 w-4" />}
            Restart now
          </Button>
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-gray-100 p-4 dark:border-gray-700 dark:bg-gray-800">
        <CliFlagsComponent flags={flags} onChange={setFlags} gowaVersion={instance.gowa_version || 'latest'} />
      </div>
    </div>
  )
}
