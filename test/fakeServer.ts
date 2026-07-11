import type { FullNote, Note, NoteStatus, UploadGrant } from '../src/shared/types'

interface StoredNote extends Note {
  body: string
  audioKey?: string
  audioBytes?: Uint8Array
}

// FakeServer implements the subset of the Muesli API the client uses, as an
// injectable `fetch`. It is deterministic and lets tests advance note status.
export class FakeServer {
  private users = new Map<string, string>() // email -> password
  private appTokens = new Set<string>()
  private sessionTokens = new Set<string>()
  private notes = new Map<string, StoredNote>()
  private seq = 0
  public lastBodyPut?: { id: string; content: string }

  // Pre-seed an account so login tests don't need setup first.
  seedUser(email: string, password: string) {
    this.users.set(email, password)
  }

  // Force a note into a given status (simulates the server pipeline).
  setStatus(id: string, status: NoteStatus) {
    const n = this.notes.get(id)
    if (n) n.status = status
  }

  fetch = async (input: string | URL, init?: RequestInit): Promise<Response> => {
    const url = new URL(typeof input === 'string' ? input : input.toString())
    const path = url.pathname
    const method = (init?.method ?? 'GET').toUpperCase()
    // Normalize headers via Headers so a Headers object or a plain record both work.
    const headers = new Headers(init?.headers)
    const auth = headers.get('Authorization') ?? undefined
    const bearer = auth?.startsWith('Bearer ') ? auth.slice(7) : undefined
    const body = init?.body

    const json = (status: number, obj: unknown) =>
      new Response(JSON.stringify(obj), {
        status,
        headers: { 'Content-Type': 'application/json' },
      })

    // --- Signed storage PUT (no bearer; key is in the path) ---
    if (method === 'PUT' && path.startsWith('/_storage/')) {
      const key = path.slice('/_storage/'.length)
      const note = [...this.notes.values()].find((n) => n.audioKey === key)
      if (!note) return new Response('no such object', { status: 403 })
      note.audioBytes = new Uint8Array(body as ArrayBuffer)
      return new Response('', { status: 200 })
    }

    if (method === 'POST' && path === '/api/setup') {
      const { email, password } = JSON.parse(body as string)
      if (this.users.size > 0) return json(409, { error: 'exists' })
      this.users.set(email, password)
      return json(201, { id: 'user-1', email })
    }

    if (method === 'POST' && path === '/api/login') {
      const { email, password } = JSON.parse(body as string)
      if (this.users.get(email) !== password) return json(401, { error: 'bad creds' })
      const token = `session-${++this.seq}`
      this.sessionTokens.add(token)
      return json(200, { token })
    }

    if (method === 'POST' && path === '/api/tokens') {
      if (!bearer || !this.sessionTokens.has(bearer)) return json(401, { error: 'unauthorized' })
      const token = `app-${++this.seq}`
      this.appTokens.add(token)
      return json(201, { token })
    }

    // Everything below requires a valid app token.
    const authed = bearer && this.appTokens.has(bearer)

    if (method === 'POST' && path === '/api/notes') {
      if (!authed) return json(401, { error: 'unauthorized' })
      const { title } = JSON.parse(body as string)
      const id = `note-${++this.seq}`
      const now = new Date().toISOString()
      this.notes.set(id, {
        id,
        title: title ?? '',
        status: 'recording',
        pinned: false,
        body: '',
        created_at: now,
        updated_at: now,
        partial_transcript: false,
      })
      return json(201, this.publicNote(id))
    }

    if (method === 'GET' && path === '/api/notes') {
      if (!authed) return json(401, { error: 'unauthorized' })
      return json(
        200,
        [...this.notes.values()]
          .sort((a, b) => {
            if (a.pinned !== b.pinned) return Number(b.pinned) - Number(a.pinned)
            return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
          })
          .map((note) => this.publicNote(note.id))
      )
    }

    const idMatch = path.match(/^\/api\/notes\/([^/]+)(\/[a-z-]+)?$/)
    if (idMatch) {
      if (!authed) return json(401, { error: 'unauthorized' })
      const id = idMatch[1]
      const sub = idMatch[2]
      const note = this.notes.get(id)
      if (!note) return json(404, { error: 'not found' })

      if (!sub && method === 'GET') return json(200, this.publicNote(id))

      if (!sub && method === 'PATCH') {
        const { title } = JSON.parse(body as string)
        if (typeof title === 'string') note.title = title
        return json(200, this.publicNote(id))
      }

      if (sub === '/pin' && method === 'POST') {
        note.pinned = true
        return json(200, { status: 'ok' })
      }

      if (sub === '/pin' && method === 'DELETE') {
        note.pinned = false
        return json(200, { status: 'ok' })
      }

      if (sub === '/audio-upload-url' && method === 'POST') {
        const key = `notes/${id}/audio/${++this.seq}`
        note.audioKey = key
        const grant: UploadGrant = {
          url: `http://fake/_storage/${key}`,
          method: 'PUT',
          key,
          expires_at: new Date(Date.now() + 900_000).toISOString(),
        }
        return json(200, grant)
      }

      if (sub === '/audio-url' && method === 'GET') {
        if (!note.audioKey) return json(404, { error: 'not found' })
        return json(200, {
          url: `http://fake/_storage/${note.audioKey}`,
          expires_at: new Date(Date.now() + 3_600_000).toISOString(),
        })
      }

      if (sub === '/audio-uploaded' && method === 'POST') {
        const { key } = JSON.parse(body as string)
        if (key !== note.audioKey || !note.audioBytes?.length) {
          return json(400, { error: 'object not found or empty' })
        }
        note.status = 'uploaded'
        return json(200, { status: 'uploaded' })
      }

      if (sub === '/body' && method === 'PUT') {
        const { content } = JSON.parse(body as string)
        note.body = content
        this.lastBodyPut = { id, content }
        return json(200, { note_id: id })
      }

      if (sub === '/full' && method === 'GET') {
        const full: FullNote = {
          note: this.publicNote(id),
          body_markdown: note.body,
          // Mirror the real server: transcript is null until the note is ready.
          transcript:
            note.status === 'ready'
              ? {
                  segments: [{ start_ms: 0, end_ms: 1000, text: 'hello world', source: 'mixed' }],
                }
              : null,
          summaries:
            note.status === 'ready'
              ? [
                  {
                    template_name: 'General meeting',
                    status: 'ready',
                    sections: [{ heading: 'Overview', content_markdown: 'A summary.' }],
                  },
                ]
              : [],
        }
        return json(200, full)
      }
    }

    return new Response('not found', { status: 404 })
  }

  private publicNote(id: string): Note {
    const n = this.notes.get(id)!
    return {
      id: n.id,
      title: n.title,
      status: n.status,
      pinned: n.pinned ?? false,
      created_at: n.created_at,
      updated_at: n.updated_at,
      partial_transcript: n.partial_transcript ?? false,
    }
  }
}
