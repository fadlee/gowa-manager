import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../../lib/api'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { Loader2, Save, AlertCircle } from 'lucide-react'
import { CliFlagsComponent } from '../CliFlags/index'
import { VersionSelector } from '../VersionSelector'
import { toast } from '../ui/use-toast'
import type { Instance, CliFlags, InstanceConfig } from '../../types'

interface SettingsSectionProps {
  instance: Instance
}

export function SettingsSection({ instance }: SettingsSectionProps) {
  const [name, setName] = useState('')
  const [version, setVersion] = useState('latest')
  const [flags, setFlags] = useState<CliFlags>({})
  const [errors, setErrors] = useState<{ name?: string }>({})
  const [hasChanges, setHasChanges] = useState(false)
  const queryClient = useQueryClient()

  // Initialize form with instance data
  useEffect(() => {
    setName(instance.name)
    setVersion(instance.gowa_version || 'latest')
    try {
      const configObj: InstanceConfig = JSON.parse(instance.config)
      if (configObj.flags) {
        setFlags(configObj.flags)
      } else {
        setFlags({
          accountValidation: true,
          os: 'GowaManager'
        })
      }
    } catch {
      setFlags({
        accountValidation: true,
        os: 'GowaManager'
      })
    }
    setHasChanges(false)
  }, [instance])

  // Track changes
  useEffect(() => {
    const originalConfig = JSON.parse(instance.config || '{}')
    const nameChanged = name !== instance.name
    const versionChanged = version !== (instance.gowa_version || 'latest')
    const flagsChanged = JSON.stringify(flags) !== JSON.stringify(originalConfig.flags || {})
    setHasChanges(nameChanged || versionChanged || flagsChanged)
  }, [name, version, flags, instance])

  const updateMutation = useMutation({
    mutationFn: (data: { name?: string; config?: string; gowa_version?: string }) =>
      apiClient.updateInstance(instance.id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
      setHasChanges(false)
      toast({ title: 'Settings saved', description: 'Your changes have been saved successfully.', variant: 'success' })
    },
    onError: (error) => {
      console.error('Failed to update instance:', error)
      toast({ title: 'Failed to save settings', description: error.message, variant: 'error' })
    },
  })

  const validateForm = () => {
    const newErrors: { name?: string } = {}
    if (name.trim().length < 1 || name.trim().length > 100) {
      newErrors.name = 'Name must be between 1 and 100 characters'
    }
    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }

  const handleSave = () => {
    if (!validateForm()) return

    const data: { name?: string; config?: string; gowa_version?: string } = {}

    if (name.trim() !== instance.name) {
      data.name = name.trim()
    }

    if (version !== (instance.gowa_version || 'latest')) {
      data.gowa_version = version
    }

    const finalConfig: InstanceConfig = {
      args: ['rest', '--port=PORT'],
      flags: flags
    }

    const normalizedConfig = JSON.stringify(finalConfig)
    if (normalizedConfig !== instance.config) {
      data.config = normalizedConfig
    }

    if (Object.keys(data).length > 0) {
      updateMutation.mutate(data)
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex justify-between items-center">
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Settings</h2>
        <Button
          onClick={handleSave}
          disabled={!hasChanges || updateMutation.isPending}
          className="bg-blue-600 hover:bg-blue-700"
        >
          {updateMutation.isPending ? (
            <Loader2 className="mr-2 w-4 h-4 animate-spin" />
          ) : (
            <Save className="mr-2 w-4 h-4" />
          )}
          Save Changes
        </Button>
      </div>

      {/* Name */}
      <div className="space-y-2">
        <label className="text-sm font-medium text-gray-700 dark:text-gray-300">Instance Name</label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={errors.name ? 'border-red-500' : ''}
          placeholder="Enter instance name..."
        />
        {errors.name && (
          <p className="text-sm text-red-400">{errors.name}</p>
        )}
      </div>

      {/* Version */}
      <div className="space-y-2">
        <label className="text-sm font-medium text-gray-700 dark:text-gray-300">GOWA Version</label>
        <div>
          <VersionSelector
            value={version}
            onChange={setVersion}
            disabled={updateMutation.isPending}
          />
        </div>
        {version !== (instance.gowa_version || 'latest') && (
          <div className="flex gap-2 items-center p-3 mt-2 bg-yellow-100 dark:bg-yellow-900/30 rounded-lg border border-yellow-300 dark:border-yellow-700">
            <AlertCircle className="w-4 h-4 text-yellow-600 dark:text-yellow-500" />
            <span className="text-sm text-yellow-800 dark:text-yellow-200">
              Changing version will require restarting the instance to take effect.
            </span>
          </div>
        )}
      </div>

      {/* Configuration */}
      <div className="space-y-4">
        <label className="text-sm font-medium text-gray-700 dark:text-gray-300">Configuration</label>
        <div className="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
          <CliFlagsComponent flags={flags} onChange={setFlags} />
        </div>
      </div>
    </div>
  )
}
