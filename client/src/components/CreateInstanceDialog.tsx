import { useState } from 'react'
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
import { Loader2 } from 'lucide-react'

interface CreateInstanceDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateInstanceDialog({ open, onOpenChange }: CreateInstanceDialogProps) {
  const [name, setName] = useState('')
  const [config, setConfig] = useState('')
  const [errors, setErrors] = useState<{name?: string, config?: string}>({})
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
    setConfig('')
    setErrors({})
    onOpenChange(false)
  }

  const validateForm = () => {
    const newErrors: {name?: string, config?: string} = {}
    
    if (name.trim() && (name.trim().length < 1 || name.trim().length > 100)) {
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
    
    if (name.trim()) {
      data.name = name.trim()
    }
    
    if (config.trim()) {
      data.config = config.trim()
    }

    createMutation.mutate(data)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[425px]">
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

          <div className="space-y-2">
            <label htmlFor="config" className="text-sm font-medium text-gray-700">
              Configuration (optional)
            </label>
            <textarea
              id="config"
              placeholder='{"port": 8080, "args": ["--debug"]}'
              value={config}
              onChange={(e) => setConfig(e.target.value)}
              className={`flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 resize-none ${
                errors.config ? 'border-red-500' : 'border-gray-300 focus-visible:ring-blue-500'
              }`}
              rows={3}
            />
            {errors.config && (
              <p className="text-sm text-red-600">{errors.config}</p>
            )}
            <p className="text-xs text-gray-500">
              JSON format for configuration parameters
            </p>
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