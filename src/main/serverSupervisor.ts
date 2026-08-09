import { createWriteStream, type WriteStream } from 'node:fs'
import { mkdir } from 'node:fs/promises'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { dirname, join } from 'node:path'
import { app } from 'electron'
import { resolveResourceEnv, resolveServerBin } from './resourcePaths'

export interface ServerSupervisor {
  baseUrl: string
  logPath: string
  waitUntilHealthy(fetchImpl: typeof fetch): Promise<void>
  shutdown(): Promise<void>
}

export interface ServerSupervisorOptions {
  env?: NodeJS.ProcessEnv
  fetchImpl?: typeof fetch
  userDataPath?: string
  logPath?: string
  healthPollIntervalMs?: number
  healthTimeoutMs?: number
  killTimeoutMs?: number
  onSecondInstance?: () => void
  onUnexpectedExit?: (info: { code: number | null; signal: NodeJS.Signals | null }) => void
  waitForHealthy?: boolean
  spawnImpl?: typeof spawn
}

interface ParsedAddr {
  host: string
  port: number
}

const DEFAULT_ADDR = '127.0.0.1:8080'
const DEFAULT_HEALTH_POLL_INTERVAL_MS = 250
// Practical startup ceiling, not an absolute worst-case: `pgStartTimeout = 60s`
// per attempt in `internal/embedded/postgres.go`, retried up to 3 times
// (`for attempts := 0; attempts < 3`), can reach 180s in a pathological full
// retry exhaustion; orphan-postgres reap can add up to 10s
// (`reapOrphanPostgres` in `internal/embedded/postgres.go`); Ollama detection
// can add 5s when absent (`DefaultOllamaDetectAttempts = 5` x
// `DefaultOllamaDetectInterval = 1s` in `internal/embedded/ollama.go`); and the
// 41 synchronous DB migration pairs in `internal/db/migrations` are typically
// fast but additive. 120s keeps the Electron check bounded while covering the
// realistic common case with headroom; a full 3x60s Postgres exhaustion should
// surface as a genuine startup failure instead of waiting longer.
export const DEFAULT_HEALTH_TIMEOUT_MS = 120_000
const DEFAULT_KILL_TIMEOUT_MS = 5_000
const DEFAULT_LOG_FILENAME = 'server.log'
const electronApp = app as unknown as {
  isPackaged: boolean
  on(event: 'second-instance' | 'before-quit', listener: (...args: any[]) => void): void
  off(event: 'second-instance' | 'before-quit', listener: (...args: any[]) => void): void
  getPath(name: 'userData'): string
  quit(): void
  exit(code?: number): void
  requestSingleInstanceLock(): boolean
}

function parseLoopbackAddr(raw: string | undefined): ParsedAddr {
  const trimmed = raw?.trim()
  if (!trimmed) return { host: '127.0.0.1', port: 8080 }

  const lastColon = trimmed.lastIndexOf(':')
  if (lastColon < 0) {
    const port = Number.parseInt(trimmed, 10)
    if (!Number.isFinite(port) || port <= 0) {
      throw new Error(`invalid MUESLI_ADDR value ${JSON.stringify(trimmed)}: expected host:port or :port`)
    }
    return { host: '127.0.0.1', port }
  }

  const host = trimmed.slice(0, lastColon).trim() || '127.0.0.1'
  const port = Number.parseInt(trimmed.slice(lastColon + 1), 10)
  if (!Number.isFinite(port) || port <= 0) {
    throw new Error(`invalid MUESLI_ADDR value ${JSON.stringify(trimmed)}: expected host:port or :port`)
  }
  return { host, port }
}

function resolveServerBinaryPath(env: NodeJS.ProcessEnv): string {
  return resolveServerBin({
    isPackaged: electronApp.isPackaged,
    resourcesPath: process.resourcesPath,
    env,
  })
}

function makeHealthUrl(baseUrl: string): string {
  return new URL('/healthz', baseUrl).toString()
}

export function makeServerLogPath(userDataPath: string): string {
  return join(userDataPath, 'logs', DEFAULT_LOG_FILENAME)
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms)
  })
}

