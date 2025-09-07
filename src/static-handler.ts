import { existsSync, readFileSync } from 'fs'
import { join } from 'path'

// Try to import embedded files (only available after build)
let embeddedFiles: any = null
let getEmbeddedFileContent: any = null

try {
  const embedded = require('./embedded-static')
  embeddedFiles = embedded.embeddedFiles
  getEmbeddedFileContent = embedded.getEmbeddedFileContent
} catch {
  // Embedded files not available (development mode)
}

export function getStaticFile(path: string): { content: string | Buffer, contentType: string } | null {
  // Normalize path
  const normalizedPath = path.startsWith('/') ? path : '/' + path
  
  // Try embedded files first (production)
  if (embeddedFiles && getEmbeddedFileContent) {
    const embeddedFile = embeddedFiles[normalizedPath]
    if (embeddedFile) {
      const content = getEmbeddedFileContent(normalizedPath)
      return {
        content,
        contentType: embeddedFile.contentType
      }
    }
    
    // Also try /index.html for SPA routing
    if (normalizedPath === '/' || normalizedPath === '/index.html') {
      const indexFile = embeddedFiles['/index.html']
      if (indexFile) {
        const content = getEmbeddedFileContent('/index.html')
        return {
          content,
          contentType: indexFile.contentType
        }
      }
    }
    
    return null
  }
  
  // Fallback to filesystem (development)
  const publicDir = join(process.cwd(), 'public')
  let filePath: string
  
  if (normalizedPath === '/') {
    filePath = join(publicDir, 'index.html')
  } else {
    filePath = join(publicDir, normalizedPath.substring(1))
  }
  
  if (!existsSync(filePath)) {
    // Try index.html for SPA routing
    const indexPath = join(publicDir, 'index.html')
    if (existsSync(indexPath)) {
      filePath = indexPath
    } else {
      return null
    }
  }
  
  try {
    const content = readFileSync(filePath)
    const contentType = getContentType(filePath)
    
    return { content, contentType }
  } catch {
    return null
  }
}

function getContentType(filePath: string): string {
  const ext = filePath.split('.').pop()?.toLowerCase()
  
  const mimeTypes: Record<string, string> = {
    'html': 'text/html; charset=utf-8',
    'css': 'text/css; charset=utf-8',
    'js': 'application/javascript; charset=utf-8',
    'json': 'application/json; charset=utf-8',
    'png': 'image/png',
    'jpg': 'image/jpeg',
    'jpeg': 'image/jpeg',
    'gif': 'image/gif',
    'svg': 'image/svg+xml; charset=utf-8',
    'ico': 'image/x-icon',
    'woff': 'font/woff',
    'woff2': 'font/woff2',
    'ttf': 'font/ttf'
  }
  
  return mimeTypes[ext || ''] || 'application/octet-stream'
}
