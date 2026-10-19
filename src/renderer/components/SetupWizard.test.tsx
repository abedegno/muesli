// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { MicStatus } from '../../main/micPermission'
import { SetupWizard, nextStep, type SetupWizardBridge } from './SetupWizard'

afterEach(cleanup)
beforeEach(() => {
  vi.restoreAllMocks()
})

function makeBridge(overrides: Partial<SetupWizardBridge> = {}): SetupWizardBridge {
  return {
    platform: 'darwin',
    systemAudioAvailable: vi.fn().mockResolvedValue(false),
    systemAudioStart: vi.fn().mockResolvedValue(null),
    systemAudioStop: vi.fn().mockResolvedValue(undefined),
    systemAudioOpenSettings: vi.fn().mockResolvedValue(undefined),
    micStatus: vi.fn().mockResolvedValue('granted'),
    micRequest: vi.fn().mockResolvedValue('granted'),
    micOpenSettings: vi.fn().mockResolvedValue(undefined),
    getReadyz: vi.fn().mockResolvedValue(null),
    getManualServer: vi.fn().mockResolvedValue(false),
    listPlugins: vi.fn().mockResolvedValue([]),
    selectTranscriptionPlugin: vi.fn().mockResolvedValue(undefined),
    saveTranscriptionService: vi.fn().mockResolvedValue({}),
    checkPluginHealth: vi.fn().mockResolvedValue({ healthy: true }),
    getLocalOllamaStatus: vi.fn().mockResolvedValue({ reachable: false, appInstalled: false }),
    downloadOllama: vi.fn().mockResolvedValue(undefined),
    openOllama: vi.fn().mockResolvedValue(undefined),
    finishOllamaSetup: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

const micStatuses: MicStatus[] = Object.keys({
  granted: true,
  denied: true,
  'not-determined': true,
  restricted: true,
  unknown: true,
} satisfies Record<MicStatus, true>) as MicStatus[]

async function continuePastTranscription() {
  await userEvent.click(await screen.findByRole('button', { name: /continue/i }))
  if (screen.queryByRole('heading', { name: 'Transcription' })) {
    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
  }
}

describe('nextStep', () => {
  it('advances through the wizard steps', () => {
    expect(nextStep('welcome')).toBe('microphone')
    expect(nextStep('microphone')).toBe('transcription')
    expect(nextStep('transcription')).toBe('ai')
    expect(nextStep('ai')).toBe('done')
    expect(nextStep('done')).toBe('done')
  })
})

describe('SetupWizard', () => {
  it('explains hosted processing without promising everything stays on the device', async () => {
    const bridge = makeBridge({ getManualServer: vi.fn().mockResolvedValue(true) })
    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    expect(screen.queryByText(/all on your device/i)).not.toBeInTheDocument()
    expect(
      await screen.findByText(/recordings and notes are processed by your connected server/i)
    ).toBeInTheDocument()
  })

  it('keeps local installation advice hidden until the connection mode is known', async () => {
    const bridge = makeBridge({
      getManualServer: vi.fn().mockReturnValue(new Promise<boolean>(() => {})),
    })
    render(<SetupWizard onDone={() => {}} bridge={bridge} />)
    await continuePastTranscription()
    await continuePastTranscription()

    expect(screen.getByRole('status')).toHaveTextContent(/checking your connection/i)
    expect(screen.queryByText(/install ollama/i)).not.toBeInTheDocument()
  })

  it('reports a failed readiness check without treating it as missing Ollama', async () => {
    const bridge = makeBridge({ getReadyz: vi.fn().mockResolvedValue(null) })
    render(<SetupWizard onDone={() => {}} bridge={bridge} />)
    await continuePastTranscription()
    await continuePastTranscription()

    expect(await screen.findByText(/could not check ai availability/i)).toBeInTheDocument()
    expect(screen.queryByText(/install ollama/i)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /re-check/i })).toBeEnabled()
  })

  it('recovers a failed connection-mode check without suggesting a local install to hosted users', async () => {
    const getManualServer = vi
      .fn()
      .mockRejectedValueOnce(new Error('unavailable'))
      .mockResolvedValue(true)
    render(<SetupWizard onDone={() => {}} bridge={makeBridge({ getManualServer })} />)
    await continuePastTranscription()
    await continuePastTranscription()

    expect(await screen.findByText(/could not check your connection/i)).toBeInTheDocument()
    expect(screen.queryByText(/install ollama/i)).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /re-check/i }))
    expect(await screen.findByText(/this server is managed remotely/i)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /visit ollama/i })).not.toBeInTheDocument()
  })

  it('waits for AI readiness without showing missing-service instructions', async () => {
    const getReadyz = vi.fn().mockReturnValue(new Promise<null>(() => {}))
    render(<SetupWizard onDone={() => {}} bridge={makeBridge({ getReadyz })} />)
    await continuePastTranscription()
    await continuePastTranscription()

    expect(await screen.findByRole('status')).toHaveTextContent(/checking ai availability/i)
    expect(screen.queryByText(/install ollama/i)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /re-check/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /skip/i })).toBeEnabled()
  })

  it('reports microphone request errors and lets the user retry', async () => {
    const micRequest = vi
      .fn()
      .mockRejectedValueOnce(new Error('permission service unavailable'))
      .mockResolvedValue('granted')
    const micStatus = vi
      .fn()
      .mockResolvedValueOnce('not-determined')
      .mockResolvedValueOnce('not-determined')
      .mockResolvedValue('granted')
    render(<SetupWizard onDone={() => {}} bridge={makeBridge({ micRequest, micStatus })} />)
    await continuePastTranscription()
    await userEvent.click(screen.getByRole('button', { name: /grant microphone/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      /could not request microphone access/i
    )
    await userEvent.click(screen.getByRole('button', { name: /grant microphone/i }))
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows setup progress and restores heading focus when going back', async () => {
    render(<SetupWizard onDone={() => {}} bridge={makeBridge()} />)
    expect(screen.getByLabelText('Setup progress')).toHaveTextContent('Welcome')
    await continuePastTranscription()
    expect(screen.getByRole('heading', { name: 'Microphone access' })).toHaveFocus()
    await userEvent.click(screen.getByRole('button', { name: /back/i }))
    expect(screen.getByRole('heading', { name: /let us set up the basics/i })).toHaveFocus()
  })

  it.each(micStatuses)(
    'enables Continue on the microphone step when status is %s',
    async (micStatus) => {
      const bridge = makeBridge({
        micStatus: vi.fn().mockResolvedValue(micStatus),
      })

      render(<SetupWizard onDone={() => {}} bridge={bridge} />)

      await continuePastTranscription()
      await waitFor(() => expect(bridge.micStatus).toHaveBeenCalledOnce())
      expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled()
    }
  )

  it('enables Continue while microphone status is unresolved', async () => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockReturnValue(new Promise<MicStatus>(() => {})),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    await waitFor(() => expect(bridge.micStatus).toHaveBeenCalledOnce())
    expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled()
  })

  it('shows the granted microphone state and does not request permission', async () => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    expect(bridge.micRequest).not.toHaveBeenCalled()
  })

  it('requests microphone permission from the not-determined state and continues after re-check', async () => {
    const micStatus = vi
      .fn()
      .mockResolvedValueOnce('not-determined')
      .mockResolvedValueOnce('granted')
    const micRequest = vi.fn().mockResolvedValue('granted')
    const bridge = makeBridge({ micStatus, micRequest })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    await userEvent.click(await screen.findByRole('button', { name: /grant microphone/i }))
    expect(micRequest).toHaveBeenCalledOnce()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())

    await continuePastTranscription()
    expect(await screen.findByText(/optional ai features/i)).toBeInTheDocument()
  })

  it('opens system settings from the denied state and re-checks permission', async () => {
    const micStatus = vi.fn().mockResolvedValueOnce('denied').mockResolvedValueOnce('granted')
    const micOpenSettings = vi.fn().mockResolvedValue(undefined)
    const bridge = makeBridge({ micStatus, micOpenSettings })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    await userEvent.click(await screen.findByRole('button', { name: /open system settings/i }))
    expect(micOpenSettings).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: /re-check/i }))
    await waitFor(() => expect(micStatus).toHaveBeenCalledTimes(2))
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
  })

  it('reports Ollama detection without claiming a model is ready', async () => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: true }),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await continuePastTranscription()
    expect(await screen.findByText(/ollama is detected/i)).toBeInTheDocument()
    expect(screen.queryByText(/ai summaries & semantic search are ready/i)).not.toBeInTheDocument()
  })

  it('shows hosted-mode AI copy without the Ollama install link', async () => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
      getManualServer: vi.fn().mockResolvedValue(true),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: false }),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await continuePastTranscription()
    expect(
      await screen.findByText(
        /this server is managed remotely\. ask your administrator to configure the default ai agent/i
      )
    ).toBeInTheDocument()
    expect(screen.queryByText(/install ollama/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /visit ollama\.com/i })).not.toBeInTheDocument()
  })

  it('re-checks the live Ollama service rather than the cached startup result', async () => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: false }),
      getLocalOllamaStatus: vi
        .fn()
        .mockResolvedValueOnce({ reachable: false, appInstalled: false })
        .mockResolvedValue({ reachable: true, appInstalled: true }),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await continuePastTranscription()
    expect(await screen.findByText(/install ollama to add local ai/i)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /re-check/i }))
    await waitFor(() => expect(bridge.getLocalOllamaStatus).toHaveBeenCalledTimes(2))
    expect(bridge.getReadyz).toHaveBeenCalledOnce()
    expect(await screen.findByText(/ollama is running/i)).toBeInTheDocument()
  })

  it('offers the native Mac download without a Homebrew command', async () => {
    const bridge = makeBridge({
      platform: 'darwin',
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: false }),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await continuePastTranscription()
    await userEvent.click(await screen.findByRole('button', { name: 'Download Ollama for Mac' }))
    expect(bridge.downloadOllama).toHaveBeenCalledOnce()
    expect(document.querySelector('code')).toBeNull()
  })

  it('shows a curl install hint on linux', async () => {
    const bridge = makeBridge({
      platform: 'linux',
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: false }),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await continuePastTranscription()
    expect(
      await screen.findByText(/curl -fssl https:\/\/ollama\.com\/install\.sh/i)
    ).toBeInTheDocument()
  })

  it('shows no shell command on win32', async () => {
    const bridge = makeBridge({
      platform: 'win32',
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: false }),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await continuePastTranscription()
    expect(screen.queryByText(/brew install ollama/i)).not.toBeInTheDocument()
    expect(
      screen.queryByText(/curl -fssl https:\/\/ollama\.com\/install\.sh/i)
    ).not.toBeInTheDocument()
    expect(document.querySelector('code')).toBeNull()
    expect(screen.getByRole('button', { name: 'Download Ollama' })).toBeInTheDocument()
  })

  it('calls onDone when the wizard reaches the done step', async () => {
    const onDone = vi.fn()
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: false }),
    })

    render(<SetupWizard onDone={onDone} bridge={bridge} />)

    await continuePastTranscription()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await continuePastTranscription()
    await userEvent.click(await screen.findByRole('button', { name: /skip/i }))
    await waitFor(() => expect(onDone).toHaveBeenCalledOnce())
  })
})