function logChildOutput(logStream: WriteStream | null, stream: 'stdout' | 'stderr', data: Buffer | string) {
  const text = typeof data === 'string' ? data : data.toString('utf8')
  const trimmed = text.trimEnd()
  if (!trimmed) return
  const prefix = stream === 'stderr' ? '[muesli-server stderr]' : '[muesli-server stdout]'
  if (stream === 'stderr') {
    console.error(prefix, trimmed)
  } else {
    console.log(prefix, trimmed)
  }
  if (!logStream) return
  const normalized = trimmed.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
  for (const line of normalized.split('\n')) {
    logStream.write(`${prefix} ${line}\n`)
  }
}

class EmbeddedServerSupervisorImpl implements ServerSupervisor {
  readonly baseUrl: string
  readonly logPath: string
  private readonly child: ChildProcessWithoutNullStreams
  private readonly healthUrl: string
  private readonly healthPollIntervalMs: number
  private readonly healthTimeoutMs: number
  private readonly killTimeoutMs: number
  private readonly onSecondInstance?: () => void
  private readonly onUnexpectedExit?: (info: { code: number | null; signal: NodeJS.Signals | null }) => void
  private readonly logStream: WriteStream | null
  private shutdownPromise: Promise<void> | null = null
  private quitting = false
  private childExited = false
  private spawnError: Error | null = null
  private hasBecomeHealthy = false
  private reportedUnexpectedExit = false

  constructor(child: ChildProcessWithoutNullStreams, baseUrl: string, logPath: string, logStream: WriteStream | null, opts: Required<Pick<ServerSupervisorOptions, 'healthPollIntervalMs' | 'healthTimeoutMs' | 'killTimeoutMs'>> & Pick<ServerSupervisorOptions, 'onSecondInstance' | 'onUnexpectedExit'>) {
    this.child = child
    this.baseUrl = baseUrl
    this.logPath = logPath
    this.healthUrl = makeHealthUrl(baseUrl)
    this.healthPollIntervalMs = opts.healthPollIntervalMs
    this.healthTimeoutMs = opts.healthTimeoutMs
    this.killTimeoutMs = opts.killTimeoutMs
    this.onSecondInstance = opts.onSecondInstance
    this.onUnexpectedExit = opts.onUnexpectedExit
    this.logStream = logStream

    this.child.once('exit', (code, signal) => {
      this.childExited = true
      this.logStream?.end()
      if (this.shouldReportUnexpectedExit()) {
        this.reportedUnexpectedExit = true
        this.onUnexpectedExit?.({ code, signal })
      }
    })
    this.child.on('error', (err) => {
      this.spawnError = err instanceof Error ? err : new Error(String(err))
      this.childExited = true
      this.logStream?.end()
    })
    this.child.stdout.on('data', (chunk) => logChildOutput(this.logStream, 'stdout', chunk))
    this.child.stderr.on('data', (chunk) => logChildOutput(this.logStream, 'stderr', chunk))
  }

  private shouldReportUnexpectedExit(): boolean {
    return this.hasBecomeHealthy && !this.quitting && this.shutdownPromise == null && !this.reportedUnexpectedExit
  }

  async waitUntilHealthy(fetchImpl: typeof fetch): Promise<void> {
    const deadline = Date.now() + this.healthTimeoutMs
    let lastError: unknown

    while (Date.now() < deadline) {
      if (this.spawnError) {
        throw new Error(`embedded server failed to start: ${this.spawnError.message}`)
      }
      if (this.childExited) {
        throw new Error('embedded server exited before becoming healthy')
      }

      try {
        const res = await fetchImpl(this.healthUrl)
        if (res.ok) {
          this.hasBecomeHealthy = true
          return
        }
        lastError = new Error(`health check returned ${res.status} ${res.statusText}`)
      } catch (err) {
        lastError = err
      }

      const remaining = deadline - Date.now()
      if (remaining <= 0) break
      await delay(Math.min(this.healthPollIntervalMs, remaining))
    }

    const reason = lastError instanceof Error ? `: ${lastError.message}` : ''
    throw new Error(`timed out waiting for ${this.healthUrl} after ${this.healthTimeoutMs}ms${reason}`)
  }

