// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ServiceReadiness } from './ServiceReadiness'
import { CalendarReadiness } from './CalendarReadiness'
import { RecoveryInbox } from './RecoveryInbox'
import { LanguageSettings } from './LanguageSettings'
import { TranscriptionModels } from './TranscriptionModels'
import { useLocale } from '@/i18n'
const api = vi.hoisted(() => ({
  getCapabilities: vi.fn(),
  getServiceReadiness: vi.fn(),
  retryAIModels: vi.fn(),
  getCalendarReadiness: vi.fn(),
  listRecordingRecoveries: vi.fn(),
  getFull: vi.fn(),
  retryRecordingUpload: vi.fn(),
  exportRecordingRecovery: vi.fn(),
  discardRecordingRecovery: vi.fn(),
  setAppLanguage: vi.fn(),
}))
vi.mock('@/api', () => ({ muesli: api }))
beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
})
afterEach(cleanup)
it('reports connected server services and offers model retry only to administrators', async () => {
  api.getCapabilities.mockResolvedValue({ isAdmin: true })
  api.retryAIModels.mockResolvedValue(undefined)
  api.getServiceReadiness.mockResolvedValue({
    services: [
      { id: 'agent', configured: true, healthy: false },
      { id: 'transcriber', configured: true, healthy: true },
    ],
    managed_models: true,
    models: [{ name: 'summary-model', status: 'failed', percent: 0 }],
  })
  render(<ServiceReadiness />)
  expect(await screen.findByText('Connection needs attention')).toBeVisible()
  expect(screen.getByText('Connection available')).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Retry AI model downloads' }))
  await waitFor(() => expect(api.retryAIModels).toHaveBeenCalledOnce())
  await waitFor(() => expect(api.getServiceReadiness).toHaveBeenCalledTimes(2))
})
it('keeps model administration out of the member interface and makes a failed check retryable', async () => {
  api.getCapabilities.mockResolvedValue({ isAdmin: false })
  api.getServiceReadiness.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({
    services: [],
    managed_models: true,
    models: [{ name: 'summary', status: 'failed', percent: 0 }],
  })
  render(<ServiceReadiness />)
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not check the connected server')
  await userEvent.click(screen.getByRole('button', { name: 'Re-check services' }))
  expect(await screen.findByText(/Download failed/)).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Retry AI model downloads' })).not.toBeInTheDocument()
})
it('distinguishes unconfigured team calendars, completed sync and a failed check', async () => {
  api.getCalendarReadiness
    .mockResolvedValueOnce({
      connections: 0,
      failures: 0,
      googleConfigured: false,
      microsoftConfigured: false,
    })
    .mockResolvedValueOnce({ connections: 1, failures: 0, lastSyncedAt: '2026-10-19T10:00:00Z' })
    .mockRejectedValueOnce(new Error('offline'))
  render(<CalendarReadiness hosted />)
  expect(await screen.findByText(/not configured on this team server/)).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Re-check calendar' }))
  expect(await screen.findByText('1 calendar connection available.')).toBeVisible()
  expect(screen.getByText(/Last synced:/)).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Re-check calendar' }))
  expect(await screen.findByText(/Could not check calendar setup/)).toBeVisible()
})
it('retains recoverable audio after a failed retry, exports it and requires confirmation to discard', async () => {
  api.listRecordingRecoveries.mockResolvedValue([
    {
      noteId: 'saved',
      mimeType: 'audio/wav',
      byteLength: 32044,
      createdAt: '2026-10-19T10:00:00Z',
    },
  ])
  api.getFull.mockRejectedValue(new Error('note removed'))
  api.retryRecordingUpload.mockRejectedValue(new Error('Upload unavailable; audio kept.'))
  api.exportRecordingRecovery.mockResolvedValue({ success: true })
  api.discardRecordingRecovery.mockResolvedValue(undefined)
  render(
    <MemoryRouter>
      <RecoveryInbox />
    </MemoryRouter>
  )
  await userEvent.click(await screen.findByRole('button', { name: 'Review recordings' }))
  await userEvent.click(screen.getByRole('button', { name: 'Retry upload' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('audio kept')
  await userEvent.click(screen.getByRole('button', { name: 'Save audio copy' }))
  await waitFor(() => expect(api.exportRecordingRecovery).toHaveBeenCalledWith('saved'))
  await userEvent.click(screen.getByRole('button', { name: 'Discard…' }))
  expect(api.discardRecordingRecovery).not.toHaveBeenCalled()
  const dialog = screen.getByRole('dialog', { name: 'Discard saved audio?' })
  await userEvent.click(within(dialog).getByRole('button', { name: 'Discard audio' }))
  await waitFor(() => expect(api.discardRecordingRecovery).toHaveBeenCalledWith('saved'))
  await waitFor(() =>
    expect(screen.queryByRole('button', { name: 'Review recordings' })).not.toBeInTheDocument()
  )
})
it('switches translated controls live and persists display and region independently', async () => {
  api.setAppLanguage.mockResolvedValue(undefined)
  function View() {
    useLocale()
    return <LanguageSettings />
  }
  render(<View />)
  await userEvent.selectOptions(screen.getByLabelText('Display language', { exact: true }), 'fr')
  expect(await screen.findByText('Langue et région')).toBeVisible()
  expect(api.setAppLanguage).toHaveBeenCalledWith('fr')
  await userEvent.selectOptions(screen.getByLabelText('Format régional', { exact: true }), 'en-GB')
  expect(localStorage.getItem('muesli-language')).toBe('fr')
  expect(localStorage.getItem('muesli-region')).toBe('en-GB')
})
it('downloads explicitly before asking to restart with a model and chosen spoken language', async () => {
  const models = [
    {
      id: 'tiny.en',
      name: 'English',
      installed: true,
      multilingual: false,
      bytes: 0,
      memoryMB: 273,
    },
    {
      id: 'tiny',
      name: 'Tiny',
      installed: false,
      multilingual: true,
      bytes: 77_000_000,
      memoryMB: 273,
    },
  ]
  const bridge = {
    getTranscriptionModels: vi
      .fn()
      .mockResolvedValue({ models, selected: 'tiny.en', language: 'en', download: null }),
    installTranscriptionModel: vi.fn().mockImplementation(async () => {
      models[1].installed = true
    }),
    cancelTranscriptionModelDownload: vi.fn(),
    activateTranscriptionModel: vi
      .fn()
      .mockRejectedValue(new Error('Stop and save your recording before changing models.')),
  }
  render(<TranscriptionModels bridge={bridge} />)
  await screen.findByText('Language and local transcription model')
  await userEvent.selectOptions(screen.getByLabelText('Local model'), 'tiny')
  await userEvent.selectOptions(screen.getByLabelText('Spoken language'), 'fr')
  await userEvent.click(screen.getByRole('button', { name: 'Download selected model' }))
  await waitFor(() => expect(bridge.installTranscriptionModel).toHaveBeenCalledWith('tiny'))
  await userEvent.click(await screen.findByRole('button', { name: 'Use model and restart…' }))
  expect(bridge.activateTranscriptionModel).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: 'Restart Muesli' }))
  await waitFor(() => expect(bridge.activateTranscriptionModel).toHaveBeenCalledWith('tiny', 'fr'))
  expect(await screen.findByRole('alert')).toHaveTextContent('Stop and save')
})
it('shows model progress without presenting optional downloads as a recording prerequisite', async () => {
  api.getCapabilities.mockResolvedValue({ isAdmin: true })
  api.getServiceReadiness.mockResolvedValue({
    services: [{ id: 'streaming-transcriber', configured: false, healthy: false }],
    models: [{ name: 'summary', status: 'downloading', percent: 42 }],
    managed_models: true,
  })
  const view = render(<ServiceReadiness />)
  expect(await screen.findByRole('progressbar', { name: 'summary download' })).toHaveAttribute(
    'value',
    '42'
  )
  expect(screen.getByText('Not configured')).toBeVisible()
  expect(screen.getByText(/does not prevent recording/)).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Retry AI model downloads' })).not.toBeInTheDocument()
  view.unmount()
})
