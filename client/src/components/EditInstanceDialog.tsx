import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import type { Instance } from '../types'
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
import { Loader2, Trash2 } from 'lucide-react'

interface EditInstanceDialogProps {
  instance: Instance
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function EditInstanceDialog({ instance, open, onOpenChange }: EditInstanceDialogProps) {
  const [name, setName] = useState('')
  const [config, setConfig] = useState('')
  const [errors, setErrors] = useState<{name?: string, config?: string}>({})
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const queryClient = useQueryClient()

  // Initialize form with instance data
  useEffect(() => {
    if (open && instance) {
      setName(instance.name)
      try {
        const configObj = JSON.parse(instance.config)
        setConfig(JSON.stringify(configObj, null, 2))
      } catch {
        setConfig(instance.config || '')
      }
      setErrors({})
      setShowDeleteConfirm(false)
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
    const newErrors: {name?: string, config?: string} = {}
    
    if (name.trim().length < 1 || name.trim().length > 100) {
      newErrors.name = 'Name must be between 1 and 100 characters'
    }
    
    if (config.trim()) {
      try {
        JSON.parse(config)
      } catch {
        newErrors.config = 'Config must be valid JSON'
      }
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
    
    const trimmedConfig = config.trim()
    try {
      const normalizedConfig = trimmedConfig ? JSON.stringify(JSON.parse(trimmedConfig)) : '{}'
      if (normalizedConfig !== instance.config) {
        data.config = normalizedConfig
      }
    } catch {
      // This should not happen due to validation, but just in case
      return
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
      <DialogContent className="sm:max-w-[425px]">
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

          <div className="space-y-2">
            <label htmlFor="edit-config" className="text-sm font-medium text-gray-700">
              Configuration
            </label>
            <textarea
              id="edit-config"
              placeholder='{"port": 8080, "args": ["--debug"]}'
              value={config}
              onChange={(e) => setConfig(e.target.value)}
              className={`flex min-h-[100px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 resize-none font-mono ${
                errors.config ? 'border-red-500' : 'border-gray-300 focus-visible:ring-blue-500'
              }`}
              rows={4}
            />
            {errors.config && (
              <p className="text-sm text-red-600">{errors.config}</p>
            )}
            <p className="text-xs text-gray-500">
              JSON format for configuration parameters
            </p>
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