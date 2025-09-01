import type { 
  Instance, 
  CreateInstanceRequest, 
  UpdateInstanceRequest, 
  InstanceStatus,
  ApiSuccess,
  SystemStatus 
} from '../types';

const API_BASE = '/api';

class ApiClient {
  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const url = `${API_BASE}${endpoint}`;
    
    const response = await fetch(url, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...options.headers,
      },
    });

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}));
      throw new Error(errorData.error || `HTTP error! status: ${response.status}`);
    }

    return response.json();
  }

  // Instance management
  async getInstances(): Promise<Instance[]> {
    return this.request<Instance[]>('/instances');
  }

  async getInstance(id: number): Promise<Instance> {
    return this.request<Instance>(`/instances/${id}`);
  }

  async createInstance(data: CreateInstanceRequest): Promise<Instance> {
    return this.request<Instance>('/instances', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateInstance(id: number, data: UpdateInstanceRequest): Promise<Instance> {
    return this.request<Instance>(`/instances/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async deleteInstance(id: number): Promise<ApiSuccess> {
    return this.request<ApiSuccess>(`/instances/${id}`, {
      method: 'DELETE',
    });
  }

  // Instance actions
  async startInstance(id: number): Promise<InstanceStatus> {
    return this.request<InstanceStatus>(`/instances/${id}/start`, {
      method: 'POST',
    });
  }

  async stopInstance(id: number): Promise<InstanceStatus> {
    return this.request<InstanceStatus>(`/instances/${id}/stop`, {
      method: 'POST',
    });
  }

  async restartInstance(id: number): Promise<InstanceStatus> {
    return this.request<InstanceStatus>(`/instances/${id}/restart`, {
      method: 'POST',
    });
  }

  async getInstanceStatus(id: number): Promise<InstanceStatus> {
    return this.request<InstanceStatus>(`/instances/${id}/status`);
  }

  // System management
  async getSystemStatus(): Promise<SystemStatus> {
    return this.request<SystemStatus>('/system/status');
  }

  // Proxy utilities
  getProxyUrl(instanceKey: string): string {
    return `http://localhost:3000/app/${instanceKey}/`;
  }
}

export const apiClient = new ApiClient();