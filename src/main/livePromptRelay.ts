import type { LivePromptsEvent, LivePromptsItem } from '../shared/ipc'
import type { ServerConfig } from '../shared/types'

interface LivePromptRelayDeps {
  getConfig: () => ServerConfig | null
  emit: (event: LivePromptsEvent) => void
  /** Injectable for tests; defaults to the global fetch (Node 18+/Electron main). */
  fetchImpl?: typeof fetch
}

/**
 * A bounded Server-Sent-Events frame parser: accumulates chunks (which may
 * split a frame, or even a multi-byte UTF-8 character, at an arbitrary byte
 * boundary) and calls onFrame once per complete `event:`/`data:` block. A
 * malformed frame (no `event:` line) is simply dropped rather than crashing
 * the parser, so one bad frame never takes down the rest of the stream.
 */
export class SSEFrameParser {
  private readonly decoder = new TextDecoder('utf-8')
  private buffer = ''

  constructor(private readonly onFrame: (frame: { event: string; data: string }) => void) {}

  push(chunk: Uint8Array): void {
    this.buffer += this.decoder.decode(chunk, { stream: true })
    this.drain()
  }

  /** Flushes any complete frame(s) currently buffered, without requiring more input. */
  private drain(): void {
    for (;;) {
      const boundary = this.buffer.indexOf('\n\n')
      if (boundary === -1) return
      const raw = this.buffer.slice(0, boundary)
      this.buffer = this.buffer.slice(boundary + 2)
      this.parseFrame(raw)
    }
  }

  private parseFrame(raw: string): void {
    let event = ''
    const dataLines: string[] = []
    for (const line of raw.split('\n')) {
      if (line.startsWith('event: ')) event = line.slice('event: '.length)
      else if (line.startsWith('data: ')) dataLines.push(line.slice('data: '.length))
    }
    if (!event) return // malformed: isolate and drop, never throw
    this.onFrame({ event, data: dataLines.join('\n') })
  }
}

const initialBackoffMs = 1000
const maxBackoffMs = 30_000

class ActiveLivePromptStream {
  private closedByClient = false
  private abortController: AbortController | null = null
  private backoffMs = initialBackoffMs
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private sent = new Map<string, number>() // "streamId:templateId" -> event_version

  constructor(
    readonly noteId: string,
    private readonly cfg: ServerConfig,
    private readonly emit: (event: LivePromptsEvent) => void,
    private readonly fetchImpl: typeof fetch,
  ) {}

  start(): void {
    void this.connect()
  }

  private async connect(): Promise<void> {
    if (this.closedByClient) return
    this.emit({ noteId: this.noteId, type: 'connecting' })

    const url = new URL(`/api/notes/${this.noteId}/live-prompts`, this.cfg.serverUrl)
    const controller = new AbortController()
    this.abortController = controller

    let resp: Response
    try {
      resp = await this.fetchImpl(url.toString(), {
        headers: { Authorization: `Bearer ${this.cfg.token}` },
        signal: controller.signal,
      })
    } catch {
      this.scheduleReconnect()
      return
    }
    if (this.closedByClient) return
    if (!resp.ok || !resp.body) {
      this.scheduleReconnect()
      return
    }

    this.backoffMs = initialBackoffMs
    this.emit({ noteId: this.noteId, type: 'live' })

    const parser = new SSEFrameParser((frame) => this.handleFrame(frame))
    const reader = resp.body.getReader()
    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        if (value) parser.push(value)
      }
    } catch {
      // aborted or transport error; fall through to reconnect handling below
    }
    if (this.closedByClient) return
    this.emit({ noteId: this.noteId, type: 'dropped' })
    this.scheduleReconnect()
  }

  private handleFrame(frame: { event: string; data: string }): void {
    if (frame.event === 'heartbeat') return
    let payload: unknown
    try {
      payload = JSON.parse(frame.data)
    } catch {
      return // malformed frame body: isolate and drop
    }
    if (!payload || typeof payload !== 'object') return

    if (frame.event === 'snapshot') {
      const snap = payload as { note_id: string; stream_id: string; active: boolean; items: LivePromptsItem[] }
      this.sent = new Map(snap.items.map((item) => [this.key(item.stream_id, item.template_id), item.event_version]))
      this.emit({ noteId: this.noteId, type: 'snapshot', streamId: snap.stream_id, active: snap.active, items: snap.items })
      return
    }
    if (frame.event === 'update') {
      const up = payload as { item: LivePromptsItem }
      const key = this.key(up.item.stream_id, up.item.template_id)
      const prev = this.sent.get(key)
      if (prev !== undefined && up.item.event_version <= prev) return // duplicate/regressing: ignore
      this.sent.set(key, up.item.event_version)
      this.emit({ noteId: this.noteId, type: 'update', item: up.item })
      return
    }
    if (frame.event === 'ended') {
      const ended = payload as { template_id: string; stream_id: string }
      this.sent.delete(this.key(ended.stream_id, ended.template_id))
      this.emit({ noteId: this.noteId, type: 'ended', templateId: ended.template_id, streamId: ended.stream_id })
    }
  }

  private key(streamId: string, templateId: string): string {
    return `${streamId}:${templateId}`
  }

  private scheduleReconnect(): void {
    if (this.closedByClient) return
    const delay = this.backoffMs
    this.backoffMs = Math.min(this.backoffMs * 2, maxBackoffMs)
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      void this.connect()
    }, delay)
  }

  stop(): void {
    this.closedByClient = true
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.abortController?.abort()
  }
}

/**
 * Electron main owns the authenticated live-prompts HTTP stream and relays
 * typed events through preload IPC (issue #764) -- mirroring
 * `NoteStreamRelay`'s ownership of the audio WebSocket. Reconnects with
 * capped exponential backoff only between `start` and `stop`/note-change
 * (i.e. only while the meeting is active, per the accepted spec), aborting
 * the HTTP request on stop so the server releases its subscriber lease
 * promptly rather than waiting out its lease TTL.
 */
export class LivePromptRelay {
  private active: ActiveLivePromptStream | null = null

  constructor(private readonly deps: LivePromptRelayDeps) {}

  start(noteId: string): void {
    this.active?.stop()
    this.active = null
    const cfg = this.deps.getConfig()
    if (!cfg) {
      this.deps.emit({ noteId, type: 'dropped' })
      return
    }
    const fetchImpl = this.deps.fetchImpl ?? fetch
    const active = new ActiveLivePromptStream(noteId, cfg, this.deps.emit, fetchImpl)
    this.active = active
    active.start()
  }

  stop(noteId: string): void {
    if (!this.active || this.active.noteId !== noteId) return
    this.active.stop()
    this.active = null
  }

  /** Stops whatever is active, regardless of note id -- window destruction. */
  stopAll(): void {
    this.active?.stop()
    this.active = null
  }
}
