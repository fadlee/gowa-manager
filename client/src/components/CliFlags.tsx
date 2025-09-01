import { useState } from 'react'
import { Plus, X, ChevronDown, ChevronRight } from 'lucide-react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import type { CliFlags } from '../types'

interface CliFlagsProps {
  flags: CliFlags
  onChange: (flags: CliFlags) => void
}

export function CliFlagsComponent({ flags, onChange }: CliFlagsProps) {
  const [newWebhook, setNewWebhook] = useState('')
  const [newAuth, setNewAuth] = useState({ username: '', password: '' })
  const [showAdvanced, setShowAdvanced] = useState(false)

  const updateFlag = (key: keyof CliFlags, value: any) => {
    const newFlags = { ...flags, [key]: value }
    onChange(newFlags)
  }

  const addWebhook = (e?: React.MouseEvent | React.KeyboardEvent) => {
    e?.preventDefault()
    if (newWebhook.trim()) {
      const webhooks = flags.webhooks || []
      const newWebhooks = [...webhooks, newWebhook.trim()]
      updateFlag('webhooks', newWebhooks)
      setNewWebhook('')
    }
  }

  const handleWebhookBlur = () => {
    // Auto-add on blur if there's content
    if (newWebhook.trim()) {
      addWebhook()
    }
  }

  const removeWebhook = (index: number, e: React.MouseEvent) => {
    e.preventDefault()
    const webhooks = flags.webhooks || []
    updateFlag('webhooks', webhooks.filter((_, i) => i !== index))
  }

  const addBasicAuth = (e?: React.MouseEvent | React.KeyboardEvent) => {
    e?.preventDefault()
    if (newAuth.username.trim() && newAuth.password.trim()) {
      const basicAuth = flags.basicAuth || []
      const newBasicAuth = [...basicAuth, { ...newAuth }]
      updateFlag('basicAuth', newBasicAuth)
      setNewAuth({ username: '', password: '' })
    }
  }

  const handleAuthBlur = (field: 'username' | 'password') => {
    // Auto-add on password blur if both fields are filled
    if (field === 'password' && newAuth.username.trim() && newAuth.password.trim()) {
      addBasicAuth()
    }
  }

  const removeBasicAuth = (index: number, e: React.MouseEvent) => {
    e.preventDefault()
    const basicAuth = flags.basicAuth || []
    updateFlag('basicAuth', basicAuth.filter((_, i) => i !== index))
  }

  const handleWebhookKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      addWebhook(e)
    }
  }

  const handleAuthKeyDown = (e: React.KeyboardEvent, field: 'username' | 'password') => {
    if (e.key === 'Enter') {
      e.preventDefault()
      if (field === 'password' && newAuth.username.trim() && newAuth.password.trim()) {
        addBasicAuth(e)
      }
    }
  }

  return (
    <div className="space-y-6">

      {/* Basic Auth */}
      <div className="space-y-3">
        <label className="text-sm font-medium">Basic Authentication</label>
        <p className="text-xs text-gray-500">Add username:password pairs for basic auth</p>

        {/* Existing Auth Pairs */}
        {flags.basicAuth && flags.basicAuth.length > 0 && (
          <div className="space-y-2">
            {flags.basicAuth.map((auth, index) => (
              <div key={index} className="flex gap-2 items-center p-2 bg-gray-50 rounded-md">
                <span className="flex-1 font-mono text-sm">
                  {auth.username}:{"*".repeat(auth.password.length)}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={(e) => removeBasicAuth(index, e)}
                  className="p-0 w-6 h-6"
                >
                  <X className="w-3 h-3" />
                </Button>
              </div>
            ))}
          </div>
        )}

        {/* Add New Auth */}
        <div className="flex gap-2">
          <Input
            placeholder="Username"
            value={newAuth.username}
            onChange={(e) => setNewAuth({ ...newAuth, username: e.target.value })}
            onKeyDown={(e) => handleAuthKeyDown(e, 'username')}
            className="flex-1"
          />
          <Input
            type="password"
            placeholder="Password"
            value={newAuth.password}
            onChange={(e) => setNewAuth({ ...newAuth, password: e.target.value })}
            onKeyDown={(e) => handleAuthKeyDown(e, 'password')}
            onBlur={() => handleAuthBlur('password')}
            className="flex-1"
          />
          {flags.basicAuth && flags.basicAuth.length > 0 && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={addBasicAuth}
              disabled={!newAuth.username.trim() || !newAuth.password.trim()}
            >
              <Plus className="w-4 h-4" />
            </Button>
          )}
        </div>
      </div>

      {/* Webhooks */}
      <div className="space-y-3">
        <label className="text-sm font-medium">Webhooks</label>
        <p className="text-xs text-gray-500">Forward events to webhook URLs</p>

        {/* Existing Webhooks */}
        {flags.webhooks && flags.webhooks.length > 0 && (
          <div className="space-y-2">
            {flags.webhooks.map((webhook, index) => (
              <div key={index} className="flex gap-2 items-center p-2 bg-gray-50 rounded-md">
                <span className="flex-1 font-mono text-sm truncate">{webhook}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={(e) => removeWebhook(index, e)}
                  className="p-0 w-6 h-6"
                >
                  <X className="w-3 h-3" />
                </Button>
              </div>
            ))}
          </div>
        )}

        {/* Add New Webhook */}
        <div className="flex gap-2">
          <Input
            placeholder="https://your-webhook-url.com/callback"
            value={newWebhook}
            onChange={(e) => setNewWebhook(e.target.value)}
            onKeyDown={handleWebhookKeyDown}
            onBlur={handleWebhookBlur}
            className="flex-1"
          />
          {flags.webhooks && flags.webhooks.length > 0 && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={addWebhook}
              disabled={!newWebhook.trim()}
            >
              <Plus className="w-4 h-4" />
            </Button>
          )}
        </div>
      </div>

      {/* Advanced Options - Collapsible */}
      <div className="pt-4 border-t">
        <Button
          type="button"
          variant="ghost"
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="flex gap-2 items-center p-0 h-auto text-sm font-medium hover:bg-transparent"
        >
          {showAdvanced ? (
            <ChevronDown className="w-4 h-4" />
          ) : (
            <ChevronRight className="w-4 h-4" />
          )}
          Advanced Options
        </Button>
        <p className="mt-1 ml-6 text-xs text-gray-500">
          Additional configuration options
        </p>

        {showAdvanced && (
          <div className="mt-4 ml-6 space-y-4">
            {/* Account Validation */}
            <div className="flex justify-between items-center">
              <div className="space-y-0.5">
                <label className="text-sm font-medium">Account Validation</label>
                <p className="text-xs text-gray-500">Enable or disable account validation</p>
              </div>
              <Switch
                checked={flags.accountValidation ?? true}
                onCheckedChange={(checked) => updateFlag('accountValidation', checked)}
              />
            </div>

            {/* OS Name */}
            <div className="space-y-2">
              <label className="text-sm font-medium">OS Name</label>
              <Input
                placeholder="e.g., Chrome, GowaManager"
                value={flags.os || ''}
                onChange={(e) => updateFlag('os', e.target.value)}
              />
              <p className="text-xs text-gray-500">Custom OS name for the instance</p>
            </div>

            {/* Auto Mark Read */}
            <div className="flex justify-between items-center">
              <div className="space-y-0.5">
                <label className="text-sm font-medium">Auto Mark Read</label>
                <p className="text-xs text-gray-500">Automatically mark incoming messages as read</p>
              </div>
              <Switch
                checked={flags.autoMarkRead ?? false}
                onCheckedChange={(checked) => updateFlag('autoMarkRead', checked)}
              />
            </div>

            {/* Debug Mode */}
            <div className="flex justify-between items-center">
              <div className="space-y-0.5">
                <label className="text-sm font-medium">Debug Mode</label>
                <p className="text-xs text-gray-500">Enable debug logging</p>
              </div>
              <Switch
                checked={flags.debug ?? false}
                onCheckedChange={(checked) => updateFlag('debug', checked)}
              />
            </div>

            {/* Auto Reply */}
            <div className="space-y-2">
              <label className="text-sm font-medium">Auto Reply Message</label>
              <Input
                placeholder="Don't reply this message"
                value={flags.autoReply || ''}
                onChange={(e) => updateFlag('autoReply', e.target.value)}
              />
              <p className="text-xs text-gray-500">Automatic reply for incoming messages</p>
            </div>

            {/* Base Path */}
            <div className="space-y-2">
              <label className="text-sm font-medium">Base Path</label>
              <Input
                placeholder="/gowa"
                value={flags.basePath || ''}
                onChange={(e) => updateFlag('basePath', e.target.value)}
              />
              <p className="text-xs text-gray-500">Base path for subpath deployment</p>
            </div>

            {/* Webhook Secret */}
            <div className="space-y-2">
              <label className="text-sm font-medium">Webhook Secret</label>
              <Input
                type="password"
                placeholder="super-secret-key"
                value={flags.webhookSecret || ''}
                onChange={(e) => updateFlag('webhookSecret', e.target.value)}
              />
              <p className="text-xs text-gray-500">Secret key to secure webhook requests</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
