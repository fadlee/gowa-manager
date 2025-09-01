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