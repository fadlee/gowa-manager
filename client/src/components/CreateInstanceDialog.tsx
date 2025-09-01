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
import { Loader2, Eye, EyeOff, Code } from 'lucide-react'
import { CliFlagsComponent } from './CliFlags/index'
import type { CliFlags, InstanceConfig } from '../types'

interface CreateInstanceDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateInstanceDialog({ open, onOpenChange }: CreateInstanceDialogProps) {
  const [name, setName] = useState('')
  const [flags, setFlags] = useState<CliFlags>({
    accountValidation: true,
    os: 'GowaManager'
  })
  const [errors, setErrors] = useState<{name?: string}>({})
  const [showJsonView, setShowJsonView] = useState(false)
  const [jsonConfig, setJsonConfig] = useState('')
  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: (data: { name?: string; config?: string }) => 
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
    setFlags({
      accountValidation: true,
      os: 'GowaManager'
    })
    setErrors({})
    setShowJsonView(false)
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

    const data: { name?: string; config?: string } = {}
    
    if (name.trim()) {
      data.name = name.trim()
    }
    
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
      <DialogContent className="sm:max-w-[600px] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Create New Instance</DialogTitle>
          <DialogDescription>
            Create a new application instance. A random name will be generated if none is provided.
          </DialogDescription>
        </DialogHeader>
        
        <form onSubmit={handleSubmit} className="space-y-4">
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

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <label className="text-sm font-medium text-gray-700">
                Configuration
              </label>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setShowJsonView(!showJsonView)}
                className="flex items-center gap-1 h-7 px-2 text-xs"
              >
                {showJsonView ? (
                  <>
                    <EyeOff className="h-3 w-3" />
                    Hide JSON
                  </>
                ) : (
                  <>
                    <Eye className="h-3 w-3" />
                    View JSON
                  </>
                )}
              </Button>
            </div>
            
            {showJsonView ? (
              <div className="space-y-2">
                <div className="flex items-center gap-2 mb-2">
                  <Code className="h-4 w-4 text-gray-500" />
                  <span className="text-xs text-gray-500">JSON Configuration (Read-only)</span>
                </div>
                <pre className="bg-gray-50 p-3 rounded-md overflow-x-auto text-xs font-mono border border-gray-200 max-h-96">
                  {jsonConfig}
                </pre>
                <p className="text-xs text-gray-500 mt-1">
                  This is the raw configuration that will be saved. Use the form above to make changes.
                </p>
              </div>
            ) : (
              <div className="border border-gray-200 rounded-md p-4 max-h-96 overflow-y-auto">
                <CliFlagsComponent flags={flags} onChange={setFlags} />
              </div>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={handleClose}>
              Cancel
            </Button>
            <Button 
              type="submit" 
              disabled={createMutation.isPending}
            >
              {createMutation.isPending && (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              )}
              Create Instance
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}