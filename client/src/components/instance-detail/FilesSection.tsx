import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  AlertCircle,
  ArrowUp,
  ChevronRight,
  Download,
  Eye,
  File,
  FileText,
  Folder,
  Image,
  Loader2,
  RefreshCw,
} from 'lucide-react'
import { apiClient } from '../../lib/api'
import type { Instance, InstanceFileEntry, InstanceFilePreviewResponse } from '../../types'
import { Alert, AlertDescription, AlertTitle } from '../ui/alert'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog'

interface FilesSectionProps {
  instance: Instance
}

const ROOT_PATH = ''

const formatFileSize = (size: number) => {
  if (!Number.isFinite(size) || size < 0) return 'Unknown size'
  if (size === 0) return '0 B'

  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(size) / Math.log(1024)), units.length - 1)
  const value = size / 1024 ** index

  return `${index === 0 ? value.toFixed(0) : value.toFixed(value >= 10 ? 1 : 2)} ${units[index]}`
}

const formatModifiedAt = (modifiedAt: string) => {
  const date = new Date(modifiedAt)
  if (Number.isNaN(date.getTime())) return 'Unknown modified time'

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

const getParentPath = (path: string) => {
  const normalized = path.replace(/^\/+|\/+$/g, '')
  if (!normalized) return ROOT_PATH

  const segments = normalized.split('/')
  segments.pop()
  return segments.join('/')
}

const getBreadcrumbs = (path: string) => {
  const normalized = path.replace(/^\/+|\/+$/g, '')
  if (!normalized) return []

  return normalized.split('/').map((segment, index, segments) => ({
    label: segment,
    path: segments.slice(0, index + 1).join('/'),
  }))
}

const isImagePreview = (preview: InstanceFilePreviewResponse) =>
  preview.encoding === 'base64' && preview.contentType.startsWith('image/')

export function FilesSection({ instance }: FilesSectionProps) {
  const [currentPath, setCurrentPath] = useState(ROOT_PATH)
  const [previewPath, setPreviewPath] = useState<string | null>(null)
  const [downloadingPath, setDownloadingPath] = useState<string | null>(null)
  const [downloadError, setDownloadError] = useState<string | null>(null)

  const filesQuery = useQuery({
    queryKey: ['instance-files', instance.id, currentPath],
    queryFn: () => apiClient.getInstanceFiles(instance.id, currentPath || undefined),
  })

  const previewQuery = useQuery({
    queryKey: ['instance-file-preview', instance.id, previewPath],
    queryFn: () => apiClient.previewInstanceFile(instance.id, previewPath ?? ROOT_PATH),
    enabled: previewPath !== null,
  })

  const sortedEntries = useMemo(() => {
    return [...(filesQuery.data?.entries ?? [])].sort((a, b) => {
      if (a.type !== b.type) return a.type === 'directory' ? -1 : 1
      return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
    })
  }, [filesQuery.data?.entries])

  const breadcrumbs = getBreadcrumbs(filesQuery.data?.path ?? currentPath)
  const canGoUp = currentPath !== ROOT_PATH

  const openPath = (entry: InstanceFileEntry) => {
    if (entry.type === 'directory') {
      setCurrentPath(entry.path)
      setPreviewPath(null)
    }
  }

  const openPreview = (entry: InstanceFileEntry) => {
    if (entry.type === 'file' && entry.previewable) {
      setPreviewPath(entry.path)
    }
  }

  const closePreview = () => {
    setPreviewPath(null)
  }

  const downloadFile = async (path: string, name: string) => {
    setDownloadingPath(path)
    setDownloadError(null)
    try {
      const response = await fetch(apiClient.getInstanceFileDownloadUrl(instance.id, path))
      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}))
        throw new Error(errorData.error || `Download failed with status ${response.status}`)
      }
      const blob = await response.blob()
      const objectUrl = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = objectUrl
      link.download = name
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(objectUrl)
    } catch (error) {
      setDownloadError(error instanceof Error ? error.message : 'Failed to download file')
    } finally {
      setDownloadingPath(null)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Files</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Browse this instance directory read-only. Large or unsafe files are download-only.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => filesQuery.refetch()}
          disabled={filesQuery.isFetching}
        >
          <RefreshCw className={`mr-2 h-4 w-4 ${filesQuery.isFetching ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {downloadError && (
        <Alert variant="destructive" className="border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950/30 dark:text-red-100">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Download failed</AlertTitle>
          <AlertDescription>{downloadError}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader className="space-y-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <CardTitle className="flex items-center gap-2 text-lg">
                <Folder className="h-5 w-5 text-blue-500" />
                Instance Directory
              </CardTitle>
              <CardDescription>
                {sortedEntries.length === 1 ? '1 item' : `${sortedEntries.length} items`} in {breadcrumbs.length > 0 ? breadcrumbs[breadcrumbs.length - 1].label : 'root'}
              </CardDescription>
            </div>
            <div className="flex flex-wrap items-center gap-1 rounded-lg bg-gray-100 p-1 text-sm dark:bg-gray-800">
              <button
                type="button"
                onClick={() => setCurrentPath(ROOT_PATH)}
                className="rounded-md px-2 py-1 font-medium text-gray-700 hover:bg-white hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white"
              >
                Root
              </button>
              {breadcrumbs.map((crumb) => (
                <div key={crumb.path} className="flex items-center gap-1">
                  <ChevronRight className="h-4 w-4 text-gray-400" />
                  <button
                    type="button"
                    onClick={() => setCurrentPath(crumb.path)}
                    className="max-w-32 truncate rounded-md px-2 py-1 text-gray-600 hover:bg-white hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-white sm:max-w-48"
                    title={crumb.path}
                  >
                    {crumb.label}
                  </button>
                </div>
              ))}
            </div>
          </div>

          {canGoUp && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setCurrentPath(getParentPath(currentPath))}
              className="w-fit"
            >
              <ArrowUp className="mr-2 h-4 w-4" />
              Up one level
            </Button>
          )}
        </CardHeader>

        <CardContent>
          {filesQuery.isLoading ? (
            <div className="flex items-center justify-center py-12 text-gray-500 dark:text-gray-400">
              <Loader2 className="mr-2 h-5 w-5 animate-spin" />
              Loading files...
            </div>
          ) : filesQuery.isError ? (
            <Alert variant="destructive" className="border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950/30 dark:text-red-100">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>Unable to load files</AlertTitle>
              <AlertDescription>{filesQuery.error.message}</AlertDescription>
            </Alert>
          ) : sortedEntries.length === 0 ? (
            <div className="rounded-lg border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
              This directory is empty.
            </div>
          ) : (
            <div className="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
              <div className="hidden grid-cols-[minmax(0,1fr)_8rem_12rem_12rem] gap-4 border-b border-gray-200 bg-gray-50 px-4 py-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:border-gray-700 dark:bg-gray-800/70 dark:text-gray-400 md:grid">
                <span>Name</span>
                <span>Size</span>
                <span>Modified</span>
                <span className="text-right">Actions</span>
              </div>
              <div className="divide-y divide-gray-200 dark:divide-gray-700">
                {sortedEntries.map((entry) => (
                  <div key={entry.path} className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(0,1fr)_8rem_12rem_12rem] md:items-center md:gap-4">
                    <button
                      type="button"
                      onClick={() => openPath(entry)}
                      disabled={entry.type !== 'directory'}
                      className="flex min-w-0 items-center gap-3 text-left disabled:cursor-default"
                    >
                      <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-100 dark:bg-gray-800">
                        {entry.type === 'directory' ? (
                          <Folder className="h-5 w-5 text-blue-500" />
                        ) : entry.previewable ? (
                          <FileText className="h-5 w-5 text-green-500" />
                        ) : (
                          <File className="h-5 w-5 text-gray-500" />
                        )}
                      </span>
                      <span className="min-w-0">
                        <span className="block truncate font-medium text-gray-900 dark:text-white">{entry.name}</span>
                        <span className="block truncate text-xs text-gray-500 dark:text-gray-400">{entry.path || '/'}</span>
                      </span>
                    </button>

                    <div className="text-sm text-gray-600 dark:text-gray-300">
                      {entry.type === 'file' ? formatFileSize(entry.size) : 'Directory'}
                    </div>
                    <div className="text-sm text-gray-600 dark:text-gray-300">{formatModifiedAt(entry.modifiedAt)}</div>
                    <div className="flex flex-wrap items-center gap-2 md:justify-end">
                      {entry.type === 'directory' ? (
                        <Button type="button" variant="outline" size="sm" onClick={() => openPath(entry)}>
                          Open
                        </Button>
                      ) : (
                        <>
                          {entry.previewable ? (
                            <Button type="button" variant="outline" size="sm" onClick={() => openPreview(entry)}>
                              <Eye className="mr-2 h-4 w-4" />
                              Preview
                            </Button>
                          ) : (
                            <Badge variant="outline" className="border-gray-300 text-gray-600 dark:border-gray-600 dark:text-gray-300">
                              Download only
                            </Badge>
                          )}
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            onClick={() => downloadFile(entry.path, entry.name)}
                            disabled={downloadingPath === entry.path}
                          >
                            {downloadingPath === entry.path ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
                            Download
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={previewPath !== null} onOpenChange={(open) => { if (!open) closePreview() }}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {previewQuery.data && isImagePreview(previewQuery.data) ? (
                <Image className="h-5 w-5 text-blue-500" />
              ) : (
                <FileText className="h-5 w-5 text-green-500" />
              )}
              File Preview
            </DialogTitle>
            <DialogDescription>
              {previewPath || 'Loading preview...'}
            </DialogDescription>
          </DialogHeader>

          {previewQuery.isLoading ? (
            <div className="flex items-center justify-center py-12 text-gray-500 dark:text-gray-400">
              <Loader2 className="mr-2 h-5 w-5 animate-spin" />
              Loading preview...
            </div>
          ) : previewQuery.isError ? (
            <Alert variant="destructive" className="border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950/30 dark:text-red-100">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>Unable to load preview</AlertTitle>
              <AlertDescription>{previewQuery.error.message}</AlertDescription>
            </Alert>
          ) : previewQuery.data ? (
            <div className="space-y-4">
              <div className="grid gap-3 rounded-lg bg-gray-100 p-3 text-sm dark:bg-gray-800 sm:grid-cols-3">
                <div>
                  <p className="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">Size</p>
                  <p className="text-gray-900 dark:text-white">{formatFileSize(previewQuery.data.size)}</p>
                </div>
                <div>
                  <p className="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">Modified</p>
                  <p className="text-gray-900 dark:text-white">{formatModifiedAt(previewQuery.data.modifiedAt)}</p>
                </div>
                <div>
                  <p className="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">Type</p>
                  <p className="truncate text-gray-900 dark:text-white">{previewQuery.data.contentType}</p>
                </div>
              </div>

              {isImagePreview(previewQuery.data) ? (
                <div className="flex justify-center rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-gray-700 dark:bg-gray-900">
                  <img
                    src={`data:${previewQuery.data.contentType};base64,${previewQuery.data.content}`}
                    alt={previewQuery.data.name}
                    className="max-h-[60vh] max-w-full rounded object-contain"
                  />
                </div>
              ) : (
                <pre className="max-h-[60vh] overflow-auto rounded-lg border border-gray-200 bg-gray-950 p-4 text-sm text-gray-100 dark:border-gray-700">
                  <code>{previewQuery.data.content}</code>
                </pre>
              )}

              <div className="flex justify-end">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => downloadFile(previewQuery.data.path, previewQuery.data.name)}
                  disabled={downloadingPath === previewQuery.data.path}
                >
                  {downloadingPath === previewQuery.data.path ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
                  Download
                </Button>
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  )
}
