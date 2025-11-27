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
import { VersionSelector } from './VersionSelector'
import { toast } from './ui/use-toast'
import type { InstanceConfig } from '../types'

interface CreateInstanceDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateInstanceDialog({ open, onOpenChange }: CreateInstanceDialogProps) {
  const [name, setName] = useState('')
  const [version, setVersion] = useState('latest')
  const [errors, setErrors] = useState<{name?: string}>({})
  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: (data: { name?: string; config?: string; gowa_version?: string }) =>
      apiClient.createInstance(data),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      toast({ title: 'Instance created', description: `${data.name} has been created successfully.`, variant: 'success' })
      handleClose()
    },
    onError: (error) => {
      console.error('Failed to create instance:', error)
      toast({ title: 'Failed to create instance', description: error.message, variant: 'error' })
    },
  })

  const handleClose = () => {
    setName('')
    setVersion('latest')
    setErrors({})
    onOpenChange(false)
  }

  const validateForm = () => {
    const newErrors: {name?: string} = {}

    if (name.trim() && name.trim().length > 100) {
      newErrors.name = 'Name must be 100 characters or less'
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

    if (name.trim()) {
      data.name = name.trim()
    }

    data.gowa_version = version

    // Default configuration
    const defaultConfig: InstanceConfig = {
      args: ['rest', '--port=PORT'],
      flags: {
        accountValidation: true,
        os: 'GowaManager'
      }
    }

    data.config = JSON.stringify(defaultConfig)

    createMutation.mutate(data)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>New Instance</DialogTitle>
          <DialogDescription>
            Create a new GOWA instance. Name is optional.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4" id="createForm">
          <div className="space-y-2">
            <label htmlFor="name" className="text-sm font-medium text-gray-300">
              Name (optional)
            </label>
            <Input
              id="name"
              placeholder="Enter instance name..."
              value={name}
              onChange={(e) => setName(e.target.value)}
              className={`bg-gray-800 border-gray-700 text-white ${errors.name ? 'border-red-500' : ''}`}
            />
            {errors.name && (
              <p className="text-sm text-red-400">{errors.name}</p>
            )}
          </div>

          <VersionSelector
            value={version}
            onChange={setVersion}
            disabled={createMutation.isPending}
          />
        </form>

        <DialogFooter>
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
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
