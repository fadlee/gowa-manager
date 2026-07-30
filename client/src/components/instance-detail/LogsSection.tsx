import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AlertCircle, Loader2, RefreshCw, Terminal } from 'lucide-react'
import { apiClient } from '../../lib/api'
import type { Instance, InstanceLogEntry } from '../../types'
import { Alert, AlertDescription, AlertTitle } from '../ui/alert'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { cn } from '../../lib/utils'

interface LogsSectionProps {
  instance: Instance
}

const LOG_TAIL = 200

const formatTimestamp = (value: string) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date)
}

const streamBadgeClass = (entry: InstanceLogEntry) =>
  entry.stream === 'stderr'
    ? 'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300'
    : 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-300'

export function LogsSection({ instance }: LogsSectionProps) {
  const logsQuery = useQuery({
    queryKey: ['instance-logs', instance.id, LOG_TAIL],
    queryFn: () => apiClient.getInstanceLogs(instance.id, LOG_TAIL),
    staleTime: 0,
  })

  const entries = useMemo(() => logsQuery.data?.entries ?? [], [logsQuery.data?.entries])

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Logs</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Recent stdout and stderr captured by the Go manager runtime for this instance.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => logsQuery.refetch()}
          disabled={logsQuery.isFetching}
        >
          <RefreshCw className={cn('mr-2 h-4 w-4', logsQuery.isFetching && 'animate-spin')} />
          Refresh
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-lg">
            <Terminal className="h-5 w-5 text-gray-500" />
            Recent Output
          </CardTitle>
          <CardDescription>
            Showing up to the latest {logsQuery.data?.tail ?? LOG_TAIL} lines. Refresh manually after starting, stopping, or troubleshooting an instance.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {logsQuery.isLoading ? (
            <div className="flex items-center justify-center py-12 text-gray-500 dark:text-gray-400">
              <Loader2 className="mr-2 h-5 w-5 animate-spin" />
              Loading logs...
            </div>
          ) : logsQuery.isError ? (
            <Alert variant="destructive" className="border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950/30 dark:text-red-100">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>Unable to load logs</AlertTitle>
              <AlertDescription>{logsQuery.error.message}</AlertDescription>
            </Alert>
          ) : entries.length === 0 ? (
            <div className="rounded-lg border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
              No logs captured yet. Start or restart the instance, then refresh this tab.
            </div>
          ) : (
            <div className="overflow-hidden rounded-lg border border-gray-200 bg-gray-950 dark:border-gray-700">
              <div className="max-h-[32rem] overflow-auto font-mono text-xs">
                {entries.map((entry, index) => (
                  <div
                    key={`${entry.timestamp}-${entry.stream}-${index}`}
                    className="grid gap-2 border-b border-white/10 px-3 py-2 last:border-b-0 sm:grid-cols-[10rem_5.5rem_minmax(0,1fr)]"
                  >
                    <time className="whitespace-nowrap text-gray-400" dateTime={entry.timestamp}>
                      {formatTimestamp(entry.timestamp)}
                    </time>
                    <Badge variant="outline" className={cn('w-fit uppercase tracking-wide', streamBadgeClass(entry))}>
                      {entry.stream}
                    </Badge>
                    <pre className="whitespace-pre-wrap break-words text-gray-100">{entry.line}</pre>
                  </div>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
