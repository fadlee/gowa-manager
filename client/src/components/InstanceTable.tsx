import { useQueries, useMutation, useQueryClient } from '@tanstack/react-query'
import { Table, Tag, Tooltip, Progress } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { apiClient } from '../lib/api'
import type { Instance, InstanceStatus } from '../types'
import { ExternalLink, Square, Play, RotateCw, Pencil, Trash2 } from 'lucide-react'

interface InstanceTableProps {
  instances: Instance[]
  onEdit: (instance: Instance) => void
}

export function InstanceTable({ instances, onEdit }: InstanceTableProps) {
  const queryClient = useQueryClient()

  // Actions
  const startMutation = useMutation({
    mutationFn: (id: number) => apiClient.startInstance(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['instances'] }),
  })
  const stopMutation = useMutation({
    mutationFn: (id: number) => apiClient.stopInstance(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['instances'] }),
  })
  const restartMutation = useMutation({
    mutationFn: (id: number) => apiClient.restartInstance(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['instances'] }),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => apiClient.deleteInstance(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['instances'] }),
  })

  // Live status for stats
  const statusResults = useQueries({
    queries: (instances || []).map((inst) => ({
      queryKey: ['instance-status', inst.id],
      queryFn: () => apiClient.getInstanceStatus(inst.id),
      refetchInterval: 5000,
      enabled: true,
    })),
  })

  const statusMap = new Map<number, InstanceStatus>()
  statusResults.forEach((q) => {
    if (q.data) statusMap.set(q.data.id, q.data)
  })

  const formatMemory = (mb?: number) => {
    if (typeof mb !== 'number') return '--'
    return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb.toFixed(1)} MB`
  }

  const getCpuStroke = (percent?: number) => {
    const p = percent ?? 0
    if (p >= 80) return '#ef4444' // red-500
    if (p >= 60) return '#f97316' // orange-500
    if (p >= 40) return '#f59e0b' // amber-500
    return '#22c55e' // green-500
  }

  const getMemoryStroke = (percent?: number) => {
    const p = percent ?? 0
    if (p >= 80) return '#ef4444'
    if (p >= 60) return '#f97316'
    if (p >= 40) return '#60a5fa' // blue-400
    return '#3b82f6' // blue-500
  }

  const columns: ColumnsType<Instance> = [
    {
      title: 'Name',
      dataIndex: 'name',
      key: 'name',
      width: 220,
      render: (text, record) => (
        <div className="flex flex-col items-start gap-1">
          <span className="font-medium text-gray-900">{text}</span>
          <Tag color={record.status.toLowerCase() === 'running' ? 'green' : record.status.toLowerCase() === 'error' ? 'red' : 'default'}>
            {record.status}
          </Tag>
        </div>
      ),
    },
    {
      title: 'ID / Key',
      key: 'id',
      render: (_, record) => (
        <div className="text-sm text-gray-600">
          #{record.id}
          <span className="ml-2 text-gray-500">{record.key}</span>
        </div>
      ),
      responsive: ['md'],
    },
    {
      title: 'Port',
      dataIndex: 'port',
      key: 'port',
      width: 90,
      render: (p) => p ?? '--',
    },
    {
      title: 'CPU',
      key: 'cpu',
      width: 110,
      render: (_, record) => {
        const s = statusMap.get(record.id)
        const cpu = Math.min(Math.max(s?.resources?.cpuPercent ?? 0, 0), 100)
        return (
          <div className="flex flex-col items-center">
            <span className="text-xs text-gray-600">{Math.round(cpu)}%</span>
            <div className="w-16">
              <Progress percent={cpu} showInfo={false} size="small" strokeColor={getCpuStroke(cpu)} trailColor="#e5e7eb" />
            </div>
          </div>
        )
      },
    },
    {
      title: 'Memory',
      key: 'memory',
      width: 140,
      render: (_, record) => {
        const s = statusMap.get(record.id)
        const memMB = s?.resources?.memoryMB
        const memPct = Math.min(Math.max(s?.resources?.memoryPercent ?? 0, 0), 100)
        return (
          <div className="flex flex-col items-center">
            <span className="text-xs text-gray-700">{formatMemory(memMB)}</span>
            <div className="w-16">
              <Progress percent={memPct} showInfo={false} size="small" strokeColor={getMemoryStroke(memPct)} trailColor="#e5e7eb" />
            </div>
          </div>
        )
      },
    },
    {
      title: 'Disk',
      key: 'disk',
      width: 120,
      render: (_, record) => {
        const s = statusMap.get(record.id)
        return <span className="text-xs text-gray-700">{formatMemory(s?.resources?.diskMB)}</span>
      },
      responsive: ['md'],
    },
    {
      title: 'Version',
      dataIndex: 'gowa_version',
      key: 'version',
      width: 110,
      render: (v: string) => <Tag>{v || 'latest'}</Tag>,
    },
    {
      title: 'Actions',
      key: 'actions',
      width: 220,
      fixed: 'right',
      align: 'right',
      render: (_, record) => (
        <div className="overflow-x-auto max-w-full whitespace-nowrap">
          <div className="flex gap-2 items-center whitespace-nowrap">
            <Tooltip title="Open">
              <button
                className="inline-flex justify-center items-center w-8 h-8 text-blue-600 rounded border border-blue-200 hover:bg-blue-50"
                onClick={() => window.open(apiClient.getProxyUrl(record.key), '_blank')}
                aria-label="Open"
              >
                <ExternalLink className="w-4 h-4" />
              </button>
            </Tooltip>
            {record.status.toLowerCase() === 'running' ? (
              <Tooltip title="Stop">
                <button
                  className="inline-flex justify-center items-center w-8 h-8 text-red-600 rounded border border-red-200 hover:bg-red-50"
                  onClick={() => stopMutation.mutate(record.id)}
                  disabled={stopMutation.isPending}
                  aria-label="Stop"
                >
                  <Square className="w-4 h-4" />
                </button>
              </Tooltip>
            ) : (
              <Tooltip title="Start">
                <button
                  className="inline-flex justify-center items-center w-8 h-8 text-green-600 rounded border border-green-200 hover:bg-green-50"
                  onClick={() => startMutation.mutate(record.id)}
                  disabled={startMutation.isPending}
                  aria-label="Start"
                >
                  <Play className="w-4 h-4" />
                </button>
              </Tooltip>
            )}
            <Tooltip title="Restart">
              <button
                className="inline-flex justify-center items-center w-8 h-8 text-gray-700 rounded border hover:bg-gray-50"
                onClick={() => restartMutation.mutate(record.id)}
                disabled={restartMutation.isPending}
                aria-label="Restart"
              >
                <RotateCw className="w-4 h-4" />
              </button>
            </Tooltip>
            <Tooltip title="Edit">
              <button
                className="inline-flex justify-center items-center w-8 h-8 text-gray-700 rounded border hover:bg-gray-50"
                onClick={() => onEdit(record)}
                aria-label="Edit"
              >
                <Pencil className="w-4 h-4" />
              </button>
            </Tooltip>
            <Tooltip title="Delete">
              <button
                className="inline-flex justify-center items-center w-8 h-8 text-red-600 rounded border border-red-200 hover:bg-red-50"
                onClick={() => {
                  if (window.confirm(`Delete instance \"${record.name}\"?`)) deleteMutation.mutate(record.id)
                }}
                disabled={deleteMutation.isPending}
                aria-label="Delete"
              >
                <Trash2 className="w-4 h-4" />
              </button>
            </Tooltip>
          </div>
        </div>
      )
    }
  ]

  return (
    <div className="p-2 bg-white rounded-lg border border-gray-200">
      <Table
        rowKey={(r) => r.id}
        columns={columns}
        dataSource={instances as Instance[]}
        size="middle"
        pagination={{ pageSize: 10 }}
        scroll={{ x: 1200 }}
      />
    </div>
  )
}
