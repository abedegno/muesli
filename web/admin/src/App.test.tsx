import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from './App'
import { SessionStore } from './auth/session'

function makeClient(overrides: Record<string, unknown>) {
  return {
    getSetupStatus: vi.fn(),
    setUserRole: vi.fn(),
    getCapabilities: vi.fn().mockResolvedValue({ is_admin: true }),
    setup: vi.fn(),
    login: vi.fn(),
    listPlugins: vi.fn().mockResolvedValue([]),
    createPlugin: vi.fn(),
    updatePlugin: vi.fn(),
    deletePlugin: vi.fn(),
    listJobs: vi.fn().mockResolvedValue([]),
    listBackups: vi.fn().mockResolvedValue([]),
    createBackup: vi.fn(),
    downloadBackup: vi.fn(),
    getAdminHealth: vi.fn().mockResolvedValue({
      server: { version: 'dev', commit: 'unknown', goVersion: 'unknown', status: 'warn' },
      plugins: [],
      jobs: { counts: { pending: 0, running: 0, done: 0, failed: 0, cancelled: 0 }, status: 'ok' },
      embedding: {
        enabled: false,
        model: '',
        dim: 768,
        minScore: 0,
        docPrefix: '',
        queryPrefix: '',
        done: 0,
        total: 0,
      },
      storage: { path: './data/audio', totalBytes: 0, freeBytes: 0 },
    }),
    ...overrides,
  }
}

describe('App boot routing', () => {
  beforeEach(() => localStorage.clear())

  it('shows the warming-up card while the status request is pending', () => {
    // getSetupStatus never resolves — stays in "loading" state
    const client = makeClient({ getSetupStatus: vi.fn().mockReturnValue(new Promise(() => {})) })
    render(<App client={client as never} session={new SessionStore()} />)
    expect(screen.getByRole('heading', { name: /muesli is starting up/i })).toBeInTheDocument()
    expect(screen.getByText(/this may take a moment on the first boot/i)).toBeInTheDocument()
  })

  it('shows the setup view when needs_setup is true', async () => {
    const client = makeClient({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: true }) })
    render(<App client={client as never} session={new SessionStore()} />)
    expect(await screen.findByText(/create your operator account/i)).toBeInTheDocument()
  })

  it('shows the login view when setup is done and no token is held', async () => {
    const client = makeClient({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) })
    render(<App client={client as never} session={new SessionStore()} />)
    expect(await screen.findByRole('heading', { name: /sign in/i })).toBeInTheDocument()
  })

  it('shows the console when a token is already held', async () => {
    const session = new SessionStore()
    session.setToken('tok')
    const client = makeClient({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) })
    render(<App client={client as never} session={session} />)
    expect(await screen.findByRole('button', { name: /^Plugins$/ })).toBeInTheDocument()
  })

  it('switches to the Health tab and loads the admin health panel', async () => {
    const session = new SessionStore()
    session.setToken('tok')
    const client = makeClient({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) })
    render(<App client={client as never} session={session} />)
    await userEvent.click(await screen.findByRole('button', { name: /^Health$/ }))
    expect(client.getAdminHealth).toHaveBeenCalled()
  })

  it('switches to the Backups tab and loads backups', async () => {
    const session = new SessionStore()
    session.setToken('tok')
    const client = makeClient({
      getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }),
      listBackups: vi.fn().mockResolvedValue([
        {
          filename: 'muesli-20240101120000.dump',
          size_bytes: 1024,
          created_at: '2024-01-01T12:00:00Z',
        },
      ]),
    })
    render(<App client={client as never} session={session} />)
    await userEvent.click(await screen.findByRole('button', { name: /^Backups$/ }))
    expect(await screen.findByText('muesli-20240101120000.dump')).toBeInTheDocument()
    expect(client.listBackups).toHaveBeenCalled()
  })
})

it('does not mount the administrative console for an ordinary member', async () => {
  const session = new SessionStore()
  session.setToken('member-token')
  const client = makeClient({ getCapabilities: vi.fn().mockResolvedValue({ is_admin: false }) })
  render(<App client={client as never} session={session} />)
  expect(
    await screen.findByRole('heading', { name: 'Administrator access required' })
  ).toBeInTheDocument()
  expect(client.listPlugins).not.toHaveBeenCalled()
})

it('distinguishes an access-check failure from an ordinary member and retries', async () => {
  const session = new SessionStore()
  session.setToken('admin-token')
  const client = makeClient({
    getCapabilities: vi
      .fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValue({ is_admin: true }),
  })
  render(<App client={client as never} session={session} />)
  expect(
    await screen.findByRole('heading', { name: 'Could not check your access' })
  ).toBeInTheDocument()
  expect(client.listPlugins).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
  expect(await screen.findByRole('button', { name: 'Plugins' })).toBeInTheDocument()
})
it('changes an existing account role and rechecks current authorization immediately', async () => {
  const session = new SessionStore()
  session.setToken('admin-token')
  const client = makeClient({
    setUserRole: vi.fn().mockResolvedValue(undefined),
    getCapabilities: vi
      .fn()
      .mockResolvedValueOnce({ is_admin: true })
      .mockResolvedValue({ is_admin: false }),
  })
  render(<App client={client as never} session={session} />)
  await userEvent.click(await screen.findByRole('button', { name: 'Account access' }))
  await userEvent.type(screen.getByLabelText('Existing account email'), 'self@example.com')
  await userEvent.click(screen.getByRole('button', { name: 'Save account access' }))
  expect(
    await screen.findByRole('heading', { name: 'Administrator access required' })
  ).toBeInTheDocument()
  expect(client.setUserRole).toHaveBeenCalledWith('self@example.com', 'member')
})
