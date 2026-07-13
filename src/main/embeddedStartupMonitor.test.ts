import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { startEmbeddedStartupMonitor } from './embeddedStartupMonitor'
import type { EmbeddedStartupStatus } from '../shared/types'

describe('embeddedStartupMonitor', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls startup progress and tolerates connection failures before ready', async () => {
    const statuses: EmbeddedStartupStatus[] = []
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new Error('ECONNREFUSED'))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        phase: 'db-init',
        detail: 'Preparing database',
        percent: 12,
        degraded: false,
        ready: false,
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        phase: 'model-pull',
        detail: 'Downloading models',
        percent: 42,
        degraded: true,
        ready: false,
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        phase: 'ready',
        detail: 'Ready',
        percent: 100,
        degraded: true,
        ready: true,
      }), { status: 200 }))

    const supervisor = {
      baseUrl: 'http://127.0.0.1:4567',
      logPath: '/tmp/userData/logs/server.log',
      waitUntilHealthy: vi.fn(async () => {}),
      shutdown: vi.fn(async () => {}),
    }

    const monitor = startEmbeddedStartupMonitor({
      supervisor,
      fetchImpl: fetchMock as unknown as typeof fetch,
      statusPollIntervalMs: 10,
      readyHoldMs: 0,
      onStatus: (status) => {
        statuses.push(status)
      },
    })

    await vi.advanceTimersByTimeAsync(50)
    monitor.stop()

    expect(statuses).toEqual([
      {
        status: 'progress',
        phase: 'db-init',
        detail: 'Preparing database',
        percent: 12,
        degraded: false,
      },
      {
        status: 'progress',
        phase: 'model-pull',
        detail: 'Downloading models',
        percent: 42,
        degraded: true,
      },
      {
        status: 'ready',
        degraded: true,
      },
    ])
  })

  it('surfaces a terminal error and keeps the path to the log file', async () => {
    const statuses: EmbeddedStartupStatus[] = []
    const supervisor = {
      baseUrl: 'http://127.0.0.1:4568',
      logPath: '/tmp/userData/logs/server.log',
      waitUntilHealthy: vi.fn(async () => {
        throw new Error('timed out waiting for /healthz after 30000ms')
      }),
      shutdown: vi.fn(async () => {}),
    }

    startEmbeddedStartupMonitor({
      supervisor,
      fetchImpl: vi.fn().mockRejectedValue(new Error('ECONNREFUSED')) as unknown as typeof fetch,
      statusPollIntervalMs: 10,
      readyHoldMs: 20,
      onStatus: (status) => {
        statuses.push(status)
      },
    })

    await vi.runAllTimersAsync()

    expect(statuses).toHaveLength(1)
    expect(statuses[0]).toEqual(expect.objectContaining({
      status: 'error',
      logPath: '/tmp/userData/logs/server.log',
    }))
    expect((statuses[0] as Extract<EmbeddedStartupStatus, { status: 'error' }>).message).toContain('timed out waiting for /healthz')
    expect(supervisor.shutdown).toHaveBeenCalledTimes(1)
  })
})
