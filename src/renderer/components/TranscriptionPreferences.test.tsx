// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TranscriptionPreferences, type TranscriptionBridge } from './TranscriptionPreferences'
import type { Plugin } from '../../shared/types'

afterEach(cleanup)
function fixture(hosted = false) {
  const plugins: Plugin[] = [
    {
      id: 'batch',
      kind: 'transcriber',
      name: 'Embedded whisper.cpp transcriber',
      endpoint_url: 'http://local:8001',
      is_default: true,
      enabled: true,
    },
    {
      id: 'live',
      kind: 'streaming-transcriber',
      name: 'Embedded whisper.cpp streaming transcriber',
      endpoint_url: 'http://local:8002',
      is_default: true,
      enabled: true,
    },
    {
      id: 'alternative',
      kind: 'transcriber',
      name: 'Parakeet',
      endpoint_url: 'http://parakeet:9000',
      is_default: false,
      enabled: true,
    },
  ]
  const bridge: TranscriptionBridge = {
    getManualServer: vi.fn().mockResolvedValue(hosted),
    getCapabilities: vi.fn().mockResolvedValue({ agentConfigured: false, isAdmin: true }),
    listPlugins: vi.fn(async () => [...plugins]),
    selectTranscriptionPlugin: vi.fn(async ({ kind, pluginId }) => {
      plugins
        .filter((p) => p.kind === kind)
        .forEach((p) => {
          p.is_default = pluginId === null ? p.is_default : p.id === pluginId
          if (p.is_default) p.enabled = pluginId !== null
        })
    }),
    saveTranscriptionService: vi.fn().mockResolvedValue(plugins[0]),
    checkPluginHealth: vi.fn().mockResolvedValue({ healthy: true }),
  }
  return { bridge, plugins }
}

describe('transcription preferences', () => {
  it('shows the default batch and live services with advanced credentials hidden', async () => {
    const { bridge } = fixture()
    render(<TranscriptionPreferences bridge={bridge} />)
    expect(await screen.findByLabelText('Final transcript')).toHaveValue('batch')
    expect(screen.getByLabelText('Live transcript')).toHaveValue('live')
    expect(screen.getByText(/live transcription on by default/i)).toBeInTheDocument()
    expect(screen.getByLabelText('Access token')).not.toBeVisible()
  })
  it('saves alternative final and off/live selections and loads them after remount', async () => {
    const { bridge } = fixture()
    const view = render(<TranscriptionPreferences bridge={bridge} />)
    await userEvent.selectOptions(await screen.findByLabelText('Final transcript'), 'alternative')
    await waitFor(() =>
      expect(screen.getByLabelText('Final transcript')).toHaveValue('alternative')
    )
    await userEvent.selectOptions(screen.getByLabelText('Live transcript'), '')
    await waitFor(() => expect(screen.getByLabelText('Live transcript')).toHaveValue(''))
    view.unmount()
    render(<TranscriptionPreferences bridge={bridge} />)
    expect(await screen.findByLabelText('Final transcript')).toHaveValue('alternative')
    expect(screen.getByLabelText('Live transcript')).toHaveValue('')
  })
  it('retains the old choice and reports save failures', async () => {
    const { bridge } = fixture()
    vi.mocked(bridge.selectTranscriptionPlugin).mockRejectedValue(new Error('offline'))
    render(<TranscriptionPreferences bridge={bridge} />)
    await userEvent.selectOptions(await screen.findByLabelText('Final transcript'), 'alternative')
    expect(await screen.findByRole('alert')).toHaveTextContent(/could not save your choice/i)
    expect(screen.getByLabelText('Final transcript')).toHaveValue('batch')
  })
  it('explains hosted choices affect everyone without claiming bundled services', async () => {
    const { bridge } = fixture(true)
    render(<TranscriptionPreferences bridge={bridge} />)
    expect(await screen.findByText(/affect everyone using this server/i)).toBeInTheDocument()
    expect(screen.queryByText(/live transcription on by default/i)).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Whisper on the server' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Whisper on this device' })).not.toBeInTheDocument()
  })
  it('keeps missing/failed configuration visible without fabricating defaults', async () => {
    const { bridge } = fixture()
    vi.mocked(bridge.listPlugins).mockRejectedValue(new Error('offline'))
    render(<TranscriptionPreferences bridge={bridge} />)
    expect(await screen.findByRole('alert')).toHaveTextContent(
      /could not load transcription services/i
    )
    expect(screen.getByLabelText('Final transcript')).toHaveValue('')
    expect(screen.getByLabelText('Final transcript')).toBeDisabled()
  })
})

it.each([
  [{ healthy: false, error: 'plugin returned 502: bad gateway' }, /HTTP 502/],
  [
    { healthy: false, error: 'dial tcp: connection refused' },
    /Could not reach http:\/\/local:8001/,
  ],
  [{ healthy: true }, /connection healthy/i],
  [{ healthy: false, error: 'plugin returned 401: unauthorized' }, /authentication failed/i],
])('tests the selected service connection %#', async (health, message) => {
  const { bridge } = fixture()
  vi.mocked(bridge.checkPluginHealth).mockResolvedValue(health)
  render(<TranscriptionPreferences bridge={bridge} />)
  await userEvent.click(
    await screen.findByRole('button', { name: 'Test connection — Final transcript' })
  )
  expect(bridge.checkPluginHealth).toHaveBeenCalledWith('batch')
  expect(await screen.findByText(message)).toBeInTheDocument()
})

it('reports a refresh failure after saving without claiming the selection was confirmed', async () => {
  const { bridge } = fixture()
  render(<TranscriptionPreferences bridge={bridge} />)
  const select = await screen.findByLabelText('Final transcript')
  vi.mocked(bridge.listPlugins).mockRejectedValue(new Error('offline'))
  await userEvent.selectOptions(select, 'alternative')
  expect(await screen.findByRole('alert')).toHaveTextContent(
    /could not load transcription services/i
  )
  expect(screen.queryByText('Saved')).not.toBeInTheDocument()
})

it('shows checking and the result beside the service that was tested', async () => {
  const { bridge } = fixture()
  let finish!: (result: { healthy: boolean }) => void
  vi.mocked(bridge.checkPluginHealth).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve
    })
  )
  render(<TranscriptionPreferences bridge={bridge} />)
  await userEvent.click(
    await screen.findByRole('button', { name: 'Test connection — Final transcript' })
  )
  expect(screen.getByRole('button', { name: /Checking.*Final transcript/i })).toBeDisabled()
  finish({ healthy: true })
  const result = await screen.findByTestId('transcription-transcriber-health')
  expect(result).toHaveTextContent(/connection healthy/i)
  expect(result.closest('div')?.querySelector('#transcription-transcriber')).toBeInTheDocument()
})

it('makes hosted shared choices read-only for a member', async () => {
  const { bridge } = fixture(true)
  vi.mocked(bridge.getCapabilities!).mockResolvedValue({ agentConfigured: false, isAdmin: false })
  render(<TranscriptionPreferences bridge={bridge} />)
  expect(await screen.findByLabelText('Final transcript')).toBeDisabled()
  expect(screen.getByLabelText('Live transcript')).toBeDisabled()
  expect(screen.queryByRole('button', { name: /Test connection/ })).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Access token')).not.toBeInTheDocument()
  expect(screen.getByText(/administrator manages these shared settings/)).toBeInTheDocument()
})
