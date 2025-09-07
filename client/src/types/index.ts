// Instance Types
export interface Instance {
  id: number;
  key: string;
  name: string;
  port: number | null;
  status: string;
  config: string;
  created_at: string;
  updated_at: string;
}

export interface CreateInstanceRequest {
  name?: string;
  config?: string;
}

export interface UpdateInstanceRequest {
  name?: string;
  config?: string;
}

export interface InstanceStatus {
  id: number;
  name: string;
  status: string;
  port: number | null;
  pid: number | null;
  uptime: number | null;
  resources?: {
    cpuPercent: number;
    memoryMB: number;
    memoryPercent: number;
    avgCpu?: number;
    avgMemory?: number;
  };
}

// API Response Types
export interface ApiError {
  error: string;
  success: false;
}

export interface ApiSuccess {
  success: true;
  message: string;
}

// System Types
export interface SystemStatus {
  status: string;
  uptime: number;
  instances: {
    total: number;
    running: number;
    stopped: number;
  };
  ports: {
    allocated: number;
    next_available: number;
  };
}

// CLI Flags Configuration Types
export interface BasicAuthPair {
  username: string;
  password: string;
}

export interface CliFlags {
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

export interface InstanceConfig {
  args?: string[];
  env?: Record<string, string>;
  flags?: CliFlags;
}