  installLifecycleHooks() {
    electronApp.on('second-instance', this.handleSecondInstance)
    electronApp.on('before-quit', this.handleBeforeQuit)
  }

  removeLifecycleHooks() {
    electronApp.off('second-instance', this.handleSecondInstance)
    electronApp.off('before-quit', this.handleBeforeQuit)
  }

  private readonly handleSecondInstance = () => {
    this.onSecondInstance?.()
  }

  private readonly handleBeforeQuit = (event: Event) => {
    if (this.quitting) return
    this.quitting = true
    event.preventDefault()
    void this.shutdown().finally(() => {
      this.removeLifecycleHooks()
      electronApp.quit()
    })
  }

  async shutdown(): Promise<void> {
    if (this.shutdownPromise) return this.shutdownPromise

    this.shutdownPromise = this.performShutdown()
    return this.shutdownPromise
  }

  private async performShutdown(): Promise<void> {
    if (this.childExited) return

    const exitPromise = new Promise<void>((resolve) => {
      this.child.once('exit', () => {
        this.childExited = true
        this.logStream?.end()
        resolve()
      })
    })

    if (this.child.exitCode == null && this.child.signalCode == null) {
      this.child.kill('SIGTERM')
    }

    const exited = await Promise.race([
      exitPromise.then(() => true),
      delay(this.killTimeoutMs).then(() => false),
    ])

    if (exited) return

    if (!this.childExited) {
      this.child.kill('SIGKILL')
    }

    await Promise.race([
      exitPromise,
      delay(1_000),
    ])
  }
}

export async function startServerSupervisor(opts: ServerSupervisorOptions = {}): Promise<ServerSupervisor | null> {
  if (!electronApp.requestSingleInstanceLock()) {
    electronApp.quit()
    return null
  }

  const env = { ...process.env, ...opts.env }
  const userDataPath = opts.userDataPath ?? electronApp.getPath('userData')
  const binaryPath = resolveServerBinaryPath(env)
  const addr = parseLoopbackAddr(env.MUESLI_ADDR ?? DEFAULT_ADDR)
  const baseUrl = `http://${addr.host}:${addr.port}`
  const logPath = opts.logPath ?? makeServerLogPath(userDataPath)
  await mkdir(dirname(logPath), { recursive: true })
  let logStream: WriteStream | null = null
  try {
    logStream = createWriteStream(logPath, { flags: 'w' })
    logStream.on('error', (err) => {
      console.error('[muesli-server log]', err)
    })
  } catch (err) {
    console.error('[muesli-server log]', err)
  }
  const child = (opts.spawnImpl ?? spawn)(binaryPath, ['--embedded'], {
    env: {
      ...resolveResourceEnv({
        isPackaged: electronApp.isPackaged,
        resourcesPath: process.resourcesPath,
      }),
      ...env,
      ...(opts.env && Object.prototype.hasOwnProperty.call(opts.env, 'MUESLI_APPDATA')
        ? {}
        : { MUESLI_APPDATA: join(userDataPath, 'embedded-server') }),
      MUESLI_ADDR: `${addr.host}:${addr.port}`,
      MUESLI_PARENT_PID: String(process.pid),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  }) as unknown as ChildProcessWithoutNullStreams

  const supervisor = new EmbeddedServerSupervisorImpl(child, baseUrl, logPath, logStream, {
    healthPollIntervalMs: opts.healthPollIntervalMs ?? DEFAULT_HEALTH_POLL_INTERVAL_MS,
    healthTimeoutMs: opts.healthTimeoutMs ?? DEFAULT_HEALTH_TIMEOUT_MS,
    killTimeoutMs: opts.killTimeoutMs ?? DEFAULT_KILL_TIMEOUT_MS,
    onSecondInstance: opts.onSecondInstance,
    onUnexpectedExit: opts.onUnexpectedExit,
  })

  supervisor.installLifecycleHooks()

  if (opts.waitForHealthy === false) {
    return supervisor
  }

  try {
    await supervisor.waitUntilHealthy(opts.fetchImpl ?? globalThis.fetch.bind(globalThis))
    return supervisor
  } catch (err) {
    await supervisor.shutdown().catch(() => {})
    supervisor.removeLifecycleHooks()
    throw err
  }
}
