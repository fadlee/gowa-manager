interface BasicAuthPair {
  username: string;
  password: string;
}

interface CliFlags {
  accountValidation?: boolean;
  basicAuth?: BasicAuthPair[];
  os?: string;
  webhooks?: string[];
  autoMarkRead?: boolean;
  autoReply?: string;
  basePath?: string;
  debug?: boolean;
  webhookSecret?: string;
}

interface InstanceConfig {
  args?: string[] | string;
  env?: Record<string, string> | string;
  envVars?: string;
  flags?: CliFlags;
}

export class ConfigParser {
  // Parse configuration to get command arguments and environment
  static parseConfig(configString: string | null): InstanceConfig {
    let config: InstanceConfig = {}
    try {
      config = JSON.parse(configString || '{}')
    } catch {
      config = {}
    }
    return config
  }

  // Convert CLI flags to command arguments
  static flagsToArgs(flags: CliFlags): string[] {
    const args: string[] = []
    
    if (flags.accountValidation !== undefined) {
      args.push(`--account-validation=${flags.accountValidation}`)
    }
    
    if (flags.basicAuth && flags.basicAuth.length > 0) {
      flags.basicAuth.forEach(auth => {
        args.push(`--basic-auth=${auth.username}:${auth.password}`)
      })
    }
    
    if (flags.os) {
      args.push(`--os=${flags.os}`)
    }
    
    if (flags.webhooks && flags.webhooks.length > 0) {
      flags.webhooks.forEach(webhook => {
        args.push(`--webhook=${webhook}`)
      })
    }
    
    if (flags.autoMarkRead !== undefined) {
      args.push(`--auto-mark-read=${flags.autoMarkRead}`)
    }
    
    if (flags.autoReply) {
      args.push(`--autoreply=${flags.autoReply}`)
    }
    
    if (flags.basePath) {
      args.push(`--base-path=${flags.basePath}`)
    }
    
    if (flags.debug !== undefined) {
      args.push(`--debug=${flags.debug}`)
    }
    
    if (flags.webhookSecret) {
      args.push(`--webhook-secret=${flags.webhookSecret}`)
    }
    
    return args
  }

  // Process command arguments, replacing PORT placeholder and adding flags
  static processArgs(config: InstanceConfig, port: number): string[] {
    let args: string[] = []
    
    // Process base args
    if (config.args) {
      if (Array.isArray(config.args)) {
        args = config.args
      } else if (typeof config.args === 'string') {
        args = config.args.trim() ? config.args.trim().split(/\s+/) : []
      }
    }
    
    // Add flag-based arguments
    if (config.flags) {
      const flagArgs = this.flagsToArgs(config.flags)
      args.push(...flagArgs)
    }

    console.log(`Debug - config.args type: ${typeof config.args}, value:`, config.args)
    console.log(`Debug - processed args:`, args)

    return args.map((arg: string) =>
      arg.replace(/PORT/g, port.toString())
    )
  }

  // Parse environment variables from configuration
  static parseEnvironmentVars(config: InstanceConfig, port: number): Record<string, string> {
    let envVars: Record<string, string> = {}
    
    if (config.env) {
      if (typeof config.env === 'object') {
        envVars = config.env
      } else if (typeof config.env === 'string') {
        // Parse environment variables from string format "KEY=value KEY2=value2"
        config.env.split(/\s+/).forEach((pair: string) => {
          const [key, ...valueParts] = pair.split('=')
          if (key && valueParts.length > 0) {
            envVars[key] = valueParts.join('=')
          }
        })
      }
    } else if (config.envVars && typeof config.envVars === 'string') {
      // Handle legacy envVars field
      config.envVars.split(/\s+/).forEach((pair: string) => {
        const [key, ...valueParts] = pair.split('=')
        if (key && valueParts.length > 0) {
          envVars[key] = valueParts.join('=')
        }
      })
    }

    return {
      ...process.env,
      PORT: port.toString(),
      ...envVars
    }
  }

  // Get default configuration
  static getDefaultConfig(): InstanceConfig {
    return {
      args: ['rest', '--port=PORT'],
      flags: {
        accountValidation: true,
        os: 'GowaManager'
      }
    }
  }
}
