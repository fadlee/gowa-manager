import { useQuery } from '@tanstack/react-query'
import { LayoutPanelTop, Play, QrCode, RefreshCw, Smartphone } from 'lucide-react'
import { apiClient } from '../../lib/api'
import type { InstanceHealth } from '../../lib/instanceHealth'
import type { Instance } from '../../types'
import { Button } from '../ui/button'
import { toast } from '../ui/use-toast'

interface WhatsAppLoginSectionProps {
  instance: Instance
  health: InstanceHealth
  onStart: () => void
  starting: boolean
  onOpenAdmin: () => void
}

// Mirrors IsLinkedDevice in internal/instances/devices.go: multi-device GOWA
// reports every slot with a state, and only "logged_in" is a paired phone.
// Older versions list only logged-in devices and send no state.
function isLinkedDevice(device: Record<string, unknown>): boolean {
  return typeof device.state !== 'string' || device.state.toLowerCase() === 'logged_in'
}

function deviceLabel(device: Record<string, unknown>): string {
  for (const key of ['display_name', 'phone_number', 'name', 'jid', 'device', 'id']) {
    const value = device[key]
    if (typeof value === 'string' && value) return value
  }
  return 'Linked device'
}

export async function openAdmin(instance: Instance) {
  try {
    const { url } = await apiClient.createAdminLink(instance.id)
    window.open(url, '_blank', 'noopener,noreferrer')
  } catch (error) {
    toast({ title: 'Could not create a sign-in link', description: error instanceof Error ? error.message : 'Opening the admin page without auto sign-in.', variant: 'error' })
    window.open(apiClient.getProxyUrl(instance.key), '_blank', 'noopener,noreferrer')
  }
}

export function WhatsAppLoginSection({ instance, health, onStart, starting, onOpenAdmin }: WhatsAppLoginSectionProps) {
  const running = health.state === 'running'
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['instance-devices', instance.id],
    queryFn: () => apiClient.getInstanceDevices(instance.id),
    enabled: running,
    refetchInterval: running ? 5000 : false,
  })

  if (!running) {
    return (
      <div className="space-y-3 rounded-xl border border-dashed border-gray-300 p-6 text-center dark:border-gray-700">
        <Smartphone className="mx-auto h-8 w-8 text-gray-400" />
        <p className="text-sm text-gray-600 dark:text-gray-300">Start the instance before linking a WhatsApp account.</p>
        <Button onClick={onStart} disabled={starting}>
          <Play className="mr-2 h-4 w-4" />Start instance
        </Button>
      </div>
    )
  }

  const devices = (data?.devices ?? []).filter(isLinkedDevice)
  const unpaired = (data?.devices?.length ?? 0) - devices.length
  const unpairedNote = unpaired > 0 && (
    <p className="text-xs text-gray-500 dark:text-gray-400">
      {unpaired} device slot{unpaired === 1 ? ' is' : 's are'} added but not paired yet. Pair {unpaired === 1 ? 'it' : 'them'} in the GOWA admin tab, or remove {unpaired === 1 ? 'it' : 'them'} there.
    </p>
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">WhatsApp login</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">Link a phone to this instance and check which devices are connected.</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
          <RefreshCw className={`mr-2 h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />Refresh
        </Button>
      </div>

      {devices.length > 0 ? (
        <div className="space-y-2 rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
          <p className="text-sm font-medium text-green-700 dark:text-green-400">
            {devices.length} device{devices.length === 1 ? '' : 's'} connected
          </p>
          {devices.map((device, index) => (
            <div key={index} className="flex items-center gap-3 rounded-lg bg-gray-50 px-3 py-2 dark:bg-gray-800/60">
              <span className="flex h-8 w-8 items-center justify-center rounded-full bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300">
                <Smartphone className="h-4 w-4" />
              </span>
              <span className="min-w-0 truncate font-mono text-sm text-gray-800 dark:text-gray-200">{deviceLabel(device)}</span>
            </div>
          ))}
          {unpairedNote}
          <p className="pt-1 text-xs text-gray-500 dark:text-gray-400">To link another number or log out, use the GOWA admin page.</p>
          <Button variant="outline" size="sm" onClick={onOpenAdmin}>
            <LayoutPanelTop className="mr-2 h-4 w-4" />Open admin page
          </Button>
        </div>
      ) : (
        <div className="grid gap-5 rounded-xl border border-amber-200 bg-amber-50/60 p-5 dark:border-amber-900/60 dark:bg-amber-950/20 md:grid-cols-[1fr_auto] md:items-center">
          <div className="space-y-3">
            <p className="font-semibold text-amber-900 dark:text-amber-200">No WhatsApp account is linked yet</p>
            <ol className="list-decimal space-y-1 pl-5 text-sm text-gray-700 dark:text-gray-300">
              <li>Click <b>Show QR code</b>. The GOWA admin page opens in the <b>GOWA admin</b> tab, already signed in.</li>
              <li>Click <b>Add device</b>, then <b>Pair</b>, to show the QR code. Older GOWA versions have a <b>Login</b> button instead.</li>
              <li>On your phone, open WhatsApp → Linked devices → Link a device, and scan it.</li>
            </ol>
            <p className="text-xs text-gray-500 dark:text-gray-400">This tab checks every 5 seconds and updates as soon as the phone is linked.</p>
            {unpairedNote}
          </div>
          <Button size="lg" onClick={onOpenAdmin}>
            <QrCode className="mr-2 h-4 w-4" />Show QR code
          </Button>
        </div>
      )}
      {data?.error && <p className="text-xs text-red-600 dark:text-red-400">Could not read devices: {data.error}</p>}
    </div>
  )
}
