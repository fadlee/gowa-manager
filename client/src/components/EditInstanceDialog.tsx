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
import { Loader2, Trash2, Eye, EyeOff, Code } from 'lucide-react'
import { CliFlagsComponent } from './CliFlags/index'

interface EditInstanceDialogProps {
  instance: Instance
  open: boolean
  onOpenChange: (open: boolean) => void
  showJsonViewInitial?: boolean
}

export function EditInstanceDialog({ instance, open, onOpenChange, showJsonViewInitial = false }: EditInstanceDialogProps) {
  const [name, setName] = useState('')
  const [flags, setFlags] = useState<CliFlags>({})
  const [errors, setErrors] = useState<{name?: string}>({})
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [showJsonView, setShowJsonView] = useState(showJsonViewInitial)
  const [jsonConfig, setJsonConfig] = useState('')
  const queryClient = useQueryClient()

  // Initialize form with instance data
  useEffect(() => {
    if (open && instance) {
      setName(instance.name)
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
      setShowDeleteConfirm(false)
      setShowJsonView(false)
    }
  }, [open, instance])

  const updateMutation = useMutation({
    mutationFn: (data: { name?: string; config?: string }) => 
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

    const data: { name?: string; config?: string } = {}
    
    if (name.trim() !== instance.name) {
      data.name = name.trim()
    }
    
    // Build configuration
    let finalConfig: InstanceConfig = {
      args: ['rest', '--port=PORT'],
      flags: flags
    }
    
    // Update the JSON view
    setJsonConfig(JSON.stringify(finalConfig, null, 2))
    
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
              onClick={() => setShowDeleteConfirm(false)}
            >
              Cancel
            </Button>
            <Button 
              type="button" 
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending && (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
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
      <DialogContent className="sm:max-w-[600px] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Edit Instance</DialogTitle>
          <DialogDescription>
            Modify the instance name and configuration parameters.
          </DialogDescription>
        </DialogHeader>
        
        <form onSubmit={handleSubmit} className="space-y-4">
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

          <DialogFooter className="flex-col sm:flex-row gap-2">
            <div className="flex gap-2 flex-1">
              <Button 
                type="button" 
                variant="destructive" 
                onClick={() => setShowDeleteConfirm(true)}
                className="flex items-center gap-2"
              >
                <Trash2 className="h-4 w-4" />
                Delete
              </Button>
            </div>
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={handleClose}>
                Cancel
              </Button>
              <Button 
                type="submit" 
                disabled={updateMutation.isPending}
              >
                {updateMutation.isPending && (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                )}
                Save Changes
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}