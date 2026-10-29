import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, Trash2, Users } from 'lucide-react'
import { apiClient } from '../lib/api'
import { useAuth } from '../lib/auth'
import { Button } from '../components/ui/button'
import { Input } from '../components/ui/input'
import { Badge } from '../components/ui/badge'
import { Switch } from '../components/ui/switch'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select'
import { toast } from '../components/ui/use-toast'
import type { User, UserRole, UpdateUserRequest } from '../types'

const USERS_KEY = ['users']

function CreateUserDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<UserRole>('user')

  const mutation = useMutation({
    mutationFn: () => apiClient.createUser({ username, password, role }),
    onSuccess: (user) => {
      queryClient.invalidateQueries({ queryKey: USERS_KEY })
      toast({ title: 'User created', description: `${user.username} can now sign in.`, variant: 'success' })
      setUsername('')
      setPassword('')
      setRole('user')
      onOpenChange(false)
    },
    onError: (error: Error) => toast({ title: 'Failed to create user', description: error.message, variant: 'error' }),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create user</DialogTitle>
          <DialogDescription>Share the username and initial password with the user. They can change it from My account.</DialogDescription>
        </DialogHeader>
        <form
          id="create-user-form"
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            mutation.mutate()
          }}
        >
          <label className="block space-y-1 text-sm font-medium text-gray-700 dark:text-gray-300">
            <span>Username</span>
            <Input required autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm font-medium text-gray-700 dark:text-gray-300">
            <span>Initial password</span>
            <Input type="password" required minLength={8} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </label>
          <div className="space-y-1 text-sm font-medium text-gray-700 dark:text-gray-300">
            <span>Role</span>
            <Select value={role} onValueChange={(value) => setRole(value as UserRole)}>
              <SelectTrigger aria-label="Role"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="user">User: manages instances and versions</SelectItem>
                <SelectItem value="superadmin">Superadmin: also manages users</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button type="submit" form="create-user-form" disabled={mutation.isPending}>{mutation.isPending ? 'Creating...' : 'Create user'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ResetPasswordDialog({ user, onClose }: { user: User | null; onClose: () => void }) {
  const [password, setPassword] = useState('')

  const mutation = useMutation({
    mutationFn: () => apiClient.resetUserPassword(user!.id, password),
    onSuccess: () => {
      toast({ title: 'Password reset', description: `${user?.username} has been signed out everywhere.`, variant: 'success' })
      setPassword('')
      onClose()
    },
    onError: (error: Error) => toast({ title: 'Failed to reset password', description: error.message, variant: 'error' }),
  })

  return (
    <Dialog open={user !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Reset password for {user?.username}</DialogTitle>
          <DialogDescription>The user's existing sessions will be signed out. Their API keys keep working.</DialogDescription>
        </DialogHeader>
        <form
          id="reset-password-form"
          onSubmit={(e) => {
            e.preventDefault()
            mutation.mutate()
          }}
        >
          <label className="block space-y-1 text-sm font-medium text-gray-700 dark:text-gray-300">
            <span>New password</span>
            <Input type="password" required minLength={8} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </label>
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button type="submit" form="reset-password-form" disabled={mutation.isPending}>{mutation.isPending ? 'Saving...' : 'Reset password'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function UsersPage() {
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const [showCreate, setShowCreate] = useState(false)
  const [resetTarget, setResetTarget] = useState<User | null>(null)

  const { data: users, isLoading, error } = useQuery({
    queryKey: USERS_KEY,
    queryFn: () => apiClient.getUsers(),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateUserRequest }) => apiClient.updateUser(id, data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: USERS_KEY }),
    onError: (error: Error) => toast({ title: 'Failed to update user', description: error.message, variant: 'error' }),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => apiClient.deleteUser(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: USERS_KEY })
      toast({ title: 'User deleted', variant: 'success' })
    },
    onError: (error: Error) => toast({ title: 'Failed to delete user', description: error.message, variant: 'error' }),
  })

  return (
    <div className="px-4 py-8 mx-auto space-y-6 max-w-5xl sm:px-6 lg:px-8">
      <div className="flex flex-wrap justify-between items-center gap-3">
        <h2 className="flex items-center gap-2 text-2xl font-semibold text-gray-900 dark:text-white"><Users className="w-6 h-6" />Users</h2>
        <Button onClick={() => setShowCreate(true)}><Plus className="mr-1 w-4 h-4" />Create user</Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Accounts</CardTitle>
          <CardDescription>All users can manage every instance. Only superadmins can manage users. Disabling a user signs them out and stops their API keys.</CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-sm text-gray-500 dark:text-gray-400">Loading users...</p>}
          {error && <p className="text-sm text-red-600 dark:text-red-400">{(error as Error).message}</p>}
          {users && (
            <ul className="divide-y divide-gray-200 dark:divide-gray-700 rounded-md border border-gray-200 dark:border-gray-700">
              {users.map((user) => {
                const isSelf = user.id === currentUser?.id
                return (
                  <li key={user.id} className="flex flex-col gap-3 p-3 lg:flex-row lg:items-center lg:justify-between">
                    <div className="flex flex-wrap items-center gap-2 min-w-0">
                      <span className="font-medium text-gray-900 dark:text-white truncate">{user.username}</span>
                      {isSelf && <Badge variant="outline">you</Badge>}
                      {!user.isActive && <Badge variant="destructive">disabled</Badge>}
                    </div>
                    <div className="flex flex-wrap items-center gap-3">
                      <Select
                        value={user.role}
                        onValueChange={(value) => updateMutation.mutate({ id: user.id, data: { role: value as UserRole } })}
                      >
                        <SelectTrigger className="w-[140px]" aria-label={`Role for ${user.username}`}><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="user">user</SelectItem>
                          <SelectItem value="superadmin">superadmin</SelectItem>
                        </SelectContent>
                      </Select>
                      <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                        <Switch
                          checked={user.isActive}
                          disabled={isSelf}
                          onCheckedChange={(checked) => updateMutation.mutate({ id: user.id, data: { isActive: checked } })}
                          aria-label={`Active state for ${user.username}`}
                        />
                        Active
                      </label>
                      <Button variant="outline" size="sm" onClick={() => setResetTarget(user)}>
                        <KeyRound className="mr-1 w-4 h-4" />Reset password
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={isSelf || deleteMutation.isPending}
                        onClick={() => {
                          if (window.confirm(`Delete user "${user.username}"? Their sessions and API keys are removed too.`)) deleteMutation.mutate(user.id)
                        }}
                      >
                        <Trash2 className="mr-1 w-4 h-4" />Delete
                      </Button>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </CardContent>
      </Card>

      <CreateUserDialog open={showCreate} onOpenChange={setShowCreate} />
      <ResetPasswordDialog user={resetTarget} onClose={() => setResetTarget(null)} />
    </div>
  )
}
