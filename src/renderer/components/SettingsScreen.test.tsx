// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ToastProvider } from '@/components/ui/Toast'

const getConfig = vi.fn()
const getReadyz = vi.fn()
const disconnect = vi.fn()
const resetToBuiltIn = vi.fn()
const getGoogleCalendarOAuthStatus = vi.fn()
const openGoogleCalendarOAuthStart = vi.fn()
const getMicrosoftCalendarOAuthStatus = vi.fn()
const openMicrosoftCalendarOAuthStart = vi.fn()
const getKeepRunningInBackground = vi.fn()
const setKeepRunningInBackground = vi.fn()
const getDigestConfig = vi.fn()
const updateDigestConfig = vi.fn()
const getServerHealth = vi.fn()
const listPlugins = vi.fn()
const checkPluginHealth = vi.fn()
const setStreamingTranscriber = vi.fn()
const clearStreamingTranscriber = vi.fn()

vi.mock('@/api', () => ({
  muesli: {
    getConfig: () => getConfig(),
    getReadyz: () => getReadyz(),
    disconnect: () => disconnect(),
    resetToBuiltIn: () => resetToBuiltIn(),
    getGoogleCalendarOAuthStatus: () => getGoogleCalendarOAuthStatus(),
    openGoogleCalendarOAuthStart: () => openGoogleCalendarOAuthStart(),
    getMicrosoftCalendarOAuthStatus: () => getMicrosoftCalendarOAuthStatus(),
    openMicrosoftCalendarOAuthStart: () => openMicrosoftCalendarOAuthStart(),
    getKeepRunningInBackground: () => getKeepRunningInBackground(),
    setKeepRunningInBackground: (next: boolean) => setKeepRunningInBackground(next),
    getDigestConfig: () => getDigestConfig(),
    updateDigestConfig: (cadence: string) => updateDigestConfig(cadence),
    getServerHealth: () => getServerHealth(),
    listPlugins: () => listPlugins(),
    checkPluginHealth: (id: string) => checkPluginHealth(id),
    setStreamingTranscriber: (req: { url: string; token: string }) => setStreamingTranscriber(req),
    clearStreamingTranscriber: () => clearStreamingTranscriber(),
  },
}))

import { ServerHealthBadge, SettingsScreen } from './SettingsScreen'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.useRealTimers()
  localStorage.clear()
})

  beforeEach(() => {
    getReadyz.mockResolvedValue(null)
    getGoogleCalendarOAuthStatus.mockResolvedValue({ configured: false })
    getMicrosoftCalendarOAuthStatus.mockResolvedValue({ configured: false })
    getKeepRunningInBackground.mockResolvedValue(false)
    getDigestConfig.mockResolvedValue({ owner_id: 'owner-1', cadence: 'off' })
    updateDigestConfig.mockResolvedValue({ owner_id: 'owner-1', cadence: 'off' })
    getServerHealth.mockResolvedValue({ reachable: true, authenticated: true })
    listPlugins.mockResolvedValue([])
    checkPluginHealth.mockResolvedValue({ healthy: true })
    setStreamingTranscriber.mockImplementation(async ({ url }: { url: string }) => ({ id: 'stream-1', kind: 'streaming-transcriber', name: 'Streaming transcriber', endpoint_url: url, enabled: true, is_default: true }))
    clearStreamingTranscriber.mockResolvedValue(undefined)
  })

function renderScreen(serverUrl = 'http://localhost:8080') {
  const onDisconnected = vi.fn()
  const onResetToBuiltIn = vi.fn()
  getConfig.mockResolvedValue({ serverUrl })
  render(
    <ToastProvider>
      <MemoryRouter>
        <SettingsScreen onDisconnected={onDisconnected} onResetToBuiltIn={onResetToBuiltIn} />
      </MemoryRouter>
    </ToastProvider>,
  )
  return { onDisconnected, onResetToBuiltIn }
}

