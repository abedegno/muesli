// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LocalAiSetup, type LocalAiSetupBridge } from './LocalAiSetup'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function bridge(overrides: Partial<LocalAiSetupBridge> = {}): LocalAiSetupBridge {
  return {
    platform: 'darwin',
    getLocalOllamaStatus: vi.fn().mockResolvedValue({ reachable: false, appInstalled: false }),
    downloadOllama: vi.fn().mockResolvedValue(undefined),
    openOllama: vi.fn().mockResolvedValue(undefined),
    finishOllamaSetup: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

describe('local AI setup', () => {
  it('offers the Mac installer without terminal commands', async () => {
    const api = bridge()
    render(<LocalAiSetup bridge={api} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Download Ollama for Mac' }))
    expect(api.downloadOllama).toHaveBeenCalledOnce()
    expect(document.querySelector('code')).toBeNull()
    expect(screen.getByText(/drag Ollama to Applications/i)).toBeInTheDocument()
  })

  it('starts an installed Mac app and reports native launch errors', async () => {
    const api = bridge({
      getLocalOllamaStatus: vi.fn().mockResolvedValue({ reachable: false, appInstalled: true }),
      openOllama: vi.fn().mockRejectedValue(new Error('Ollama could not be opened')),
    })
    render(<LocalAiSetup bridge={api} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Open Ollama' }))
    expect(api.openOllama).toHaveBeenCalledOnce()
    expect(await screen.findByRole('alert')).toHaveTextContent('Ollama could not be opened')
  })

  it('detects Ollama automatically and only restarts when requested', async () => {
    vi.useFakeTimers()
    const api = bridge({
      getLocalOllamaStatus: vi
        .fn()
        .mockResolvedValueOnce({ reachable: false, appInstalled: false })
        .mockResolvedValue({ reachable: true, appInstalled: true }),
    })
    render(<LocalAiSetup bridge={api} />)
    await act(async () => {})
    expect(screen.queryByRole('button', { name: /restart and set up ai/i })).not.toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })
    expect(screen.getByText(/Ollama is running/i)).toBeInTheDocument()
    expect(api.finishOllamaSetup).not.toHaveBeenCalled()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /restart and set up ai/i }))
    })
    expect(api.finishOllamaSetup).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: /restarting/i })).toBeDisabled()
  })

  it('stops automatic probes when the step unmounts', async () => {
    vi.useFakeTimers()
    const api = bridge()
    const view = render(<LocalAiSetup bridge={api} />)
    await act(async () => {})
    view.unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    expect(api.getLocalOllamaStatus).toHaveBeenCalledOnce()
  })

  it('caps automatic checks and lets users retry after a failed probe', async () => {
    vi.useFakeTimers()
    const api = bridge({
      getLocalOllamaStatus: vi.fn().mockRejectedValue(new Error('unavailable')),
    })
    render(<LocalAiSetup bridge={api} />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_000)
    })
    expect(screen.getByText(/automatic checks paused/i)).toBeInTheDocument()
    const calls = vi.mocked(api.getLocalOllamaStatus).mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000)
    })
    expect(api.getLocalOllamaStatus).toHaveBeenCalledTimes(calls)
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Re-check Ollama' }))
    })
    expect(api.getLocalOllamaStatus).toHaveBeenCalledTimes(calls + 1)
  })
})
