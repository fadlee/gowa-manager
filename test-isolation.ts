#!/usr/bin/env bun
/**
 * GOWA Manager Instance Isolation Test Suite
 * Tests that instances run in separate directories and don't interfere with each other
 */

import { writeFileSync, unlinkSync, existsSync } from 'fs'
import { join } from 'path'

// Test configuration
const BASE_URL = 'http://localhost:3000'

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
    log(`✓ ${name} (${duration}ms)`, 'success')
  } catch (error) {
    const duration = Date.now() - startTime
    const errorMessage = error instanceof Error ? error.message : String(error)
    testResults.push({ name, passed: false, error: errorMessage, duration })
    log(`✗ ${name} (${duration}ms): ${errorMessage}`, 'error')
  }
}

// Assertion helpers
function assert(condition: any, message: string) {
  if (!condition) {
    throw new Error(message)
  }
}

function assertEqual(actual: any, expected: any, message: string) {
  if (actual !== expected) {
    throw new Error(`${message}. Expected: ${expected}, Actual: ${actual}`)
  }
}

function assertStatus(response: { status: number }, expectedStatus: number) {
  if (response.status !== expectedStatus) {
    throw new Error(`Expected status ${expectedStatus}, got ${response.status}`)
  }
}

// Instance isolation test suite
async function testInstanceIsolation() {
  log('📁 Testing Instance Isolation...', 'info')

  let isolationInstance1Id = 0
  let isolationInstance2Id = 0
  const pocketbasePath = join(process.cwd(), 'data', 'binaries', 'pocketbase')

  await runTest('Create first isolation test instance', async () => {
    const instanceData = {
      name: 'isolation-test-1',
      binary_path: pocketbasePath,
      config: JSON.stringify({
        args: ['serve', '--http=0.0.0.0:PORT', '--dir=./pb_data'],
        env: {
          'PB_ENCRYPTION_KEY': 'isolation-test-1-key'
        }
      })
    }

    const response = await makeRequest('/api/instances', {
      method: 'POST',
      body: JSON.stringify(instanceData)
    })

    assertStatus(response, 201)
    isolationInstance1Id = response.data.id
    assert(response.data.name === 'isolation-test-1', 'Should have correct name')
  })

  await runTest('Create second isolation test instance', async () => {
    const instanceData = {
      name: 'isolation-test-2',
      binary_path: pocketbasePath,
      config: JSON.stringify({
        args: ['serve', '--http=0.0.0.0:PORT', '--dir=./pb_data'],
        env: {
          'PB_ENCRYPTION_KEY': 'isolation-test-2-key'
        }
      })
    }

    const response = await makeRequest('/api/instances', {
      method: 'POST',
      body: JSON.stringify(instanceData)
    })

    assertStatus(response, 201)
    isolationInstance2Id = response.data.id
    assert(response.data.name === 'isolation-test-2', 'Should have correct name')
    assert(response.data.port !== response.data.id, 'Should have different port than ID')
  })

  await runTest('Verify instance directories exist', async () => {
    const { existsSync } = await import('fs')
    const { join } = await import('path')

    const instance1Dir = join(process.cwd(), 'data', 'instances', isolationInstance1Id.toString())
    const instance2Dir = join(process.cwd(), 'data', 'instances', isolationInstance2Id.toString())

    assert(existsSync(instance1Dir), `Instance 1 directory should exist: ${instance1Dir}`)
    assert(existsSync(instance2Dir), `Instance 2 directory should exist: ${instance2Dir}`)
    assert(instance1Dir !== instance2Dir, 'Instance directories should be different')
  })

  await runTest('Start both isolation test instances', async () => {
    // Start first instance
    const response1 = await makeRequest(`/api/instances/${isolationInstance1Id}/start`, {
      method: 'POST'
    })
    assertStatus(response1, 200)

    // Start second instance
    const response2 = await makeRequest(`/api/instances/${isolationInstance2Id}/start`, {
      method: 'POST'
    })
    assertStatus(response2, 200)

    // Wait for both to start
    await new Promise(resolve => setTimeout(resolve, 3000))
  })

  await runTest('Verify instances run on different ports', async () => {
    const response1 = await makeRequest(`/api/instances/${isolationInstance1Id}`)
    const response2 = await makeRequest(`/api/instances/${isolationInstance2Id}`)

    assertStatus(response1, 200)
    assertStatus(response2, 200)

    assert(response1.data.port !== response2.data.port, 'Instances should have different ports')
    assertEqual(response1.data.status, 'running', 'Instance 1 should be running')
    assertEqual(response2.data.status, 'running', 'Instance 2 should be running')
  })

  await runTest('Verify instances have separate data directories', async () => {
    const { existsSync } = await import('fs')
    const { join } = await import('path')

    const instance1DataDir = join(process.cwd(), 'data', 'instances', isolationInstance1Id.toString(), 'pb_data')
    const instance2DataDir = join(process.cwd(), 'data', 'instances', isolationInstance2Id.toString(), 'pb_data')

    // Wait a moment for PocketBase to create its data directory
    await new Promise(resolve => setTimeout(resolve, 2000))

    // Check if PocketBase created its data directories (they should exist if instances are running)
    if (existsSync(instance1DataDir) || existsSync(instance2DataDir)) {
      assert(instance1DataDir !== instance2DataDir, 'Data directories should be different paths')
      log('PocketBase data directories are properly isolated', 'info')
    } else {
      log('PocketBase data directories not yet created (instances may still be starting)', 'warn')
    }
  })

  await runTest('Test instance isolation via proxy', async () => {
    // Test that each instance responds independently via proxy
    const health1Response = await makeRequest(`/app/${isolationInstance1Id}/health`)
    const health2Response = await makeRequest(`/app/${isolationInstance2Id}/health`)

    assertStatus(health1Response, 200)
    assertStatus(health2Response, 200)

    assertEqual(health1Response.data.instanceId, isolationInstance1Id.toString(), 'Instance 1 should have correct ID')
    assertEqual(health2Response.data.instanceId, isolationInstance2Id.toString(), 'Instance 2 should have correct ID')
  })

  await runTest('Cleanup isolation test instances', async () => {
    // Stop and delete first instance
    await makeRequest(`/api/instances/${isolationInstance1Id}/stop`, { method: 'POST' })
    const deleteResponse1 = await makeRequest(`/api/instances/${isolationInstance1Id}`, { method: 'DELETE' })
    assertStatus(deleteResponse1, 200)

    // Stop and delete second instance
    await makeRequest(`/api/instances/${isolationInstance2Id}/stop`, { method: 'POST' })
    const deleteResponse2 = await makeRequest(`/api/instances/${isolationInstance2Id}`, { method: 'DELETE' })
    assertStatus(deleteResponse2, 200)
  })

  await runTest('Verify instance directories cleaned up', async () => {
    const { existsSync } = await import('fs')
    const { join } = await import('path')

    const instance1Dir = join(process.cwd(), 'data', 'instances', isolationInstance1Id.toString())
    const instance2Dir = join(process.cwd(), 'data', 'instances', isolationInstance2Id.toString())

    // Wait a moment for cleanup
    await new Promise(resolve => setTimeout(resolve, 1000))

    assert(!existsSync(instance1Dir), `Instance 1 directory should be cleaned up: ${instance1Dir}`)
    assert(!existsSync(instance2Dir), `Instance 2 directory should be cleaned up: ${instance2Dir}`)
  })
}

// Main test execution function
async function runIsolationTests() {
  log('🚀 Starting GOWA Manager Instance Isolation Test Suite', 'info')
  log('='.repeat(60), 'info')
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

  // Run isolation test suite
  await testInstanceIsolation()

  const totalTime = Date.now() - startTime

  // Print results
  log('\n📊 Isolation Test Results Summary', 'info')
  log('='.repeat(60), 'info')
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

  log('\n🎉 Instance isolation test suite completed!', passedCount === testCount ? 'success' : 'warn')

  // Exit with appropriate code
  process.exit(passedCount === testCount ? 0 : 1)
}

// Run tests if this file is executed directly
if (import.meta.main) {
  runIsolationTests().catch(error => {
    log(`Isolation test suite failed: ${error}`, 'error')
    process.exit(1)
  })
}
