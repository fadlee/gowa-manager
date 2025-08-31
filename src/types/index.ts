// Common API Response Types
export type ApiResponse = {
  message: string;
  success: true;
}

export type ErrorResponse = {
  error: string;
  success: false;
}

export type ValidationError = {
  error: string;
  success: false;
}

// Instance Types
export namespace Instance {
  export type CreateRequest = {
    name?: string;
    config?: string;
  }

  export type UpdateRequest = {
    name?: string;
    config?: string;
  }

  export type Response = {
    id: number;
    key: string;
    name: string;
    port: number | null;
    status: string;
    config: string;
    created_at: string;
    updated_at: string;
  }

  export type ListResponse = Response[];

  export type ControlAction = {
    action: 'start' | 'stop' | 'restart';
  }

  export type StatusResponse = {
    id: number;
    name: string;
    status: string;
    port: number | null;
    pid: number | null;
    uptime: number | null;
  }

  export type NotFoundError = {
    error: 'Instance not found';
    success: false;
  }

  export type SuccessResponse = {
    success: true;
    message: string;
  }
}

// System Types
export namespace System {
  export type StatusResponse = {
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

  export type PortInfo = {
    port: number;
    is_allocated: boolean;
    instance_id: number | null;
  }

  export type PortsResponse = PortInfo[];

  export type ConfigResponse = {
    port_range: {
      min: number;
      max: number;
    };
    data_directory: string;
    binaries_directory: string;
  }

  export type PortAvailabilityResponse = {
    available: boolean;
    port: number | null;
  }

  export type PortCheckResponse = {
    port: number;
    available: boolean;
  }
}

// Proxy Types
export namespace Proxy {
  export const PREFIX = 'app';

  export type Request = {
    instanceId: string;
    path: string;
    method: string;
    headers: Record<string, string>;
    body?: any;
  }

  export type Response = {
    status: number;
    headers: Record<string, string>;
    body: any;
  }

  export type InstanceNotFoundError = {
    error: 'Instance not found';
    success: false;
  }

  export type InstanceOfflineError = {
    error: string;
    success: false;
    instanceId?: string;
  }

  export type ProxyError = {
    error: string;
    success: false;
  }

  export type Status = {
    instanceId: string;
    instanceName: string;
    status: string;
    port: number | null;
    targetPort: number | null;
    proxyPath: string;
  }

  export type StatusList = Status[];

  export type HealthResponse = {
    instanceId: string;
    healthy: boolean;
    status: string;
  }

  export type HealthErrorResponse = {
    error: string;
    success: false;
  }

  export type WSConnectionStatus = {
    instanceId: string;
    connected: boolean;
    targetUrl?: string;
  }

  export type WSMessage = {
    type?: string;
    data: any;
  }

  export type WSError = {
    error: string;
    instanceId: string;
  }
}
