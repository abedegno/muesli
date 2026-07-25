import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FakeServer } from '../../test/fakeServer'
import { createHandlers } from './ipcHandlers'
import { type SecretStore } from './secretStore'
import { TokenStore, type SafeStorageLike } from './tokenStore'

const fakeSafe: SafeStorageLike = {
  isEncryptionAvailable: () => true,
  encryptString: (s) => Buffer.from(`enc:${s}`, 'utf8'),
  decryptString: (b) => b.toString('utf8').replace(/^enc:/, ''),
}

describe('ipc handlers', () => {
  let dir: string
  let server: FakeServer

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'muesli-ipc-'))
    server = new FakeServer()
  })
  afterEach(() => rmSync(dir, { recursive: true, force: true }))

  function makeHandlers() {
    const tokenStore = new TokenStore(dir, fakeSafe)
    return createHandlers({
      tokenStore,
      fetch: server.fetch,
      onProgress: () => {},
    })
  }

  it('connect (first run) sets up, logs in, mints + persists an app token', async () => {
    const h = makeHandlers()
    const out = await h.connect({
      serverUrl: 'http://localhost',
      email: 'o@example.com',
      password: 'password123',
      isFirstRun: true,
    })
    expect(out.serverUrl).toBe('http://localhost')

    const cfg = await h.getConfig()
    expect(cfg?.serverUrl).toBe('http://localhost')
    expect(cfg?.token).toMatch(/^app-/)
  })

  it('connect (login, not first run) skips setup', async () => {
    server.seedUser('o@example.com', 'password123')
    const h = makeHandlers()
    await h.connect({
      serverUrl: 'http://localhost',
      email: 'o@example.com',
      password: 'password123',
      isFirstRun: false,
    })
    expect((await h.getConfig())?.token).toMatch(/^app-/)
  })

  it('connect blocks plain HTTP to a non-loopback server (no network call)', async () => {
    let called = false
    const tokenStore = new TokenStore(dir, fakeSafe)
    const h = createHandlers({
      tokenStore,
      fetch: ((...a: Parameters<typeof server.fetch>) => { called = true; return server.fetch(...a) }) as typeof server.fetch,
      onProgress: () => {},
    })
    await expect(
      h.connect({ serverUrl: 'http://192.168.1.50:8080', email: 'o@example.com', password: 'password123', isFirstRun: true }),
    ).rejects.toThrow(/ERR_INSECURE_CONNECTION/)
    expect(called).toBe(false)
    expect(await h.getConfig()).toBeNull()
  })

  it('connect proceeds to a non-loopback HTTP server when allowInsecure is set', async () => {
    server.seedUser('o@example.com', 'password123')
    const h = makeHandlers()
    await h.connect({ serverUrl: 'http://192.168.1.50:8080', email: 'o@example.com', password: 'password123', isFirstRun: false, allowInsecure: true })
    expect((await h.getConfig())?.token).toMatch(/^app-/)
  })

  it('connect allows plain HTTP to a non-loopback server when MUESLI_ALLOW_INSECURE is set', async () => {
    server.seedUser('o@example.com', 'password123')
    process.env.MUESLI_ALLOW_INSECURE = '1'
    try {
      const h = makeHandlers()
      await h.connect({ serverUrl: 'http://192.168.1.50:8080', email: 'o@example.com', password: 'password123', isFirstRun: false })
      expect((await h.getConfig())?.token).toMatch(/^app-/)
    } finally {
      delete process.env.MUESLI_ALLOW_INSECURE
    }
  })

  it('getConfig and getManualServer reflect manual server mode', async () => {
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost:1234', token: 'app-token' })
    const secretStore: Pick<SecretStore, 'loadCreds' | 'saveCreds' | 'clearCreds' | 'getManualServer' | 'setManualServer' | 'getOnboarded' | 'setOnboarded'> = {
      loadCreds: () => null,
      saveCreds: () => {},
      clearCreds: () => {},
      getManualServer: () => true,
      setManualServer: () => {},
      getOnboarded: () => false,
      setOnboarded: () => {},
    }
    const h = createHandlers({
      tokenStore,
      fetch: server.fetch,
      onProgress: () => {},
      secretStore,
    })

    await expect(h.getManualServer()).resolves.toBe(true)
    await expect(h.getConfig()).resolves.toEqual({
      serverUrl: 'http://localhost:1234',
      token: 'app-token',
      manualServer: true,
    })
  })

  it('getReadyz forwards the bearer token and parses ollama detection', async () => {
    const seen: { auth?: string }[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const parsed = new URL(String(url))
      seen.push({ auth: new Headers(init?.headers).get('Authorization') ?? undefined })
      if (parsed.pathname === '/readyz') {
        return new Response(JSON.stringify({ embedded: { ollamaDetected: true } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response('unexpected', { status: 500 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost:1234', token: 'app-token' })
    const h = createHandlers({
      tokenStore,
      fetch: fetchMock,
      onProgress: () => {},
      embedded: true,
      embeddedBaseUrl: 'http://127.0.0.1:9000',
    })

    await expect(h.getReadyz()).resolves.toEqual({ ollamaDetected: true })
    expect(seen).toEqual([{ auth: 'Bearer app-token' }])
  })

  it('getReadyz uses the connected manual server url when manual mode is enabled', async () => {
    const seen: { url: string; auth?: string }[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push({
        url: String(url),
        auth: new Headers(init?.headers).get('Authorization') ?? undefined,
      })
      return new Response(JSON.stringify({ embedded: { ollamaDetected: true } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://remote.example:9000', token: 'app-token' })
    const secretStore: Pick<SecretStore, 'loadCreds' | 'saveCreds' | 'clearCreds' | 'getManualServer' | 'setManualServer' | 'getOnboarded' | 'setOnboarded'> = {
      loadCreds: () => null,
      saveCreds: () => {},
      clearCreds: () => {},
      getManualServer: () => true,
      setManualServer: () => {},
      getOnboarded: () => false,
      setOnboarded: () => {},
    }
    const h = createHandlers({
      tokenStore,
      fetch: fetchMock,
      onProgress: () => {},
      embedded: true,
      embeddedBaseUrl: 'http://127.0.0.1:9000',
      secretStore,
    })

    await expect(h.getReadyz()).resolves.toEqual({ ollamaDetected: true })
    expect(seen).toEqual([{ url: 'http://remote.example:9000/readyz', auth: 'Bearer app-token' }])
  })

  it('getReadyz returns null when the fetch payload is invalid', async () => {
    const h = createHandlers({
      tokenStore: new TokenStore(dir, fakeSafe),
      fetch: async () => new Response('not-json', { status: 200 }),
      onProgress: () => {},
      embedded: true,
      embeddedBaseUrl: 'http://127.0.0.1:9000',
    })

    await expect(h.getReadyz()).resolves.toBeNull()
  })

  it('getReadyz falls back to the global fetch when the caller does not inject one', async () => {
    vi.stubGlobal(
      'fetch',
      async () => new Response(JSON.stringify({ embedded: { ollamaDetected: true } }), { status: 200 }),
    )
    try {
      const h = createHandlers({
        tokenStore: new TokenStore(dir, fakeSafe),
        onProgress: () => {},
        embedded: true,
        embeddedBaseUrl: 'http://127.0.0.1:9000',
      })

      await expect(h.getReadyz()).resolves.toEqual({ ollamaDetected: true })
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('getServerHealth returns reachable with version for an ok healthz response', async () => {
    const fetchMock = async (url: string | URL): Promise<Response> => {
      const path = new URL(String(url)).pathname
      if (path === '/healthz') {
        return new Response(JSON.stringify({ status: 'ok', version: '1.2.3' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (path === '/api/digest/config') {
        return new Response(JSON.stringify({ owner_id: 'owner-1', cadence: 'off' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      throw new Error(`unexpected path: ${path}`)
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost:1234', token: 'app-token' })
    const h = createHandlers({
      tokenStore,
      fetch: fetchMock,
      onProgress: () => {},
    })

    await expect(h.getServerHealth()).resolves.toEqual({ reachable: true, authenticated: true, version: '1.2.3' })
  })

  it('clears the token and emits reconnect when an authed call gets a 401', async () => {
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost:1234', token: 'app-token' })
    const clearSpy = vi.spyOn(tokenStore, 'clear')
    const authInvalidated = vi.fn()
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const path = new URL(String(url)).pathname
      if (path === '/api/notes') {
        expect(new Headers(init?.headers).get('Authorization')).toBe('Bearer app-token')
        return new Response(JSON.stringify({ message: 'unauthorized' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      throw new Error(`unexpected path: ${path}`)
    }
    const h = createHandlers({
      tokenStore,
      fetch: fetchMock,
      onProgress: () => {},
      onAuthInvalidated: authInvalidated,
    })

    await expect(h.createNote('My meeting')).rejects.toMatchObject({
      name: 'AuthInvalidatedError',
      code: 'AUTH_INVALIDATED',
    })
    expect(clearSpy).toHaveBeenCalledTimes(1)
    expect(authInvalidated).toHaveBeenCalledWith({
      message: 'Your saved sign-in is no longer valid for this server. Sign in again to reconnect.',
    })
  })

  it('keeps auth marked as unknown when the authed health probe fails for a non-401 reason', async () => {
    const fetchMock = async (url: string | URL): Promise<Response> => {
      const path = new URL(String(url)).pathname
      if (path === '/healthz') {
        return new Response(JSON.stringify({ status: 'ok', version: '1.2.3' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (path === '/api/digest/config') {
        return new Response(JSON.stringify({ message: 'boom' }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      throw new Error(`unexpected path: ${path}`)
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost:1234', token: 'app-token' })
    const h = createHandlers({
      tokenStore,
      fetch: fetchMock,
      onProgress: () => {},
    })

    await expect(h.getServerHealth()).resolves.toEqual({ reachable: true, authenticated: true, version: '1.2.3' })
  })

  it('does not clear the token or emit reconnect for non-401 failures', async () => {
    const cases = [
      {
        name: '500',
        fetch: async () =>
          new Response(JSON.stringify({ message: 'boom' }), {
            status: 500,
            headers: { 'Content-Type': 'application/json' },
          }),
      },
      {
        name: 'timeout',
        fetch: async () => {
          throw new Error('timeout')
        },
      },
    ] as const

    for (const testCase of cases) {
      const tokenStore = new TokenStore(dir, fakeSafe)
      tokenStore.save({ serverUrl: 'http://localhost:1234', token: 'app-token' })
      const clearSpy = vi.spyOn(tokenStore, 'clear')
      const authInvalidated = vi.fn()
      const h = createHandlers({
        tokenStore,
        fetch: testCase.fetch as typeof server.fetch,
        onProgress: () => {},
        onAuthInvalidated: authInvalidated,
      })

      await expect(h.createNote('My meeting')).rejects.toThrow()
      expect(clearSpy, testCase.name).not.toHaveBeenCalled()
      expect(authInvalidated, testCase.name).not.toHaveBeenCalled()
    }
  })

  it('getServerHealth returns unreachable when the fetch rejects', async () => {
    const h = createHandlers({
      tokenStore: new TokenStore(dir, fakeSafe),
      fetch: async () => {
        throw new Error('offline')
      },
      onProgress: () => {},
    })

    await expect(h.getServerHealth()).resolves.toEqual({ reachable: false, authenticated: false })
  })

  it('getServerHealth returns unreachable when no server is configured', async () => {
    let called = false
    const h = createHandlers({
      tokenStore: new TokenStore(dir, fakeSafe),
      fetch: async () => {
        called = true
        return new Response('unexpected', { status: 500 })
      },
      onProgress: () => {},
    })

    await expect(h.getServerHealth()).resolves.toEqual({ reachable: false, authenticated: false })
    expect(called).toBe(false)
  })

  it('connect always allows https to any host', async () => {
    server.seedUser('o@example.com', 'password123')
    const h = makeHandlers()
    await h.connect({ serverUrl: 'https://muesli.example.com', email: 'o@example.com', password: 'password123', isFirstRun: false })
    expect((await h.getConfig())?.token).toMatch(/^app-/)
  })

  it('granular create/body/title/audio drive the expected API calls', async () => {
    server.seedUser('o@example.com', 'password123')
    const calls: { method: string; path: string }[] = []
    const baseFetch = server.fetch
    const spyFetch: typeof baseFetch = (input, init) => {
      const url = new URL(typeof input === 'string' ? input : input.toString())
      calls.push({ method: (init?.method ?? 'GET').toUpperCase(), path: url.pathname })
      return baseFetch(input, init)
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    const h = createHandlers({ tokenStore, fetch: spyFetch, onProgress: () => {} })
    await h.connect({
      serverUrl: 'http://localhost',
      email: 'o@example.com',
      password: 'password123',
      isFirstRun: false,
    })

    const note = await h.createNote('My meeting')
    expect(note.id).toMatch(/^note-/)
    await h.updateBody(note.id, '# notes')
    await h.updateTitle(note.id, 'Renamed')
    const res = await h.uploadAudio({
      noteId: note.id,
      audio: new Uint8Array([1]).buffer,
      audioMimeType: 'audio/webm',
    })
    expect(res.noteId).toBe(note.id)

    const has = (method: string, path: string) =>
      calls.some((c) => c.method === method && c.path === path)
    expect(has('POST', '/api/notes')).toBe(true)
    expect(has('PUT', `/api/notes/${note.id}/body`)).toBe(true)
    expect(has('PATCH', `/api/notes/${note.id}`)).toBe(true)
    expect(has('POST', `/api/notes/${note.id}/audio-upload-url`)).toBe(true)
    expect(server.lastBodyPut?.content).toBe('# notes')
  })

  it('getPerson, getPersonNotes, and getCompany call the expected authenticated endpoints', async () => {
    server.seedUser('o@example.com', 'password123')
    const seen: string[] = []
    const baseFetch = server.fetch
    const spyFetch: typeof baseFetch = (input, init) => {
      seen.push(`${init?.method ?? 'GET'} ${new URL(String(input)).pathname}`)
      return baseFetch(input, init)
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    const h = createHandlers({ tokenStore, fetch: spyFetch, onProgress: () => {} })
    await h.connect({
      serverUrl: 'http://localhost',
      email: 'o@example.com',
      password: 'password123',
      isFirstRun: false,
    })
    seen.length = 0

    await expect(h.getPerson('p1')).rejects.toMatchObject({ status: 404 })
    await expect(h.getPersonNotes('p1')).rejects.toMatchObject({ status: 404 })
    await expect(h.getCompany('c1')).rejects.toMatchObject({ status: 404 })

    expect(seen).toEqual([
      'GET /api/people/p1',
      'GET /api/people/p1/notes',
      'GET /api/companies/c1',
    ])
  })

  it('listNoteActionItems and updateActionItem call the note action-item endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const path = new URL(String(url)).pathname
      seen.push(`${init?.method ?? 'GET'} ${path}`)
      if (path === '/api/notes/n1/action-items') {
        return new Response(JSON.stringify({
          action_items: [
            {
              id: 'ai-1',
              note_id: 'n1',
              owner_id: 'owner-1',
              text: 'Ship the launch notes',
              owner_person_id: null,
              status: 'open',
              due_hint: 'Tomorrow',
              created_at: '2026-07-11T00:00:00Z',
            },
          ],
          decisions: [],
        }), { status: 200 })
      }
      if (path === '/api/action-items/ai-1') {
        return new Response(JSON.stringify({
          id: 'ai-1',
          note_id: 'n1',
          owner_id: 'owner-1',
          text: 'Ship the launch notes',
          owner_person_id: null,
          status: 'done',
          due_hint: 'Tomorrow',
          created_at: '2026-07-11T00:00:00Z',
        }), { status: 200 })
      }
      return new Response('unexpected', { status: 500 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const h = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })

    const listed = await h.listNoteActionItems('n1')
    expect(listed.actionItems).toHaveLength(1)
    expect(listed.actionItems[0]).toMatchObject({ id: 'ai-1', status: 'open' })

    const updated = await h.updateActionItem('ai-1', { status: 'done' })
    expect(updated.status).toBe('done')
    expect(seen).toEqual([
      'GET /api/notes/n1/action-items',
      'PATCH /api/action-items/ai-1',
    ])
  })

  it('listActionItems calls the cross-note action-item list endpoint', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const parsed = new URL(String(url))
      seen.push(`${init?.method ?? 'GET'} ${parsed.pathname}${parsed.search}`)
      if (parsed.pathname === '/api/action-items' && parsed.search === '?status=all') {
        return new Response(JSON.stringify([
          {
            id: 'ai-1',
            note_id: 'n1',
            owner_id: 'owner-1',
            text: 'Ship the launch notes',
            owner_person_id: null,
            status: 'done',
            due_hint: 'Tomorrow',
            created_at: '2026-07-11T00:00:00Z',
          },
        ]), { status: 200 })
      }
      return new Response('unexpected', { status: 500 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const h = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })

    const listed = await h.listActionItems('all')
    expect(listed).toHaveLength(1)
    expect(listed[0]).toMatchObject({ id: 'ai-1', status: 'done' })
    expect(seen).toEqual(['GET /api/action-items?status=all'])
  })

  it('updateFolder returns the updated folder body', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const path = new URL(String(url)).pathname
      seen.push(`${init?.method ?? 'GET'} ${path}`)
      if (path === '/api/folders/f1' && (init?.method ?? 'GET').toUpperCase() === 'PUT') {
        return new Response(JSON.stringify({ id: 'f1', name: 'Accounts', created_at: '2026-07-10T00:00:00Z' }), { status: 200 })
      }
      return new Response(JSON.stringify({ id: 'f1', name: 'Clients', created_at: '2026-07-10T00:00:00Z' }), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const h = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })

    const updated = await h.updateFolder('f1', 'Accounts')

    expect(seen).toContain('PUT /api/folders/f1')
    expect(updated).toMatchObject({ id: 'f1', name: 'Accounts', created_at: '2026-07-10T00:00:00Z' })
  })

  it('pinNote and unpinNote call the note pin endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const h = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await h.pinNote('note-1')
    await h.unpinNote('note-1')
    expect(seen).toContain('POST http://localhost/api/notes/note-1/pin')
    expect(seen).toContain('DELETE http://localhost/api/notes/note-1/pin')
  })

  it('linkNoteEvent and unlinkNoteEvent call the note event endpoints (CALLNK02)', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const h = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await h.linkNoteEvent('note-1', 'evt-1')
    await h.unlinkNoteEvent('note-1')
    expect(seen).toContain('POST http://localhost/api/notes/note-1/event')
    expect(seen).toContain('DELETE http://localhost/api/notes/note-1/event')
  })

  it('getNoteAudioUrl returns null for missing audio and a grant once uploaded', async () => {
    server.seedUser('o@example.com', 'password123')
    const tokenStore = new TokenStore(dir, fakeSafe)
    const h = createHandlers({
      tokenStore,
      fetch: server.fetch,
      onProgress: () => {},
    })
    await h.connect({
      serverUrl: 'http://localhost',
      email: 'o@example.com',
      password: 'password123',
      isFirstRun: false,
    })

    const note = await h.createNote('My meeting')
    await expect(h.getNoteAudioUrl(note.id)).resolves.toBeNull()

    const res = await h.uploadAudio({
      noteId: note.id,
      audio: new Uint8Array([1]).buffer,
      audioMimeType: 'audio/webm',
    })
    expect(res.noteId).toBe(note.id)

    const grant = await h.getNoteAudioUrl(note.id)
    expect(grant?.url).toContain(`/_storage/notes/${note.id}/audio/`)
    expect(grant?.expires_at).toBeTruthy()
  })

  it('openGoogleCalendarOAuthStart opens the authenticated start URL', async () => {
    server.seedUser('o@example.com', 'password123')
    const opened: string[] = []
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const h = createHandlers({
      tokenStore,
      fetch: server.fetch,
      onProgress: () => {},
      openExternal: async (url) => {
        opened.push(url)
      },
    })

    await h.openGoogleCalendarOAuthStart()
    expect(opened).toEqual(['http://localhost/api/calendar/oauth/google/start?token=app-test'])
  })

  it('openMicrosoftCalendarOAuthStart opens the authenticated start URL', async () => {
    server.seedUser('o@example.com', 'password123')
    const opened: string[] = []
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const h = createHandlers({
      tokenStore,
      fetch: server.fetch,
      onProgress: () => {},
      openExternal: async (url) => {
        opened.push(url)
      },
    })

    await h.openMicrosoftCalendarOAuthStart()
    expect(opened).toEqual(['http://localhost/api/calendar/oauth/microsoft/start?token=app-test'])
  })

  it('disconnect clears stored credentials', async () => {
    server.seedUser('o@example.com', 'password123')
    const h = makeHandlers()
    await h.connect({
      serverUrl: 'http://localhost',
      email: 'o@example.com',
      password: 'password123',
      isFirstRun: false,
    })
    await h.disconnect()
    expect(await h.getConfig()).toBeNull()
  })

  it('listNotes throws when not connected', async () => {
    const h = makeHandlers()
    await expect(h.listNotes()).rejects.toThrow(/not connected/i)
  })

  it('smart-list handlers call the client', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(
        JSON.stringify({ id: 'l1', name: 'X', rule: { op: 'and', children: [] }, created_at: '' }),
        { status: 200 },
      )
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    const rule = { op: 'and' as const, children: [] }
    await handlers.createSmartList('X', rule)
    await handlers.listSmartLists()
    await handlers.updateSmartList('l1', 'Y', rule)
    await handlers.deleteSmartList('l1')
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/smart-lists'))).toBe(true)
    expect(seen.some((s) => s.startsWith('GET') && s.endsWith('/api/smart-lists'))).toBe(true)
    expect(seen.some((s) => s.startsWith('PUT') && s.endsWith('/api/smart-lists/l1'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/smart-lists/l1'))).toBe(true)
  })

  it('folder handlers call the client endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(
        JSON.stringify({ id: 'f1', name: 'Clients', created_at: '' }),
        { status: 200 },
      )
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.listFolders()
    await handlers.createFolder('Clients')
    await handlers.updateFolder('f1', 'Accounts')
    await handlers.deleteFolder('f1')
    await handlers.addNoteFolder('n1', 'f1')
    await handlers.removeNoteFolder('n1', 'f1')
    expect(seen.some((s) => s.startsWith('GET') && s.endsWith('/api/folders'))).toBe(true)
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/folders'))).toBe(true)
    expect(seen.some((s) => s.startsWith('PUT') && s.endsWith('/api/folders/f1'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/folders/f1'))).toBe(true)
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/notes/n1/folders'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/notes/n1/folders/f1'))).toBe(true)
  })

  it('reorderNoteInFolder calls the client endpoint', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.reorderNoteInFolder('f1', 'n1', 'n0')
    expect(seen.some((s) => s.startsWith('PUT') && s.endsWith('/api/folders/f1/notes/n1/reorder'))).toBe(true)
  })

  it('template handlers call the client endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(
        JSON.stringify({ id: 't1', name: 'Default', phase: 'after', sections: [], built_in: false }),
        { status: 200 },
      )
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    const sections = [{ heading: 'Overview', instruction: 'Summarise' }]
    await handlers.listTemplates()
    await handlers.createTemplate('Default', 'after', sections, true)
    await handlers.updateTemplate('t1', 'Renamed', 'after', sections, false)
    await handlers.deleteTemplate('t1')
    expect(seen.some((s) => s.startsWith('GET') && s.endsWith('/api/templates'))).toBe(true)
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/templates'))).toBe(true)
    expect(seen.some((s) => s.startsWith('PUT') && s.endsWith('/api/templates/t1'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/templates/t1'))).toBe(true)
  })

  it('deleteNote calls DELETE /api/notes/{id}', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response('', { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.deleteNote('n1')
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/notes/n1'))).toBe(true)
  })

  it('duplicateNote POSTs /api/notes/{id}/duplicate', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify({ id: 'n2', title: 'Copy of N1', status: 'recording' }), { status: 201 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    const note = await handlers.duplicateNote('n1')
    expect(note.id).toBe('n2')
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/notes/n1/duplicate'))).toBe(true)
  })

  it('trash handlers call the client endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.listTrash()
    await handlers.restoreNote('n1')
    await handlers.permanentDeleteNote('n1')
    expect(seen.some((s) => s.startsWith('GET') && s.endsWith('/api/notes/trash'))).toBe(true)
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/notes/n1/restore'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/notes/n1/permanent'))).toBe(true)
  })

  it('folder trash handlers call the client endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.listTrashedFolders()
    await handlers.restoreFolder('f1')
    await handlers.permanentDeleteFolder('f1')
    expect(seen.some((s) => s.startsWith('GET') && s.endsWith('/api/folders/trash'))).toBe(true)
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/folders/f1/restore'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/folders/f1/permanent'))).toBe(true)
  })

  it('smart-list trash handlers call the client endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.listTrashedSmartLists()
    await handlers.restoreSmartList('l1')
    await handlers.permanentDeleteSmartList('l1')
    expect(seen.some((s) => s.startsWith('GET') && s.endsWith('/api/smart-lists/trash'))).toBe(true)
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/smart-lists/l1/restore'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.endsWith('/api/smart-lists/l1/permanent'))).toBe(true)
  })

  it('resummarize calls POST /api/notes/{id}/resummarize', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response('', { status: 202 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.resummarize('n1')
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/notes/n1/resummarize'))).toBe(true)
  })

  it('retranscribeNote calls POST /api/notes/{id}/retranscribe with the provided overrides', async () => {
    const seen: Array<{ method: string; path: string; body?: unknown }> = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const parsedUrl = new URL(String(url))
      seen.push({
        method: (init?.method ?? 'GET').toUpperCase(),
        path: parsedUrl.pathname,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
      })
      return new Response(JSON.stringify({ status: 'transcribing' }), { status: 202 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    const out = await handlers.retranscribeNote('n1', { language: 'en' })
    expect(out).toEqual({ status: 'transcribing' })
    expect(seen).toContainEqual({
      method: 'POST',
      path: '/api/notes/n1/retranscribe',
      body: { language: 'en' },
    })
  })

  it('share handlers call the note share endpoints', async () => {
    const seen: Array<{ method: string; path: string; body?: unknown }> = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const parsedUrl = new URL(String(url))
      seen.push({
        method: (init?.method ?? 'GET').toUpperCase(),
        path: parsedUrl.pathname,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
      })
      if (parsedUrl.pathname.endsWith('/share')) {
        return new Response(JSON.stringify({ token: 'share-token', url: 'http://localhost/shared/share-token' }), { status: 201 })
      }
      if (parsedUrl.pathname.endsWith('/shares')) {
        return new Response(JSON.stringify([]), { status: 200 })
      }
      return new Response(null, { status: 204 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })

    const created = await handlers.createShare('n1', { expires_at: '2026-07-13T00:00:00.000Z' })
    const shares = await handlers.listNoteShares('n1')
    await handlers.revokeShare('share-token')

    expect(created).toEqual({ token: 'share-token', url: 'http://localhost/shared/share-token' })
    expect(shares).toEqual([])
    expect(seen).toContainEqual({
      method: 'POST',
      path: '/api/notes/n1/share',
      body: { expires_at: '2026-07-13T00:00:00.000Z' },
    })
    expect(seen).toContainEqual({
      method: 'GET',
      path: '/api/notes/n1/shares',
      body: undefined,
    })
    expect(seen).toContainEqual({
      method: 'DELETE',
      path: '/api/shares/share-token',
      body: undefined,
    })
  })

  it('regenerateSummary calls POST /api/notes/{id}/templates/{templateId}/summarize', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response('', { status: 202 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.regenerateSummary('n1', 't1')
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/notes/n1/templates/t1/summarize'))).toBe(true)
  })

  it('search delegates to GET /api/search?q=', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify([
        { note_id: 'id1', match_type: 'title' },
        { note_id: 'id2', match_type: 'transcript', segment_id: 'seg1', start_ms: 500, snippet: '…budget review…' },
      ]), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    const matches = await handlers.search('budget review')
    expect(matches).toEqual([
      { note_id: 'id1', match_type: 'title' },
      { note_id: 'id2', match_type: 'transcript', segment_id: 'seg1', start_ms: 500, snippet: '…budget review…' },
    ])
    expect(seen.some((s) => s.startsWith('GET') && s.includes('/api/search?q=budget+review'))).toBe(true)
  })

  it('search threads from/to onto the querystring when present', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response('[]', { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.search('budget', { from: '2024-01-01', to: '2024-02-01' })
    expect(seen.some((s) => s.includes('/api/search?q=budget&from=2024-01-01&to=2024-02-01'))).toBe(true)
  })

  it('search threads person/folder/tag filters onto the querystring when present', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response('[]', { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.search('budget', { personId: 'p-1', folderId: 'f-1', tag: 'weekly sync' })
    expect(
      seen.some((s) => s.includes('/api/search?q=budget&person_id=p-1&folder_id=f-1&tag=weekly+sync')),
    ).toBe(true)
  })

  it('addTag and removeTag call the client endpoints', async () => {
    const seen: string[] = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push(`${init?.method ?? 'GET'} ${String(url)}`)
      return new Response(JSON.stringify({ id: 't1', name: '1on1' }), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.addTag('n1', '1on1')
    await handlers.removeTag('n1', '1on1')
    expect(seen.some((s) => s.startsWith('POST') && s.endsWith('/api/notes/n1/tags'))).toBe(true)
    expect(seen.some((s) => s.startsWith('DELETE') && s.includes('/api/notes/n1/tags?name=1on1'))).toBe(true)
  })

  it('renameTag PUTs /api/tags/{id} with the saved token', async () => {
    const seen: Array<{ method?: string; url: string; auth?: string | null }> = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      const auth = new Headers(init?.headers).get('authorization')
      seen.push({ method: init?.method, url: String(url), auth })
      return new Response(JSON.stringify({ id: 't1', name: 'renamed' }), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    const out = await handlers.renameTag('t1', 'renamed')
    expect(out).toEqual({ id: 't1', name: 'renamed' })
    expect(seen[0].method).toBe('PUT')
    expect(seen[0].url).toBe('http://localhost/api/tags/t1')
    expect(seen[0].auth).toBe('Bearer app-test')
  })
  it('chat handlers hit the right endpoints with the saved token', async () => {
    const seen: Array<{ method?: string; url: string }> = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push({ method: init?.method, url: String(url) })
      return new Response(JSON.stringify({ id: 'c1', title: 'Q&A', created_at: '', updated_at: '' }), { status: 200 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await handlers.listConversations()
    await handlers.listConversations('note-1')
    await handlers.createConversation({ title: 'Q&A' })
    await handlers.getConversation('c1')
    await handlers.deleteConversation('c1')
    await handlers.listMessages('c1')
    await handlers.sendMessage('c1', { content: 'hi' })
    expect(seen).toEqual([
      { method: 'GET', url: 'http://localhost/api/conversations' },
      { method: 'GET', url: 'http://localhost/api/conversations?note_id=note-1' },
      { method: 'POST', url: 'http://localhost/api/conversations' },
      { method: 'GET', url: 'http://localhost/api/conversations/c1' },
      { method: 'DELETE', url: 'http://localhost/api/conversations/c1' },
      { method: 'GET', url: 'http://localhost/api/conversations/c1/messages' },
      { method: 'POST', url: 'http://localhost/api/conversations/c1/messages' },
    ])
  })

  it('surfaces a 409 in-flight send guard and a 500 plugin failure as a [NNN] message prefix', async () => {
    let calls = 0
    const fetchMock = async (): Promise<Response> => {
      calls += 1
      if (calls === 1) return new Response(JSON.stringify({ error: 'message send already in progress' }), { status: 409 })
      return new Response(JSON.stringify({ error: 'internal error' }), { status: 500 })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })
    await expect(handlers.sendMessage('c1', { content: 'hi' })).rejects.toThrow('[409] message send already in progress')
    await expect(handlers.sendMessage('c1', { content: 'hi' })).rejects.toThrow('[500] internal error')
  })

  it('exportNote fetches the note export and parses the suggested filename', async () => {
    const seen: Array<{ url: string; method?: string }> = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push({ url: String(url), method: init?.method })
      return new Response('exported note body', {
        status: 200,
        headers: {
          'Content-Disposition': 'attachment; filename="Team Notes.md"',
          'Content-Type': 'text/markdown; charset=utf-8',
        },
      })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })

    const out = await handlers.exportNote('note-123', 'md', { includeTranscript: false, redactSpeakers: true })

    expect(seen[0]).toEqual({ method: 'GET', url: 'http://localhost/api/notes/note-123/export?format=md&include_transcript=false&redact_speakers=true' })
    expect(Buffer.from(out.bytes).toString('utf8')).toBe('exported note body')
    expect(out.filename).toBe('Team Notes.md')
    expect(out.contentType).toBe('text/markdown; charset=utf-8')
  })

  it('exportFolder fetches the batch export and parses the suggested filename', async () => {
    const seen: Array<{ url: string; method?: string; body?: string }> = []
    const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
      seen.push({ url: String(url), method: init?.method, body: init?.body ? String(init.body) : undefined })
      return new Response('exported zip body', {
        status: 200,
        headers: {
          'Content-Disposition': 'attachment; filename="Team Notes.zip"',
          'Content-Type': 'application/zip',
        },
      })
    }
    const tokenStore = new TokenStore(dir, fakeSafe)
    tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
    const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })

    const out = await handlers.exportFolder('folder-123', 'pdf', { includeTranscript: false, redactSpeakers: true })

    expect(seen[0]).toEqual({
      method: 'POST',
      url: 'http://localhost/api/export/batch',
      body: JSON.stringify({ folder_id: 'folder-123', format: 'pdf', include_transcript: false, redact_speakers: true }),
    })
    expect(Buffer.from(out.bytes).toString('utf8')).toBe('exported zip body')
    expect(out.filename).toBe('Team Notes.zip')
    expect(out.contentType).toBe('application/zip')
  })

  describe('exportAllNotes', () => {
    it('writes a zip containing one .md file per note with correct filename and content', async () => {
      const { mkdtempSync, rmSync, readFileSync } = await import('node:fs')
      const JSZip = (await import('jszip')).default
      const { fullNoteToMarkdown } = await import('../renderer/lib/noteMarkdown')

      const tmpDir = mkdtempSync(join(tmpdir(), 'muesli-export-'))
      const outPath = join(tmpDir, 'test-export.zip')

      try {
        const fakeNotes = [
          {
            id: 'note-aaa',
            title: 'Board Meeting Q1',
            status: 'ready',
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
            deleted_at: null,
          },
          {
            id: 'note-bbb',
            title: '',
            status: 'ready',
            created_at: '2024-01-02T00:00:00Z',
            updated_at: '2024-01-02T00:00:00Z',
            deleted_at: null,
          },
        ]

        const fakeFullNotes: Record<string, import('../../src/shared/types').FullNote> = {
          'note-aaa': {
            note: fakeNotes[0] as import('../../src/shared/types').Note,
            body_markdown: 'action items here',
            summaries: [],
            transcript: null,
          },
          'note-bbb': {
            note: fakeNotes[1] as import('../../src/shared/types').Note,
            body_markdown: 'empty title note',
            summaries: [],
            transcript: null,
          },
        }

        // Stub fetch to serve listNotes and getFull
        const fetchMock = async (url: string | URL, init?: RequestInit): Promise<Response> => {
          const u = String(url)
          if (u.endsWith('/api/notes') && (init?.method ?? 'GET') === 'GET') {
            return new Response(JSON.stringify(fakeNotes), { status: 200 })
          }
          for (const id of Object.keys(fakeFullNotes)) {
            if (u.endsWith(`/api/notes/${id}/full`)) {
              return new Response(JSON.stringify(fakeFullNotes[id]), { status: 200 })
            }
          }
          return new Response('not found', { status: 404 })
        }

        const tokenStore = new TokenStore(dir, fakeSafe)
        tokenStore.save({ serverUrl: 'http://localhost', token: 'app-test' })
        const handlers = createHandlers({ tokenStore, fetch: fetchMock, onProgress: () => {} })

        const result = await handlers.exportAllNotes(outPath)

        expect(result.success).toBe(true)
        if (!result.success) throw new Error('expected success')
        expect(result.path).toBe(outPath)

        // Load and verify zip
        const zipBuf = readFileSync(outPath)
        const zip = await JSZip.loadAsync(zipBuf)
        const entries = Object.keys(zip.files)

        // Should have exactly 2 entries
        expect(entries).toHaveLength(2)

        // note-aaa: slug = 'board-meeting-q1'
        const entry1 = 'note-aaa-board-meeting-q1.md'
        expect(entries).toContain(entry1)
        const content1 = await zip.files[entry1].async('string')
        const expected1 = fullNoteToMarkdown(fakeFullNotes['note-aaa'])
        expect(content1).toBe(expected1)

        // note-bbb: empty title → 'untitled'
        const entry2 = 'note-bbb-untitled.md'
        expect(entries).toContain(entry2)
        const content2 = await zip.files[entry2].async('string')
        const expected2 = fullNoteToMarkdown(fakeFullNotes['note-bbb'])
        expect(content2).toBe(expected2)
      } finally {
        rmSync(outPath, { force: true })
        rmSync(tmpDir, { recursive: true, force: true })
      }
    })

    it('returns success:false when not connected', async () => {
      const tokenStore = new TokenStore(dir, fakeSafe)
      // no tokenStore.save → not connected
      const handlers = createHandlers({ tokenStore, fetch: server.fetch, onProgress: () => {} })
      const result = await handlers.exportAllNotes('/tmp/nope.zip')
      expect(result.success).toBe(false)
      if (result.success) throw new Error('expected failure')
      expect(result.error).toMatch(/not connected/i)
    })
  })

})
