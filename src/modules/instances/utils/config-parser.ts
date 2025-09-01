export class ConfigParser {
  // Parse configuration to get command arguments and environment
  static parseConfig(configString: string | null): any {
    let config: any = {}
    try {
      config = JSON.parse(configString || '{}')
    } catch {
      config = {}
    }
    return config
  }

  // Process command arguments, replacing PORT placeholder
  static processArgs(config: any, port: number): string[] {
    let args: string[] = []
    if (config.args) {
      if (Array.isArray(config.args)) {
        // If args is already an array, use it directly
        args = config.args
      } else if (typeof config.args === 'string') {
        // If args is a string, split it by spaces (handling quoted arguments)
        args = config.args.trim() ? config.args.trim().split(/\s+/) : []
      }
    }

    console.log(`Debug - config.args type: ${typeof config.args}, value:`, config.args)
    console.log(`Debug - processed args:`, args)

    return args.map((arg: string) =>
      arg.replace(/PORT/g, port.toString())
    )
  }

  // Parse environment variables from configuration
  static parseEnvironmentVars(config: any, port: number): Record<string, string> {
    let envVars: Record<string, string> = {}
    
    if (config.env) {
      if (typeof config.env === 'object') {
        envVars = config.env
      } else if (typeof config.env === 'string' || typeof config.envVars === 'string') {
        // Parse environment variables from string format "KEY=value KEY2=value2"
        const envString = config.env || config.envVars || ''
        envString.split(/\s+/).forEach((pair: string) => {
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
  static getDefaultConfig(): any {
    return {
      args: ['rest', '--port=PORT']
    }
  }
}
