import { EventEmitter } from 'node:events'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { PassThrough } from 'node:stream'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { appMock, execFileSyncMock, spawnMock } = vi.hoisted(() => {
  const listeners = new Map<string, Set<(...args: any[]) => void>>()
  const app = {
    isPackaged: false,
    resourcesPath: '/tmp/resources' as string | undefined,
    getPath: vi.fn((name: string) => `/tmp/${name}`),
    quit: vi.fn(),
    exit: vi.fn(),
    requestSingleInstanceLock: vi.fn(),
    on(event: string, listener: (...args: any[]) => void) {
      const set = listeners.get(event) ?? new Set()
      set.add(listener)
      listeners.set(event, set)
      return this
    },
    off(event: string, listener: (...args: any[]) => void) {
      listeners.get(event)?.delete(listener)
      return this
    },
    emit(event: string, ...args: any[]) {
      for (const listener of listeners.get(event) ?? []) listener(...args)
      return true
    },
    removeAllListeners() {
      listeners.clear()
      return this
    },
  }
  return {
    appMock: app,
    execFileSyncMock: vi.fn(),
    spawnMock: vi.fn(),
  }
})

vi.mock('electron', () => ({
  app: appMock,
}))

vi.mock('node:child_process', () => ({
  execFileSync: execFileSyncMock,
  spawn: spawnMock,
}))

type FakeChild = EventEmitter & {
  pid: number
  stdout: PassThrough
  stderr: PassThrough
  exitCode: number | null
  signalCode: NodeJS.Signals | null
  kill: ReturnType<typeof vi.fn>
}

function createFakeChild(mode: 'exits-on-sigkill' | 'never-exits' = 'exits-on-sigkill'): FakeChild {
  const child = new EventEmitter() as FakeChild
  child.pid = 4242
  child.stdout = new PassThrough()
  child.stderr = new PassThrough()
  child.exitCode = null
  child.signalCode = null
  child.kill = vi.fn((signal?: NodeJS.Signals | number) => {
    if (mode === 'never-exits') return true
    if (signal === 'SIGKILL') {
      child.signalCode = 'SIGKILL'
      child.exitCode = 0
      queueMicrotask(() => child.emit('exit', 0, 'SIGKILL'))
      return true
    }
    if (signal === 'SIGTERM') {
      child.signalCode = 'SIGTERM'
      return true
    }
    return true
  })
  return child
}

async function loadSupervisor() {
  return await import('./serverSupervisor')
}

