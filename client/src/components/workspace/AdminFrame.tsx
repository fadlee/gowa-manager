import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ExternalLink, Loader2, RotateCw } from 'lucide-react'
import { apiClient } from '../../lib/api'
import { cn } from '../../lib/utils'
import type { Instance } from '../../types'
import { Button } from '../ui/button'
import { openAdmin } from './WhatsAppLoginSection'

interface AdminFrameProps {
  instance: Instance
  className?: string
}

// AdminFrame shows the instance's GOWA admin page inside the manager. It is
// served from the manager's own origin under /app/<key>/, and the admin link
// signs the frame in with the instance's Basic Auth.
export function AdminFrame({ instance, className }: AdminFrameProps) {
  const [reloads, setReloads] = useState(0)
  const { data: src, isFetching } = useQuery({
    queryKey: ['admin-link', instance.id, reloads],
    queryFn: async () => {
      try {
        return (await apiClient.createAdminLink(instance.id)).url
      } catch {
        // Without a link the page still opens, and GOWA asks for Basic Auth.
        return apiClient.getProxyUrl(instance.key)
      }
    },
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
  })

  return (
    <div className={cn('min-h-0 min-w-0 flex-1 flex-col', className)}>
      <div className="flex items-center gap-2 border-b border-gray-200 bg-white px-4 py-1.5 text-xs text-gray-500 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-400">
        <span className="min-w-0 flex-1 truncate">GOWA admin page, signed in automatically</span>
        <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => setReloads(value => value + 1)} disabled={isFetching}>
          <RotateCw className={cn('mr-1.5 h-3.5 w-3.5', isFetching && 'animate-spin')} />Reload
        </Button>
        <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => openAdmin(instance)}>
          <ExternalLink className="mr-1.5 h-3.5 w-3.5" />Open in new tab
        </Button>
      </div>
      <div className="relative min-h-0 flex-1 bg-white">
        {src ? (
          <iframe
            key={src}
            src={src}
            title={`GOWA admin for ${instance.name}`}
            allow="clipboard-read; clipboard-write"
            className="absolute inset-0 h-full w-full border-0"
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-gray-500">
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />Signing in…
          </div>
        )}
      </div>
    </div>
  )
}
