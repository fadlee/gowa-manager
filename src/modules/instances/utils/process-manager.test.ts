import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from 'bun:test'
import treeKill from 'tree-kill'
import { ProcessManager } from './process-manager'

// Shared fake subprocess shape used by ProcessManager.addProcess.
function makeFakeProcess(pid: number): Bun.Subprocess {
  return { pid } as unknown as Bun.Subprocess
}

function makeProcessInfo(pid: number, overrides: Partial<{ cleanup: () => void }> = {}) {
  return {
    process: makeFakeProcess(pid),
    pid,
    startTime: Date.now(),
    cleanup: overrides.cleanup,
  }
}

const treeKillMock = mock((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
  if (cb) cb(null)
})

// Replace the default export so ProcessManager's internal `treeKill` calls hit our mock.
;(treeKill as unknown as { __mock?: typeof treeKillMock }).__mock = treeKillMock
mock.module('tree-kill', () => ({
  default: treeKillMock,
  __esModule: true,
}))

const consoleLogSpy = spyOn(console, 'log').mockImplementation(() => {})
const consoleWarnSpy = spyOn(console, 'warn').mockImplementation(() => {})
const consoleErrorSpy = spyOn(console, 'error').mockImplementation(() => {})

function resetProcessManagerState() {
  // Clear any tracked processes between tests.
  for (const id of ProcessManager.getRunningProcesses().keys()) {
    ProcessManager.removeProcess(id)
  }
  // Reset the shutdown guard by re-importing the module state is not trivial,
  // so we exercise cleanupAllInstances carefully (see tests below).
}

describe('ProcessManager registry', () => {
  afterEach(() => {
    resetProcessManagerState()
    treeKillMock.mockReset()
    treeKillMock.mockImplementation((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(null)
    })
  })

  test('getRunningProcesses returns the internal map and is empty by default', () => {
    const map = ProcessManager.getRunningProcesses()
    expect(map).toBeInstanceOf(Map)
    expect(map.size).toBe(0)
  })

  test('addProcess tracks the process and isReallyRunning reflects it', () => {
    const info = makeProcessInfo(42001)
    ProcessManager.addProcess(1, info)

    expect(ProcessManager.isReallyRunning(1)).toBe(true)
    expect(ProcessManager.isReallyRunning(2)).toBe(false)
    expect(ProcessManager.getProcessInfo(1)).toBe(info)
  })

  test('removeProcess drops the tracked entry', () => {
    ProcessManager.addProcess(2, makeProcessInfo(42002))
    ProcessManager.removeProcess(2)

    expect(ProcessManager.isReallyRunning(2)).toBe(false)
    expect(ProcessManager.getProcessInfo(2)).toBeUndefined()
  })

  test('getProcessInfo returns undefined for unknown instance', () => {
    expect(ProcessManager.getProcessInfo(999)).toBeUndefined()
  })
})

describe('ProcessManager.stopProcess', () => {
  afterEach(() => {
    resetProcessManagerState()
    treeKillMock.mockReset()
    treeKillMock.mockImplementation((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(null)
    })
  })

  test('returns false for unknown instance and does not call treeKill', () => {
    expect(ProcessManager.stopProcess(123)).toBe(false)
    expect(treeKillMock).not.toHaveBeenCalled()
  })

  test('calls treeKill with SIGTERM and removes the entry', () => {
    ProcessManager.addProcess(3, makeProcessInfo(42003))
    const result = ProcessManager.stopProcess(3)

    expect(result).toBe(true)
    expect(treeKillMock).toHaveBeenCalledTimes(1)
    expect(treeKillMock.mock.calls[0][0]).toBe(42003)
    expect(treeKillMock.mock.calls[0][1]).toBe('SIGTERM')
    expect(ProcessManager.isReallyRunning(3)).toBe(false)
  })

  test('logs warning when treeKill reports an error', () => {
    treeKillMock.mockImplementationOnce((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(new Error('boom'))
    })
    ProcessManager.addProcess(4, makeProcessInfo(42004))
    ProcessManager.stopProcess(4)

    expect(consoleErrorSpy).toHaveBeenCalled()
  })
})