describe('serverSupervisor', () => {
  it('uses a health timeout that covers the embedded startup budget', async () => {
    const { DEFAULT_HEALTH_TIMEOUT_MS } = await loadSupervisor()
    expect(DEFAULT_HEALTH_TIMEOUT_MS).toBe(120_000)
  })

  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn())
    appMock.isPackaged = false
    appMock.resourcesPath = '/tmp/resources'
    ;(appMock as { resourcesPath: string | undefined }).resourcesPath = undefined
    ;(process as unknown as { resourcesPath: string | undefined }).resourcesPath = undefined
    appMock.quit.mockClear()
    appMock.exit.mockClear()
    appMock.requestSingleInstanceLock.mockReset()
    appMock.getPath.mockClear()
    appMock.removeAllListeners()
    spawnMock.mockReset()
    execFileSyncMock.mockReset()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    ;(appMock as { resourcesPath: string | undefined }).resourcesPath = undefined
    ;(process as unknown as { resourcesPath: string | undefined }).resourcesPath = undefined
    appMock.removeAllListeners()
  })

  it('resolves the loopback URL after health checks eventually pass', async () => {
    vi.useRealTimers()
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock
      .mockRejectedValueOnce(new Error('connect ECONNREFUSED'))
      .mockResolvedValueOnce(new Response('not ready', { status: 503 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))

    const originalAppData = process.env.MUESLI_APPDATA
    process.env.MUESLI_APPDATA = '/tmp/process-appdata'
    try {
      const { startServerSupervisor } = await loadSupervisor()
      const supervisorPromise = startServerSupervisor({
        env: {
          MUESLI_SERVER_BIN: '/tmp/muesli',
          MUESLI_ADDR: '127.0.0.1:4567',
        },
        fetchImpl: fetchMock,
        healthPollIntervalMs: 10,
        healthTimeoutMs: 100,
        killTimeoutMs: 1_000,
      })
      const supervisor = await supervisorPromise

      expect(supervisor?.baseUrl).toBe('http://127.0.0.1:4567')
      expect(supervisor?.logPath).toBe('/tmp/userData/logs/server.log')
      expect(fetchMock).toHaveBeenCalledWith(
        'http://127.0.0.1:4567/healthz',
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      )
      expect(spawnMock).toHaveBeenCalledWith('/tmp/muesli', ['--embedded'], expect.objectContaining({
        env: expect.objectContaining({
          MUESLI_ADDR: '127.0.0.1:4567',
          MUESLI_APPDATA: '/tmp/userData/embedded-server',
          MUESLI_PARENT_PID: String(process.pid),
        }),
        stdio: ['ignore', 'pipe', 'pipe'],
      }))
    } finally {
      if (originalAppData === undefined) {
        delete process.env.MUESLI_APPDATA
      } else {
        process.env.MUESLI_APPDATA = originalAppData
      }
    }
  })

  it('keeps caller-provided MUESLI_APPDATA and other env overrides intact', async () => {
    vi.useRealTimers()
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))

    const originalServerBin = process.env.MUESLI_SERVER_BIN
    const originalAddr = process.env.MUESLI_ADDR
    const originalAppData = process.env.MUESLI_APPDATA
    process.env.MUESLI_SERVER_BIN = '/tmp/process-bin'
    process.env.MUESLI_ADDR = '127.0.0.1:9999'
    process.env.MUESLI_APPDATA = '/tmp/process-appdata'
    try {
      const { startServerSupervisor } = await loadSupervisor()
      const supervisor = await startServerSupervisor({
        env: {
          MUESLI_SERVER_BIN: '/tmp/muesli',
          MUESLI_ADDR: '127.0.0.1:4567',
          MUESLI_APPDATA: '/tmp/caller-appdata',
        },
        fetchImpl: fetchMock,
        healthPollIntervalMs: 10,
        healthTimeoutMs: 100,
        killTimeoutMs: 1_000,
      })

      expect(supervisor?.baseUrl).toBe('http://127.0.0.1:4567')
      expect(spawnMock).toHaveBeenCalledWith('/tmp/muesli', ['--embedded'], expect.objectContaining({
        env: expect.objectContaining({
          MUESLI_SERVER_BIN: '/tmp/muesli',
          MUESLI_ADDR: '127.0.0.1:4567',
          MUESLI_APPDATA: '/tmp/caller-appdata',
        }),
      }))
    } finally {
      if (originalServerBin === undefined) {
        delete process.env.MUESLI_SERVER_BIN
      } else {
        process.env.MUESLI_SERVER_BIN = originalServerBin
      }
      if (originalAddr === undefined) {
        delete process.env.MUESLI_ADDR
      } else {
        process.env.MUESLI_ADDR = originalAddr
      }
      if (originalAppData === undefined) {
        delete process.env.MUESLI_APPDATA
      } else {
        process.env.MUESLI_APPDATA = originalAppData
      }
    }
  })

  it('calls onUnexpectedExit when the child exits on its own after becoming healthy', async () => {
    vi.useRealTimers()
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))
    const onUnexpectedExit = vi.fn()

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4567',
      },
      fetchImpl: fetchMock,
      healthPollIntervalMs: 10,
      healthTimeoutMs: 100,
      killTimeoutMs: 1_000,
      onUnexpectedExit,
    })

    expect(supervisor?.baseUrl).toBe('http://127.0.0.1:4567')

    child.emit('exit', 1, null)

    expect(onUnexpectedExit).toHaveBeenCalledTimes(1)
    expect(onUnexpectedExit).toHaveBeenCalledWith({ code: 1, signal: null })
  })

  it('reports the terminating signal', async () => {
    vi.useRealTimers()
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))
    const onUnexpectedExit = vi.fn()

    const { startServerSupervisor } = await loadSupervisor()
    await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4567',
      },
      fetchImpl: fetchMock,
      healthPollIntervalMs: 10,
      healthTimeoutMs: 100,
      killTimeoutMs: 1_000,
      onUnexpectedExit,
    })

    child.emit('exit', null, 'SIGKILL')

    expect(onUnexpectedExit).toHaveBeenCalledTimes(1)
    expect(onUnexpectedExit).toHaveBeenCalledWith({ code: null, signal: 'SIGKILL' })
  })

  it('sweeps the Unix process group after an unexpected child exit', async () => {
    vi.useRealTimers()
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const processKill = vi.spyOn(process, 'kill').mockReturnValue(true)
    const userDataPath = mkdtempSync(join(tmpdir(), 'muesli-supervisor-'))
    const postgresDataPath = join(userDataPath, 'embedded-server', 'postgres', 'data')
    mkdirSync(postgresDataPath, { recursive: true })
    writeFileSync(join(postgresDataPath, 'postmaster.pid'), '5252\n/data/path\n')
    execFileSyncMock.mockReturnValue(`postgres -D ${postgresDataPath}`)

    try {
      const { startServerSupervisor } = await loadSupervisor()
      await startServerSupervisor({
        env: { MUESLI_SERVER_BIN: '/tmp/muesli', MUESLI_ADDR: '127.0.0.1:4567' },
        userDataPath,
        waitForHealthy: false,
      })

      child.emit('exit', null, 'SIGKILL')

      if (process.platform === 'win32') {
        expect(processKill).not.toHaveBeenCalled()
      } else {
        expect(processKill).toHaveBeenCalledWith(-child.pid, 'SIGKILL')
        expect(processKill).toHaveBeenCalledWith(5252, 'SIGKILL')
      }
    } finally {
      await new Promise<void>((resolve) => setImmediate(resolve))
      processKill.mockRestore()
      rmSync(userDataPath, { recursive: true, force: true })
    }
  })

  it('resolves packaged server and resource paths from the app resources directory', async () => {
    vi.useRealTimers()
    appMock.isPackaged = true
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    ;(process as NodeJS.Process & { resourcesPath: string }).resourcesPath = '/R'

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_ADDR: '127.0.0.1:4572',
      },
      fetchImpl: fetchMock,
      healthPollIntervalMs: 10,
      healthTimeoutMs: 100,
      killTimeoutMs: 1_000,
    })

    expect(supervisor?.baseUrl).toBe('http://127.0.0.1:4572')
    expect(spawnMock).toHaveBeenCalledWith('/R/server/muesli', ['--embedded'], expect.objectContaining({
      env: expect.objectContaining({
        MUESLI_EMBEDDED_PG_BINARIES: '/R/pg',
        MUESLI_EMBEDDED_PGVECTOR_DIR: '/R/pgvector',
        MUESLI_WHISPER_CPP_TRANSCRIBER_BIN: '/R/server/whisper-cpp-transcriber',
        MUESLI_WHISPER_MODEL_DIR: '/R/models/whisper',
        MUESLI_WHISPER_MODEL: 'ggml-tiny.en',
        MUESLI_FFMPEG_BIN: '/R/bin/ffmpeg',
        MUESLI_ADDR: '127.0.0.1:4572',
        MUESLI_APPDATA: '/tmp/userData/embedded-server',
      }),
      stdio: ['ignore', 'pipe', 'pipe'],
    }))
  })

  it('rejects when the health check never becomes ready', async () => {
    vi.useRealTimers()
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockRejectedValue(new Error('still starting'))

    const { startServerSupervisor } = await loadSupervisor()
    const supervisorPromise = startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4568',
      },
      fetchImpl: fetchMock,
      healthPollIntervalMs: 10,
      healthTimeoutMs: 30,
      killTimeoutMs: 1,
    }).then(
      () => new Error('expected rejection'),
      (err: unknown) => err,
    )
    const err = await supervisorPromise
    expect(err).toBeInstanceOf(Error)
    expect((err as Error).message).toMatch(/timed out waiting for http:\/\/127\.0\.0\.1:4568\/healthz/i)
  })

  it('aborts a health check that never responds when the health timeout expires', async () => {
    vi.useRealTimers()
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = vi.fn((_input: string | URL | Request, init?: RequestInit) => {
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener(
          'abort',
          () => reject(new DOMException('The operation was aborted', 'AbortError')),
          { once: true },
        )
      })
    })

    const { startServerSupervisor } = await loadSupervisor()
    await expect(
      startServerSupervisor({
        env: {
          MUESLI_SERVER_BIN: '/tmp/muesli',
          MUESLI_ADDR: '127.0.0.1:4574',
        },
        fetchImpl: fetchMock as typeof fetch,
        healthPollIntervalMs: 10,
        healthTimeoutMs: 20,
        killTimeoutMs: 1,
      }),
    ).rejects.toThrow(/timed out waiting for http:\/\/127\.0\.0\.1:4574\/healthz/i)
  })

  it('rejects promptly when spawning the embedded server fails', async () => {
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4573',
      },
      waitForHealthy: false,
      fetchImpl: fetchMock,
      healthPollIntervalMs: 10,
      healthTimeoutMs: 30_000,
      killTimeoutMs: 1,
    })

    expect(supervisor).not.toBeNull()

    const spawnFailure = new Error('spawn ENOENT')
    child.emit('error', spawnFailure)

    const waitStart = Date.now()
    await expect(supervisor!.waitUntilHealthy(fetchMock)).rejects.toThrow('embedded server failed to start: spawn ENOENT')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(Date.now() - waitStart).toBe(0)
  })

  it('sends SIGTERM on shutdown and escalates to SIGKILL after the kill timeout', async () => {
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    child.kill.mockImplementation((signal?: NodeJS.Signals | number) => {
      if (signal === 'SIGTERM') {
        child.signalCode = 'SIGTERM'
        return true
      }
      if (signal === 'SIGKILL') {
        child.signalCode = 'SIGKILL'
        child.exitCode = 0
        queueMicrotask(() => child.emit('exit', 0, 'SIGKILL'))
        return true
      }
      return true
    })
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4569',
      },
      fetchImpl: fetchMock,
      healthPollIntervalMs: 100,
      healthTimeoutMs: 1_000,
      killTimeoutMs: 250,
    })

    expect(supervisor).not.toBeNull()

    const shutdownPromise = supervisor!.shutdown()
    expect(child.kill).toHaveBeenCalledWith('SIGTERM')

    await vi.advanceTimersByTimeAsync(251)
    await shutdownPromise

    expect(child.kill).toHaveBeenCalledWith('SIGKILL')
    expect(child.kill.mock.calls.map((call) => call[0])).toEqual(['SIGTERM', 'SIGKILL'])
  })

  it('does not call onUnexpectedExit during an intentional shutdown', async () => {
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    child.kill.mockImplementation((signal?: NodeJS.Signals | number) => {
      if (signal === 'SIGTERM') {
        child.signalCode = 'SIGTERM'
        return true
      }
      if (signal === 'SIGKILL') {
        child.signalCode = 'SIGKILL'
        child.exitCode = 0
        queueMicrotask(() => child.emit('exit', 0, 'SIGKILL'))
        return true
      }
      return true
    })
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))
    const onUnexpectedExit = vi.fn()

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4569',
      },
      fetchImpl: fetchMock,
      healthPollIntervalMs: 100,
      healthTimeoutMs: 1_000,
      killTimeoutMs: 250,
      onUnexpectedExit,
    })

    expect(supervisor).not.toBeNull()

    const shutdownPromise = supervisor!.shutdown()
    expect(child.kill).toHaveBeenCalledWith('SIGTERM')

    await vi.advanceTimersByTimeAsync(251)
    await shutdownPromise

    expect(child.kill).toHaveBeenCalledWith('SIGKILL')
    expect(onUnexpectedExit).not.toHaveBeenCalled()
  })

  it('rejects shutdown when the child is not reaped after SIGKILL', async () => {
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild('never-exits')
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4575',
      },
      fetchImpl: fetchMock,
      healthPollIntervalMs: 10,
      healthTimeoutMs: 100,
      killTimeoutMs: 25,
    })

    expect(supervisor).not.toBeNull()
    const shutdownPromise = supervisor!.shutdown()
    const shutdownRejection = expect(shutdownPromise).rejects.toThrow(
      'embedded server did not exit after SIGKILL within 25ms',
    )

    await vi.advanceTimersByTimeAsync(50)

    await shutdownRejection
    expect(child.kill.mock.calls.map((call) => call[0])).toEqual(['SIGTERM', 'SIGKILL'])
  })

  it('quits immediately without spawning when the single-instance lock is not acquired', async () => {
    appMock.requestSingleInstanceLock.mockReturnValue(false)

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4570',
      },
    })

    expect(supervisor).toBeNull()
    expect(appMock.quit).toHaveBeenCalledTimes(1)
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('surfaces an embedded-startup error payload instead of force-exiting when health times out', async () => {
    appMock.requestSingleInstanceLock.mockReturnValue(true)
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockRejectedValue(new Error('still starting'))

    const { startServerSupervisor } = await loadSupervisor()
    const supervisor = await startServerSupervisor({
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
        MUESLI_ADDR: '127.0.0.1:4571',
      },
      waitForHealthy: false,
      healthPollIntervalMs: 10,
      healthTimeoutMs: 40,
      killTimeoutMs: 1,
    })

    expect(supervisor).not.toBeNull()
    const { startEmbeddedStartupMonitor } = await import('./embeddedStartupMonitor')
    const onStatus = vi.fn()
    startEmbeddedStartupMonitor({
      supervisor: supervisor!,
      onStatus,
      fetchImpl: fetchMock,
      statusPollIntervalMs: 10,
      readyHoldMs: 1,
    })

    await vi.advanceTimersByTimeAsync(60)

    expect(onStatus).toHaveBeenCalledWith(expect.objectContaining({
      status: 'error',
      logPath: supervisor!.logPath,
    }))
    expect(appMock.exit).not.toHaveBeenCalled()
  })
})
