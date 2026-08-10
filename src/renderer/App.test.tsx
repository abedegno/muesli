// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Outlet, useLocation } from 'react-router-dom'

const getConfig = vi.fn()
const hasLocalSession = vi.fn()
const getOnboarded = vi.fn()
const authListeners: Array<(notice: { message: string }) => void> = []
const startupListeners: Array<(status: import('../shared/types').EmbeddedStartupStatus) => void> = []
const trayListeners: Array<(target: '/new' | '/settings') => void> = []
const connect = vi.fn()
const startupCalls: string[] = []

vi.mock('@/api', () => ({
  muesli: {
    getConfig: () => {
      startupCalls.push('getConfig')
      return getConfig()
    },
    hasLocalSession: () => {
      startupCalls.push('hasLocalSession')
      return hasLocalSession()
    },
    getOnboarded: () => getOnboarded(),
    onAuthInvalidated: (listener: (notice: { message: string }) => void) => {
      authListeners.push(listener)
      return () => {}
    },
    onEmbeddedStartupStatus: (listener: (status: import('../shared/types').EmbeddedStartupStatus) => void) => {
      startupListeners.push(listener)
      return () => {}
    },
    onTrayNavigate: (listener: (target: '/new' | '/settings') => void) => {
      trayListeners.push(listener)
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
  NotesListScreen: () => <div>all notes app</div>,
}))

vi.mock('./components/NewMeetingScreen', () => ({
  NewMeetingScreen: () => <div data-testid="new-route">new route</div>,
}))

vi.mock('./components/HomeScreen', () => ({
  HomeScreen: () => <div>home app</div>,
}))

import { App } from './App'

afterEach(() => {
  vi.useRealTimers()
  cleanup()
  vi.clearAllMocks()
  authListeners.splice(0, authListeners.length)
  startupListeners.splice(0, startupListeners.length)
  trayListeners.splice(0, trayListeners.length)
  startupCalls.splice(0, startupCalls.length)
})

function emitStartup(status: import('../shared/types').EmbeddedStartupStatus) {
  startupListeners[startupListeners.length - 1]?.(status)
}

function emitTray(target: '/new' | '/settings') {
  trayListeners[trayListeners.length - 1]?.(target)
}

describe('App', () => {
  it('keeps the startup state visible while an embedded local session reconnects', async () => {
    vi.useFakeTimers()
    hasLocalSession.mockResolvedValue(true)
    getConfig
      .mockResolvedValueOnce(null)
      .mockRejectedValueOnce(new Error('connection refused'))
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({ serverUrl: 'http://localhost:8080', token: 'app-token' })
    getOnboarded.mockResolvedValue(true)

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: false })
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByText('Starting Muesli…')).toBeInTheDocument()
    expect(screen.queryByText('Connect to Muesli')).not.toBeInTheDocument()
    expect(screen.queryByText('First run (create the account)')).not.toBeInTheDocument()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1750)
    })

    expect(screen.getByText('home app')).toBeInTheDocument()
    expect(screen.queryByText('Connect to Muesli')).not.toBeInTheDocument()
    expect(screen.queryByText('First run (create the account)')).not.toBeInTheDocument()
    expect(getConfig).toHaveBeenCalledTimes(4)
    expect(startupCalls[0]).toBe('getConfig')
    vi.useRealTimers()
  })

  it('explains when the embedded local server remains unreachable without offering first-run setup', async () => {
    vi.useFakeTimers()
    hasLocalSession.mockResolvedValue(true)
    getConfig.mockResolvedValue(null)
    getOnboarded.mockResolvedValue(true)

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: false })
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3250)
    })

    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't reach the local server")
    expect(screen.queryByText('First run (create the account)')).not.toBeInTheDocument()
    expect(getConfig).toHaveBeenCalledTimes(5)
    vi.useRealTimers()
  })

  it('keeps the first-run connect path for a genuinely unconfigured install', async () => {
    vi.useFakeTimers()
    hasLocalSession.mockResolvedValue(false)
    getConfig.mockResolvedValue(null)
    getOnboarded.mockResolvedValue(false)

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: false })
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3250)
    })

    expect(screen.getByText('Connect to Muesli')).toBeInTheDocument()
    expect(screen.getByText('First run (create the account)')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'First run (create the account)' })).toBeVisible()
  })

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
    expect(await screen.findByText('home app')).toBeInTheDocument()
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
    expect(await screen.findByText('home app')).toBeInTheDocument()

    await user.click(await screen.findByRole('button', { name: /open ai settings/i }))

    expect(await screen.findByTestId('settings-route')).toHaveTextContent('/settings#ai-transcription')
  })

  it('routes tray commands into the requested view', async () => {
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
    expect(await screen.findByText('home app')).toBeInTheDocument()

    await act(async () => {
      emitTray('/settings')
    })
    expect(await screen.findByTestId('settings-route')).toHaveTextContent('/settings')

    await act(async () => {
      emitTray('/new')
    })
    expect(await screen.findByTestId('new-route')).toHaveTextContent('new route')
  })

  it('renders Home at / and All notes at /notes', async () => {
    getConfig.mockResolvedValue({ serverUrl: 'http://localhost:8080', token: 'app-token' })
    getOnboarded.mockResolvedValue(true)

    const { unmount } = render(
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: false })
    })
    expect(await screen.findByText('home app')).toBeInTheDocument()
    expect(screen.queryByText('all notes app')).not.toBeInTheDocument()
    unmount()

    render(
      <MemoryRouter initialEntries={['/notes']}>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: false })
    })
    expect(await screen.findByText('all notes app')).toBeInTheDocument()
  })

  it('redirects the legacy /coming-up route to Home', async () => {
    getConfig.mockResolvedValue({ serverUrl: 'http://localhost:8080', token: 'app-token' })
    getOnboarded.mockResolvedValue(true)

    render(
      <MemoryRouter initialEntries={['/coming-up']}>
        <App />
      </MemoryRouter>,
    )

    await act(async () => {
      emitStartup({ status: 'ready', degraded: false })
    })
    expect(await screen.findByText('home app')).toBeInTheDocument()
  })
})
