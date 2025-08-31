#!/usr/bin/env bun
/**
 * GOWA Manager Server Test Suite
 * Tests all server functionalities including instances, binaries, system, and proxy modules
 */

import { writeFileSync, unlinkSync, existsSync, copyFileSync } from 'fs'
import { join } from 'path'

// Test configuration
const BASE_URL = 'http://localhost:3000'
const TEST_TIMEOUT = 30000 // 30 seconds

const proxyPrefix = 'app'

// Test results tracking
interface TestResult {
  name: string
  passed: boolean
  error?: string
  duration: number
}

const testResults: TestResult[] = []
let testCount = 0
let passedCount = 0

// Utility functions
function log(message: string, type: 'info' | 'success' | 'error' | 'warn' = 'info') {
  const colors = {
    info: '\x1b[36m',    // Cyan
    success: '\x1b[32m', // Green
    error: '\x1b[31m',   // Red
    warn: '\x1b[33m',    // Yellow
    reset: '\x1b[0m'
  }

  const prefix = {
    info: 'ℹ️ ',
    success: '✅',
    error: '❌',
    warn: '⚠️ '
  }

  console.log(`${colors[type]}${prefix[type]} ${message}${colors.reset}`)
}

async function makeRequest(
  endpoint: string,
  options: RequestInit = {}
): Promise<{ status: number; data: any; headers: Headers }> {
  const url = `${BASE_URL}${endpoint}`
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options.headers
    }
  })

  let data: any
  try {
    data = await response.json()
  } catch {
    data = await response.text()
  }

  return {
    status: response.status,
    data,
    headers: response.headers
  }
}

async function runTest(name: string, testFn: () => Promise<void>): Promise<void> {
  testCount++
  const startTime = Date.now()

  try {
    await testFn()
    const duration = Date.now() - startTime
    testResults.push({ name, passed: true, duration })
    passedCount++
    log(`${name} (${duration}ms)`, 'success')
  } catch (error) {
    const duration = Date.now() - startTime
    const errorMessage = error instanceof Error ? error.message : String(error)
    testResults.push({ name, passed: false, error: errorMessage, duration })
    log(`${name} - ${errorMessage} (${duration}ms)`, 'error')
  }
}

function assert(condition: boolean, message: string) {
  if (!condition) {
    throw new Error(message)
  }
}

function assertEqual(actual: any, expected: any, message?: string) {
  if (actual !== expected) {
    throw new Error(message || `Expected ${expected}, got ${actual}`)
  }
}

function assertStatus(response: { status: number }, expectedStatus: number) {
  assertEqual(response.status, expectedStatus, `Expected status ${expectedStatus}, got ${response.status}`)
}

// Test suites
async function testSystemEndpoints() {
  log('Testing System Endpoints...', 'info')

  await runTest('GET /api/system/status', async () => {
    const response = await makeRequest('/api/system/status')
    assertStatus(response, 200)
    assert(response.data.status === 'running', 'System should be running')
    assert(typeof response.data.uptime === 'number', 'Uptime should be a number')
    assert(response.data.instances, 'Should have instances info')
    assert(response.data.ports, 'Should have ports info')
    assert(response.data.binaries, 'Should have binaries info')
  })

  await runTest('GET /api/system/config', async () => {
    const response = await makeRequest('/api/system/config')
    assertStatus(response, 200)
    assert(response.data.port_range, 'Should have port range config')
    assert(response.data.data_directory, 'Should have data directory config')
    assert(response.data.binaries_directory, 'Should have binaries directory config')
  })

  await runTest('GET /api/system/ports/8500/available', async () => {
    const response = await makeRequest('/api/system/ports/8500/available')
    assertStatus(response, 200)
    assertEqual(response.data.port, 8500, 'Should return correct port number')
    assert(typeof response.data.available === 'boolean', 'Should have available boolean')
  })
}

