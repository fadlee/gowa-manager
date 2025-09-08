import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../lib/api'
import { Button } from './ui/button'
import { Badge } from './ui/badge'
import { Download, AlertCircle, CheckCircle2 } from 'lucide-react'

interface VersionSelectorProps {
  value: string
  onChange: (version: string) => void
  disabled?: boolean
}

export function VersionSelector({ value, onChange, disabled }: VersionSelectorProps) {
  const [isInstalling, setIsInstalling] = useState(false)
  const queryClient = useQueryClient()

  // Get installed versions
  const { data: installedVersions = [], refetch: refetchInstalled } = useQuery({
    queryKey: ['versions', 'installed'],
    queryFn: () => apiClient.getInstalledVersions(),
  })

  // Get available versions
  const { data: availableVersions = [], refetch: refetchAvailable } = useQuery({
    queryKey: ['versions', 'available'],
    queryFn: () => apiClient.getAvailableVersions(5),
  })

  // Combine and deduplicate versions
  const allVersions = [
    ...availableVersions.slice(0, 6), // Show available versions from GitHub (includes 'latest' as first option)
    ...installedVersions.filter(v => 
      // Only add installed versions that aren't already in available list
      !availableVersions.some(av => av.version === v.version)
    )
  ]

  const selectedVersion = allVersions.find(v => v.version === value) || 
    { version: value, installed: false, isLatest: false }

  const handleInstallVersion = async (version: string) => {
    setIsInstalling(true)
    try {
      await apiClient.installVersion(version)
      
      // Invalidate and refetch all version-related queries
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['versions', 'installed'] }),
        queryClient.invalidateQueries({ queryKey: ['versions', 'available'] }),
        refetchInstalled(),
        refetchAvailable()
      ])
      
      // Keep the currently selected version (it should now show as installed)
      // Don't need to change selection since user already selected this version
    } catch (error) {
      console.error('Failed to install version:', error)
    } finally {
      setIsInstalling(false)
    }
  }

  return (
    <div className="space-y-2">
      <label className="text-sm font-medium text-gray-700">
        GOWA Version
      </label>
      
      <div className="flex gap-2 items-center">
        <select
          value={value}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          className="flex-1 px-3 py-2 border border-gray-300 rounded-md bg-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        >
          {allVersions.map((version) => (
            <option key={version.version} value={version.version}>
              {version.version} {version.isLatest ? '(Latest)' : ''} {!version.installed ? '(Not Installed)' : ''}
            </option>
          ))}
        </select>

        {!selectedVersion?.installed && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => handleInstallVersion(value)}
            disabled={isInstalling || disabled}
            className="flex items-center gap-1"
          >
            <Download className="h-4 w-4" />
            {isInstalling ? 'Installing...' : 'Install'}
          </Button>
        )}
      </div>

      {/* Status indicators */}
      <div className="flex items-center gap-2 text-sm">
        {selectedVersion?.installed ? (
          <div className="flex items-center gap-1 text-green-600">
            <CheckCircle2 className="h-4 w-4" />
            <span>Installed</span>
          </div>
        ) : (
          <div className="flex items-center gap-1 text-yellow-600">
            <AlertCircle className="h-4 w-4" />
            <span>Not installed</span>
          </div>
        )}
        {selectedVersion?.isLatest && (
          <Badge variant="secondary" className="text-xs">
            Latest
          </Badge>
        )}
      </div>

      {!selectedVersion?.installed && (
        <p className="text-xs text-yellow-600 flex items-center gap-1">
          <AlertCircle className="h-3 w-3" />
          This version needs to be installed before creating an instance
        </p>
      )}
    </div>
  )
}
