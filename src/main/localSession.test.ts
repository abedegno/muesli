import { describe, expect, it, vi } from 'vitest'
import { ensureLocalSession } from './localSession'

describe('ensureLocalSession', () => {
  it('waits for readiness before trusting an existing token', async () => {
    const isReady = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(false).mockResolvedValue(true)
    const sleep = vi.fn(async () => {})
    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: 'http://localhost:8080',
      tokenStore: { load: () => ({ serverUrl: 'http://localhost:8080', token: 'saved' }), save: vi.fn() },
      secretStore: { loadCreds: () => null, saveCreds: vi.fn(), getManualServer: () => false },
      makeClient: vi.fn(),
      generatePassword: () => 'unused',
      isReady,
      sleep,
      readinessDelaysMs: [0, 10, 20],
    })

    expect(out).toBe('connected')
    expect(isReady).toHaveBeenCalledTimes(3)
    expect(sleep).toHaveBeenCalledTimes(2)
  })

  it('returns server-unreachable without clearing an existing token when readiness stays false', async () => {
    const token = { serverUrl: 'http://localhost:8080', token: 'saved' }
    const tokenStore = { load: vi.fn(() => token), save: vi.fn() }
    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: token.serverUrl,
      tokenStore,
      secretStore: { loadCreds: () => null, saveCreds: vi.fn(), getManualServer: () => false },
      makeClient: vi.fn(),
      generatePassword: () => 'unused',
      isReady: async () => false,
      sleep: async () => {},
      readinessDelaysMs: [0, 10],
    })

    expect(out).toBe('server-unreachable')
    expect(tokenStore.load()).toEqual(token)
    expect(tokenStore.save).not.toHaveBeenCalled()
  })

  it('provisions a fresh embedded install', async () => {
    const tokenStore = {
      load: vi.fn(() => null),
      save: vi.fn(),
    }
    const secretStore = {
      loadCreds: vi.fn(() => null),
      saveCreds: vi.fn(),
      getManualServer: vi.fn(() => false),
    }
    const client = {
      setupNeeded: vi.fn(async () => true),
      setup: vi.fn(async () => ({ id: 'u1', email: 'desktop@localhost' })),
      login: vi.fn(async () => 'session-1'),
      createToken: vi.fn(async () => 'app-1'),
    }

    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: 'http://localhost:8080',
      tokenStore,
      secretStore,
      makeClient: () => client,
      generatePassword: () => 'pw-1',
    })

    expect(out).toBe('connected')
    expect(client.setup).toHaveBeenCalledWith('desktop@localhost', 'pw-1')
    expect(client.login).toHaveBeenCalledWith('desktop@localhost', 'pw-1')
    expect(client.createToken).toHaveBeenCalledWith('muesli-desktop', 'session-1')
    expect(secretStore.saveCreds).toHaveBeenCalledWith({ email: 'desktop@localhost', password: 'pw-1' })
    expect(tokenStore.save).toHaveBeenCalledWith({ serverUrl: 'http://localhost:8080', token: 'app-1' })
  })

  it('retries readiness before making the first fresh-install provisioning request', async () => {
    const callOrder: string[] = []
    const isReady = vi.fn(async () => {
      callOrder.push('readyz')
      if (isReady.mock.calls.length === 1) throw new TypeError('fetch failed')
      return true
    })
    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: 'http://localhost:8080',
      tokenStore: { load: () => null, save: vi.fn() },
      secretStore: { loadCreds: () => null, saveCreds: vi.fn(), getManualServer: () => false },
      makeClient: () => ({
        setupNeeded: vi.fn(async () => { callOrder.push('setupNeeded'); return true }),
        setup: vi.fn(async () => { callOrder.push('setup'); return { id: 'u1', email: 'desktop@localhost' } }),
        login: vi.fn(async () => { callOrder.push('login'); return 'session-1' }),
        createToken: vi.fn(async () => { callOrder.push('createToken'); return 'app-1' }),
      }),
      generatePassword: () => 'pw-1',
      isReady,
      sleep: async () => {},
      readinessDelaysMs: [0, 10],
    })

    expect(out).toBe('connected')
    expect(callOrder).toEqual(['readyz', 'readyz', 'setupNeeded', 'setup', 'login', 'createToken'])
  })

  it('skips when a token already exists', async () => {
    const tokenStore = {
      load: vi.fn(() => ({ serverUrl: 'http://localhost:8080', token: 'app-1' })),
      save: vi.fn(),
    }
    const secretStore = {
      loadCreds: vi.fn(),
      saveCreds: vi.fn(),
      getManualServer: vi.fn(() => false),
    }
    const client = {
      setupNeeded: vi.fn(),
      setup: vi.fn(),
      login: vi.fn(),
      createToken: vi.fn(),
    }

    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: 'http://localhost:8080',
      tokenStore,
      secretStore,
      makeClient: () => client,
      generatePassword: () => 'pw-1',
    })

    expect(out).toBe('connected')
    expect(client.setupNeeded).not.toHaveBeenCalled()
    expect(client.setup).not.toHaveBeenCalled()
    expect(client.login).not.toHaveBeenCalled()
    expect(client.createToken).not.toHaveBeenCalled()
  })

  it('uses stored creds when setup is already complete', async () => {
    const tokenStore = {
      load: vi.fn(() => null),
      save: vi.fn(),
    }
    const secretStore = {
      loadCreds: vi.fn(() => ({ email: 'desktop@localhost', password: 'saved-pw' })),
      saveCreds: vi.fn(),
      getManualServer: vi.fn(() => false),
    }
    const client = {
      setupNeeded: vi.fn(async () => false),
      setup: vi.fn(),
      login: vi.fn(async () => 'session-2'),
      createToken: vi.fn(async () => 'app-2'),
    }

    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: 'http://localhost:8080',
      tokenStore,
      secretStore,
      makeClient: () => client,
      generatePassword: () => 'pw-2',
    })

    expect(out).toBe('connected')
    expect(client.setup).not.toHaveBeenCalled()
    expect(client.login).toHaveBeenCalledWith('desktop@localhost', 'saved-pw')
    expect(client.createToken).toHaveBeenCalledWith('muesli-desktop', 'session-2')
    expect(secretStore.saveCreds).not.toHaveBeenCalled()
    expect(tokenStore.save).toHaveBeenCalledWith({ serverUrl: 'http://localhost:8080', token: 'app-2' })
  })

  it('returns needs-setup when setup is complete but no creds are stored', async () => {
    const tokenStore = {
      load: vi.fn(() => null),
      save: vi.fn(),
    }
    const secretStore = {
      loadCreds: vi.fn(() => null),
      saveCreds: vi.fn(),
      getManualServer: vi.fn(() => false),
    }
    const client = {
      setupNeeded: vi.fn(async () => false),
      setup: vi.fn(),
      login: vi.fn(),
      createToken: vi.fn(),
    }

    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: 'http://localhost:8080',
      tokenStore,
      secretStore,
      makeClient: () => client,
      generatePassword: () => 'pw-3',
    })

    expect(out).toBe('needs-setup')
    expect(client.setupNeeded).toHaveBeenCalledTimes(1)
    expect(client.setup).not.toHaveBeenCalled()
    expect(client.login).not.toHaveBeenCalled()
    expect(client.createToken).not.toHaveBeenCalled()
    expect(tokenStore.save).not.toHaveBeenCalled()
    expect(secretStore.saveCreds).not.toHaveBeenCalled()
  })

  it('returns manual immediately when the user opted into a remote server', async () => {
    const tokenStore = {
      load: vi.fn(),
      save: vi.fn(),
    }
    const secretStore = {
      loadCreds: vi.fn(),
      saveCreds: vi.fn(),
      getManualServer: vi.fn(() => true),
    }
    const client = {
      setupNeeded: vi.fn(),
      setup: vi.fn(),
      login: vi.fn(),
      createToken: vi.fn(),
    }

    const out = await ensureLocalSession({
      embedded: true,
      baseUrl: 'http://localhost:8080',
      tokenStore,
      secretStore,
      makeClient: () => client,
      generatePassword: () => 'pw-4',
    })

    expect(out).toBe('manual')
    expect(tokenStore.load).not.toHaveBeenCalled()
    expect(client.setupNeeded).not.toHaveBeenCalled()
    expect(client.setup).not.toHaveBeenCalled()
    expect(client.login).not.toHaveBeenCalled()
    expect(client.createToken).not.toHaveBeenCalled()
    expect(secretStore.saveCreds).not.toHaveBeenCalled()
  })

  it('skips entirely when not embedded', async () => {
    const tokenStore = {
      load: vi.fn(),
      save: vi.fn(),
    }
    const secretStore = {
      loadCreds: vi.fn(),
      saveCreds: vi.fn(),
      getManualServer: vi.fn(),
    }
    const client = {
      setupNeeded: vi.fn(),
      setup: vi.fn(),
      login: vi.fn(),
      createToken: vi.fn(),
    }

    const out = await ensureLocalSession({
      embedded: false,
      baseUrl: 'http://localhost:8080',
      tokenStore,
      secretStore,
      makeClient: () => client,
      generatePassword: () => 'pw-5',
    })

    expect(out).toBe('skipped')
    expect(tokenStore.load).not.toHaveBeenCalled()
    expect(secretStore.loadCreds).not.toHaveBeenCalled()
    expect(client.setupNeeded).not.toHaveBeenCalled()
  })

  it('propagates login failures without saving a token', async () => {
    const tokenStore = {
      load: vi.fn(() => null),
      save: vi.fn(),
    }
    const secretStore = {
      loadCreds: vi.fn(() => ({ email: 'desktop@localhost', password: 'saved-pw' })),
      saveCreds: vi.fn(),
      getManualServer: vi.fn(() => false),
    }
    const client = {
      setupNeeded: vi.fn(async () => false),
      setup: vi.fn(),
      login: vi.fn(async () => {
        throw new Error('bad login')
      }),
      createToken: vi.fn(),
    }

    await expect(
      ensureLocalSession({
        embedded: true,
        baseUrl: 'http://localhost:8080',
        tokenStore,
        secretStore,
        makeClient: () => client,
        generatePassword: () => 'pw-6',
      }),
    ).rejects.toThrow('bad login')
    expect(tokenStore.save).not.toHaveBeenCalled()
  })
})
