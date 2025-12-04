import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import type { Instance, CliFlags, InstanceConfig } from '../types'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Loader2, Trash2, Eye, EyeOff, Code, ChevronDown, ChevronUp, Settings, AlertCircle } from 'lucide-react'
import { CliFlagsComponent } from './CliFlags/index'
import { VersionSelector } from './VersionSelector'

interface EditInstanceDialogProps {
  instance: Instance
  open: boolean
  onOpenChange: (open: boolean) => void
  showJsonViewInitial?: boolean
}

export function EditInstanceDialog({ instance, open, onOpenChange, showJsonViewInitial = false }: EditInstanceDialogProps) {
  const [name, setName] = useState('')
  const [version, setVersion] = useState('latest')
  const [flags, setFlags] = useState<CliFlags>({})
  const [errors, setErrors] = useState<{name?: string}>({})
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [showJsonView, setShowJsonView] = useState(showJsonViewInitial)
  const [showConfiguration, setShowConfiguration] = useState(true) // Expanded by default for edit
  const [jsonConfig, setJsonConfig] = useState('')
  const [jsonError, setJsonError] = useState<string | null>(null)
  const queryClient = useQueryClient()

  // Initialize form with instance data
  useEffect(() => {
    if (open && instance) {
      setName(instance.name)
      setVersion(instance.gowa_version || 'latest')
      try {
        const configObj: InstanceConfig = JSON.parse(instance.config)
        setJsonConfig(JSON.stringify(configObj, null, 2))

        // Extract flags from config if they exist
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
        setJsonConfig('{}')
      }
      setErrors({})
      setJsonError(null)
      setShowDeleteConfirm(false)
      setShowJsonView(false)
    }
  }, [open, instance])

  const updateMutation = useMutation({
    mutationFn: (data: { name?: string; config?: string; gowa_version?: string }) =>
      apiClient.updateInstance(instance.id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      queryClient.invalidateQueries({ queryKey: ['instance-status', instance.id] })
      handleClose()
    },
    onError: (error) => {
      console.error('Failed to update instance:', error)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteInstance(instance.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      handleClose()
    },
    onError: (error) => {
      console.error('Failed to delete instance:', error)
    },
  })

  const handleClose = () => {
    setErrors({})
    setShowDeleteConfirm(false)
    onOpenChange(false)
  }

  const validateForm = () => {
    const newErrors: {name?: string} = {}

    if (name.trim().length < 1 || name.trim().length > 100) {
      newErrors.name = 'Name must be between 1 and 100 characters'
    }

    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!validateForm()) {
      return
    }

    const data: { name?: string; config?: string; gowa_version?: string } = {}

    if (name.trim() !== instance.name) {
      data.name = name.trim()
    }

    if (version !== (instance.gowa_version || 'latest')) {
      data.gowa_version = version
    }

    // Build configuration
    let finalConfig: InstanceConfig

    if (showJsonView) {
      // Use manually edited JSON
      try {
        setJsonError(null)
        const parsed = JSON.parse(jsonConfig) as InstanceConfig
        finalConfig = parsed
      } catch (err) {
        setJsonError('Invalid JSON. Please fix the syntax and try again.')
        return
      }
    } else {
      // Use values from the form-driven flags
      finalConfig = {
        args: ['rest', '--port=PORT'],
        flags: flags
      }
      // Sync JSON preview to reflect current form state
      setJsonConfig(JSON.stringify(finalConfig, null, 2))
    }

    const normalizedConfig = JSON.stringify(finalConfig)
    if (normalizedConfig !== instance.config) {
      data.config = normalizedConfig
    }

    // Only update if there are actual changes
    if (Object.keys(data).length > 0) {
      updateMutation.mutate(data)
    } else {
      handleClose()
    }
  }

  const handleDelete = () => {
    deleteMutation.mutate()
  }

  if (showDeleteConfirm) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-[425px]">
          <DialogHeader>
            <DialogTitle>Delete Instance</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete "{instance.name}"? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setShowDeleteConfirm(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending && (
                <Loader2 className="mr-2 w-4 h-4 animate-spin" />
              )}
              Delete Instance
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[600px] max-h-[90vh] flex flex-col">
        <div className="overflow-y-auto flex-1">
          <DialogHeader>
            <DialogTitle>Edit Instance</DialogTitle>
            <DialogDescription>
              Modify the instance name, GOWA version, and configuration parameters.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleSubmit} className="space-y-4" id="editForm">
            <div className="space-y-2">
              <label htmlFor="edit-name" className="text-sm font-medium text-gray-700">
                Name
              </label>
              <Input
                id="edit-name"
                placeholder="Enter instance name..."
                value={name}
                onChange={(e) => setName(e.target.value)}
                className={errors.name ? 'border-red-500' : ''}
                required
              />
              {errors.name && (
                <p className="text-sm text-red-600">{errors.name}</p>
              )}
            </div>

            <VersionSelector
              value={version}
              onChange={setVersion}
              disabled={updateMutation.isPending}
            />

            {version !== (instance.gowa_version || 'latest') && (
              <div className="p-3 bg-yellow-50 rounded-md border border-yellow-200">
                <div className="flex gap-2 items-center text-sm text-yellow-800">
                  <AlertCircle className="w-4 h-4" />
                  <span>
                    <strong>Version Change:</strong> Changing the GOWA version will require restarting the instance to take effect.
                  </span>
                </div>
              </div>
            )}

            {/* Collapsible Configuration Section */}
            <div className="space-y-3">
              <div className="rounded-md border border-gray-200">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowConfiguration(!showConfiguration)}
                  className="flex justify-between items-center p-3 w-full h-auto font-medium text-gray-700 hover:bg-gray-50"
                >
                  <div className="flex gap-2 items-center">
                    <Settings className="w-4 h-4" />
                    <span>Configuration</span>
                  </div>
                  {showConfiguration ? (
                    <ChevronUp className="w-4 h-4" />
                  ) : (
                    <ChevronDown className="w-4 h-4" />
                  )}
                </Button>

                {showConfiguration && (
                  <div className="p-4 space-y-4 border-t border-gray-200">
                    <div className="flex justify-between items-center">
                      <span className="text-sm text-gray-600">
                        GOWA settings for this instance
                      </span>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => setShowJsonView(!showJsonView)}
                        className="flex gap-1 items-center px-2 h-7 text-xs"
                      >
                        {showJsonView ? (
                          <>
                            <EyeOff className="w-3 h-3" />
                            Hide JSON
                          </>
                        ) : (
                          <>
                            <Eye className="w-3 h-3" />
                            View JSON
                          </>
                        )}
                      </Button>
                    </div>

                    {showJsonView ? (
                      <div className="space-y-2">
                        <div className="flex gap-2 items-center mb-2">
                          <Code className="w-4 h-4 text-gray-500" />
                          <span className="text-xs text-gray-500">JSON Configuration (Editable)</span>
                        </div>
                        <textarea
                          value={jsonConfig}
                          onChange={(e) => setJsonConfig(e.target.value)}
                          className={`w-full bg-gray-50 p-3 rounded-md overflow-x-auto text-xs font-mono border ${jsonError ? 'border-red-300' : 'border-gray-200'}`}
                          spellCheck={false}
                        />
                        {jsonError && (
                          <p className="mt-1 text-xs text-red-600">{jsonError}</p>
                        )}
                        <p className="mt-1 text-xs text-gray-500">
                          You can edit the raw JSON configuration directly. It should match the expected shape of InstanceConfig.
                        </p>
                      </div>
                    ) : (
                      <div>
                        <CliFlagsComponent flags={flags} onChange={setFlags} />
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>

          </form>
        </div>
        <DialogFooter className="flex-col gap-2 border-t sm:flex-row">
          <div className="flex flex-1 gap-2">
            <Button
              type="button"
              variant="destructive"
              size="sm"
              onClick={() => setShowDeleteConfirm(true)}
              className="flex gap-2 items-center"
            >
              <Trash2 className="w-4 h-4" />
              Delete
            </Button>
          </div>
          <div className="flex gap-2">
            <Button type="button" variant="outline" size="sm" onClick={handleClose}>
              Cancel
            </Button>
            <Button
              form="editForm"
              type="submit"
              size="sm"
              disabled={updateMutation.isPending}
            >
              {updateMutation.isPending && (
                <Loader2 className="mr-2 w-4 h-4 animate-spin" />
              )}
              Save Changes
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
