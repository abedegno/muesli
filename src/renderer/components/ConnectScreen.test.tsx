// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ConnectScreen } from './ConnectScreen'
import { INSECURE_CONNECTION_CODE } from '../../shared/url'

// Mock the preload bridge: ConnectScreen imports `muesli` from '../api'.
// We replace the module with a controllable connect mock.
import type { ConnectRequest } from '../../shared/ipc'

const connect = vi.fn<(req: ConnectRequest) => Promise<{ serverUrl: string }>>()
vi.mock('../api', () => ({
  muesli: { connect: (req: ConnectRequest) => connect(req) },
}))

afterEach(() => {
  cleanup()
  connect.mockReset()
})

describe('ConnectScreen', () => {
  it('submits server/email/password and calls onConnected', async () => {
    connect.mockResolvedValue({ serverUrl: 'http://x' })
    const onConnected = vi.fn()
    render(<ConnectScreen onConnected={onConnected} />)
    await userEvent.clear(screen.getByLabelText('Server URL'))
    await userEvent.type(screen.getByLabelText('Server URL'), 'http://localhost:8080')
    await userEvent.type(screen.getByLabelText('Email'), 'a@b.co')
    await userEvent.type(screen.getByLabelText('Password'), 'secret123')
    await userEvent.click(screen.getByRole('button', { name: /connect/i }))
    expect(connect).toHaveBeenCalledWith(
      expect.objectContaining({ serverUrl: 'http://localhost:8080', email: 'a@b.co', password: 'secret123' }),
    )
    expect(onConnected).toHaveBeenCalled()
  })

  it('shows the insecure-connection warning + checkbox and retries with allowInsecure', async () => {
    // First attempt is blocked by the guardrail; after ticking the box it succeeds.
    connect
      .mockRejectedValueOnce(new Error(INSECURE_CONNECTION_CODE))
      .mockResolvedValueOnce({ serverUrl: 'http://192.168.1.50:8080' })
    const onConnected = vi.fn()
    render(<ConnectScreen onConnected={onConnected} />)
    await userEvent.clear(screen.getByLabelText('Server URL'))
    await userEvent.type(screen.getByLabelText('Server URL'), 'http://192.168.1.50:8080')
    await userEvent.type(screen.getByLabelText('Email'), 'a@b.co')
    await userEvent.type(screen.getByLabelText('Password'), 'secret123')
    await userEvent.click(screen.getByRole('button', { name: /connect/i }))

    // A friendly warning is shown — not the raw error code.
    expect(screen.getByText(/sent in the clear/i)).toBeInTheDocument()
    expect(screen.queryByText(INSECURE_CONNECTION_CODE)).not.toBeInTheDocument()
    expect(onConnected).not.toHaveBeenCalled()

    // Tick the override and reconnect.
    await userEvent.click(screen.getByLabelText(/connect anyway/i))
    await userEvent.click(screen.getByRole('button', { name: /connect/i }))
    expect(connect).toHaveBeenLastCalledWith(expect.objectContaining({ allowInsecure: true }))
    expect(onConnected).toHaveBeenCalled()
  })
})
