import type { CliFlags } from '../../types'
import { BasicAuthSection } from './BasicAuthSection'
import { WebhooksSection } from './WebhooksSection'
import { AdvancedOptionsSection } from './AdvancedOptionsSection'

interface CliFlagsProps {
  flags: CliFlags
  onChange: (flags: CliFlags) => void
  // The instance's selected GOWA version, used to warn when it needs Basic Auth.
  gowaVersion?: string
}

export function CliFlagsComponent({ flags, onChange, gowaVersion }: CliFlagsProps) {
  const updateFlag = (key: keyof CliFlags, value: any) => {
    const newFlags = { ...flags, [key]: value }
    onChange(newFlags)
  }

  // Update multiple flags atomically in a single onChange call.
  // Use this when a single user action must change more than one field,
  // e.g. removing a webhook also needs to prune it from disabledWebhooks.
  // Calling updateFlag twice in a row would hit a stale-closure bug because
  // `flags` here only updates on the next render, so the second call would
  // overwrite the first.
  const updateFlags = (updates: Partial<CliFlags>) => {
    const newFlags = { ...flags, ...updates }
    onChange(newFlags)
  }

  return (
    <div className="space-y-6">
      <BasicAuthSection flags={flags} updateFlag={updateFlag} gowaVersion={gowaVersion} />
      <WebhooksSection flags={flags} updateFlag={updateFlag} updateFlags={updateFlags} />
      <AdvancedOptionsSection flags={flags} updateFlag={updateFlag} />
    </div>
  )
}
