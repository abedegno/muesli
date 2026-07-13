// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { EmbeddedStartupGate } from './StartupScreen'

vi.mock('@/api', () => ({
  muesli: {},
}))

afterEach(() => {
  cleanup()
})

describe('EmbeddedStartupGate without startup bridge', () => {
  it('renders children unchanged', () => {
    render(
      <EmbeddedStartupGate>
        <div>App body</div>
      </EmbeddedStartupGate>,
    )

    expect(screen.getByText('App body')).toBeInTheDocument()
  })
})
