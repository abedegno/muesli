// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EmbeddedStartupGate } from './StartupScreen'
import type { EmbeddedStartupStatus } from '../../shared/types'

let listener: ((status: EmbeddedStartupStatus) => void) | null = null

vi.mock('@/api', () => ({
  muesli: {
    onEmbeddedStartupStatus: vi.fn((cb: (status: EmbeddedStartupStatus) => void) => {
      listener = cb
      return () => {
        if (listener === cb) listener = null
      }
    }),
  },
}))

afterEach(() => {
  cleanup()
  listener = null
})

function emit(status: EmbeddedStartupStatus) {
  listener?.(status)
}

describe('EmbeddedStartupGate', () => {
  it('renders phase-specific progress copy', async () => {
    render(
      <EmbeddedStartupGate>
        <div>App body</div>
      </EmbeddedStartupGate>,
    )

    await act(async () => emit({ status: 'progress', phase: 'db-init', detail: 'Starting database', percent: 12, degraded: false }))
    expect(screen.getByText('Initializing database...')).toBeInTheDocument()

    await act(async () => emit({ status: 'progress', phase: 'ollama-check', detail: 'Checking model runtime', percent: 37, degraded: false }))
    expect(screen.getByText('Checking for Ollama...')).toBeInTheDocument()

    await act(async () => emit({ status: 'progress', phase: 'model-pull', detail: 'Downloading base model', percent: 42, degraded: false }))
    expect(screen.getByText('Downloading models... 42%')).toBeInTheDocument()

    await act(async () => emit({ status: 'progress', phase: 'future-phase', detail: 'Still working', percent: 7, degraded: false }))
    expect(screen.getByText('Starting future-phase...')).toBeInTheDocument()
  })

  it('reveals the app body once ready arrives', async () => {
    render(
      <EmbeddedStartupGate>
        <div>App body</div>
      </EmbeddedStartupGate>,
    )

    await act(async () => emit({ status: 'progress', phase: 'migrate', detail: 'Applying migrations', percent: 55, degraded: false }))
    expect(screen.getByText('Initializing database...')).toBeInTheDocument()

    await act(async () => emit({ status: 'ready', degraded: false }))
    await waitFor(() => expect(screen.getByText('App body')).toBeInTheDocument())
  })

  it('shows an error screen with the log path', async () => {
    render(
      <EmbeddedStartupGate>
        <div>App body</div>
      </EmbeddedStartupGate>,
    )

    await act(async () => emit({
      status: 'error',
      message: 'The embedded Muesli server could not start. timed out waiting for /healthz',
      logPath: '/tmp/userData/logs/server.log',
    }))

    expect(screen.getByText('Muesli could not start')).toBeInTheDocument()
    expect(screen.getByText(/timed out waiting for \/healthz/)).toBeInTheDocument()
    expect(screen.getByText('/tmp/userData/logs/server.log')).toBeInTheDocument()
  })

  it('shows and dismisses the degraded banner without hiding the app', async () => {
    const user = userEvent.setup()

    render(
      <EmbeddedStartupGate>
        <div>App body</div>
      </EmbeddedStartupGate>,
    )

    await act(async () => emit({ status: 'ready', degraded: true }))

    expect(screen.getByText('App body')).toBeInTheDocument()
    expect(screen.getByText(/install ollama to enable summaries & search/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /download ollama/i })).toHaveAttribute('href', 'https://ollama.com/download')
    expect(screen.getByRole('link', { name: /learn more/i })).toHaveAttribute(
      'href',
      'https://github.com/abedegno/muesli/blob/main/docs/DESKTOP-ONBOARDING.md',
    )

    await user.click(screen.getByRole('button', { name: /dismiss/i }))
    expect(screen.getByText('App body')).toBeInTheDocument()
    expect(screen.queryByText(/install ollama to enable summaries & search/i)).not.toBeInTheDocument()
  })

  it('keeps the banner and shell inside one viewport-sized flex column', async () => {
    const { container } = render(
      <EmbeddedStartupGate>
        <div>App body</div>
      </EmbeddedStartupGate>,
    )

    await act(async () => emit({ status: 'ready', degraded: true }))

    const root = container.firstElementChild as HTMLElement
    expect(root).toHaveClass('flex', 'h-screen', 'min-h-screen', 'overflow-hidden')
    expect(root.children).toHaveLength(2)
    expect((root.children[0] as HTMLElement)).toHaveTextContent(/install ollama to enable summaries & search/i)
    expect((root.children[1] as HTMLElement)).toHaveClass('min-h-0', 'flex-1')
    expect(screen.getByText('App body')).toBeInTheDocument()
  })
})
