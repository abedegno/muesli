// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const getConfig = vi.fn()
const getOnboarded = vi.fn()
const authListeners: Array<(notice: { message: string }) => void> = []
const connect = vi.fn()

vi.mock('@/api', () => ({
  muesli: {
    getConfig: () => getConfig(),
    getOnboarded: () => getOnboarded(),
    onAuthInvalidated: (listener: (notice: { message: string }) => void) => {
      authListeners.push(listener)
      return () => {}
    },
    connect: (...args: Parameters<typeof connect>) => connect(...args),
  },
}))

vi.mock('./components/shell/AppLayout', () => ({
  AppLayout: () => <div>main app</div>,
}))

import { App } from './App'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('App', () => {
  it('switches to the reconnect screen when auth invalidation is signaled', async () => {
    getConfig.mockResolvedValue({ serverUrl: 'http://localhost:8080', token: 'app-token' })
    getOnboarded.mockResolvedValue(true)

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    )

    expect(await screen.findByText('main app')).toBeInTheDocument()
    await waitFor(() => expect(authListeners).toHaveLength(1))
    const listener = authListeners[0]
    expect(listener).toBeTypeOf('function')

    listener({ message: 'Your saved sign-in is no longer valid for this server. Sign in again to reconnect.' })

    expect(await screen.findByText('Connect to Muesli')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Your saved sign-in is no longer valid for this server. Sign in again to reconnect.')
    expect(screen.queryByText('Error invoking remote method')).toBeNull()
  })
})
