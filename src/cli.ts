import { exit } from 'process'

export interface CliConfig {
  port: number
  adminUsername: string
  adminPassword: string
  dataDir: string
  help: boolean
  version: boolean
}

function showHelp() {
  const helpText = `
🚀 GOWA Manager - WhatsApp Instance Manager

USAGE:
  gowa-manager [OPTIONS]

OPTIONS:
  -p, --port <port>              Server port (default: 3000)
  -u, --admin-username <user>    Admin username (default: admin)
  -P, --admin-password <pass>    Admin password (default: password)
  -d, --data-dir <path>          Data directory (default: ./data)
  -h, --help                     Show this help message
  -v, --version                  Show version information

EXAMPLES:
  gowa-manager                                    # Run with defaults
  gowa-manager --port 8080                       # Custom port
  gowa-manager -u admin -P mypassword            # Custom credentials
  gowa-manager --port 8080 -u myuser -P mypass   # Full custom config

ENVIRONMENT VARIABLES:
  PORT              Server port
  ADMIN_USERNAME    Admin username
  ADMIN_PASSWORD    Admin password
  DATA_DIR          Data directory

Note: Command line arguments take precedence over environment variables.

For more information, visit: https://github.com/fadlee/gowa-manager
`
  console.log(helpText)
}

function showVersion() {
  // Try to read package.json version
  try {
    const packageJson = require('../package.json')
    console.log(`GOWA Manager v${packageJson.version}`)
  } catch {
    console.log('GOWA Manager (version unknown)')
  }
  console.log('Built with Bun and Elysia')
}

function parsePort(value: string): number {
  const port = parseInt(value, 10)
  if (isNaN(port) || port < 1 || port > 65535) {
    console.error(`❌ Invalid port: ${value}. Port must be between 1 and 65535.`)
    exit(1)
  }
  return port
}

function validateUsername(username: string): string {
  if (username.length < 1) {
    console.error('❌ Username cannot be empty')
    exit(1)
  }
  if (username.length > 50) {
    console.error('❌ Username cannot be longer than 50 characters')
    exit(1)
  }
  return username
}

function validatePassword(password: string): string {
  if (password.length < 1) {
    console.error('❌ Password cannot be empty')
    exit(1)
  }
  if (password.length > 100) {
    console.error('❌ Password cannot be longer than 100 characters')
    exit(1)
  }
  return password
}

export function parseCliArgs(args: string[]): CliConfig {
  const config: CliConfig = {
    // Default values with environment variable fallbacks
    port: parseInt(process.env.PORT || '3000', 10),
    adminUsername: process.env.ADMIN_USERNAME || 'admin',
    adminPassword: process.env.ADMIN_PASSWORD || 'password',
    dataDir: process.env.DATA_DIR || './data',
    help: false,
    version: false
  }

  let i = 0
  while (i < args.length) {
    const arg = args[i]

    switch (arg) {
      case '-h':
      case '--help':
        config.help = true
        break

      case '-v':
      case '--version':
        config.version = true
        break

      case '-p':
      case '--port':
        if (i + 1 >= args.length) {
          console.error(`❌ Missing value for ${arg}`)
          exit(1)
        }
        config.port = parsePort(args[i + 1])
        i++ // Skip next argument
        break

      case '-u':
      case '--admin-username':
        if (i + 1 >= args.length) {
          console.error(`❌ Missing value for ${arg}`)
          exit(1)
        }
        config.adminUsername = validateUsername(args[i + 1])
        i++ // Skip next argument
        break

      case '-P':
      case '--admin-password':
        if (i + 1 >= args.length) {
          console.error(`❌ Missing value for ${arg}`)
          exit(1)
        }
        config.adminPassword = validatePassword(args[i + 1])
        i++ // Skip next argument
        break

      case '-d':
      case '--data-dir':
        if (i + 1 >= args.length) {
          console.error(`❌ Missing value for ${arg}`)
          exit(1)
        }
        config.dataDir = args[i + 1]
        i++ // Skip next argument
        break

      default:
        if (arg.startsWith('-')) {
          console.error(`❌ Unknown option: ${arg}`)
          console.error('Use --help to see available options')
          exit(1)
        } else {
          console.error(`❌ Unexpected argument: ${arg}`)
          console.error('Use --help to see usage information')
          exit(1)
        }
    }

    i++
  }

  // Handle help and version
  if (config.help) {
    showHelp()
    exit(0)
  }

  if (config.version) {
    showVersion()
    exit(0)
  }

  return config
}

export function getConfig(): CliConfig {
  // Get command line arguments (skip node/bun and script name)
  let args = process.argv.slice(2)

  // Fix for Linux/Windows: If the first argument is the binary path itself, remove it
  if (args.length > 0 && (
      args[0].endsWith('gowa-manager') ||
      args[0].endsWith('.exe') ||
      args[0].includes('/gowa-manager-') ||
      args[0].includes('\\gowa-manager-')
    )) {
    args = args.slice(1)
  }

  return parseCliArgs(args)
}
