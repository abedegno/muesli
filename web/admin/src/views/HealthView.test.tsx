import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { HealthView } from './HealthView'
import type { AdminHealthResponse } from '../api/types'

function makeHealth(overrides: Partial<AdminHealthResponse> = {}): AdminHealthResponse {
  return {
    server: { version: '1.2.3', commit: 'abc1234', goVersion: 'go1.25.6', status: 'ok' },
    plugins: [{ id: 'p1', kind: 'transcriber', name: 'whisper', status: 'ok' }],
    jobs: { counts: { pending: 0, running: 0, done: 3, failed: 0, cancelled: 0 }, status: 'ok' },
    embedding: {
      enabled: true,
      model: 'nomic-embed-text',
      dim: 768,
      minScore: 0.6,
      docPrefix: '',
      queryPrefix: '',
      done: 5,
      total: 5,
    },
    storage: { path: '/data/audio', totalBytes: 1_000_000_000, freeBytes: 500_000_000 },
    ...overrides,
  }
}

describe('HealthView', () => {
  it('shows a loading state before the first fetch resolves', () => {
    const client = { getAdminHealth: vi.fn().mockReturnValue(new Promise(() => {})) }
    render(<HealthView client={client} />)
    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('renders an all-OK snapshot, including an ok badge on the Server section', async () => {
    const client = { getAdminHealth: vi.fn().mockResolvedValue(makeHealth()) }
    render(<HealthView client={client} />)

    expect(await screen.findByText('1.2.3')).toBeInTheDocument()
    expect(screen.getByText('abc1234')).toBeInTheDocument()
    expect(screen.getByText('whisper')).toBeInTheDocument()
    expect(screen.getByText('5 / 5')).toBeInTheDocument()

    // Every section (including Server, which previously had no badge at all)
    // renders an "ok" badge with the ok styling.
    const okBadges = screen.getAllByText('ok')
    expect(okBadges.length).toBeGreaterThanOrEqual(3) // server, plugins, jobs (at least)
    for (const badge of okBadges) {
      expect(badge).toHaveClass('pill-ok')
    }
  })

  it('renders a mixed/error state, badging the jobs section warn (not error) when jobs have merely failed', async () => {
    const health = makeHealth({
      plugins: [
        { id: 'p1', kind: 'transcriber', name: 'whisper', status: 'ok' },
        { id: 'p2', kind: 'agent', name: 'ollama', status: 'error', error: 'connection refused' },
      ],
      jobs: { counts: { pending: 0, running: 0, done: 0, failed: 2, cancelled: 0 }, status: 'warn' },
      embedding: {
        enabled: true,
        model: 'nomic-embed-text',
        dim: 768,
        minScore: 0.6,
        docPrefix: '',
        queryPrefix: '',
        done: 0,
        total: 0,
        error: 'embedding store lookup failed',
      },
      storage: { path: '/data/audio', totalBytes: 0, freeBytes: 0, error: 'no such file or directory' },
    })
    const client = { getAdminHealth: vi.fn().mockResolvedValue(health) }
    render(<HealthView client={client} />)

    expect(await screen.findByText('ollama')).toBeInTheDocument()
    expect(screen.getByText('connection refused')).toBeInTheDocument()
    expect(screen.getByText('embedding store lookup failed')).toBeInTheDocument()
    expect(screen.getByText('no such file or directory')).toBeInTheDocument()
    // The healthy plugin row must still show ok even though another failed.
    expect(screen.getByText('whisper')).toBeInTheDocument()

    // The jobs section must render a distinct "warn" badge (not "error" or
    // "ok") with its own pill-warn styling — this is the headline
    // code-review finding: warn must actually be reachable and
    // visually distinct.
    const warnBadge = screen.getByText('warn')
    expect(warnBadge).toBeInTheDocument()
    expect(warnBadge).toHaveClass('pill-warn')
    expect(warnBadge).not.toHaveClass('pill-ok')
    expect(warnBadge).not.toHaveClass('pill-error')

    // Plugin and embedding errors still use the distinct error styling.
    const errorBadges = screen.getAllByText('error')
    expect(errorBadges.length).toBeGreaterThan(0)
    for (const badge of errorBadges) {
      expect(badge).toHaveClass('pill-error')
    }
  })

  it('renders a disabled plugin distinctly from ok/error/warn', async () => {
    const health = makeHealth({
      plugins: [{ id: 'p1', kind: 'agent', name: 'ollama', status: 'disabled' }],
    })
    const client = { getAdminHealth: vi.fn().mockResolvedValue(health) }
    render(<HealthView client={client} />)

    const disabledBadge = await screen.findByText('disabled')
    expect(disabledBadge).toHaveClass('pill-disabled')
  })

  it('renders a warn badge on the Server section when build info falls back', async () => {
    const health = makeHealth({
      server: { version: 'dev', commit: 'unknown', goVersion: 'go1.25.6', status: 'warn' },
    })
    const client = { getAdminHealth: vi.fn().mockResolvedValue(health) }
    render(<HealthView client={client} />)

    await screen.findByText('dev')
    const warnBadge = screen.getByText('warn')
    expect(warnBadge).toHaveClass('pill-warn')
  })

  it('re-fetches when Refresh is clicked', async () => {
    const client = {
      getAdminHealth: vi
        .fn()
        .mockResolvedValueOnce(
          makeHealth({ server: { version: '1.0.0', commit: 'aaa', goVersion: 'go1.25.6', status: 'ok' } })
        )
        .mockResolvedValueOnce(
          makeHealth({ server: { version: '2.0.0', commit: 'bbb', goVersion: 'go1.25.6', status: 'ok' } })
        ),
    }
    render(<HealthView client={client} />)

    expect(await screen.findByText('1.0.0')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /refresh/i }))

    await waitFor(() => expect(screen.getByText('2.0.0')).toBeInTheDocument())
    expect(client.getAdminHealth).toHaveBeenCalledTimes(2)
  })
})
