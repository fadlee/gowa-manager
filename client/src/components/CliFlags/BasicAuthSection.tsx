import { useState } from 'react'
import { X, Plus, Eye, EyeOff, Copy, Check } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import type { CliFlags } from '../../types'

interface BasicAuthSectionProps {
  flags: CliFlags
  updateFlag: (key: keyof CliFlags, value: any) => void
}

export function BasicAuthSection({ flags, updateFlag }: BasicAuthSectionProps) {
  const [newAuth, setNewAuth] = useState({ username: '', password: '' })
  const [visiblePasswords, setVisiblePasswords] = useState<{[key: number]: boolean}>({})
  const [copiedStates, setCopiedStates] = useState<{[key: number]: boolean}>({})

  const addBasicAuth = (e?: React.MouseEvent | React.KeyboardEvent) => {
    e?.preventDefault()
    if (newAuth.username.trim() && newAuth.password.trim()) {
      const basicAuth = flags.basicAuth || []
      const newBasicAuth = [...basicAuth, { ...newAuth }]
      updateFlag('basicAuth', newBasicAuth)
      setNewAuth({ username: '', password: '' })
    }
  }

  const handleAuthBlur = (field: 'username' | 'password') => {
    // Auto-add on password blur if both fields are filled
    if (field === 'password' && newAuth.username.trim() && newAuth.password.trim()) {
      addBasicAuth()
    }
  }

  const removeBasicAuth = (index: number, e: React.MouseEvent) => {
    e.preventDefault()
    const basicAuth = flags.basicAuth || []
    updateFlag('basicAuth', basicAuth.filter((_, i) => i !== index))
  }

  const handleAuthKeyDown = (e: React.KeyboardEvent, field: 'username' | 'password') => {
    if (e.key === 'Enter') {
      e.preventDefault()
      if (field === 'password' && newAuth.username.trim() && newAuth.password.trim()) {
        addBasicAuth(e)
      }
    }
  }
  
  const togglePasswordVisibility = (index: number) => {
    setVisiblePasswords(prev => ({
      ...prev,
      [index]: !prev[index]
    }))
  }
  
  const copyToClipboard = (text: string, index: number) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopiedStates(prev => ({ ...prev, [index]: true }))
      setTimeout(() => {
        setCopiedStates(prev => ({ ...prev, [index]: false }))
      }, 2000)
    })
  }

  return (
    <div className="space-y-3">
      <label className="text-sm font-medium text-gray-300">Basic Authentication</label>
      <p className="text-xs text-gray-400">Add username:password pairs for basic auth</p>

      {/* Existing Auth Pairs */}
      {flags.basicAuth && flags.basicAuth.length > 0 && (
        <div className="space-y-2">
          {flags.basicAuth.map((auth, index) => (
            <div key={index} className="flex gap-2 items-center p-2 bg-gray-700 rounded-md">
              <span className="flex-1 font-mono text-sm text-white">
                {auth.username}:
                <span className="ml-1">
                  {visiblePasswords[index] ? auth.password : "*".repeat(auth.password.length)}
                </span>
              </span>
              <div className="flex gap-1">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => togglePasswordVisibility(index)}
                        className="p-0 w-6 h-6"
                      >
                        {visiblePasswords[index] ? (
                          <EyeOff className="w-3 h-3" />
                        ) : (
                          <Eye className="w-3 h-3" />
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>{visiblePasswords[index] ? 'Hide' : 'Show'} password</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
                
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => copyToClipboard(auth.password, index)}
                        className="p-0 w-6 h-6"
                      >
                        {copiedStates[index] ? (
                          <Check className="w-3 h-3 text-green-500" />
                        ) : (
                          <Copy className="w-3 h-3" />
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>{copiedStates[index] ? 'Copied!' : 'Copy password'}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
                
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={(e) => removeBasicAuth(index, e)}
                        className="p-0 w-6 h-6"
                      >
                        <X className="w-3 h-3" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Remove</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Add New Auth */}
      <div className="flex gap-2">
        <Input
          placeholder="Username"
          value={newAuth.username}
          onChange={(e) => setNewAuth({ ...newAuth, username: e.target.value })}
          onKeyDown={(e) => handleAuthKeyDown(e, 'username')}
          className="flex-1 bg-gray-700 border-gray-600 text-white"
        />
        <Input
          type="password"
          placeholder="Password"
          value={newAuth.password}
          onChange={(e) => setNewAuth({ ...newAuth, password: e.target.value })}
          onKeyDown={(e) => handleAuthKeyDown(e, 'password')}
          onBlur={() => handleAuthBlur('password')}
          className="flex-1 bg-gray-700 border-gray-600 text-white"
        />
        {flags.basicAuth && flags.basicAuth.length > 0 && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={addBasicAuth}
            disabled={!newAuth.username.trim() || !newAuth.password.trim()}
          >
            <Plus className="w-4 h-4" />
          </Button>
        )}
      </div>
    </div>
  )
}
