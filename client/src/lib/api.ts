import type {
  Instance,
  CreateInstanceRequest,
  UpdateInstanceRequest,
  InstanceStatus,
  InstanceDevicesResponse,
  InstanceFilesResponse,
  InstanceFilePreviewResponse,
  InstanceLogsResponse,
  ApiSuccess,
  AdminLinkResponse,
  SystemStatus,
  VersionInfo,
} from '../types';

const API_BASE = '/api';
const MANAGER_RELEASE_URL = 'https://api.github.com/repos/fadlee/gowa-manager/releases/latest';
const MANAGER_VERSION_CHECK_DISABLED = import.meta.env.VITE_DISABLE_MANAGER_VERSION_CHECK === '1';

const buildPathQuery = (path?: string) => {
  if (!path) return '';
  return `?path=${encodeURIComponent(path)}`;
};

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

  async resetInstanceData(id: number): Promise<ApiSuccess> {
    return this.request<ApiSuccess>(`/instances/${id}/reset-data`, {
      method: 'POST',
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

  async killInstance(id: number): Promise<InstanceStatus> {
    return this.request<InstanceStatus>(`/instances/${id}/kill`, {
      method: 'POST',
    });
  }

  async getInstanceStatus(id: number): Promise<InstanceStatus> {
    return this.request<InstanceStatus>(`/instances/${id}/status`);
  }

  async getInstanceDevices(id: number): Promise<InstanceDevicesResponse> {
    return this.request<InstanceDevicesResponse>(`/instances/${id}/devices`);
  }

  async getInstanceFiles(id: number, path?: string): Promise<InstanceFilesResponse> {
    return this.request<InstanceFilesResponse>(`/instances/${id}/files${buildPathQuery(path)}`);
  }

  async previewInstanceFile(id: number, path: string): Promise<InstanceFilePreviewResponse> {
    return this.request<InstanceFilePreviewResponse>(`/instances/${id}/files/preview${buildPathQuery(path)}`);
  }

  async getInstanceLogs(id: number, tail: number = 200): Promise<InstanceLogsResponse> {
    return this.request<InstanceLogsResponse>(`/instances/${id}/logs?tail=${tail}`);
  }

  getInstanceFileDownloadUrl(id: number, path: string): string {
    return `${API_BASE}/instances/${id}/files/download${buildPathQuery(path)}`;
  }

  async createAdminLink(id: number): Promise<AdminLinkResponse> {
    return this.request<AdminLinkResponse>(`/instances/${id}/admin-link`, {
      method: 'POST',
    });
  }

  async testInstanceConnection(id: number): Promise<{ ok: boolean; status?: number; message: string; body?: string }> {
    return this.request<{ ok: boolean; status?: number; message: string; body?: string }>(`/instances/${id}/test-connection`, {
      method: 'POST',
    });
  }

  // System management
  async getSystemStatus(): Promise<SystemStatus> {
    return this.request<SystemStatus>('/system/status');
  }

  async getLatestManagerVersion(): Promise<string | null> {
    if (MANAGER_VERSION_CHECK_DISABLED) return null;

    try {
      const response = await fetch(MANAGER_RELEASE_URL, {
        headers: { Accept: 'application/vnd.github+json' },
      });

      if (!response.ok) return null;

      const release = await response.json() as { tag_name?: unknown };
      return typeof release.tag_name === 'string' ? release.tag_name : null;
    } catch {
      return null;
    }
  }

  // Version management
  async getInstalledVersions(): Promise<VersionInfo[]> {
    return this.request<VersionInfo[]>('/system/versions/installed');
  }

  async getAvailableVersions(limit: number = 10): Promise<VersionInfo[]> {
    return this.request<VersionInfo[]>(`/system/versions/available?limit=${limit}`);
  }

  async installVersion(version: string): Promise<ApiSuccess> {
    return this.request<ApiSuccess>('/system/versions/install', {
      method: 'POST',
      body: JSON.stringify({ version }),
    });
  }

  async removeVersion(version: string): Promise<ApiSuccess> {
    return this.request<ApiSuccess>(`/system/versions/${version}`, {
      method: 'DELETE',
    });
  }

  async isVersionAvailable(version: string): Promise<{ version: string; available: boolean; path: string }> {
    return this.request(`/system/versions/${version}/available`);
  }

  // Proxy utilities
  getProxyUrl(instanceKey: string): string {
    return window.location.origin + `/app/${instanceKey}/`;
  }
}

export const apiClient = new ApiClient();
