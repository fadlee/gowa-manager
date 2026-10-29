import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Lock, Trash2 } from 'lucide-react'
import { apiClient } from '../lib/api'
import { useAuth } from '../lib/auth'
import { Button } from '../components/ui/button'
import { Input } from '../components/ui/input'
import { Badge } from '../components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { CopyButton } from '../components/ui/shadcn-io/copy-button'
import { toast } from '../components/ui/use-toast'

const formatDate = (value: string | null) => (value ? new Date(value).toLocaleString() : 'Never')

function ChangePasswordCard() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirm, setConfirm] = useState('')

  const mutation = useMutation({
    mutationFn: () => apiClient.changePassword(currentPassword, newPassword),
    onSuccess: () => {
      setCurrentPassword('')
      setNewPassword('')
      setConfirm('')
      toast({ title: 'Password changed', description: 'Your other sessions have been signed out.', variant: 'success' })
    },
    onError: (error: Error) => toast({ title: 'Failed to change password', description: error.message, variant: 'error' }),
  })

  const mismatch = confirm !== '' && newPassword !== confirm

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg"><Lock className="w-5 h-5" />Change password</CardTitle>
        <CardDescription>Changing your password signs out your other sessions.</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-4 max-w-md"
          onSubmit={(e) => {
            e.preventDefault()
            if (!mismatch) mutation.mutate()
          }}
        >
          <label className="block space-y-1 text-sm font-medium text-gray-700 dark:text-gray-300">
            <span>Current password</span>
            <Input type="password" autoComplete="current-password" required value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm font-medium text-gray-700 dark:text-gray-300">
            <span>New password</span>
            <Input type="password" autoComplete="new-password" required minLength={8} value={newPassword} onChange={(e) => setNewPassword(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm font-medium text-gray-700 dark:text-gray-300">
            <span>Confirm new password</span>
            <Input type="password" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
          </label>
          {mismatch && <p className="text-sm text-red-600 dark:text-red-400">Passwords do not match</p>}
          <Button type="submit" disabled={mutation.isPending || mismatch}>
            {mutation.isPending ? 'Saving...' : 'Change password'}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function ApiKeysCard() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [createdKey, setCreatedKey] = useState<string | null>(null)

  const { data: keys, isLoading } = useQuery({
    queryKey: ['apiKeys'],
    queryFn: () => apiClient.getApiKeys(),
  })

  const createMutation = useMutation({
    mutationFn: () => apiClient.createApiKey(name),
    onSuccess: (data) => {
      setName('')
      setCreatedKey(data.key)
      queryClient.invalidateQueries({ queryKey: ['apiKeys'] })
    },
    onError: (error: Error) => toast({ title: 'Failed to create API key', description: error.message, variant: 'error' }),
  })

  const revokeMutation = useMutation({
    mutationFn: (id: number) => apiClient.revokeApiKey(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apiKeys'] })
      toast({ title: 'API key revoked', variant: 'success' })
    },
    onError: (error: Error) => toast({ title: 'Failed to revoke API key', description: error.message, variant: 'error' }),
  })

  const exampleUrl = `${window.location.origin}/api/instances`

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg"><KeyRound className="w-5 h-5" />API keys</CardTitle>
        <CardDescription>
          Use an API key to call the manager API from scripts. A key has the same access as your account, but cannot manage users or API keys.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <form
          className="flex flex-col gap-2 sm:flex-row max-w-xl"
          onSubmit={(e) => {
            e.preventDefault()
            createMutation.mutate()
          }}
        >
          <Input placeholder="Key name, e.g. CI deploy" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} aria-label="API key name" />
          <Button type="submit" disabled={createMutation.isPending}>{createMutation.isPending ? 'Generating...' : 'Generate key'}</Button>
        </form>

        {createdKey && (
          <div className="p-4 space-y-3 rounded-md border border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/30">
            <p className="text-sm font-medium text-amber-800 dark:text-amber-200">Copy this key now. It will not be shown again.</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 min-w-0 px-3 py-2 font-mono text-sm break-all bg-white rounded border border-gray-200 dark:bg-gray-900 dark:border-gray-700 text-gray-900 dark:text-gray-100">{createdKey}</code>
              <CopyButton content={createdKey} variant="ghost" aria-label="Copy API key" />
            </div>
            <pre className="overflow-x-auto p-3 text-xs bg-gray-900 rounded text-gray-100">{`curl -H "Authorization: Bearer ${createdKey}" ${exampleUrl}`}</pre>
            <Button variant="outline" size="sm" onClick={() => setCreatedKey(null)}>Done</Button>
          </div>
        )}

        {isLoading ? (
          <p className="text-sm text-gray-500 dark:text-gray-400">Loading keys...</p>
        ) : keys && keys.length > 0 ? (
          <ul className="divide-y divide-gray-200 dark:divide-gray-700 rounded-md border border-gray-200 dark:border-gray-700">
            {keys.map((key) => (
              <li key={key.id} className="flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <p className="font-medium text-gray-900 dark:text-white truncate">{key.name}</p>
                  <p className="text-xs text-gray-500 dark:text-gray-400">
                    <code>{key.prefix}…</code> · Created {formatDate(key.createdAt)} · Last used {formatDate(key.lastUsedAt)}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={revokeMutation.isPending}
                  onClick={() => {
                    if (window.confirm(`Revoke API key "${key.name}"? Scripts using it will stop working.`)) revokeMutation.mutate(key.id)
                  }}
                >
                  <Trash2 className="mr-1 w-4 h-4" />Revoke
                </Button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-gray-500 dark:text-gray-400">No API keys yet.</p>
        )}
      </CardContent>
    </Card>
  )
}

export function AccountPage() {
  const { user } = useAuth()

  return (
    <div className="px-4 py-8 mx-auto space-y-6 max-w-4xl sm:px-6 lg:px-8">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-2xl font-semibold text-gray-900 dark:text-white">My account</h2>
        {user && (
          <>
            <span className="text-gray-600 dark:text-gray-400">{user.username}</span>
            <Badge variant={user.role === 'superadmin' ? 'default' : 'secondary'}>{user.role}</Badge>
          </>
        )}
      </div>
      <ChangePasswordCard />
      <ApiKeysCard />
    </div>
  )
}
