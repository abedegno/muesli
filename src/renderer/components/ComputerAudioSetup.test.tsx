// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ComputerAudioSetup, type ComputerAudioBridge } from './ComputerAudioSetup'

afterEach(cleanup)
function fixture(): ComputerAudioBridge {
  return {
    systemAudioAvailable: vi.fn().mockResolvedValue(true),
    systemAudioStart: vi.fn().mockResolvedValue({ sampleRate: 48000, channels: 2 }),
    systemAudioStop: vi.fn().mockResolvedValue(undefined),
    systemAudioOpenSettings: vi.fn().mockResolvedValue(undefined),
  }
}
it('explains the separate permission and checks only after a user gesture, stopping the probe', async () => {
  const bridge = fixture()
  render(<ComputerAudioSetup bridge={bridge} />)
  const check = await screen.findByRole('button', { name: 'Enable computer audio' })
  expect(bridge.systemAudioStart).not.toHaveBeenCalled()
  expect(
    screen.getByText(/macOS grants this separately from microphone access/i)
  ).toBeInTheDocument()
  await userEvent.click(check)
  expect(await screen.findByRole('status')).toHaveTextContent('Computer audio is available')
  expect(bridge.systemAudioStop).toHaveBeenCalledTimes(1)
})
it('does not claim access when the helper is unavailable or permission is unconfirmed', async () => {
  const bridge = fixture()
  vi.mocked(bridge.systemAudioStart).mockResolvedValue(null)
  render(<ComputerAudioSetup bridge={bridge} />)
  await userEvent.click(await screen.findByRole('button', { name: 'Enable computer audio' }))
  expect(await screen.findByRole('status')).toHaveTextContent('could not be confirmed')
  await userEvent.click(screen.getByRole('button', { name: 'Open audio privacy settings' }))
  expect(bridge.systemAudioOpenSettings).toHaveBeenCalledTimes(1)
  expect(bridge.systemAudioStop).not.toHaveBeenCalled()
})