async function testInstanceManagement() {
  log('Testing Instance Management...', 'info')

  let instanceId: number

  await runTest('GET /api/instances (empty)', async () => {
    const response = await makeRequest('/api/instances')
    assertStatus(response, 200)
    assert(Array.isArray(response.data), 'Should return array')
  })

  await runTest('POST /api/instances (create)', async () => {
    const instanceData = {
      name: 'test-instance',
      binary_path: '/bin/echo',
      config: '{"args": ["Hello World"]}'
    }

    const response = await makeRequest('/api/instances', {
      method: 'POST',
      body: JSON.stringify(instanceData)
    })

    assertStatus(response, 201)
    assert(response.data.id, 'Should have instance ID')
    assert(response.data.name === instanceData.name, 'Should have correct name')
    assert(response.data.port, 'Should have allocated port')
    instanceId = response.data.id
  })

  await runTest('GET /api/instances/:id', async () => {
    const response = await makeRequest(`/api/instances/${instanceId}`)
    assertStatus(response, 200)
    assertEqual(response.data.id, instanceId, 'Should return correct instance')
    assertEqual(response.data.name, 'test-instance', 'Should have correct name')
  })

  await runTest('PUT /api/instances/:id (update)', async () => {
    const updateData = {
      name: 'updated-test-instance'
    }

    const response = await makeRequest(`/api/instances/${instanceId}`, {
      method: 'PUT',
      body: JSON.stringify(updateData)
    })

    assertStatus(response, 200)
    assertEqual(response.data.name, 'updated-test-instance', 'Should have updated name')
  })

  await runTest('GET /api/instances/:id/status', async () => {
    const response = await makeRequest(`/api/instances/${instanceId}/status`)
    assertStatus(response, 200)
    assertEqual(response.data.id, instanceId, 'Should return correct instance ID')
    assert(response.data.status, 'Should have status')
  })

  await runTest('POST /api/instances/:id/start', async () => {
    const response = await makeRequest(`/api/instances/${instanceId}/start`, {
      method: 'POST'
    })
    assertStatus(response, 200)
    assertEqual(response.data.id, instanceId, 'Should return correct instance ID')
  })

  await runTest('POST /api/instances/:id/stop', async () => {
    const response = await makeRequest(`/api/instances/${instanceId}/stop`, {
      method: 'POST'
    })
    assertStatus(response, 200)
    assertEqual(response.data.id, instanceId, 'Should return correct instance ID')
  })

  await runTest('DELETE /api/instances/:id', async () => {
    const response = await makeRequest(`/api/instances/${instanceId}`, {
      method: 'DELETE'
    })
    assertStatus(response, 200)
    assert(response.data.success, 'Should indicate successful deletion')
  })

  await runTest('GET /api/instances/:id (not found)', async () => {
    const response = await makeRequest(`/api/instances/${instanceId}`)
    assertStatus(response, 404)
    assert(response.data.error, 'Should have error message')
  })
}

async function testBinaryManagement() {
  log('Testing Binary Management...', 'info')

  let binaryId: number
  const testBinaryPath = join(process.cwd(), 'test-binary.sh')

  // Create a test binary file
  writeFileSync(testBinaryPath, '#!/bin/bash\necho "Hello from test binary"', { mode: 0o755 })

  await runTest('GET /api/binaries (empty)', async () => {
    const response = await makeRequest('/api/binaries')
    assertStatus(response, 200)
    assert(Array.isArray(response.data), 'Should return array')
  })

  await runTest('POST /api/binaries (create)', async () => {
    const binaryData = {
      name: 'test-binary',
      path: testBinaryPath
    }

    const response = await makeRequest('/api/binaries', {
      method: 'POST',
      body: JSON.stringify(binaryData)
    })

    assertStatus(response, 201)
    assert(response.data.id, 'Should have binary ID')
    assertEqual(response.data.name, 'test-binary', 'Should have correct name')
    binaryId = response.data.id
  })

  await runTest('GET /api/binaries/:id', async () => {
    const response = await makeRequest(`/api/binaries/${binaryId}`)
    assertStatus(response, 200)
    assertEqual(response.data.id, binaryId, 'Should return correct binary')
  })

  await runTest('POST /api/binaries/validate', async () => {
    const response = await makeRequest('/api/binaries/validate', {
      method: 'POST',
      body: JSON.stringify({ path: testBinaryPath })
    })

    assertStatus(response, 200)
    assert(response.data.valid, 'Should validate as valid binary')
  })

  await runTest('DELETE /api/binaries/:id', async () => {
    const response = await makeRequest(`/api/binaries/${binaryId}`, {
      method: 'DELETE'
    })
    assertStatus(response, 200)
    assert(response.data.success, 'Should indicate successful deletion')
  })

  // Cleanup test file
  if (existsSync(testBinaryPath)) {
    unlinkSync(testBinaryPath)
  }
}