describe('ProcessManager.killProcess', () => {
  afterEach(() => {
    resetProcessManagerState()
    treeKillMock.mockReset()
    treeKillMock.mockImplementation((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(null)
    })
  })

  test('returns false for unknown instance and does not call treeKill', () => {
    expect(ProcessManager.killProcess(321)).toBe(false)
    expect(treeKillMock).not.toHaveBeenCalled()
  })

  test('calls treeKill with SIGKILL and removes the entry', () => {
    ProcessManager.addProcess(5, makeProcessInfo(42005))
    const result = ProcessManager.killProcess(5)

    expect(result).toBe(true)
    expect(treeKillMock).toHaveBeenCalledTimes(1)
    expect(treeKillMock.mock.calls[0][0]).toBe(42005)
    expect(treeKillMock.mock.calls[0][1]).toBe('SIGKILL')
    expect(ProcessManager.isReallyRunning(5)).toBe(false)
  })

  test('swallows ESRCH error silently without logging', () => {
    const esrch = new Error('no such process') as Error & { code: string }
    esrch.code = 'ESRCH'
    treeKillMock.mockImplementationOnce((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(esrch)
    })
    ProcessManager.addProcess(6, makeProcessInfo(42006))

    const beforeErrorCalls = consoleErrorSpy.mock.calls.length
    ProcessManager.killProcess(6)

    expect(consoleErrorSpy.mock.calls.length).toBe(beforeErrorCalls)
  })

  test('logs non-ESRCH errors', () => {
    const other = new Error('permission denied') as Error & { code: string }
    other.code = 'EPERM'
    treeKillMock.mockImplementationOnce((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(other)
    })
    ProcessManager.addProcess(7, makeProcessInfo(42007))
    ProcessManager.killProcess(7)

    expect(consoleErrorSpy).toHaveBeenCalled()
  })
})

describe('ProcessManager.cleanupAllInstances', () => {
  afterEach(() => {
    resetProcessManagerState()
    treeKillMock.mockReset()
    treeKillMock.mockImplementation((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(null)
    })
  })

  test('kills every tracked process with SIGKILL, tolerates per-process errors, and clears the registry', async () => {
    // The isShuttingDown guard is a one-shot module-level flag, so we only
    // exercise cleanupAllInstances once per test session. Combine success
    // and error behavior in a single run.
    treeKillMock.mockImplementation((_pid: number, _signal: string, cb?: (err?: Error | null) => void) => {
      if (cb) cb(_pid === 42020 ? new Error('fail') : null)
    })
    ProcessManager.addProcess(20, makeProcessInfo(42020))
    ProcessManager.addProcess(21, makeProcessInfo(42021))

    await ProcessManager.cleanupAllInstances()

    expect(treeKillMock).toHaveBeenCalledTimes(2)
    const signals = treeKillMock.mock.calls.map((c) => c[1])
    expect(signals.every((s) => s === 'SIGKILL')).toBe(true)
    expect(ProcessManager.getRunningProcesses().size).toBe(0)
    expect(consoleWarnSpy).toHaveBeenCalled()
  })

  test('is a no-op when already shutting down', async () => {
    // After the previous test, isShuttingDown is true. Subsequent calls must
    // return without invoking treeKill again.
    ProcessManager.addProcess(30, makeProcessInfo(42030))
    const callsBefore = treeKillMock.mock.calls.length

    await ProcessManager.cleanupAllInstances()

    expect(treeKillMock.mock.calls.length).toBe(callsBefore)
    // The newly added process is NOT cleaned up because shutdown is in progress.
    expect(ProcessManager.isReallyRunning(30)).toBe(true)
    ProcessManager.removeProcess(30)
  })
})

