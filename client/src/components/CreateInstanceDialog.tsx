import { useState, useEffect, useRef } from 'react'
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
import { Switch } from './ui/switch'
import { CopyButton } from './ui/shadcn-io/copy-button'
import { Loader2, RefreshCw } from 'lucide-react'
import { VersionSelector } from './VersionSelector'
import { BasicAuthWarning, useBasicAuthRequirement } from './BasicAuthRequirement'
import { toast } from './ui/use-toast'
import { generateInstanceName, generatePassword, generateUsername } from '../lib/utils'
import { buildCreateInstanceConfig } from '../lib/instanceConfig'
import type { BasicAuthPair } from '../types'

const newCredentials = (): BasicAuthPair => ({ username: generateUsername(), password: generatePassword() })

interface CreateInstanceDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateInstanceDialog({ open, onOpenChange }: CreateInstanceDialogProps) {
  const [name, setName] = useState('')
  const [version, setVersion] = useState('latest')
  const [errors, setErrors] = useState<{name?: string}>({})
  // null follows the default: on when the selected GOWA version needs
  // Basic Auth to start (v9.8.0+). A toggle by the user overrides it.
  const [generateAuthChoice, setGenerateAuthChoice] = useState<boolean | null>(null)
  const [credentials, setCredentials] = useState<BasicAuthPair>(newCredentials)
  const { required: authRequired } = useBasicAuthRequirement(version)
  const generateAuth = generateAuthChoice ?? authRequired
  const queryClient = useQueryClient()
  const nameInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setName(generateInstanceName())
      setCredentials(newCredentials())
      // Select the generated name so the user can immediately type
      // to replace it if they want a custom name. The small delay
      // lets the Radix Dialog finish its open animation.
      const timer = setTimeout(() => nameInputRef.current?.select(), 50)
      return () => clearTimeout(timer)
    }
  }, [open])

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
    setGenerateAuthChoice(null)
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

    data.config = JSON.stringify(buildCreateInstanceConfig(generateAuth ? credentials : null))

    createMutation.mutate(data)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>New Instance</DialogTitle>
          <DialogDescription>
            Create a new GOWA instance. A random name is generated — edit if you want a custom one.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4" id="createForm">
          <div className="space-y-2">
            <label htmlFor="name" className="text-sm font-medium text-gray-700 dark:text-gray-300">
              Name
            </label>
            <Input
              id="name"
              ref={nameInputRef}
              placeholder="Enter instance name..."
              value={name}
              onChange={(e) => setName(e.target.value)}
              className={errors.name ? 'border-red-500' : ''}
            />
            {errors.name && (
              <p className="text-sm text-red-500 dark:text-red-400">{errors.name}</p>
            )}
          </div>

          <VersionSelector
            value={version}
            onChange={setVersion}
            disabled={createMutation.isPending}
          />

          <div className="space-y-2">
            <div className="flex gap-3 justify-between items-start">
              <div className="space-y-0.5">
                <label htmlFor="generate-basic-auth" className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  Generate Basic Auth credentials
                </label>
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  Protects the instance's GOWA API and UI. You can change them later in Settings.
                </p>
              </div>
              <Switch
                id="generate-basic-auth"
                checked={generateAuth}
                onCheckedChange={setGenerateAuthChoice}
                disabled={createMutation.isPending}
              />
            </div>
            {generateAuth && (
              <div className="flex gap-2 items-center p-2 bg-gray-100 rounded-md dark:bg-gray-800">
                <code className="flex-1 min-w-0 font-mono text-sm break-all text-gray-900 dark:text-gray-100">
                  {credentials.username}:{credentials.password}
                </code>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setCredentials(newCredentials())}
                  className="p-0 w-7 h-7 shrink-0"
                  aria-label="Generate new credentials"
                >
                  <RefreshCw className="w-4 h-4" />
                </Button>
                <CopyButton content={`${credentials.username}:${credentials.password}`} variant="ghost" aria-label="Copy credentials" />
              </div>
            )}
          </div>

          {!generateAuth && (
            <BasicAuthWarning version={version}>
              <p>Turn on "Generate Basic Auth credentials", or add Basic Auth in the instance's Settings before starting it.</p>
            </BasicAuthWarning>
          )}
        </form>

        <DialogFooter>
          <Button type="button" variant="outline" size="sm" onClick={handleClose}>
            Cancel
          </Button>
          <Button
            form="createForm"
            type="submit"
            size="sm"
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