it('offers transcription before AI and waits for a selection to save before continuing', async () => {
  let finishSave!: () => void
  const bridge = makeBridge({
    listPlugins: vi.fn().mockResolvedValue([
      {
        id: 'batch',
        kind: 'transcriber',
        name: 'Alternative',
        endpoint_url: 'http://other',
        enabled: true,
        is_default: true,
      },
      {
        id: 'live',
        kind: 'streaming-transcriber',
        name: 'Live',
        endpoint_url: 'http://live',
        enabled: true,
        is_default: true,
      },
    ]),
    selectTranscriptionPlugin: vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finishSave = resolve
        })
    ),
  })
  render(<SetupWizard onDone={() => {}} bridge={bridge} />)
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
  expect(screen.getByRole('heading', { name: 'Transcription' })).toBeInTheDocument()
  await userEvent.selectOptions(await screen.findByLabelText('Live transcript'), '')
  expect(bridge.selectTranscriptionPlugin).toHaveBeenCalledWith({
    kind: 'streaming-transcriber',
    pluginId: null,
  })
  expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled()
  finishSave()
  await waitFor(() => expect(screen.getByRole('button', { name: 'Continue' })).toBeEnabled())
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
  expect(screen.getByRole('heading', { name: 'Optional AI features' })).toBeInTheDocument()
})