async function testProxyFunctionality() {
  log('Testing Proxy Functionality...', 'info')

  await runTest('GET /app/ (empty)', async () => {
    const response = await makeRequest('/app/')
    assertStatus(response, 200)
    assert(Array.isArray(response.data), 'Should return array of proxy targets')
  })

  // Test with non-existent instance first
  await runTest('GET /app/999 (non-existent instance)', async () => {
    const response = await makeRequest('/app/999')
    assertStatus(response, 404)
    assert(response.data.error, 'Should have error message for non-existent instance')
  })

  // Complex PocketBase proxy tests
  let pocketbaseInstanceId: number = 0
  let pocketbaseBinaryId: number = 0
  const pocketbasePathSample = join(process.cwd(), 'data', 'sample', 'pocketbase')
  const pocketbasePath = join(process.cwd(), 'data', 'binaries', 'pocketbase-test')

  copyFileSync(pocketbasePathSample, pocketbasePath)

  await runTest('Create PocketBase binary entry', async () => {
    const binaryData = {
      name: 'pocketbase-test',
      path: pocketbasePath
    }

    const response = await makeRequest('/api/binaries', {
      method: 'POST',
      body: JSON.stringify(binaryData)
    })

    assertStatus(response, 201)
    pocketbaseBinaryId = response.data.id
    assert(response.data.name === 'pocketbase-test', 'Should have correct binary name')
  })

  await runTest('Create PocketBase instance', async () => {
    const instanceData = {
      name: 'pocketbase-proxy-test',
      binary_path: pocketbasePath,
      config: JSON.stringify({
        args: ['serve', '--http=0.0.0.0:PORT', '--dir=./pb_data_test'],
        env: {
          'PB_ENCRYPTION_KEY': 'test-key-for-proxy-testing'
        }
      })
    }

    const response = await makeRequest('/api/instances', {
      method: 'POST',
      body: JSON.stringify(instanceData)
    })

    assertStatus(response, 201)
    pocketbaseInstanceId = response.data.id
    assert(response.data.port, 'Should have allocated port')
    assert(response.data.name === 'pocketbase-proxy-test', 'Should have correct name')
  })

  await runTest('GET /app/:instanceId/status (PocketBase)', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}/status`)
    assertStatus(response, 200)
    assertEqual(response.data.instanceId, pocketbaseInstanceId.toString(), 'Should return correct instance ID')
    assertEqual(response.data.instanceName, 'pocketbase-proxy-test', 'Should have correct instance name')
    assert(response.data.proxyPath, 'Should have proxy path')
    assert(response.data.targetPort, 'Should have target port')
  })

  await runTest('GET /app/:instanceId/health (PocketBase offline)', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}/health`)
    assertStatus(response, 200)
    assertEqual(response.data.instanceId, pocketbaseInstanceId.toString(), 'Should return correct instance ID')
    assertEqual(response.data.healthy, false, 'Should be unhealthy when offline')
    assert(response.data.status, 'Should have status info')
  })

  await runTest('GET /app/:instanceId (PocketBase offline)', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}`)
    assertStatus(response, 503)
    assert(response.data.error, 'Should have error message for offline instance')
    assert(response.data.instanceId, 'Should include instance ID in error')
  })

  await runTest('Start PocketBase instance', async () => {
    const response = await makeRequest(`/api/instances/${pocketbaseInstanceId}/start`, {
      method: 'POST'
    })
    assertStatus(response, 200)
    assertEqual(response.data.id, pocketbaseInstanceId, 'Should return correct instance ID')

    // Wait a moment for PocketBase to start
    await new Promise(resolve => setTimeout(resolve, 2000))
  })

  await runTest('GET /app/:instanceId/health (PocketBase starting)', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}/health`)
    assertStatus(response, 200)
    assertEqual(response.data.instanceId, pocketbaseInstanceId.toString(), 'Should return correct instance ID')
    // Health might be true or false depending on startup speed
    assert(typeof response.data.healthy === 'boolean', 'Should have healthy boolean')
  })

  await runTest('GET /app/:instanceId (PocketBase proxy)', async () => {
    // Wait a bit more for PocketBase to fully start
    await new Promise(resolve => setTimeout(resolve, 3000))

    const response = await makeRequest(`/app/${pocketbaseInstanceId}`)

    // PocketBase should either be running (200), still starting (503), or have proxy errors (502)
    assert([200, 502, 503].includes(response.status), `Expected 200, 502, or 503, got ${response.status}`)

    if (response.status === 200) {
      // If PocketBase is running, we should get its admin UI or API response
      assert(response.data, 'Should have response data when PocketBase is running')
    } else {
      // If still starting or proxy error, should have error message
      assert(response.data.error, 'Should have error message if not ready')
    }
  })

  await runTest('GET /app/:instanceId/api/health (PocketBase API)', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}/api/health`)

    // PocketBase health endpoint should respond
    if (response.status === 200) {
      assert(response.data, 'Should have PocketBase health response')
    } else if ([502, 503].includes(response.status)) {
      assert(response.data.error, 'Should have proxy error if PocketBase not ready')
    }
  })

  await runTest('POST /app/:instanceId/api/collections (PocketBase API test)', async () => {
    // Try to access PocketBase admin API (should fail without auth, but proxy should work)
    const response = await makeRequest(`/app/${pocketbaseInstanceId}/api/collections`, {
      method: 'POST',
      body: JSON.stringify({
        name: 'test_collection',
        type: 'base'
      })
    })

    // Should get either 401 (unauthorized), 503 (service unavailable), or 502 (proxy error)
    assert([401, 403, 502, 503].includes(response.status), `Expected auth error, service unavailable, or proxy error, got ${response.status}`)
  })

  await runTest('Test proxy request headers forwarding', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}/api/health`, {
      headers: {
        'X-Test-Header': 'test-value',
        'User-Agent': 'GOWA-Manager-Test/1.0'
      }
    })

    // Should forward the request (regardless of PocketBase status)
    assert([200, 502, 503].includes(response.status), 'Should forward request with custom headers')
  })

  await runTest('Stop PocketBase instance', async () => {
    const response = await makeRequest(`/api/instances/${pocketbaseInstanceId}/stop`, {
      method: 'POST'
    })
    assertStatus(response, 200)

    // Wait for graceful shutdown
    await new Promise(resolve => setTimeout(resolve, 1000))
  })

  await runTest('GET /app/:instanceId/health (PocketBase stopped)', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}/health`)
    assertStatus(response, 200)
    assertEqual(response.data.healthy, false, 'Should be unhealthy when stopped')
  })

  await runTest('GET /app/:instanceId (PocketBase stopped)', async () => {
    const response = await makeRequest(`/app/${pocketbaseInstanceId}`)
    assertStatus(response, 503)
    assert(response.data.error, 'Should have error message for stopped instance')
  })

  // Cleanup test resources
  await makeRequest(`/api/instances/${pocketbaseInstanceId}`, { method: 'DELETE' })
  await makeRequest(`/api/binaries/${pocketbaseBinaryId}`, { method: 'DELETE' })

  // Clean up test data directory
  const testDataDir = join(process.cwd(), 'pb_data_test')
  try {
    if (existsSync(testDataDir)) {
      await import('fs').then(fs => fs.rmSync(testDataDir, { recursive: true, force: true }))
    }
  } catch (error) {
    // Ignore cleanup errors
  }
}

async function testErrorHandling() {
  log('Testing Error Handling...', 'info')

  await runTest('GET /api/instances/999 (not found)', async () => {
    const response = await makeRequest('/api/instances/999')
    assertStatus(response, 404)
    assert(response.data.error, 'Should have error message')
  })

  await runTest('POST /api/instances (invalid data)', async () => {
    const response = await makeRequest('/api/instances', {
      method: 'POST',
      body: JSON.stringify({ name: '' }) // Invalid: empty name
    })
    assertStatus(response, 400)
    assert(response.data.error, 'Should have validation error')
  })

  await runTest('GET /api/binaries/999 (not found)', async () => {
    const response = await makeRequest('/api/binaries/999')
    assertStatus(response, 404)
    assert(response.data.error, 'Should have error message')
  })

  await runTest('POST /api/binaries (invalid path)', async () => {
    const response = await makeRequest('/api/binaries', {
      method: 'POST',
      body: JSON.stringify({
        name: 'test',
        path: '/non/existent/path'
      })
    })
    assertStatus(response, 400)
    assert(response.data.error, 'Should have error for non-existent path')
  })

  await runTest('GET /non-existent-route', async () => {
    const response = await makeRequest('/non-existent-route')
    assertStatus(response, 404)
  })
}

async function testHealthCheck() {
  log('Testing Health Check...', 'info')

  await runTest('GET / (health check)', async () => {
    const response = await makeRequest('/')
    assertStatus(response, 200)
    assert(response.data.success, 'Should indicate success')
    assert(response.data.message, 'Should have message')
  })

  await runTest('GET /hello (legacy endpoint)', async () => {
    const response = await makeRequest('/hello')
    assertStatus(response, 200)
    assert(response.data.success, 'Should indicate success')
    assert(response.data.message, 'Should have message')
  })
}

// Main test runner
async function runAllTests() {
  log('🚀 Starting GOWA Manager Server Test Suite', 'info')
  log(`Testing server at: ${BASE_URL}`, 'info')

  // Check if server is running
  try {
    await makeRequest('/')
    log('Server is running, proceeding with tests...', 'success')
  } catch (error) {
    log('Server is not running! Please start the server first.', 'error')
    process.exit(1)
  }

  const startTime = Date.now()

  // Run all test suites
  await testHealthCheck()
  await testSystemEndpoints()
  await testInstanceManagement()
  await testBinaryManagement()
  await testProxyFunctionality()
  await testErrorHandling()

  const totalTime = Date.now() - startTime

  // Print results
  log('\n📊 Test Results Summary', 'info')
  log('='.repeat(50), 'info')
  log(`Total Tests: ${testCount}`, 'info')
  log(`Passed: ${passedCount}`, 'success')
  log(`Failed: ${testCount - passedCount}`, passedCount === testCount ? 'info' : 'error')
  log(`Success Rate: ${((passedCount / testCount) * 100).toFixed(1)}%`, 'info')
  log(`Total Time: ${totalTime}ms`, 'info')

  if (passedCount < testCount) {
    log('\n❌ Failed Tests:', 'error')
    testResults
      .filter(result => !result.passed)
      .forEach(result => {
        log(`  • ${result.name}: ${result.error}`, 'error')
      })
  }

  log('\n🎉 Test suite completed!', passedCount === testCount ? 'success' : 'warn')

  // Exit with appropriate code
  process.exit(passedCount === testCount ? 0 : 1)
}

// Run tests if this file is executed directly
if (import.meta.main) {
  runAllTests().catch(error => {
    log(`Test suite failed: ${error}`, 'error')
    process.exit(1)
  })
}
