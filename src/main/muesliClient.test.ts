import { beforeEach, describe, expect, it } from 'vitest'
import { FakeServer } from '../../test/fakeServer'
import { MuesliClient, ApiError, type FetchLike } from './muesliClient'

const BASE = 'http://fake'

describe('MuesliClient', () => {
  let server: FakeServer

  beforeEach(() => {
    server = new FakeServer()
  })

  it('runs first-run setup then mints an app token via a session login', async () => {
    const c = new MuesliClient({ baseUrl: BASE, fetch: server.fetch })
    await c.setup('owner@example.com', 'password123')
    const session = await c.login('owner@example.com', 'password123')
    expect(session).toMatch(/^session-/)
    const appToken = await c.createToken('desktop', session)
    expect(appToken).toMatch(/^app-/)
  })

  it('rejects setup once an account exists', async () => {
    server.seedUser('owner@example.com', 'password123')
    const c = new MuesliClient({ baseUrl: BASE, fetch: server.fetch })
    await expect(c.setup('x@example.com', 'password123')).rejects.toBeInstanceOf(ApiError)
    await expect(c.setup('x@example.com', 'password123')).rejects.toMatchObject({ status: 409 })
  })

  it('login with bad password throws ApiError 401', async () => {
    server.seedUser('owner@example.com', 'password123')
    const c = new MuesliClient({ baseUrl: BASE, fetch: server.fetch })
    await expect(c.login('owner@example.com', 'nope')).rejects.toMatchObject({ status: 401 })
  })

  it('authenticated note CRUD uses the stored bearer token', async () => {
    server.seedUser('owner@example.com', 'password123')
    const session = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).login(
      'owner@example.com',
      'password123',
    )
    const appToken = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).createToken(
      'desktop',
      session,
    )

    const c = new MuesliClient({ baseUrl: BASE, fetch: server.fetch, token: appToken })
    const note = await c.createNote('Sprint planning')
    expect(note.status).toBe('recording')

    const list = await c.listNotes()
    expect(list).toHaveLength(1)
    expect(list[0].id).toBe(note.id)
    expect(list[0].pinned).toBe(false)

    const got = await c.getNote(note.id)
    expect(got.id).toBe(note.id)
    expect(got.pinned).toBe(false)
  })

  it('fetches person and company detail payloads from the expected endpoints', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      const path = new URL(String(url)).pathname
      if (path === '/api/people/p1/notes') {
        return new Response(JSON.stringify([{ id: 'n1', title: 'Meeting', status: 'ready', created_at: '', updated_at: '', partial_transcript: false }]), { status: 200 })
      }
      if (path === '/api/people/p1') {
        return new Response(JSON.stringify({ id: 'p1', primary_email: 'alex@example.com', display_name: 'Alex Doe', first_seen_at: '', updated_at: '', company: { id: 'c1', owner_id: 'o1', domain: 'example.com', name: 'Example Inc', created_at: '', updated_at: '' } }), { status: 200 })
      }
      if (path === '/api/companies/c1') {
        return new Response(JSON.stringify({ id: 'c1', owner_id: 'o1', domain: 'example.com', name: 'Example Inc', created_at: '', updated_at: '', people: [{ id: 'p1', primary_email: 'alex@example.com', display_name: 'Alex Doe', first_seen_at: '', updated_at: '' }] }), { status: 200 })
      }
      return new Response('unexpected', { status: 500 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await expect(client.getPerson('p1')).resolves.toMatchObject({ id: 'p1', company: { id: 'c1' } })
    await expect(client.getPersonNotes('p1')).resolves.toHaveLength(1)
    await expect(client.getCompany('c1')).resolves.toMatchObject({ id: 'c1', people: [{ id: 'p1' }] })
    expect(calls.map((c) => `${c.method ?? 'GET'} ${c.url}`)).toEqual([
      'GET http://x/api/people/p1',
      'GET http://x/api/people/p1/notes',
      'GET http://x/api/companies/c1',
    ])
  })

  it('listNoteActionItems GETs the note action-items endpoint and maps the wrapper response', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      const path = new URL(String(url)).pathname
      if (path === '/api/notes/note-1/action-items') {
        return new Response(JSON.stringify({
          action_items: [
            {
              id: 'ai-1',
              note_id: 'note-1',
              owner_id: 'owner-1',
              text: 'Ship the launch notes',
              owner_person_id: null,
              status: 'open',
              due_hint: 'Tomorrow',
              created_at: '2026-07-11T00:00:00Z',
            },
          ],
          decisions: [
            {
              id: 'd-1',
              note_id: 'note-1',
              owner_id: 'owner-1',
              text: 'Use the weekly cadence',
              created_at: '2026-07-11T00:00:00Z',
            },
          ],
        }), { status: 200 })
      }
      return new Response('unexpected', { status: 500 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.listNoteActionItems('note-1')
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/action-items')
    expect(out.actionItems).toHaveLength(1)
    expect(out.actionItems[0]).toMatchObject({ id: 'ai-1', status: 'open' })
    expect(out.decisions).toHaveLength(1)
    expect(out.decisions[0]).toMatchObject({ id: 'd-1', text: 'Use the weekly cadence' })
  })

  it('listActionItems GETs /api/action-items with the status query param', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      const fullUrl = String(url)
      if (fullUrl === 'http://x/api/action-items?status=all') {
        return new Response(JSON.stringify([
          {
            id: 'ai-1',
            note_id: 'note-1',
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
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.listActionItems('all')
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/action-items?status=all')
    expect(out).toHaveLength(1)
    expect(out[0]).toMatchObject({ id: 'ai-1', status: 'done' })
  })

  it('exportNote GETs the export endpoint and parses the filename/content type', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response('exported markdown', {
        status: 200,
        headers: {
          'Content-Disposition': 'attachment; filename="Quarterly Review.md"',
          'Content-Type': 'text/markdown; charset=utf-8',
        },
      })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.exportNote('note-1', 'md', { includeTranscript: false, redactSpeakers: true })
    expect(calls[0].url).toBe('http://x/api/notes/note-1/export?format=md&include_transcript=false&redact_speakers=true')
    expect(calls[0].init?.method).toBe('GET')
    expect(Buffer.from(out.bytes).toString('utf8')).toBe('exported markdown')
    expect(out.filename).toBe('Quarterly Review.md')
    expect(out.contentType).toBe('text/markdown; charset=utf-8')
  })

  it('exportFolder POSTs the batch export endpoint with the selected options', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response('exported zip', {
        status: 200,
        headers: {
          'Content-Disposition': 'attachment; filename="Client Notes.zip"',
          'Content-Type': 'application/zip',
        },
      })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.exportFolder('folder-1', 'pdf', { includeTranscript: false, redactSpeakers: true })
    expect(calls[0].url).toBe('http://x/api/export/batch')
    expect(calls[0].init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      folder_id: 'folder-1',
      format: 'pdf',
      include_transcript: false,
      redact_speakers: true,
    })
    expect(Buffer.from(out.bytes).toString('utf8')).toBe('exported zip')
    expect(out.filename).toBe('Client Notes.zip')
    expect(out.contentType).toBe('application/zip')
  })

  it('exportNote throws ApiError when the export endpoint returns an error', async () => {
    const fetchMock: FetchLike = async () =>
      new Response(JSON.stringify({ error: 'unsupported format' }), {
        status: 400,
        headers: { 'Content-Type': 'application/json' },
      })
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await expect(client.exportNote('note-1', 'docx')).rejects.toMatchObject({ status: 400, message: 'unsupported format' })
  })

  it('walks the full presigned upload + body flow', async () => {
    server.seedUser('o@example.com', 'password123')
    const session = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).login(
      'o@example.com',
      'password123',
    )
    const appToken = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).createToken(
      'desktop',
      session,
    )
    const c = new MuesliClient({ baseUrl: BASE, fetch: server.fetch, token: appToken })

    const note = await c.createNote('M')
    const grant = await c.getAudioUploadUrl(note.id)
    expect(grant.method).toBe('PUT')

    await c.putAudio(grant, new Uint8Array([1, 2, 3, 4]))
    const res = await c.markAudioUploaded(note.id, grant.key)
    expect(res.status).toBe('uploaded')

    await c.putBody(note.id, '# live notes')
    expect(server.lastBodyPut).toEqual({ id: note.id, content: '# live notes' })

    const fresh = await c.getNote(note.id)
    expect(fresh.status).toBe('uploaded')
  })

  it('getNoteAudioUrl GETs the audio grant endpoint', async () => {
    server.seedUser('o@example.com', 'password123')
    const session = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).login(
      'o@example.com',
      'password123',
    )
    const appToken = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).createToken(
      'desktop',
      session,
    )
    const c = new MuesliClient({ baseUrl: BASE, fetch: server.fetch, token: appToken })

    const note = await c.createNote('M')
    await expect(c.getNoteAudioUrl(note.id)).rejects.toMatchObject({ status: 404 })

    const grant = await c.getAudioUploadUrl(note.id)
    await c.putAudio(grant, new Uint8Array([1, 2, 3, 4]))
    await c.markAudioUploaded(note.id, grant.key)

    const audio = await c.getNoteAudioUrl(note.id)
    expect(audio.url).toContain(`/notes/${note.id}/audio/`)
    expect(audio.expires_at).toBeTruthy()
  })

  it('getGoogleCalendarOAuthStatus GETs the oauth status endpoint', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify({ configured: true }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.getGoogleCalendarOAuthStatus()
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/calendar/oauth/google/status')
    expect(out.configured).toBe(true)
  })

  it('getMicrosoftCalendarOAuthStatus GETs the oauth status endpoint', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify({ configured: true }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.getMicrosoftCalendarOAuthStatus()
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/calendar/oauth/microsoft/status')
    expect(out.configured).toBe(true)
  })

  it('updateTitle PATCHes the note title', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.updateTitle('note-1', 'Renamed')
    expect(calls[0].url).toBe('http://x/api/notes/note-1')
    expect(calls[0].init?.method).toBe('PATCH')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ title: 'Renamed' })
  })

  it('updateActionItem PATCHes /api/action-items/{id} with the mapped body', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response(JSON.stringify({
        id: 'ai-1',
        note_id: 'note-1',
        owner_id: 'owner-1',
        text: 'Ship the launch notes',
        owner_person_id: null,
        status: 'done',
        due_hint: 'Tomorrow',
        created_at: '2026-07-11T00:00:00Z',
      }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.updateActionItem('ai-1', { status: 'done' })
    expect(calls[0].url).toBe('http://x/api/action-items/ai-1')
    expect(calls[0].init?.method).toBe('PATCH')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ status: 'done' })
    expect(out.status).toBe('done')
  })

  it('deleteNote DELETEs /api/notes/{id}', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.deleteNote('note-1')
    expect(calls[0].method).toBe('DELETE')
    expect(calls[0].url).toBe('http://x/api/notes/note-1')
  })

  it('duplicateNote POSTs /api/notes/{id}/duplicate', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response(JSON.stringify({ id: 'note-copy', title: 'Copy of Standup', status: 'recording' }), { status: 201 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.duplicateNote('note-1')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/duplicate')
    expect(calls[0].init?.method).toBe('POST')
    expect(out.id).toBe('note-copy')
    expect(out.status).toBe('recording')
  })

  it('pinNote and unpinNote hit the pin endpoints', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.pinNote('note-1')
    await client.unpinNote('note-1')
    expect(calls).toEqual([
      { url: 'http://x/api/notes/note-1/pin', method: 'POST' },
      { url: 'http://x/api/notes/note-1/pin', method: 'DELETE' },
    ])
  })

  it('linkNoteEvent and unlinkNoteEvent hit the note event endpoints (CALLNK02)', async () => {
    const calls: Array<{ url: string; method?: string; body?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body as string | undefined })
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.linkNoteEvent('note-1', 'evt-1')
    await client.unlinkNoteEvent('note-1')
    expect(calls[0]).toEqual({ url: 'http://x/api/notes/note-1/event', method: 'POST', body: JSON.stringify({ event_id: 'evt-1' }) })
    expect(calls[1]).toEqual({ url: 'http://x/api/notes/note-1/event', method: 'DELETE', body: undefined })
  })

  it('listNotes can scope to a single folder via folder_id', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.listNotes('folder-1')
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/notes?folder_id=folder-1')
  })

  it('renameTag PUTs /api/tags/{id} with the new name', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response(JSON.stringify({ id: 'tag-1', name: 'renamed' }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const out = await client.renameTag('tag-1', 'renamed')
    expect(calls[0].url).toBe('http://x/api/tags/tag-1')
    expect(calls[0].init?.method).toBe('PUT')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ name: 'renamed' })
    expect(out).toEqual({ id: 'tag-1', name: 'renamed' })
  })

  it('listTrash GETs /api/notes/trash', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.listTrash()
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/notes/trash')
  })

  it('restoreNote POSTs /api/notes/{id}/restore', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.restoreNote('note-1')
    expect(calls[0].method).toBe('POST')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/restore')
  })

  it('retranscribeNote POSTs /api/notes/{id}/retranscribe and omits undefined overrides', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response(JSON.stringify({ status: 'transcribing' }), { status: 202 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })

    const out = await client.retranscribeNote('note-1', { model: 'gpt-4o-mini' })

    expect(calls[0].init?.method).toBe('POST')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/retranscribe')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ model: 'gpt-4o-mini' })
    expect(out).toEqual({ status: 'transcribing' })
  })

  it('createShare POSTs /api/notes/{id}/share and omits an empty expiry', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response(JSON.stringify({ token: 'share-token', url: 'http://x/shared/share-token' }), { status: 201 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })

    const out = await client.createShare('note-1', { expires_at: '   ' })

    expect(calls[0].init?.method).toBe('POST')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/share')
    expect(calls[0].init?.body).toBeUndefined()
    expect(out).toEqual({ token: 'share-token', url: 'http://x/shared/share-token' })
  })

  it('listNoteShares GETs /api/notes/{id}/shares', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.listNoteShares('note-1')
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/shares')
  })

  it('revokeShare DELETEs /api/shares/{token} with encoding', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(null, { status: 204 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.revokeShare('share token/1')
    expect(calls[0].method).toBe('DELETE')
    expect(calls[0].url).toBe('http://x/api/shares/share%20token%2F1')
  })

  it('permanentDeleteNote DELETEs /api/notes/{id}/permanent', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.permanentDeleteNote('note-1')
    expect(calls[0].method).toBe('DELETE')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/permanent')
  })

  it('listTrashedFolders GETs /api/folders/trash', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.listTrashedFolders()
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/folders/trash')
  })

  it('restoreFolder POSTs /api/folders/{id}/restore', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.restoreFolder('folder-1')
    expect(calls[0].method).toBe('POST')
    expect(calls[0].url).toBe('http://x/api/folders/folder-1/restore')
  })

  it('permanentDeleteFolder DELETEs /api/folders/{id}/permanent', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.permanentDeleteFolder('folder-1')
    expect(calls[0].method).toBe('DELETE')
    expect(calls[0].url).toBe('http://x/api/folders/folder-1/permanent')
  })

  it('reorderNoteInFolder PUTs the folder-note reorder endpoint with after_id', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), init })
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.reorderNoteInFolder('folder-1', 'note-1', 'note-0')
    expect(calls[0].init?.method).toBe('PUT')
    expect(calls[0].url).toBe('http://x/api/folders/folder-1/notes/note-1/reorder')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ after_id: 'note-0' })
  })

  it('listTrashedSmartLists GETs /api/smart-lists/trash', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify([]), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.listTrashedSmartLists()
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/smart-lists/trash')
  })

  it('restoreSmartList POSTs /api/smart-lists/{id}/restore', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.restoreSmartList('list-1')
    expect(calls[0].method).toBe('POST')
    expect(calls[0].url).toBe('http://x/api/smart-lists/list-1/restore')
  })

  it('permanentDeleteSmartList DELETEs /api/smart-lists/{id}/permanent', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.permanentDeleteSmartList('list-1')
    expect(calls[0].method).toBe('DELETE')
    expect(calls[0].url).toBe('http://x/api/smart-lists/list-1/permanent')
  })

  it('resummarize POSTs to /api/notes/{id}/resummarize with no body', async () => {
    const calls: Array<{ url: string; method?: string; body?: string | null }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body as string | null ?? null })
      return new Response('', { status: 202 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.resummarize('note-1')
    expect(calls[0].method).toBe('POST')
    expect(calls[0].url).toBe('http://x/api/notes/note-1/resummarize')
    expect(calls[0].body).toBeNull()
  })

  it('addTag POSTs and removeTag DELETEs (by name, encoded)', async () => {
    const calls: Array<{ url: string; method?: string; body?: unknown }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      return new Response(JSON.stringify({ id: 't1', name: '1on1' }), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })

    const tag = await client.addTag('n1', '1on1')
    expect(tag).toEqual({ id: 't1', name: '1on1' })
    expect(calls[0]).toMatchObject({ url: 'http://x/api/notes/n1/tags', method: 'POST', body: { name: '1on1' } })

    await client.removeTag('n1', '1on1')
    expect(calls[1].method).toBe('DELETE')
    expect(calls[1].url).toBe('http://x/api/notes/n1/tags?name=1on1')

    await client.removeTag('n1', 'Q&A meeting')
    const last = calls[calls.length - 1]
    expect(last.method).toBe('DELETE')
    expect(last.url).toBe('http://x/api/notes/n1/tags?name=Q%26A%20meeting')
  })

  it('listTags GETs /api/tags and returns name/count pairs', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify([{ name: 'alpha', count: 2 }]), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const tags = await client.listTags()
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/tags')
    expect(tags).toEqual([{ name: 'alpha', count: 2 }])
  })

  it('smart-list CRUD hits the right endpoints', async () => {
    const calls: Array<{ url: string; method?: string; body?: unknown }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      return new Response(JSON.stringify({ id: 'l1', name: 'X', rule: { op: 'and', children: [] }, created_at: '' }), { status: 200 })
    }
    const c = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const rule = { op: 'and' as const, children: [] }
    await c.createSmartList('X', rule)
    await c.listSmartLists()
    await c.updateSmartList('l1', 'Y', rule)
    await c.deleteSmartList('l1')
    expect(calls[0]).toMatchObject({ url: 'http://x/api/smart-lists', method: 'POST', body: { name: 'X', rule } })
    expect(calls[1]).toMatchObject({ url: 'http://x/api/smart-lists', method: 'GET' })
    expect(calls[2]).toMatchObject({ url: 'http://x/api/smart-lists/l1', method: 'PUT', body: { name: 'Y', rule } })
    expect(calls[3]).toMatchObject({ url: 'http://x/api/smart-lists/l1', method: 'DELETE' })
  })

  it('folder CRUD hits the right endpoints', async () => {
    const calls: Array<{ url: string; method?: string; body?: unknown }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      const path = new URL(String(url)).pathname
      const body =
        init?.method === 'PUT' && path === '/api/folders/f1'
          ? { id: 'f1', name: 'Accounts', created_at: '2026-07-10T00:00:00Z' }
          : { id: 'f1', name: 'Clients', created_at: '2026-07-10T00:00:00Z' }
      return new Response(JSON.stringify(body), { status: 200 })
    }
    const c = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await c.listFolders()
    await c.createFolder('Clients')
    const updated = await c.updateFolder('f1', 'Accounts')
    await c.deleteFolder('f1')
    await c.addNoteFolder('n1', 'f1')
    await c.removeNoteFolder('n1', 'f1')
    expect(calls[0]).toMatchObject({ url: 'http://x/api/folders', method: 'GET' })
    expect(calls[1]).toMatchObject({ url: 'http://x/api/folders', method: 'POST', body: { name: 'Clients', parent_id: null } })
    expect(calls[2]).toMatchObject({ url: 'http://x/api/folders/f1', method: 'PUT', body: { name: 'Accounts', parent_id: null } })
    expect(calls[3]).toMatchObject({ url: 'http://x/api/folders/f1', method: 'DELETE' })
    expect(calls[4]).toMatchObject({ url: 'http://x/api/notes/n1/folders', method: 'POST', body: { folder_id: 'f1' } })
    expect(calls[5]).toMatchObject({ url: 'http://x/api/notes/n1/folders/f1', method: 'DELETE' })
    expect(updated).toMatchObject({ id: 'f1', name: 'Accounts', created_at: '2026-07-10T00:00:00Z' })

    // createFolder with a parentId sends parent_id in the request body
    await c.createFolder('X', 'p1')
    const withParent = calls[calls.length - 1]
    expect(withParent).toMatchObject({ url: 'http://x/api/folders', method: 'POST', body: { name: 'X', parent_id: 'p1' } })
  })

  it('reorderFolder PUTs /api/folders/{id}/reorder with after_id (sibling and null)', async () => {
    const calls: Array<{ url: string; method?: string; body?: unknown }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    }
    const c = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await c.reorderFolder('f1', 'f0')
    expect(calls[0]).toMatchObject({ url: 'http://x/api/folders/f1/reorder', method: 'PUT', body: { after_id: 'f0' } })
    await c.reorderFolder('f1', null)
    expect(calls[1]).toMatchObject({ url: 'http://x/api/folders/f1/reorder', method: 'PUT', body: { after_id: null } })
  })

  it('template CRUD hits the right endpoints', async () => {
    const calls: Array<{ url: string; method?: string; body?: unknown }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      return new Response(JSON.stringify({ id: 't1', name: 'Default', sections: [], built_in: false }), { status: 200 })
    }
    const c = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const sections = [{ heading: 'Overview', instruction: 'Summarise' }]
    await c.listTemplates()
    await c.createTemplate('Default', sections)
    await c.updateTemplate('t1', 'Renamed', sections)
    await c.deleteTemplate('t1')
    expect(calls[0]).toMatchObject({ url: 'http://x/api/templates', method: 'GET' })
    expect(calls[1]).toMatchObject({ url: 'http://x/api/templates', method: 'POST', body: { name: 'Default', sections } })
    expect(calls[2]).toMatchObject({ url: 'http://x/api/templates/t1', method: 'PUT', body: { name: 'Renamed', sections } })
    expect(calls[3]).toMatchObject({ url: 'http://x/api/templates/t1', method: 'DELETE' })
  })

  it('search GETs /api/search?q= with the query URL-encoded', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response(JSON.stringify([
        { note_id: 'id1', match_type: 'title' },
        { note_id: 'id2', match_type: 'summary', snippet: 'a b in context' },
      ]), { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    const matches = await client.search('a b')
    expect(matches).toEqual([
      { note_id: 'id1', match_type: 'title' },
      { note_id: 'id2', match_type: 'summary', snippet: 'a b in context' },
    ])
    expect(calls[0].method).toBe('GET')
    expect(calls[0].url).toBe('http://x/api/search?q=a+b')
  })

  it('search appends from/to onto the querystring when present, encoded', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('[]', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.search('a b', { from: '2024-01-01T00:00:00Z', to: '2024-02-01' })
    expect(calls[0].url).toBe('http://x/api/search?q=a+b&from=2024-01-01T00%3A00%3A00Z&to=2024-02-01')
  })

  it('search omits from/to from the querystring when absent', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('[]', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.search('a b')
    expect(calls[0].url).toBe('http://x/api/search?q=a+b')
  })

  it('search includes person/folder/tag filters when present, encoded', async () => {
    const calls: Array<{ url: string; method?: string }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method })
      return new Response('[]', { status: 200 })
    }
    const client = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await client.search('budget review', {
      from: '2024-01-01',
      to: '2024-02-01T00:00:00Z',
      personId: 'p-123',
      folderId: 'f-123',
      tag: 'weekly sync',
    })
    expect(calls[0].url).toBe(
      'http://x/api/search?q=budget+review&from=2024-01-01&to=2024-02-01T00%3A00%3A00Z&person_id=p-123&folder_id=f-123&tag=weekly+sync',
    )
  })

  it('getFull returns transcript + summaries once ready', async () => {
    server.seedUser('o@example.com', 'password123')
    const session = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).login(
      'o@example.com',
      'password123',
    )
    const appToken = await new MuesliClient({ baseUrl: BASE, fetch: server.fetch }).createToken(
      'desktop',
      session,
    )
    const c = new MuesliClient({ baseUrl: BASE, fetch: server.fetch, token: appToken })
    const note = await c.createNote('M')

    let full = await c.getFull(note.id)
    // Before ready, the server returns transcript: null — clients must tolerate it.
    expect(full.transcript).toBeNull()
    expect(full.summaries).toHaveLength(0)

    server.setStatus(note.id, 'ready')
    full = await c.getFull(note.id)
    expect(full.note.status).toBe('ready')
    expect(full.transcript?.segments[0].text).toBe('hello world')
    expect(full.summaries[0].sections[0].heading).toBe('Overview')
  })

  it('chat CRUD hits the right endpoints (list/create/get/delete/messages/send)', async () => {
    const calls: Array<{ url: string; method?: string; body?: unknown }> = []
    const fetchMock: FetchLike = async (url, init) => {
      calls.push({ url: String(url), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      return new Response(JSON.stringify({ id: 'c1', title: 'Q&A', created_at: '', updated_at: '' }), { status: 200 })
    }
    const c = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await c.listConversations()
    await c.listConversations('note-1')
    await c.createConversation({ title: 'Q&A', note_id: 'note-1', content: 'Hi' })
    await c.getConversation('c1')
    await c.deleteConversation('c1')
    await c.listMessages('c1')
    await c.sendMessage('c1', { content: 'Hello' })

    expect(calls[0]).toMatchObject({ url: 'http://x/api/conversations', method: 'GET' })
    expect(calls[1]).toMatchObject({ url: 'http://x/api/conversations?note_id=note-1', method: 'GET' })
    expect(calls[2]).toMatchObject({ url: 'http://x/api/conversations', method: 'POST', body: { title: 'Q&A', note_id: 'note-1', content: 'Hi' } })
    expect(calls[3]).toMatchObject({ url: 'http://x/api/conversations/c1', method: 'GET' })
    expect(calls[4]).toMatchObject({ url: 'http://x/api/conversations/c1', method: 'DELETE' })
    expect(calls[5]).toMatchObject({ url: 'http://x/api/conversations/c1/messages', method: 'GET' })
    expect(calls[6]).toMatchObject({ url: 'http://x/api/conversations/c1/messages', method: 'POST', body: { content: 'Hello' } })
  })

  it('sendMessage rejects with ApiError(409) on an in-flight send guard', async () => {
    const fetchMock: FetchLike = async () =>
      new Response(JSON.stringify({ error: 'message send already in progress' }), { status: 409 })
    const c = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await expect(c.sendMessage('c1', { content: 'hi' })).rejects.toMatchObject({
      status: 409,
      message: 'message send already in progress',
    })
  })

  it('sendMessage rejects with ApiError(500) on a generic/plugin failure', async () => {
    const fetchMock: FetchLike = async () =>
      new Response(JSON.stringify({ error: 'internal error' }), { status: 500 })
    const c = new MuesliClient({ baseUrl: 'http://x', token: 't', fetch: fetchMock })
    await expect(c.sendMessage('c1', { content: 'hi' })).rejects.toMatchObject({ status: 500, message: 'internal error' })
  })
})
