import WebSocket, { type RawData } from 'ws'
import type { NoteStreamEvent } from '../shared/ipc'
import type { ServerConfig } from '../shared/types'

interface NoteStreamRelayDeps {
  getConfig: () => ServerConfig | null
  emit: (event: NoteStreamEvent) => void
}

type StreamStatus = 'idle' | 'connecting' | 'loading' | 'live' | 'unavailable' | 'dropped'

class ActiveStream {
  private readonly pendingFrames: ArrayBuffer[] = []
  private ws: WebSocket | null = null
  private status: StreamStatus = 'idle'
  private closedByClient = false

  constructor(
    readonly noteId: string,
    private readonly cfg: ServerConfig,
    private readonly emit: (event: NoteStreamEvent) => void,
  ) {}

  start(): void {
    this.status = 'connecting'
    this.emit({ noteId: this.noteId, type: 'connecting' })

    const url = new URL(`/api/notes/${this.noteId}/stream`, this.cfg.serverUrl)
    this.ws = new WebSocket(url.toString(), {
      headers: {
        Authorization: `Bearer ${this.cfg.token}`,
      },
    })

    this.ws.on('open', () => {
      if (this.closedByClient) {
        this.ws?.close()
        return
      }
      this.status = 'live'
      this.emit({ noteId: this.noteId, type: 'live' })
      while (this.pendingFrames.length > 0 && this.ws?.readyState === WebSocket.OPEN) {
        const frame = this.pendingFrames.shift()
        if (frame) this.ws.send(Buffer.from(new Uint8Array(frame)))
      }
    })

    this.ws.on('message', (data: RawData) => {
      const text = rawDataToBuffer(data).toString('utf8')
      let payload: unknown
      try {
        payload = JSON.parse(text)
      } catch {
        return
      }
      if (!payload || typeof payload !== 'object') return
      const message = payload as { type?: string; text?: string; start_ms?: number; end_ms?: number; speaker?: string | null; provisional?: boolean; final?: boolean; stream_id?: string; dropped_duration_ms?: number }
      if (message.type === 'unavailable') {
        this.status = 'unavailable'
        this.pendingFrames.length = 0
        this.emit({ noteId: this.noteId, type: 'unavailable' })
        this.close()
        return
      }
      if (message.type === 'loading') {
        this.status = 'loading'
        this.emit({ noteId: this.noteId, type: 'loading' })
        return
      }
      if (message.type === 'ready') {
        this.status = 'live'
        this.emit({ noteId: this.noteId, type: 'live' })
        return
      }
      if (message.type === 'gap') {
        this.emit({
          noteId: this.noteId,
          type: 'gap',
          stream_id: message.stream_id ?? '',
          dropped_duration_ms: message.dropped_duration_ms ?? 0,
        })
        return
      }
      if (message.type === 'segment') {
        this.emit({
          noteId: this.noteId,
          type: 'segment',
          text: message.text ?? '',
          start_ms: message.start_ms ?? 0,
          end_ms: message.end_ms ?? 0,
          speaker: message.speaker ?? null,
          provisional: true,
          final: message.final ?? true,
        })
      }
    })

    this.ws.on('close', () => {
      if (this.closedByClient || this.status === 'unavailable') return
      if (this.status !== 'dropped') {
        this.status = 'dropped'
        this.pendingFrames.length = 0
        this.emit({ noteId: this.noteId, type: 'dropped' })
      }
    })

    this.ws.on('error', () => {
      if (this.closedByClient || this.status === 'unavailable') return
      this.status = 'dropped'
      this.pendingFrames.length = 0
      this.emit({ noteId: this.noteId, type: 'dropped' })
    })
  }

  sendFrame(audio: ArrayBuffer): void {
    if (this.status === 'unavailable' || this.status === 'dropped') return
    const copy = audio.slice(0)
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(Buffer.from(new Uint8Array(copy)))
      return
    }
    this.pendingFrames.push(copy)
  }

  close(): void {
    this.stop()
  }

  stop(): void {
    this.closedByClient = true
    this.pendingFrames.length = 0
    this.ws?.close()
  }
}

function rawDataToBuffer(data: RawData): Buffer {
  if (typeof data === 'string') return Buffer.from(data)
  if (Array.isArray(data)) return Buffer.concat(data)
  if (data instanceof Buffer) return data
  return Buffer.from(new Uint8Array(data))
}

export class NoteStreamRelay {
  private active: ActiveStream | null = null

  constructor(private readonly deps: NoteStreamRelayDeps) {}

  start(noteId: string): void {
    this.active?.stop()
    this.active = null
    const cfg = this.deps.getConfig()
    if (!cfg) {
      this.deps.emit({ noteId, type: 'dropped' })
      return
    }
    const active = new ActiveStream(noteId, cfg, this.deps.emit)
    this.active = active
    active.start()
  }

  sendAudio(noteId: string, audio: ArrayBuffer): void {
    if (!this.active || this.activeNoteId() !== noteId) return
    this.active.sendFrame(audio)
  }

  stop(noteId: string): void {
    if (!this.active || this.activeNoteId() !== noteId) return
    this.active.stop()
    this.active = null
  }

  private activeNoteId(): string | null {
    return this.active ? this.active.noteId : null
  }
}
