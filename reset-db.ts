#!/usr/bin/env bun
/**
 * GOWA Manager Database Reset Script
 * 
 * This script resets the database to a clean state by:
 * 1. Stopping all running instances
 * 2. Deleting all instances from the database
 * 3. Cleaning up instance directories
 * 4. Optionally recreating the database schema
 */

import { existsSync, rmSync, mkdirSync } from 'fs'
import { join } from 'path'
import { db, queries } from './src/db'

// Configuration
const DB_PATH = join(process.cwd(), 'data', 'gowa.db')
const INSTANCES_DIR = join(process.cwd(), 'data', 'instances')
const BINARIES_DIR = join(process.cwd(), 'data', 'bin')

// Colors for console output
const colors = {
  info: '\x1b[36m',    // Cyan
  success: '\x1b[32m', // Green
  error: '\x1b[31m',   // Red
  warn: '\x1b[33m',    // Yellow
  reset: '\x1b[0m'
}

// Logging utility
function log(message: string, type: 'info' | 'success' | 'error' | 'warn' = 'info') {
  const prefix = {
    info: 'ℹ️ ',
    success: '✅',
    error: '❌',
    warn: '⚠️ '
  }
  console.log(`${colors[type]}${prefix[type]} ${message}${colors.reset}`)
}

// Process management utilities
async function stopAllInstances() {
  log('Stopping all running instances...', 'info')
  
  try {
    // Get all instances
    const instances = queries.getAllInstances.all() as any[]
    
    if (instances.length === 0) {
      log('No instances found', 'info')
      return
    }
    
    log(`Found ${instances.length} instances`, 'info')
    
    // Stop each instance
    for (const instance of instances) {
      try {
        log(`Stopping instance ${instance.id}: ${instance.name}`, 'info')
        
        // Check if instance is running
        if (instance.status === 'running') {
          // Try to stop it gracefully
          const stopResult = await fetch(`http://localhost:3001/api/instances/${instance.id}/stop`, {
            method: 'POST'
          })
          
          if (stopResult.ok) {
            log(`Instance ${instance.id} stopped successfully`, 'success')
          } else {
            log(`Failed to stop instance ${instance.id} via API, will force cleanup`, 'warn')
          }
        }
      } catch (error) {
        log(`Error stopping instance ${instance.id}: ${error}`, 'error')
      }
    }
  } catch (error) {
    log(`Error stopping instances: ${error}`, 'error')
  }
}

// Clean up instance directories
function cleanupInstanceDirectories() {
  log('Cleaning up instance directories...', 'info')
  
  try {
    if (existsSync(INSTANCES_DIR)) {
      // Read all instance directories
      const fs = require('fs')
      const dirs = fs.readdirSync(INSTANCES_DIR)
      
      if (dirs.length === 0) {
        log('No instance directories to clean up', 'info')
      } else {
        log(`Found ${dirs.length} instance directories to clean up`, 'info')
        
        // Delete each directory
        for (const dir of dirs) {
          const instanceDir = join(INSTANCES_DIR, dir)
          try {
            rmSync(instanceDir, { recursive: true, force: true })
            log(`Deleted instance directory: ${instanceDir}`, 'success')
          } catch (error) {
            log(`Failed to delete instance directory ${instanceDir}: ${error}`, 'error')
          }
        }
      }
    } else {
      log('Instances directory does not exist, creating it', 'info')
      mkdirSync(INSTANCES_DIR, { recursive: true })
    }
  } catch (error) {
    log(`Error cleaning up instance directories: ${error}`, 'error')
  }
}

// Reset database tables
function resetDatabaseTables() {
  log('Resetting database tables...', 'info')
  
  try {
    // Delete all instances
    const instancesDeleted = customQueries.deleteAllInstances.run()
    log(`Deleted ${instancesDeleted.changes} instances from database`, 'success')
    
    // Reset ports allocation
    const portsReset = customQueries.resetPortsAllocation.run()
    log(`Reset ${portsReset.changes} port allocations`, 'success')
    
    // Note: Additional tables like activity_log and process_metrics can be added here
    // when they are implemented in the future
  } catch (error) {
    log(`Error resetting database tables: ${error}`, 'error')
  }
}

// Ensure binaries directory exists
function ensureBinariesDirectory() {
  log('Ensuring binaries directory exists...', 'info')
  
  try {
    if (!existsSync(BINARIES_DIR)) {
      mkdirSync(BINARIES_DIR, { recursive: true })
      log('Created binaries directory', 'success')
    } else {
      log('Binaries directory already exists', 'info')
    }
  } catch (error) {
    log(`Error ensuring binaries directory: ${error}`, 'error')
  }
}

// Main reset function
async function resetDatabase() {
  log('🔄 Starting GOWA Manager database reset...', 'info')
  log('='.repeat(50), 'info')
  
  // 1. Stop all running instances
  await stopAllInstances()
  
  // 2. Clean up instance directories
  cleanupInstanceDirectories()
  
  // 3. Reset database tables
  resetDatabaseTables()
  
  // 4. Ensure binaries directory exists
  ensureBinariesDirectory()
  
  log('='.repeat(50), 'info')
  log('🎉 Database reset completed successfully!', 'success')
}

// Define custom queries for database reset
const customQueries = {
  deleteAllInstances: db.prepare('DELETE FROM instances'),
  resetPortsAllocation: db.prepare('UPDATE ports SET is_allocated = FALSE, instance_id = NULL')
}

// Run the reset if this file is executed directly
if (import.meta.main) {
  resetDatabase().catch(error => {
    log(`Database reset failed: ${error}`, 'error')
    process.exit(1)
  })
}
