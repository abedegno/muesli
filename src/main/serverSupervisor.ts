import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { app } from 'electron'

export interface ServerSupervisor {
  baseUrl: string
  shutdown(): Promise<void>
}

export interface ServerSupervisorOptions {
  env?: NodeJS.ProcessEnv
  fetchImpl?: typeof fetch
  healthPollIntervalMs?: number
  healthTimeoutMs?: number
  killTimeoutMs?: number
  onSecondInstance?: () => void
  spawnImpl?: typeof spawn
}

interface ParsedAddr {
  host: string
  port: number
}

const DEFAULT_ADDR = '127.0.0.1:8080'
const DEFAULT_HEALTH_POLL_INTERVAL_MS = 250
const DEFAULT_HEALTH_TIMEOUT_MS = 30_000
const DEFAULT_KILL_TIMEOUT_MS = 5_000
const electronApp = app as unknown as {
  isPackaged: boolean
  on(event: 'second-instance' | 'before-quit', listener: (...args: any[]) => void): void
  off(event: 'second-instance' | 'before-quit', listener: (...args: any[]) => void): void
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
  if (electronApp.isPackaged) {
    throw new Error('packaged server binary resolution is TODO (phase 5c)')
  }

  const configured = env.MUESLI_SERVER_BIN?.trim()
  if (configured) return configured

  throw new Error('MUESLI_SERVER_BIN must point to the muesli server binary in development')
}

function makeHealthUrl(baseUrl: string): string {
  return new URL('/healthz', baseUrl).toString()
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms)
  })
}

function logChildOutput(stream: 'stdout' | 'stderr', data: Buffer | string) {
  const text = typeof data === 'string' ? data : data.toString('utf8')
  const trimmed = text.trimEnd()
  if (!trimmed) return
  const prefix = stream === 'stderr' ? '[muesli-server stderr]' : '[muesli-server stdout]'
  if (stream === 'stderr') {
    console.error(prefix, trimmed)
  } else {
    console.log(prefix, trimmed)
  }
}

class EmbeddedServerSupervisorImpl implements ServerSupervisor {
  readonly baseUrl: string
  private readonly child: ChildProcessWithoutNullStreams
  private readonly healthUrl: string
  private readonly healthPollIntervalMs: number
  private readonly healthTimeoutMs: number
  private readonly killTimeoutMs: number
  private readonly onSecondInstance?: () => void
  private shutdownPromise: Promise<void> | null = null
  private quitting = false
  private childExited = false

  constructor(child: ChildProcessWithoutNullStreams, baseUrl: string, opts: Required<Pick<ServerSupervisorOptions, 'healthPollIntervalMs' | 'healthTimeoutMs' | 'killTimeoutMs'>> & Pick<ServerSupervisorOptions, 'onSecondInstance'>) {
    this.child = child
    this.baseUrl = baseUrl
    this.healthUrl = makeHealthUrl(baseUrl)
    this.healthPollIntervalMs = opts.healthPollIntervalMs
    this.healthTimeoutMs = opts.healthTimeoutMs
    this.killTimeoutMs = opts.killTimeoutMs
    this.onSecondInstance = opts.onSecondInstance

    this.child.once('exit', () => {
      this.childExited = true
    })
    this.child.stdout.on('data', (chunk) => logChildOutput('stdout', chunk))
    this.child.stderr.on('data', (chunk) => logChildOutput('stderr', chunk))
  }

  async waitUntilHealthy(fetchImpl: typeof fetch): Promise<void> {
    const deadline = Date.now() + this.healthTimeoutMs
    let lastError: unknown

    while (Date.now() < deadline) {
      if (this.childExited) {
        throw new Error('embedded server exited before becoming healthy')
      }

      try {
        const res = await fetchImpl(this.healthUrl)
        if (res.ok) return
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
  const binaryPath = resolveServerBinaryPath(env)
  const addr = parseLoopbackAddr(env.MUESLI_ADDR ?? DEFAULT_ADDR)
  const baseUrl = `http://${addr.host}:${addr.port}`
  const child = (opts.spawnImpl ?? spawn)(binaryPath, ['--embedded'], {
    env: {
      ...env,
      MUESLI_ADDR: `${addr.host}:${addr.port}`,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  }) as unknown as ChildProcessWithoutNullStreams

  const supervisor = new EmbeddedServerSupervisorImpl(child, baseUrl, {
    healthPollIntervalMs: opts.healthPollIntervalMs ?? DEFAULT_HEALTH_POLL_INTERVAL_MS,
    healthTimeoutMs: opts.healthTimeoutMs ?? DEFAULT_HEALTH_TIMEOUT_MS,
    killTimeoutMs: opts.killTimeoutMs ?? DEFAULT_KILL_TIMEOUT_MS,
    onSecondInstance: opts.onSecondInstance,
  })

  supervisor.installLifecycleHooks()

  try {
    await supervisor.waitUntilHealthy(opts.fetchImpl ?? globalThis.fetch.bind(globalThis))
    return supervisor
  } catch (err) {
    await supervisor.shutdown().catch(() => {})
    supervisor.removeLifecycleHooks()
    throw err
  }
}
