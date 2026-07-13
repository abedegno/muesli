// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'

const getConfig = vi.fn()
const disconnect = vi.fn()
const getGoogleCalendarOAuthStatus = vi.fn()
const openGoogleCalendarOAuthStart = vi.fn()
const getMicrosoftCalendarOAuthStatus = vi.fn()
const openMicrosoftCalendarOAuthStart = vi.fn()
const getDigestConfig = vi.fn()
const updateDigestConfig = vi.fn()

vi.mock('@/api', () => ({
  muesli: {
    getConfig: () => getConfig(),
    disconnect: () => disconnect(),
    getGoogleCalendarOAuthStatus: () => getGoogleCalendarOAuthStatus(),
    openGoogleCalendarOAuthStart: () => openGoogleCalendarOAuthStart(),
    getMicrosoftCalendarOAuthStatus: () => getMicrosoftCalendarOAuthStatus(),
    openMicrosoftCalendarOAuthStart: () => openMicrosoftCalendarOAuthStart(),
    getDigestConfig: () => getDigestConfig(),
    updateDigestConfig: (cadence: string) => updateDigestConfig(cadence),
  },
}))

import { ServerHealthBadge, SettingsScreen } from './SettingsScreen'

const fetchMock = vi.fn<typeof fetch>()

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
})

beforeEach(() => {
  getGoogleCalendarOAuthStatus.mockResolvedValue({ configured: false })
  getMicrosoftCalendarOAuthStatus.mockResolvedValue({ configured: false })
  getDigestConfig.mockResolvedValue({ owner_id: 'owner-1', cadence: 'off' })
  updateDigestConfig.mockResolvedValue({ owner_id: 'owner-1', cadence: 'off' })
})

function renderScreen(serverUrl = 'http://localhost:8080') {
  getConfig.mockResolvedValue({ serverUrl })
  vi.stubGlobal('fetch', fetchMock)
  return render(
    <MemoryRouter>
      <SettingsScreen onDisconnected={vi.fn()} />
    </MemoryRouter>,
  )
}

describe('SettingsScreen', () => {
  it('hides the Google Calendar connect button until the server reports oauth configured', async () => {
    getGoogleCalendarOAuthStatus.mockResolvedValue({ configured: false })

    renderScreen()

    await waitFor(() => expect(getGoogleCalendarOAuthStatus).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'Connect Google Calendar' })).toBeNull()
  })

  it('shows the Google Calendar connect button when oauth is configured', async () => {
    getGoogleCalendarOAuthStatus.mockResolvedValue({ configured: true })

    renderScreen()

    expect(await screen.findByRole('button', { name: 'Connect Google Calendar' })).toBeInTheDocument()
  })

  it('hides the Microsoft Calendar connect button until the server reports oauth configured', async () => {
    getMicrosoftCalendarOAuthStatus.mockResolvedValue({ configured: false })

    renderScreen()

    await waitFor(() => expect(getMicrosoftCalendarOAuthStatus).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'Connect Microsoft Calendar' })).toBeNull()
  })

  it('shows the Microsoft Calendar connect button when oauth is configured', async () => {
    getMicrosoftCalendarOAuthStatus.mockResolvedValue({ configured: true })

    renderScreen()

    expect(await screen.findByRole('button', { name: 'Connect Microsoft Calendar' })).toBeInTheDocument()
  })

  it('opens the Google Calendar connect URL when the button is clicked', async () => {
    const user = userEvent.setup()
    getGoogleCalendarOAuthStatus.mockResolvedValue({ configured: true })

    renderScreen()

    await user.click(await screen.findByRole('button', { name: 'Connect Google Calendar' }))
    expect(openGoogleCalendarOAuthStart).toHaveBeenCalledTimes(1)
  })

  it('opens the Microsoft Calendar connect URL when the button is clicked', async () => {
    const user = userEvent.setup()
    getMicrosoftCalendarOAuthStatus.mockResolvedValue({ configured: true })

    renderScreen()

    await user.click(await screen.findByRole('button', { name: 'Connect Microsoft Calendar' }))
    expect(openMicrosoftCalendarOAuthStart).toHaveBeenCalledTimes(1)
  })

  it('loads and updates the digest cadence', async () => {
    const user = userEvent.setup()
    getDigestConfig.mockResolvedValue({ owner_id: 'owner-1', cadence: 'daily' })

    renderScreen()

    const cadence = await screen.findByRole('combobox', { name: 'Cadence' })
    expect(cadence).toHaveValue('daily')

    await user.selectOptions(cadence, 'weekly')

    await waitFor(() => expect(updateDigestConfig).toHaveBeenCalledWith('weekly'))
  })

  it('shows Connected for an ok health response', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok' }),
    } as Response)

    renderScreen()

    expect(await screen.findByText('Connected')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Connected')
    expect(fetchMock).toHaveBeenCalledWith('http://localhost:8080/healthz', expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })

  it('shows a version when health includes one and normalizes trailing slashes', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok', version: '1.2.3' }),
    } as Response)

    renderScreen('http://localhost:8080/')

    expect(await screen.findByText('Connected · v1.2.3')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('http://localhost:8080/healthz', expect.anything())
  })

  it('shows Unreachable when the health request rejects', async () => {
    fetchMock.mockRejectedValue(new Error('offline'))

    renderScreen()

    expect(await screen.findByText('Unreachable')).toBeInTheDocument()
  })

  it('shows Unreachable for a non-ok health response', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      json: async () => ({ status: 'ok' }),
    } as Response)

    renderScreen()

    expect(await screen.findByText('Unreachable')).toBeInTheDocument()
  })

  it('skips health fetch and badge when no server URL is configured', async () => {
    renderScreen('')

    expect(await screen.findByText('Not connected')).toBeInTheDocument()
    await waitFor(() => expect(fetchMock).not.toHaveBeenCalled())
    expect(screen.queryByText('Connected')).toBeNull()
    expect(screen.queryByText('Unreachable')).toBeNull()
  })

  it('checks health again when the server URL changes', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok' }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    const { rerender } = render(<ServerHealthBadge serverUrl="http://localhost:8080" />)

    expect(await screen.findByText('Connected')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('http://localhost:8080/healthz', expect.anything())

    rerender(<ServerHealthBadge serverUrl="http://localhost:9090/" />)

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(fetchMock).toHaveBeenLastCalledWith('http://localhost:9090/healthz', expect.anything())
  })

  it('renders the auto-record toggle, defaults it off, and persists changes', async () => {
    const user = userEvent.setup()

    renderScreen('')

    const toggle = await screen.findByRole('checkbox', { name: 'Auto-record detected meetings' })
    expect(toggle).not.toBeChecked()
    expect(localStorage.getItem('muesli.calendar.autoRecordDetectedMeetings')).toBeNull()

    await user.click(toggle)

    expect(toggle).toBeChecked()
    expect(localStorage.getItem('muesli.calendar.autoRecordDetectedMeetings')).toBe('1')
  })
})
