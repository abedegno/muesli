// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SetupWizard, nextStep, type SetupWizardBridge } from './SetupWizard'

afterEach(cleanup)
beforeEach(() => {
  vi.restoreAllMocks()
})

function makeBridge(overrides: Partial<SetupWizardBridge> = {}): SetupWizardBridge {
  return {
    micStatus: vi.fn().mockResolvedValue('granted'),
    micRequest: vi.fn().mockResolvedValue('granted'),
    micOpenSettings: vi.fn().mockResolvedValue(undefined),
    getReadyz: vi.fn().mockResolvedValue(null),
    ...overrides,
  } as SetupWizardBridge
}

describe('nextStep', () => {
  it('advances through the wizard steps', () => {
    expect(nextStep('welcome')).toBe('microphone')
    expect(nextStep('microphone')).toBe('ai')
    expect(nextStep('ai')).toBe('done')
    expect(nextStep('done')).toBe('done')
  })
})

describe('SetupWizard', () => {
  it('shows the granted microphone state and does not request permission', async () => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    expect(bridge.micRequest).not.toHaveBeenCalled()
  })

  it('requests microphone permission from the not-determined state and continues after re-check', async () => {
    const micStatus = vi.fn().mockResolvedValueOnce('not-determined').mockResolvedValueOnce('granted')
    const micRequest = vi.fn().mockResolvedValue('granted')
    const bridge = makeBridge({ micStatus, micRequest })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    await userEvent.click(await screen.findByRole('button', { name: /grant microphone/i }))
    expect(micRequest).toHaveBeenCalledOnce()
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/optional ai features/i)).toBeInTheDocument()
  })

  it('opens system settings from the denied state and re-checks permission', async () => {
    const micStatus = vi.fn().mockResolvedValueOnce('denied').mockResolvedValueOnce('granted')
    const micOpenSettings = vi.fn().mockResolvedValue(undefined)
    const bridge = makeBridge({ micStatus, micOpenSettings })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    await userEvent.click(await screen.findByRole('button', { name: /open system settings/i }))
    expect(micOpenSettings).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: /re-check/i }))
    await waitFor(() => expect(micStatus).toHaveBeenCalledTimes(2))
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
  })

  it('shows the AI ready copy when readyz reports ollama is detected', async () => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: true }),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await userEvent.click(await screen.findByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/ai summaries & semantic search are ready/i)).toBeInTheDocument()
  })

  it.each([[{ ollamaDetected: false }], [null]])('shows the install prompt when readyz returns %s', async (readyz) => {
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue(readyz),
    })

    render(<SetupWizard onDone={() => {}} bridge={bridge} />)

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await userEvent.click(await screen.findByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/optional: install ollama/i)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /skip/i }))
    await waitFor(() => expect(screen.queryByText(/optional: install ollama/i)).toBeNull())
  })

  it('calls onDone when the wizard reaches the done step', async () => {
    const onDone = vi.fn()
    const bridge = makeBridge({
      micStatus: vi.fn().mockResolvedValue('granted'),
      getReadyz: vi.fn().mockResolvedValue({ ollamaDetected: false }),
    })

    render(<SetupWizard onDone={onDone} bridge={bridge} />)

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/microphone access granted/i)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled())
    await userEvent.click(await screen.findByRole('button', { name: /continue/i }))
    await userEvent.click(await screen.findByRole('button', { name: /skip/i }))
    await waitFor(() => expect(onDone).toHaveBeenCalledOnce())
  })
})
