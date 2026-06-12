import { useState } from 'react'
import { X, Plus, Info } from 'lucide-react'
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
      <div className="space-y-1">
        <label className="text-sm font-medium text-gray-700 dark:text-gray-300">Webhooks</label>
        <p className="text-xs text-gray-500 dark:text-gray-400">
          Add one or more HTTPS endpoints. GOWA sends WhatsApp events to every configured URL after the instance restarts.
        </p>
      </div>

      <div className="flex gap-2 rounded-md border border-blue-200 bg-blue-50 p-3 text-xs text-blue-800 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <div>
          <p className="font-medium">Multiple webhook URLs are supported.</p>
          <p>Press Enter or click Add to queue a URL, then save settings. Changes require restart to apply.</p>
        </div>
      </div>

      {/* Existing Webhooks */}
      {flags.webhooks && flags.webhooks.length > 0 && (
        <div className="space-y-2">
          {flags.webhooks.map((webhook, index) => (
            <div key={index} className="flex gap-2 items-center p-2 bg-gray-200 dark:bg-gray-700 rounded-md">
              <span className="rounded bg-gray-300 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-gray-700 dark:bg-gray-600 dark:text-gray-200">
                {index + 1}
              </span>
              <span className="flex-1 font-mono text-sm text-gray-900 dark:text-white truncate">{webhook}</span>
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
          placeholder="https://myapp.example.com/webhooks/gowa"
          value={newWebhook}
          onChange={(e) => setNewWebhook(e.target.value)}
          onKeyDown={handleWebhookKeyDown}
          onBlur={handleWebhookBlur}
          className="flex-1"
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addWebhook}
          disabled={!newWebhook.trim()}
        >
          <Plus className="mr-2 w-4 h-4" />
          Add
        </Button>
      </div>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        Tip: use a public HTTPS URL or a tunnel such as ngrok/localtunnel for local development.
      </p>
    </div>
  )
}
