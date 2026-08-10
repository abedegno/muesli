// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Outlet, useLocation } from 'react-router-dom'

const getConfig = vi.fn()
const getLocalSessionStatus = vi.fn()
const getOnboarded = vi.fn()
const authListeners: Array<(notice: { message: string }) => void> = []
const startupListeners: Array<(status: import('../shared/types').EmbeddedStartupStatus) => void> = []
const trayListeners: Array<(target: '/new' | '/settings') => void> = []
const connect = vi.fn()

vi.mock('@/api', () => ({
  muesli: {
    getConfig: () => getConfig(),
    getLocalSessionStatus: () => getLocalSessionStatus(),
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

beforeEach(() => {
  getLocalSessionStatus.mockResolvedValue('connected')
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
  vi.clearAllMocks()
  authListeners.splice(0, authListeners.length)
  startupListeners.splice(0, startupListeners.length)
  trayListeners.splice(0, trayListeners.length)
})

function emitStartup(status: import('../shared/types').EmbeddedStartupStatus) {
  startupListeners[startupListeners.length - 1]?.(status)
}

function emitTray(target: '/new' | '/settings') {
  trayListeners[trayListeners.length - 1]?.(target)
}

describe('App', () => {
  it('keeps the connect screen hidden while a saved local session becomes ready', async () => {
    vi.useFakeTimers()
    getLocalSessionStatus.mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve('connected'), 200)),
    )
    getConfig.mockResolvedValue({ serverUrl: 'http://localhost:8080', token: 'app-token' })
    getOnboarded.mockResolvedValue(true)

    render(<MemoryRouter><App /></MemoryRouter>)
    act(() => emitStartup({ status: 'ready', degraded: false }))
    expect(screen.getByText('Starting up the local server…')).toBeInTheDocument()
    expect(screen.queryByText('First run (create the account)')).not.toBeInTheDocument()

    await act(async () => vi.advanceTimersByTimeAsync(200))
    expect(screen.getByText('home app')).toBeInTheDocument()
    expect(screen.queryByText('First run (create the account)')).not.toBeInTheDocument()
    vi.useRealTimers()
  })

  it('shows a local-server error rather than first-run setup when the bound is exceeded', async () => {
    vi.useFakeTimers()
    getLocalSessionStatus.mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve('server-unreachable'), 200)),
    )
    getOnboarded.mockResolvedValue(true)

    render(<MemoryRouter><App /></MemoryRouter>)
    act(() => emitStartup({ status: 'ready', degraded: false }))
    await act(async () => vi.advanceTimersByTimeAsync(200))
    expect(screen.getByText("Couldn't reach the local server")).toBeInTheDocument()
    expect(screen.queryByText('First run (create the account)')).not.toBeInTheDocument()
    vi.useRealTimers()
  })

  it('renders the connect screen for a genuinely unconfigured install', async () => {
    getLocalSessionStatus.mockResolvedValue('needs-setup')
    getConfig.mockResolvedValue(null)
    getOnboarded.mockResolvedValue(false)

    render(<MemoryRouter><App /></MemoryRouter>)
    await act(async () => emitStartup({ status: 'ready', degraded: false }))
    expect(await screen.findByText('First run (create the account)')).toBeInTheDocument()
    expect(screen.getByText('Connect to Muesli')).toBeInTheDocument()
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
