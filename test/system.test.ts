import { describe, it, expect, beforeAll, afterAll } from 'bun:test';
import { Elysia } from 'elysia';
import { testDb } from './test-db';
import { systemModule } from '../src/modules/system';
import { SystemService } from '../src/modules/system/service';

// Mock the SystemService for testing
const originalIsPortAvailable = SystemService.isPortAvailable;

describe('System API Endpoints', () => {
  let app: Elysia;

  beforeAll(() => {
    // Create a test app with the system module
    app = new Elysia()
      .use(systemModule);
    
    // Mock isPortAvailable to avoid actual network checks
    SystemService.isPortAvailable = async (port: number) => {
      // Mock implementation: port 3000 is unavailable, others are available
      if (port === 3000) return false;
      return true;
    };
  });

  it('GET /api/system/config should return system configuration', async () => {
    const response = await app.handle(
      new Request('http://localhost/api/system/config')
    );
    
    expect(response.status).toBe(200);
    
    const data = await response.json();
    expect(data).toBeDefined();
    expect(data.port_range).toBeDefined();
    expect(data.port_range.min).toBeDefined();
    expect(data.port_range.max).toBeDefined();
    expect(data.data_directory).toBeDefined();
    expect(data.binaries_directory).toBeDefined();
  });

  it('GET /api/system/ports/next should return the next available port', async () => {
    const response = await app.handle(
      new Request('http://localhost/api/system/ports/next')
    );
    
    expect(response.status).toBe(200);
    
    const data = await response.json();
    expect(data).toBeDefined();
    expect(data.port).toBeDefined();
    expect(typeof data.port).toBe('number');
    expect(data.port).toBeGreaterThanOrEqual(8000);
  });

  it('GET /api/system/ports/:port/available should return port availability', async () => {
    // Test with port 3000 (unavailable)
    const response1 = await app.handle(
      new Request('http://localhost/api/system/ports/3000/available')
    );
    
    expect(response1.status).toBe(200);
    
    const data1 = await response1.json();
    expect(data1).toBeDefined();
    expect(data1.port).toBe(3000);
    expect(data1.available).toBe(false);

    // Test with port 8001 (available)
    const response2 = await app.handle(
      new Request('http://localhost/api/system/ports/8001/available')
    );
    
    expect(response2.status).toBe(200);
    
    const data2 = await response2.json();
    expect(data2).toBeDefined();
    expect(data2.port).toBe(8001);
    expect(data2.available).toBe(true);
  });

  // Restore original implementation after tests
  afterAll(() => {
    SystemService.isPortAvailable = originalIsPortAvailable;
  });
});
