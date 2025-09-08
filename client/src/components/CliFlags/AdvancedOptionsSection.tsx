import { useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { Switch } from '../ui/switch'
import type { CliFlags } from '../../types'

interface AdvancedOptionsSectionProps {
  flags: CliFlags
  updateFlag: (key: keyof CliFlags, value: any) => void
}

export function AdvancedOptionsSection({ flags, updateFlag }: AdvancedOptionsSectionProps) {
  const [showAdvanced, setShowAdvanced] = useState(false)

  return (
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
          {/* <div className="flex justify-between items-center">
            <div className="space-y-0.5">
              <label className="text-sm font-medium">Debug Mode</label>
              <p className="text-xs text-gray-500">Enable debug logging</p>
            </div>
            <Switch
              checked={flags.debug ?? false}
              onCheckedChange={(checked) => updateFlag('debug', checked)}
            />
          </div> */}

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
          {/* <div className="space-y-2">
            <label className="text-sm font-medium">Base Path</label>
            <Input
              placeholder="/gowa"
              value={flags.basePath || ''}
              onChange={(e) => updateFlag('basePath', e.target.value)}
            />
            <p className="text-xs text-gray-500">Base path for subpath deployment</p>
          </div> */}

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
  )
}
