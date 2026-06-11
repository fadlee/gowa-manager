import { useState } from 'react'
import { Braces, BookOpen, ExternalLink, KeyRound, MessageSquare, QrCode, Server, Smartphone, Terminal, Webhook } from 'lucide-react'
import { Button } from '../ui/button'
import { CopyButton } from '../ui/shadcn-io/copy-button'
import type { BasicAuthPair, Instance, InstanceConfig } from '../../types'

interface ApiSectionProps {
  instance: Instance
}

export function ApiSection({ instance }: ApiSectionProps) {
  const proxyUrl = `${window.location.origin}/app/${instance.key}`
  const docsUrl = `${proxyUrl}/docs`
  const upstreamDocsUrl = 'https://github.com/aldinokemal/go-whatsapp-web-multidevice'
  const [activeSnippet, setActiveSnippet] = useState<'curl' | 'javascript'>('curl')

  let basicAuthPairs: BasicAuthPair[] = []
  try {
    const config: InstanceConfig = JSON.parse(instance.config || '{}')
    basicAuthPairs = config.flags?.basicAuth || []
  } catch {
    // Keep snippets auth-free when config is invalid.
  }

  const firstAuthPair = basicAuthPairs[0]
  const authHeader = firstAuthPair ? `Basic ${btoa(`${firstAuthPair.username}:${firstAuthPair.password}`)}` : null
  const devicesUrl = `${proxyUrl}/devices`
  const curlSnippet = [
    `curl -X GET '${devicesUrl}'`,
    authHeader ? `  -H 'Authorization: ${authHeader}'` : null,
    `  -H 'Accept: application/json'`,
  ].filter(Boolean).join(' \\\n')
  const jsSnippet = `const response = await fetch('${devicesUrl}', {${authHeader ? `
  headers: {
    Authorization: '${authHeader}',
    Accept: 'application/json',
  },` : `
  headers: {
    Accept: 'application/json',
  },`}
});

if (!response.ok) {
  throw new Error(\`GOWA request failed: \${response.status}\`);
}

const devices = await response.json();`
  const activeSnippetContent = activeSnippet === 'curl' ? curlSnippet : jsSnippet

  const endpoints = [
    {
      method: 'GET',
      path: '/devices',
      title: 'List devices',
      description: 'Check connected WhatsApp devices and validate credentials.',
      icon: Smartphone,
    },
    {
      method: 'POST',
      path: '/send/message',
      title: 'Send message',
      description: 'Send a WhatsApp text message through the active session.',
      icon: MessageSquare,
    },
    {
      method: 'GET',
      path: '/login/qr',
      title: 'Login QR',
      description: 'Fetch or display QR login flow when the session is not paired.',
      icon: QrCode,
    },
    {
      method: 'POST',
      path: '/webhook',
      title: 'Webhook events',
      description: 'Receive or configure events depending on active GOWA flags.',
      icon: Webhook,
    },
  ]

  return (
    <div className="space-y-8">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">API</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">Quick references for integrating with this proxied GOWA instance.</p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button asChild variant="outline" size="sm">
            <a href={docsUrl} target="_blank" rel="noopener noreferrer">
              <ExternalLink className="mr-2 h-4 w-4" />
              Open Swagger
            </a>
          </Button>
          <Button asChild variant="outline" size="sm">
            <a href={upstreamDocsUrl} target="_blank" rel="noopener noreferrer">
              <BookOpen className="mr-2 h-4 w-4" />
              Upstream Docs
            </a>
          </Button>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <div className="rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-200">
            <Server className="h-4 w-4" />
            Base URL
          </div>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded-md bg-gray-100 px-3 py-2 font-mono text-sm text-gray-900 dark:bg-gray-800 dark:text-white">
              {proxyUrl}
            </code>
            <CopyButton content={proxyUrl} variant="ghost" className="text-gray-600 dark:text-gray-400" />
          </div>
        </div>

        <div className="rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-200">
            <ExternalLink className="h-4 w-4" />
            Swagger / OpenAPI
          </div>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded-md bg-gray-100 px-3 py-2 font-mono text-sm text-gray-900 dark:bg-gray-800 dark:text-white">
              {docsUrl}
            </code>
            <CopyButton content={docsUrl} variant="ghost" className="text-gray-600 dark:text-gray-400" />
          </div>
        </div>

        <div className="rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-200">
            <KeyRound className="h-4 w-4" />
            Authentication
          </div>
          <p className="rounded-md bg-gray-100 px-3 py-2 text-sm text-gray-600 dark:bg-gray-800 dark:text-gray-300">
            {authHeader ? 'Basic auth is configured. Examples include the first credential.' : 'No basic auth configured for this instance.'}
          </p>
        </div>
      </div>

      <div className="space-y-4">
        <div>
          <h3 className="text-sm font-medium text-gray-900 dark:text-white">Quick Test</h3>
          <p className="text-sm text-gray-500 dark:text-gray-400">Start with <code className="font-mono">GET /devices</code> to verify URL and credentials.</p>
        </div>

        <div className="overflow-hidden rounded-lg border border-gray-200 bg-gray-100 dark:border-gray-700 dark:bg-gray-800">
          <div className="flex items-center justify-between border-b border-gray-200 px-3 py-2 dark:border-gray-700">
            <div className="flex rounded-md bg-white p-1 dark:bg-gray-950">
              <button
                type="button"
                onClick={() => setActiveSnippet('curl')}
                className={`inline-flex items-center gap-2 rounded px-3 py-1.5 text-sm font-medium transition-colors ${activeSnippet === 'curl'
                  ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'
                }`}
              >
                <Terminal className="h-4 w-4" />
                curl
              </button>
              <button
                type="button"
                onClick={() => setActiveSnippet('javascript')}
                className={`inline-flex items-center gap-2 rounded px-3 py-1.5 text-sm font-medium transition-colors ${activeSnippet === 'javascript'
                  ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'
                }`}
              >
                <Braces className="h-4 w-4" />
                JavaScript
              </button>
            </div>
            <CopyButton content={activeSnippetContent} variant="ghost" className="text-gray-600 dark:text-gray-400" />
          </div>
          <pre className="overflow-x-auto p-4 text-sm text-gray-800 dark:text-gray-200"><code>{activeSnippetContent}</code></pre>
        </div>
      </div>

      <div className="space-y-3">
        <h3 className="text-sm font-medium text-gray-900 dark:text-white">Common Endpoints</h3>
        <div className="grid gap-3 md:grid-cols-2">
          {endpoints.map((endpoint) => (
            <div key={endpoint.path} className="rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
              <div className="mb-3 flex items-center justify-between gap-3">
                <div className="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                  <endpoint.icon className="h-4 w-4 text-gray-500 dark:text-gray-400" />
                  {endpoint.title}
                </div>
                <span className="rounded bg-gray-100 px-2 py-1 font-mono text-xs font-semibold text-gray-700 dark:bg-gray-800 dark:text-gray-300">
                  {endpoint.method}
                </span>
              </div>
              <div className="mb-3 flex items-center gap-2">
                <code className="min-w-0 flex-1 truncate rounded-md bg-gray-100 px-3 py-2 font-mono text-sm text-gray-900 dark:bg-gray-800 dark:text-white">
                  {proxyUrl}{endpoint.path}
                </code>
                <CopyButton content={`${proxyUrl}${endpoint.path}`} variant="ghost" className="text-gray-600 dark:text-gray-400" />
              </div>
              <p className="text-sm text-gray-500 dark:text-gray-400">{endpoint.description}</p>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
