import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
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
import { Loader2, Eye, EyeOff, Code, ChevronDown, ChevronUp, Settings } from 'lucide-react'
import { CliFlagsComponent } from './CliFlags/index'
import { VersionSelector } from './VersionSelector'
import type { CliFlags, InstanceConfig } from '../types'

interface CreateInstanceDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateInstanceDialog({ open, onOpenChange }: CreateInstanceDialogProps) {
  const [name, setName] = useState('')
  const [version, setVersion] = useState('latest')
  const [flags, setFlags] = useState<CliFlags>({
    accountValidation: true,
    os: 'GowaManager'
  })
  const [errors, setErrors] = useState<{name?: string}>({})
  const [showJsonView, setShowJsonView] = useState(false)
  const [showConfiguration, setShowConfiguration] = useState(false)
  const [jsonConfig, setJsonConfig] = useState('')
  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: (data: { name?: string; config?: string; gowa_version?: string }) =>
      apiClient.createInstance(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      handleClose()
    },
    onError: (error) => {
      console.error('Failed to create instance:', error)
    },
  })

  const handleClose = () => {
    setName('')
    setVersion('latest')
    setFlags({
      accountValidation: true,
      os: 'GowaManager'
    })
    setErrors({})
    setShowJsonView(false)
    setShowConfiguration(false)
    onOpenChange(false)
  }

  const validateForm = () => {
    const newErrors: {name?: string} = {}

    if (name.trim() && (name.trim().length < 1 || name.trim().length > 100)) {
      newErrors.name = 'Name must be between 1 and 100 characters'
    }

    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }

  // Update JSON view whenever flags change
  useEffect(() => {
    const config: InstanceConfig = {
      args: ['rest', '--port=PORT'],
      flags: flags
    }
    setJsonConfig(JSON.stringify(config, null, 2))
  }, [flags])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!validateForm()) {
      return
    }

    const data: { name?: string; config?: string; gowa_version?: string } = {}

    if (name.trim()) {
      data.name = name.trim()
    }

    data.gowa_version = version

    // Build configuration
    let finalConfig: InstanceConfig = {
      args: ['rest', '--port=PORT'],
      flags: flags
    }

    // Update the JSON view
    setJsonConfig(JSON.stringify(finalConfig, null, 2))

    data.config = JSON.stringify(finalConfig)

    createMutation.mutate(data)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[600px] max-h-[90vh] flex flex-col">
        <div className="overflow-y-auto flex-1">
          <DialogHeader>
            <DialogTitle>Create New Instance</DialogTitle>
            <DialogDescription>
              Create a new application instance. A random name will be generated if none is provided.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleSubmit} className="space-y-4" id="createForm">
            <div className="space-y-2">
              <label htmlFor="name" className="text-sm font-medium text-gray-700">
                Name (optional)
              </label>
              <Input
                id="name"
                placeholder="Enter instance name..."
                value={name}
                onChange={(e) => setName(e.target.value)}
                className={errors.name ? 'border-red-500' : ''}
              />
              {errors.name && (
                <p className="text-sm text-red-600">{errors.name}</p>
              )}
            </div>

            <VersionSelector
              value={version}
              onChange={setVersion}
              disabled={createMutation.isPending}
            />

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
                    <span className="px-2 py-1 text-xs text-gray-500 bg-gray-100 rounded">
                      Optional
                    </span>
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
                        Advanced GOWA settings
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
                          <span className="text-xs text-gray-500">JSON Configuration (Read-only)</span>
                        </div>
                        <pre className="overflow-x-auto p-3 font-mono text-xs bg-gray-50 rounded-md border border-gray-200">
                          {jsonConfig}
                        </pre>
                        <p className="mt-1 text-xs text-gray-500">
                          This is the raw configuration that will be saved. Use the form above to make changes.
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
        <DialogFooter className="border-t">
          <Button type="button" variant="outline" onClick={handleClose}>
            Cancel
          </Button>
          <Button
            form="createForm"
            type="submit"
            disabled={createMutation.isPending}
          >
            {createMutation.isPending && (
              <Loader2 className="mr-2 w-4 h-4 animate-spin" />
            )}
            Create Instance
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
