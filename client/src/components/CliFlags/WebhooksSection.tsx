import { useState } from 'react'
import { X, Plus } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import type { CliFlags } from '../../types'

interface WebhooksSectionProps {
  flags: CliFlags
  updateFlag: (key: keyof CliFlags, value: any) => void
}

export function WebhooksSection({ flags, updateFlag }: WebhooksSectionProps) {
  const [newWebhook, setNewWebhook] = useState('')

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

  const handleWebhookKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      addWebhook(e)
    }
  }

  return (
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
  )
}