describe('ProcessManager.setupExitHandlers', () => {
  const registeredHandlers = new Map<string, (...args: any[]) => any>()
  let onSpy: ReturnType<typeof spyOn>
  let exitMock: ReturnType<typeof mock>
  let originalExit: typeof process.exit
  let originalOn: typeof process.on

  beforeEach(() => {
    registeredHandlers.clear()

    originalOn = process.on
    onSpy = spyOn(process, 'on').mockImplementation((event: string, handler: any) => {
      registeredHandlers.set(event, handler)
      return process
    })

    originalExit = process.exit
    exitMock = mock((code?: number) => {})
    Object.defineProperty(process, 'exit', {
      value: exitMock,
      configurable: true,
      writable: true,
    })
  })

  afterEach(() => {
    // Remove handlers that were registered via the real process.on before the spy
    for (const [event, handler] of registeredHandlers) {
      process.removeListener(event as any, handler)
    }
    registeredHandlers.clear()

    // Restore process.exit
    Object.defineProperty(process, 'exit', {
      value: originalExit,
      configurable: true,
      writable: true,
    })

    onSpy.mockRestore()
  })

  test('registers handlers for all five process events', () => {
    ProcessManager.setupExitHandlers()

    const events = onSpy.mock.calls.map((c) => c[0])
    expect(events).toContain('SIGTERM')
    expect(events).toContain('SIGINT')
    expect(events).toContain('beforeExit')
    expect(events).toContain('uncaughtException')
    expect(events).toContain('unhandledRejection')
    expect(onSpy).toHaveBeenCalledTimes(5)
  })

  test('SIGTERM handler cleans up and exits with code 0', async () => {
    const cleanupSpy = spyOn(ProcessManager, 'cleanupAllInstances').mockResolvedValue(undefined)
    ProcessManager.setupExitHandlers()

    const handler = registeredHandlers.get('SIGTERM')!
    await handler()

    expect(cleanupSpy).toHaveBeenCalledTimes(1)
    expect(exitMock).toHaveBeenCalledWith(0)
    cleanupSpy.mockRestore()
  })

  test('SIGINT handler cleans up and exits with code 0', async () => {
    const cleanupSpy = spyOn(ProcessManager, 'cleanupAllInstances').mockResolvedValue(undefined)
    ProcessManager.setupExitHandlers()

    const handler = registeredHandlers.get('SIGINT')!
    await handler()

    expect(cleanupSpy).toHaveBeenCalledTimes(1)
    expect(exitMock).toHaveBeenCalledWith(0)
    cleanupSpy.mockRestore()
  })

  test('beforeExit handler cleans up without calling process.exit', async () => {
    const cleanupSpy = spyOn(ProcessManager, 'cleanupAllInstances').mockResolvedValue(undefined)
    ProcessManager.setupExitHandlers()

    const handler = registeredHandlers.get('beforeExit')!
    await handler()

    expect(cleanupSpy).toHaveBeenCalledTimes(1)
    expect(exitMock).not.toHaveBeenCalled()
    cleanupSpy.mockRestore()
  })

  test('uncaughtException handler logs the error, cleans up, and exits with code 1', async () => {
    const cleanupSpy = spyOn(ProcessManager, 'cleanupAllInstances').mockResolvedValue(undefined)
    ProcessManager.setupExitHandlers()

    const handler = registeredHandlers.get('uncaughtException')!
    const testError = new Error('test uncaught')
    await handler(testError)

    expect(consoleErrorSpy).toHaveBeenCalledWith('Uncaught exception:', testError)
    expect(cleanupSpy).toHaveBeenCalledTimes(1)
    expect(exitMock).toHaveBeenCalledWith(1)
    cleanupSpy.mockRestore()
  })

  test('unhandledRejection handler logs reason+promise, cleans up, and exits with code 1', async () => {
    const cleanupSpy = spyOn(ProcessManager, 'cleanupAllInstances').mockResolvedValue(undefined)
    ProcessManager.setupExitHandlers()

    const handler = registeredHandlers.get('unhandledRejection')!
    const testReason = new Error('test rejection')
    const testPromise = Promise.resolve()
    await handler(testReason, testPromise)

    expect(consoleErrorSpy).toHaveBeenCalledWith('Unhandled rejection at:', testPromise, 'reason:', testReason)
    expect(cleanupSpy).toHaveBeenCalledTimes(1)
    expect(exitMock).toHaveBeenCalledWith(1)
    cleanupSpy.mockRestore()
  })
})
