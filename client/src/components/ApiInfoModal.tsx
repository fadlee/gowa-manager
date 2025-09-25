import { useState, useEffect } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from './ui/dialog'
import { Badge } from './ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Info } from 'lucide-react'
import type { Instance, InstanceConfig, BasicAuthPair } from '../types'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { CopyButton } from '@/components/ui/shadcn-io/copy-button'

interface ApiInfoModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instance: Instance
}

export function ApiInfoModal({ open, onOpenChange, instance }: ApiInfoModalProps) {
  const [parseError, setParseError] = useState<string | null>(null)
  const [basicAuthPairs, setBasicAuthPairs] = useState<BasicAuthPair[]>([])

  // Parse config on open (side effect)
  useEffect(() => {
    if (!open) return
    try {
      const config: InstanceConfig = JSON.parse(instance.config || '{}')
      const pairs = config.flags?.basicAuth || []
      setBasicAuthPairs(pairs)
      setParseError(null)
    } catch (error) {
      setParseError('Invalid configuration: Could not parse JSON. Check instance config.')
      setBasicAuthPairs([])
    }
  }, [open, instance.config])

  const proxyUrl = `${window.location.origin}/app/${instance.key}`

  const generateToken = (pair: BasicAuthPair): string => {
    return `Basic ${btoa(`${pair.username}:${pair.password}`)}`
  }

  const generateCurl = (token: string, url: string): string => {
    return `curl -H "Authorization: ${token}" "${url}"`
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[600px] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex gap-2 items-center">
            <Info className="w-5 h-5" />
            Gowa API Information for {instance.name}
          </DialogTitle>
          <DialogDescription>
            Access details for the proxied Gowa instance API/UI.
          </DialogDescription>
        </DialogHeader>

        {parseError && (
          <Alert variant="destructive" className="mb-4">
            <AlertDescription>{parseError}</AlertDescription>
          </Alert>
        )}

        <div className="space-y-6">
          {/* API URL */}
          <div className="space-y-2">
            <h3 className="text-sm font-medium text-gray-900">API Base URL</h3>
            <div className="flex justify-between items-center">
              <code className="px-3 py-1 font-mono text-sm break-all bg-gray-100 rounded-md">
                {proxyUrl}
              </code>
              <CopyButton
                variant="ghost"
                size="sm"
                className="shrink-0"
                content={proxyUrl}
                aria-label="Copy API base URL"
              />
            </div>
          </div>

          {/* Auth Tokens */}
          <div className="space-y-2">
            <h3 className="text-sm font-medium text-gray-900">Authentication Tokens</h3>
            {basicAuthPairs.length === 0 ? (
              <Alert>
                <Info className="w-4 h-4" />
                <AlertDescription>No basic auth configured for this instance.</AlertDescription>
              </Alert>
            ) : (
              <div className="space-y-2">
                {basicAuthPairs.length === 1 ? (
                  <div className="flex justify-between items-center">
                    <code className="flex-1 px-3 py-1 mr-2 font-mono text-sm break-all bg-gray-100 rounded-md">
                      {generateToken(basicAuthPairs[0])}
                    </code>
                    <CopyButton
                      variant="ghost"
                      size="sm"
                      className="shrink-0"
                      content={generateToken(basicAuthPairs[0])}
                      aria-label="Copy basic auth token"
                    />
                  </div>
                ) : (
                  <Accordion type="single" collapsible className="w-full">
                    {basicAuthPairs.map((pair, index) => (
                      <AccordionItem key={index} value={`item-${index}`}>
                        <AccordionTrigger className="hover:no-underline">
                          <div className="flex gap-2 items-center">
                            <Badge variant="secondary" className="text-xs">
                              Auth {index + 1}
                            </Badge>
                            <span className="text-sm font-medium">{pair.username}</span>
                          </div>
                        </AccordionTrigger>
                        <AccordionContent>
                          <div className="pt-2 space-y-2">
                            <div className="flex justify-between items-center">
                              <code className="flex-1 px-3 py-1 mr-2 font-mono text-sm break-all bg-gray-100 rounded-md">
                                {generateToken(pair)}
                              </code>
                              <CopyButton
                                variant="ghost"
                                size="sm"
                                className="shrink-0"
                                content={generateToken(pair)}
                                aria-label={`Copy basic auth token for ${pair.username}`}
                              />
                            </div>
                          </div>
                        </AccordionContent>
                      </AccordionItem>
                    ))}
                  </Accordion>
                )}
              </div>
            )}
          </div>

          {/* Curl Samples */}
          <div className="space-y-2">
            <h3 className="text-sm font-medium text-gray-900">Curl Sample</h3>
            {basicAuthPairs.length === 0 ? (
              <div className="flex gap-2 items-start">
                <pre className="flex-1 p-3 font-mono text-xs whitespace-pre-wrap break-words bg-gray-100 rounded-md border">
                  {`curl "${proxyUrl}/app/devices"`}
                </pre>
                <CopyButton
                  variant="ghost"
                  size="sm"
                  className="shrink-0"
                  content={`curl "${proxyUrl}/app/devices"`}
                  aria-label="Copy curl command"
                />
              </div>
            ) : (
              <div className="space-y-3">
                {basicAuthPairs.map((pair, index) => (
                  <div key={index}>
                    <div className="flex gap-2 items-center mb-1">
                      <Badge variant="secondary" className="text-xs">
                        Auth {index + 1}
                      </Badge>
                      <span className="text-xs text-gray-600">{pair.username}</span>
                    </div>
                    <div className="flex gap-2 items-start">
                      <pre className="flex-1 p-3 font-mono text-xs whitespace-pre-wrap break-words bg-gray-100 rounded-md border">
                        {generateCurl(generateToken(pair), proxyUrl + '/app/devices')}
                      </pre>
                      <CopyButton
                        variant="ghost"
                        size="sm"
                        className="shrink-0"
                        content={generateCurl(generateToken(pair), proxyUrl + '/app/devices')}
                        aria-label={`Copy curl command for auth ${index + 1}`}
                      />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
