// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Outlet, useLocation } from 'react-router-dom'

const getConfig = vi.fn()
const getOnboarded = vi.fn()
const authListeners: Array<(notice: { message: string }) => void> = []
const startupListeners: Array<(status: import('../shared/types').EmbeddedStartupStatus) => void> = []
const connect = vi.fn()

vi.mock('@/api', () => ({
  muesli: {
    getConfig: () => getConfig(),
    getOnboarded: () => getOnboarded(),
    onAuthInvalidated: (listener: (notice: { message: string }) => void) => {
      authListeners.push(listener)
      return () => {}
    },
    onEmbeddedStartupStatus: (listener: (status: import('../shared/types').EmbeddedStartupStatus) => void) => {
      startupListeners.push(listener)
      return () => {}
    },
    connect: (...args: Parameters<typeof connect>) => connect(...args),
  },
}))

vi.mock('./components/shell/AppLayout', () => ({
  AppLayout: () => <Outlet />,
}))

vi.mock('./components/SettingsScreen', () => ({
  SettingsScreen: () => {
    const location = useLocation()
    return <div data-testid="settings-route">{`${location.pathname}${location.hash}`}</div>
  },
}))

vi.mock('./components/NotesListScreen', () => ({
  NotesListScreen: () => <div>main app</div>,
}))

import { App } from './App'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  authListeners.splice(0, authListeners.length)
  startupListeners.splice(0, startupListeners.length)
})

function emitStartup(status: import('../shared/types').EmbeddedStartupStatus) {
  startupListeners[0]?.(status)
}

describe('App', () => {
  it('switches to the reconnect screen when auth invalidation is signaled', async () => {
    getConfig.mockResolvedValue({ serverUrl: 'http://localhost:8080', token: 'app-token' })
    getOnboarded.mockResolvedValue(true)

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: false })
    })
    expect(await screen.findByText('main app')).toBeInTheDocument()
    await waitFor(() => expect(authListeners).toHaveLength(1))
    const listener = authListeners[0]
    expect(listener).toBeTypeOf('function')

    listener({ message: 'Your saved sign-in is no longer valid for this server. Sign in again to reconnect.' })

    expect(await screen.findByText('Connect to Muesli')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Your saved sign-in is no longer valid for this server. Sign in again to reconnect.')
    expect(screen.queryByText('Error invoking remote method')).toBeNull()
  })

  it('routes the startup banner into the AI / Transcription settings section', async () => {
    getConfig.mockResolvedValue({ serverUrl: 'http://localhost:8080', token: 'app-token' })
    getOnboarded.mockResolvedValue(true)

    const user = userEvent.setup()

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: true })
    })
    expect(await screen.findByText('main app')).toBeInTheDocument()

    await user.click(await screen.findByRole('button', { name: /open ai settings/i }))

    expect(await screen.findByTestId('settings-route')).toHaveTextContent('/settings#ai-transcription')
  })
})
