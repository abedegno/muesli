import type { EmbeddedStartupStatus } from '../shared/types'
import type { ServerSupervisor } from './serverSupervisor'

export interface EmbeddedStartupMonitorOptions {
  onStatus: (status: EmbeddedStartupStatus) => void
  supervisor: Pick<ServerSupervisor, 'baseUrl' | 'logPath' | 'shutdown' | 'waitUntilHealthy'>
  fetchImpl?: typeof fetch
  statusPollIntervalMs?: number
  readyHoldMs?: number
}

const DEFAULT_STATUS_POLL_INTERVAL_MS = 400
const DEFAULT_READY_HOLD_MS = 5_000
const STARTUP_ENDPOINT = '/api/embedded/startup'

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms)
  })
}

function toMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

function normalizePercent(percent: unknown): number | null {
  return typeof percent === 'number' && Number.isFinite(percent) ? percent : null
}

export function startEmbeddedStartupMonitor(opts: EmbeddedStartupMonitorOptions): { stop(): void } {
  const fetchImpl = opts.fetchImpl ?? globalThis.fetch.bind(globalThis)
  const statusPollIntervalMs = opts.statusPollIntervalMs ?? DEFAULT_STATUS_POLL_INTERVAL_MS
  const readyHoldMs = opts.readyHoldMs ?? DEFAULT_READY_HOLD_MS
  const url = new URL(STARTUP_ENDPOINT, opts.supervisor.baseUrl).toString()

  let stopped = false
  let readyAt: number | null = null

  const emit = (status: EmbeddedStartupStatus) => {
    if (stopped) return
    opts.onStatus(status)
  }

  const stop = () => {
    stopped = true
  }

  const emitLatest = async () => {
    try {
      const res = await fetchImpl(url)

      const data = (await res.json()) as {
        phase?: string
        detail?: string
        percent?: number | null
        degraded?: boolean
        ready?: boolean
      }

      if (data.ready) {
        readyAt ??= Date.now()
        emit({ status: 'ready', degraded: !!data.degraded })
        return
      }

      emit({
        status: 'progress',
        phase: data.phase ?? 'starting',
        detail: data.detail ?? '',
        percent: normalizePercent(data.percent),
        degraded: !!data.degraded,
      })
    } catch {
      // The embedded server may not be listening yet. Keep retrying.
    }
  }

  void (async () => {
    while (!stopped) {
      await emitLatest()

      if (readyAt !== null) {
        if (Date.now() - readyAt >= readyHoldMs) {
          stop()
          break
        }
      }

      await delay(statusPollIntervalMs)
    }
  })()

  void opts.supervisor.waitUntilHealthy(fetchImpl).then(async () => {
    if (stopped) return
    await emitLatest()
  }).catch(async (err) => {
    if (stopped) return
    emit({
      status: 'error',
      message: `The embedded Muesli server could not start. ${toMessage(err)}`,
      logPath: opts.supervisor.logPath,
    })
    stop()
    await opts.supervisor.shutdown().catch(() => {})
  })

  return { stop }
}
