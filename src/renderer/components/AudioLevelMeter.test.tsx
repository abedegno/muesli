// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AudioLevelMeter, SUSTAINED_SILENCE_MS } from './AudioLevelMeter'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('AudioLevelMeter', () => {
  it('renders the inactive state', () => {
    render(<AudioLevelMeter level={0.5} active={false} />)
    expect(screen.getByText('Inactive')).toBeVisible()
  })

  it('renders a visual level and visible state text', () => {
    render(<AudioLevelMeter level={0.5} />)
    expect(screen.getByRole('meter', { name: /microphone level/i })).toHaveAttribute(
      'aria-valuenow',
      '50',
    )
    expect(screen.getByText('Sound detected')).toBeVisible()
  })

  it('shows sustained silence only after the threshold', () => {
    vi.useFakeTimers()
    render(<AudioLevelMeter level={0} />)
    expect(screen.getByText('Listening…')).toBeVisible()

    act(() => vi.advanceTimersByTime(SUSTAINED_SILENCE_MS - 1))
    expect(screen.queryByText('No sound detected')).toBeNull()

    act(() => vi.advanceTimersByTime(1))
    expect(screen.getByText('No sound detected')).toBeVisible()
  })
})