describe('SettingsScreen', () => {
  const transcriber = { id: 'transcriber-1', kind: 'transcriber', name: 'Whisper', endpoint_url: 'http://whisper', enabled: true, is_default: true }
  const streaming = { id: 'stream-1', kind: 'streaming-transcriber', name: 'Streaming', endpoint_url: 'http://stream', enabled: true, is_default: true }

  it('shows a healthy current transcriber', async () => {
    listPlugins.mockResolvedValue([transcriber])
    renderScreen()
    expect(await screen.findByText('Connection healthy.')).toBeInTheDocument()
    expect(screen.getByText('Whisper · http://whisper')).toBeInTheDocument()
  })

  it('shows when the current transcriber is missing', async () => {
    renderScreen()
    expect(await screen.findByText('No transcriber configured.')).toBeInTheDocument()
  })

  it('distinguishes a misconfigured current transcriber HTTP response', async () => {
    listPlugins.mockResolvedValue([transcriber])
    checkPluginHealth.mockResolvedValue({ healthy: false, error: 'plugin returned 500: broken' })
    renderScreen()
    expect(await screen.findByText('Plugin is misconfigured (HTTP 500).')).toBeInTheDocument()
  })

  it('names the endpoint when the current transcriber is unreachable', async () => {
    listPlugins.mockResolvedValue([transcriber])
    checkPluginHealth.mockResolvedValue({ healthy: false, error: 'connect: connection refused' })
    renderScreen()
    expect(await screen.findByText('Plugin is unreachable at http://whisper.')).toBeInTheDocument()
  })

  it.each([
    ['creates', [], 'http://new-stream', 'new-token'],
    ['updates', [streaming], 'http://updated-stream', ''],
  ])('%s a streaming transcriber from the form', async (_label, plugins, url, token) => {
    const user = userEvent.setup()
    listPlugins.mockResolvedValue(plugins)
    renderScreen()
    const urlInput = await screen.findByLabelText('URL')
    await user.clear(urlInput)
    await user.type(urlInput, url)
    if (token) await user.type(screen.getByLabelText('Token'), token)
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(setStreamingTranscriber).toHaveBeenCalledWith({ url, token }))
    expect(screen.getByLabelText('Token')).toHaveValue('')
  })

  it('prefills only the streaming URL and clears the saved configuration', async () => {
    const user = userEvent.setup()
    listPlugins.mockResolvedValue([streaming])
    renderScreen()
    expect(await screen.findByLabelText('URL')).toHaveValue('http://stream')
    expect(screen.getByLabelText('Token')).toHaveValue('')
    await user.click(screen.getByRole('button', { name: 'Clear' }))
    await waitFor(() => expect(clearStreamingTranscriber).toHaveBeenCalled())
    expect(screen.getByText('No streaming transcriber configured.')).toBeInTheDocument()
  })

  it.each([
    [{ healthy: true }, 'Connection healthy.'],
    [{ healthy: false, error: 'plugin returned 401: unauthorized' }, 'Authentication failed: check the plugin token.'],
    [{ healthy: false, error: 'plugin returned 502: bad gateway' }, 'Plugin is misconfigured (HTTP 502).'],
    [{ healthy: false, error: 'dial tcp: connection refused' }, 'Plugin is unreachable at http://stream.'],
  ])('renders the distinct streaming connection result %#', async (health, copy) => {
    const user = userEvent.setup()
    listPlugins.mockResolvedValue([streaming])
    checkPluginHealth.mockResolvedValue(health)
    renderScreen()
    await user.click(await screen.findByRole('button', { name: 'Test connection' }))
    expect(await screen.findByText(copy)).toBeInTheDocument()
  })

  it('reports the distinct no-streaming-configured test state', async () => {
    const user = userEvent.setup()
    renderScreen()
    await user.click(await screen.findByRole('button', { name: 'Test connection' }))
    expect(screen.getByText('No streaming transcriber configured.')).toBeInTheDocument()
    expect(checkPluginHealth).not.toHaveBeenCalled()
  })
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

  it('shows the AI / Transcription section with Ollama not detected and a download affordance', async () => {
    getReadyz.mockResolvedValue({ ollamaDetected: false })

    renderScreen()

    expect(
      await screen.findByText((_, element) => element?.tagName === 'P' && /ollama status: not detected/i.test(element.textContent ?? '')),
    ).toBeInTheDocument()
    expect(screen.getByText(/without ollama, summaries and search stay unavailable/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /download ollama/i })).toHaveAttribute('href', 'https://ollama.com/download')
  })

  it('shows the AI / Transcription section with Ollama detected', async () => {
    getReadyz.mockResolvedValue({ ollamaDetected: true })

    renderScreen()

    expect(
      await screen.findByText((_, element) => element?.tagName === 'P' && /ollama status: detected/i.test(element.textContent ?? '')),
    ).toBeInTheDocument()
    expect(screen.getByText(/summaries and search are available/i)).toBeInTheDocument()
  })

  it('renders the keep-running setting and auto-record dependency copy', async () => {
    renderScreen()

    expect(await screen.findByRole('checkbox', { name: /keep muesli running in the menu bar when the window is closed/i })).toBeInTheDocument()
    expect(screen.getByText(/auto-record only runs while muesli is still open/i)).toBeInTheDocument()
    expect(screen.getByText(/if menu bar running is off, it can only work during the current session before the window closes/i)).toBeInTheDocument()
  })

  it('loads and saves the keep-running setting through the bridge', async () => {
    const user = userEvent.setup()
    getKeepRunningInBackground.mockResolvedValue(false)

    renderScreen()

    const checkbox = await screen.findByRole('checkbox', { name: /keep muesli running in the menu bar when the window is closed/i })
    expect(checkbox).not.toBeChecked()
    await user.click(checkbox)

    await waitFor(() => expect(setKeepRunningInBackground).toHaveBeenCalledWith(true))
  })

  it('keeps polling for Ollama detection changes without a restart', async () => {
    vi.useFakeTimers()
    getReadyz.mockResolvedValueOnce({ ollamaDetected: false }).mockResolvedValueOnce({ ollamaDetected: true })

    renderScreen()

    await act(async () => {
      await Promise.resolve()
    })
    expect(
      screen.getByText((_, element) => element?.tagName === 'P' && /ollama status: not detected/i.test(element.textContent ?? '')),
    ).toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(1000)
      await Promise.resolve()
    })

    expect(
      screen.getByText((_, element) => element?.tagName === 'P' && /ollama status: detected/i.test(element.textContent ?? '')),
    ).toBeInTheDocument()
  })

  it('renders the AI / Transcription section when getReadyz returns null', async () => {
    getReadyz.mockResolvedValue(null)

    renderScreen()

    expect(
      await screen.findByText((_, element) => element?.tagName === 'P' && /ollama status: detection unavailable/i.test(element.textContent ?? '')),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /download ollama/i })).toBeInTheDocument()
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

  it('keeps the cadence control a real, keyboard-operable combobox associated with its Cadence label', async () => {
    const user = userEvent.setup()
    getDigestConfig.mockResolvedValue({ owner_id: 'owner-1', cadence: 'off' })

    renderScreen()

    const cadence = await screen.findByRole('combobox', { name: 'Cadence' })
    expect(screen.getByLabelText('Cadence')).toBe(cadence)
    expect(cadence.tagName).toBe('SELECT')

    cadence.focus()
    expect(cadence).toHaveFocus()

    await user.selectOptions(cadence, 'daily')
    await waitFor(() => expect(updateDigestConfig).toHaveBeenCalledWith('daily'))
  })

  it('reverts the digest cadence and shows an error when updating fails', async () => {
    const user = userEvent.setup()
    getDigestConfig.mockResolvedValue({ owner_id: 'owner-1', cadence: 'daily' })
    updateDigestConfig.mockRejectedValue(new Error('digest failed'))

    renderScreen()

    const cadence = await screen.findByRole('combobox', { name: 'Cadence' })
    expect(cadence).toHaveValue('daily')

    await user.selectOptions(cadence, 'weekly')

    await waitFor(() => expect(cadence).toHaveValue('daily'))
    expect(await screen.findByRole('alert')).toHaveTextContent('digest failed')
  })

  it('marks the active theme button with aria-pressed and updates on selection', async () => {
    const user = userEvent.setup()

    renderScreen()

    expect(screen.getByRole('button', { name: 'system' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'light' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'dark' })).toHaveAttribute('aria-pressed', 'false')

    await user.click(screen.getByRole('button', { name: 'dark' }))

    expect(screen.getByRole('button', { name: 'dark' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'system' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('shows Connected for an ok health response', async () => {
    getServerHealth.mockResolvedValue({ reachable: true, authenticated: true })

    renderScreen()

    expect(await screen.findByText('Connected')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Connected')
    expect(getServerHealth).toHaveBeenCalledTimes(1)
  })

  it('shows a version when health includes one', async () => {
    getServerHealth.mockResolvedValue({ reachable: true, authenticated: true, version: '1.2.3' })

    renderScreen('http://localhost:8080/')

    expect(await screen.findByText('Connected · v1.2.3')).toBeInTheDocument()
    expect(getServerHealth).toHaveBeenCalledTimes(1)
  })

  it('shows Unreachable when the health request rejects', async () => {
    getServerHealth.mockRejectedValue(new Error('offline'))

    renderScreen()

    expect(await screen.findByText('Unreachable')).toBeInTheDocument()
  })

  it('shows Unreachable for a non-ok health response', async () => {
    getServerHealth.mockResolvedValue({ reachable: false })

    renderScreen()

    expect(await screen.findByText('Unreachable')).toBeInTheDocument()
  })

  it('shows Sign in required when the server is reachable but auth is rejected', async () => {
    getServerHealth.mockResolvedValue({ reachable: true, authenticated: false })

    renderScreen()

    expect(await screen.findByText('Sign in required')).toBeInTheDocument()
    expect(screen.queryByText('Connected')).toBeNull()
  })

  it('skips health fetch and badge when no server URL is configured', async () => {
    renderScreen('')

    expect(await screen.findByText('Not connected')).toBeInTheDocument()
    await waitFor(() => expect(getServerHealth).not.toHaveBeenCalled())
    expect(screen.queryByText('Connected')).toBeNull()
    expect(screen.queryByText('Unreachable')).toBeNull()
  })

  it('checks health again when the server URL changes', async () => {
    getServerHealth.mockResolvedValue({ reachable: true, authenticated: true })

    const { rerender } = render(<ServerHealthBadge serverUrl="http://localhost:8080" />)

    expect(await screen.findByText('Connected')).toBeInTheDocument()
    expect(getServerHealth).toHaveBeenCalledTimes(1)

    rerender(<ServerHealthBadge serverUrl="http://localhost:9090/" />)

    await waitFor(() => expect(getServerHealth).toHaveBeenCalledTimes(2))
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

  it('keeps the auto-record control a real, keyboard-operable checkbox with an accessible name', async () => {
    renderScreen('')

    const toggle = await screen.findByRole('checkbox', { name: /auto-record/i })
    expect(toggle.tagName).toBe('INPUT')
    expect(toggle).toHaveAttribute('type', 'checkbox')
    expect(toggle).not.toBeChecked()

    toggle.focus()
    expect(toggle).toHaveFocus()

    fireEvent.click(toggle)
    expect(toggle).toBeChecked()
  })

  it('opens the built-in reset dialog and confirms the reset', async () => {
    const user = userEvent.setup()

    const { onResetToBuiltIn } = renderScreen()

    await user.click(await screen.findByRole('button', { name: "Use this device's built-in server" }))
    const dialog = await screen.findByRole('dialog', { name: "Switch to this device's built-in server?" })
    expect(dialog).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Switch to built-in server' }))
    await waitFor(() => expect(resetToBuiltIn).toHaveBeenCalledTimes(1))
    expect(onResetToBuiltIn).toHaveBeenCalledTimes(1)
  })

  it('does not reset to the built-in server when the dialog is cancelled', async () => {
    const user = userEvent.setup()

    renderScreen()

    await user.click(await screen.findByRole('button', { name: "Use this device's built-in server" }))
    const dialog = await screen.findByRole('dialog', { name: "Switch to this device's built-in server?" })
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(resetToBuiltIn).not.toHaveBeenCalled()
  })

  it('opens the disconnect dialog and confirms the disconnect', async () => {
    const user = userEvent.setup()

    const { onDisconnected } = renderScreen()

    await user.click(await screen.findByRole('button', { name: 'Disconnect' }))
    const dialog = await screen.findByRole('dialog', { name: 'Disconnect from this server?' })
    expect(dialog).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Disconnect' }))
    await waitFor(() => expect(disconnect).toHaveBeenCalledTimes(1))
    expect(onDisconnected).toHaveBeenCalledTimes(1)
  })

  it('does not disconnect when the dialog is cancelled', async () => {
    const user = userEvent.setup()

    renderScreen()

    await user.click(await screen.findByRole('button', { name: 'Disconnect' }))
    const dialog = await screen.findByRole('dialog', { name: 'Disconnect from this server?' })
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(disconnect).not.toHaveBeenCalled()
  })

  it('shows an error when disconnecting fails', async () => {
    const user = userEvent.setup()
    disconnect.mockRejectedValue(new Error('disconnect failed'))

    const { onDisconnected } = renderScreen()

    await user.click(await screen.findByRole('button', { name: 'Disconnect' }))
    const dialog = await screen.findByRole('dialog', { name: 'Disconnect from this server?' })
    await user.click(within(dialog).getByRole('button', { name: 'Disconnect' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('disconnect failed')
    expect(onDisconnected).not.toHaveBeenCalled()
  })

  it('shows an error when resetting to the built-in server fails', async () => {
    const user = userEvent.setup()
    resetToBuiltIn.mockRejectedValue(new Error('reset failed'))

    renderScreen()

    await user.click(await screen.findByRole('button', { name: "Use this device's built-in server" }))
    const dialog = await screen.findByRole('dialog', { name: "Switch to this device's built-in server?" })
    await user.click(within(dialog).getByRole('button', { name: 'Switch to built-in server' }))

    await waitFor(() => expect(resetToBuiltIn).toHaveBeenCalledTimes(1))
    expect(await screen.findByRole('alert')).toHaveTextContent('reset failed')
  })

  it('shows an error when Google Calendar connect fails', async () => {
    const user = userEvent.setup()
    getGoogleCalendarOAuthStatus.mockResolvedValue({ configured: true })
    openGoogleCalendarOAuthStart.mockRejectedValue(new Error('google connect failed'))

    renderScreen()

    await user.click(await screen.findByRole('button', { name: 'Connect Google Calendar' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('google connect failed')
  })

  it('shows an error when Microsoft Calendar connect fails', async () => {
    const user = userEvent.setup()
    getMicrosoftCalendarOAuthStatus.mockResolvedValue({ configured: true })
    openMicrosoftCalendarOAuthStart.mockRejectedValue(new Error('microsoft connect failed'))

    renderScreen()

    await user.click(await screen.findByRole('button', { name: 'Connect Microsoft Calendar' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('microsoft connect failed')
  })
})
