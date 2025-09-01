import type { CliFlags } from '../../types'
import { BasicAuthSection } from './BasicAuthSection'
import { WebhooksSection } from './WebhooksSection'
import { AdvancedOptionsSection } from './AdvancedOptionsSection'

interface CliFlagsProps {
  flags: CliFlags
  onChange: (flags: CliFlags) => void
}

export function CliFlagsComponent({ flags, onChange }: CliFlagsProps) {
  const updateFlag = (key: keyof CliFlags, value: any) => {
    const newFlags = { ...flags, [key]: value }
    onChange(newFlags)
  }

  return (
    <div className="space-y-6">
      <BasicAuthSection flags={flags} updateFlag={updateFlag} />
      <WebhooksSection flags={flags} updateFlag={updateFlag} />
      <AdvancedOptionsSection flags={flags} updateFlag={updateFlag} />
    </div>
  )
}
