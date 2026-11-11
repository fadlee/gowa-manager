import type { Instance, InstanceStatus } from '../../types'
import { getInstanceHealth, type InstanceHealth } from '../../lib/instanceHealth'
import type { InstanceFilter } from './primitives'

export interface InstanceRowData {
  instance: Instance
  status: InstanceStatus | undefined
  health: InstanceHealth
}

export function buildRows(instances: Instance[], statuses: Map<number, InstanceStatus>, latestVersion?: string): InstanceRowData[] {
  return instances.map(instance => {
    const status = statuses.get(instance.id)
    return { instance, status, health: getInstanceHealth(instance, status, latestVersion) }
  })
}

export function matchesFilter(row: InstanceRowData, filter: InstanceFilter): boolean {
  switch (filter) {
    case 'attention': return row.health.needsAttention
    case 'running': return row.health.state === 'running' || row.health.state === 'starting'
    case 'stopped': return row.health.state === 'stopped'
    case 'error': return row.health.state === 'error'
    case 'update': return row.health.outdated
    default: return true
  }
}

export function matchesSearch(row: InstanceRowData, search: string): boolean {
  const query = search.trim().toLowerCase()
  if (!query) return true
  const { instance } = row
  return `${instance.name} ${instance.key} ${instance.port ?? ''}`.toLowerCase().includes(query)
}

// sortRows puts instances that need attention first, then sorts by name.
export function sortRows(rows: InstanceRowData[]): InstanceRowData[] {
  return [...rows].sort((a, b) =>
    Number(b.health.needsAttention) - Number(a.health.needsAttention) ||
    a.instance.name.localeCompare(b.instance.name, undefined, { sensitivity: 'base' }),
  )
}

export function countFilters(rows: InstanceRowData[]): Record<InstanceFilter, number> {
  return {
    all: rows.length,
    attention: rows.filter(row => matchesFilter(row, 'attention')).length,
    running: rows.filter(row => matchesFilter(row, 'running')).length,
    stopped: rows.filter(row => matchesFilter(row, 'stopped')).length,
    error: rows.filter(row => matchesFilter(row, 'error')).length,
    update: rows.filter(row => matchesFilter(row, 'update')).length,
  }
}

export function rowSubtitle(row: InstanceRowData): string {
  const { health, instance } = row
  if (health.state === 'error') return health.errorMessage || 'Error'
  if (health.needsLogin) return 'WhatsApp not logged in'
  if (health.state === 'running') {
    const devices = health.devices
    const deviceText = devices === undefined ? 'Running' : `${devices} device${devices === 1 ? '' : 's'} connected`
    return instance.port ? `${deviceText} · :${instance.port}` : deviceText
  }
  if (health.state === 'starting') return 'Starting…'
  return 'Stopped'
}

export function formatUptime(uptime: number | null | undefined): string {
  if (!uptime) return '—'
  const minutes = Math.floor(uptime / 60000)
  const hours = Math.floor(minutes / 60)
  const days = Math.floor(hours / 24)
  if (days > 0) return `${days}d ${hours % 24}h`
  if (hours > 0) return `${hours}h ${minutes % 60}m`
  return `${minutes}m`
}

export function formatMemory(memoryMB: number | undefined): string {
  if (memoryMB === undefined) return '—'
  return memoryMB >= 1024 ? `${(memoryMB / 1024).toFixed(1)} GB` : `${memoryMB.toFixed(0)} MB`
}
