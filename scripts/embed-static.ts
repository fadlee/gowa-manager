#!/usr/bin/env bun
/**
 * Embed static files into TypeScript for binary compilation
 */
import { readdirSync, statSync, readFileSync, writeFileSync, existsSync, mkdirSync } from 'fs'
import { join, relative, extname } from 'path'

const PUBLIC_DIR = join(process.cwd(), 'public')
const EMBEDDED_FILE = join(process.cwd(), 'src', 'embedded-static.ts')

interface EmbeddedFile {
  path: string
  content: string
  contentType: string
  encoding: 'utf8' | 'base64'
}

function getContentType(filePath: string): string {
  const ext = extname(filePath).toLowerCase()
  const mimeTypes: Record<string, string> = {
    '.html': 'text/html; charset=utf-8',
    '.css': 'text/css; charset=utf-8',
    '.js': 'application/javascript; charset=utf-8',
    '.json': 'application/json; charset=utf-8',
    '.png': 'image/png',
    '.jpg': 'image/jpeg',
    '.jpeg': 'image/jpeg',
    '.gif': 'image/gif',
    '.svg': 'image/svg+xml; charset=utf-8',
    '.ico': 'image/x-icon',
    '.woff': 'font/woff',
    '.woff2': 'font/woff2',
    '.ttf': 'font/ttf',
    '.eot': 'application/vnd.ms-fontobject'
  }
  return mimeTypes[ext] || 'application/octet-stream'
}

function isTextFile(filePath: string): boolean {
  const textExtensions = ['.html', '.css', '.js', '.json', '.svg', '.txt', '.md']
  return textExtensions.includes(extname(filePath).toLowerCase())
}

function getAllFiles(dir: string): string[] {
  if (!existsSync(dir)) {
    console.warn(`⚠️  Directory ${dir} does not exist`)
    return []
  }

  let files: string[] = []
  
  function walkDir(currentDir: string) {
    const items = readdirSync(currentDir)
    
    for (const item of items) {
      const fullPath = join(currentDir, item)
      const stat = statSync(fullPath)
      
      if (stat.isDirectory()) {
        walkDir(fullPath)
      } else {
        files.push(fullPath)
      }
    }
  }
  
  walkDir(dir)
  return files
}

function embedFiles() {
  console.log('📦 Embedding static files...')
  
  const files = getAllFiles(PUBLIC_DIR)
  const embeddedFiles: EmbeddedFile[] = []
  
  for (const filePath of files) {
    const relativePath = '/' + relative(PUBLIC_DIR, filePath).replace(/\\/g, '/')
    const isText = isTextFile(filePath)
    const encoding = isText ? 'utf8' : 'base64'
    const content = readFileSync(filePath, encoding)
    const contentType = getContentType(filePath)
    
    embeddedFiles.push({
      path: relativePath,
      content,
      contentType,
      encoding
    })
    
    console.log(`  ✓ ${relativePath} (${encoding})`)
  }
  
  // Generate TypeScript file
  const tsContent = `// Auto-generated file - do not edit
// Generated on: ${new Date().toISOString()}

export interface EmbeddedFile {
  path: string
  content: string
  contentType: string
  encoding: 'utf8' | 'base64'
}

export const embeddedFiles: Record<string, EmbeddedFile> = {
${embeddedFiles.map(file => `  ${JSON.stringify(file.path)}: {
    path: ${JSON.stringify(file.path)},
    content: ${JSON.stringify(file.content)},
    contentType: ${JSON.stringify(file.contentType)},
    encoding: ${JSON.stringify(file.encoding)}
  }`).join(',\n')}
}

export function getEmbeddedFile(path: string): EmbeddedFile | null {
  return embeddedFiles[path] || null
}

export function getEmbeddedFileContent(path: string): Buffer | string | null {
  const file = getEmbeddedFile(path)
  if (!file) return null
  
  if (file.encoding === 'base64') {
    return Buffer.from(file.content, 'base64')
  }
  
  return file.content
}

export const embeddedFilePaths = Object.keys(embeddedFiles)
`

  // Ensure src directory exists
  const srcDir = join(process.cwd(), 'src')
  if (!existsSync(srcDir)) {
    mkdirSync(srcDir, { recursive: true })
  }
  
  writeFileSync(EMBEDDED_FILE, tsContent, 'utf8')
  
  console.log(`✅ Embedded ${embeddedFiles.length} files into ${EMBEDDED_FILE}`)
  console.log(`📁 Total size: ${embeddedFiles.reduce((acc, f) => acc + f.content.length, 0)} bytes`)
}

embedFiles()
