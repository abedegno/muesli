import { createHash, randomUUID } from 'node:crypto'
import { mkdir, open, readdir, readFile, stat, unlink } from 'node:fs/promises'
import type { FileHandle } from 'node:fs/promises'
import { join } from 'node:path'
import type { RecordingRecoveryInfo, ServerConfig } from '../shared/types'
import { PCM_FRAME_BYTES, PCM_TARGET_SAMPLE_RATE } from '../shared/pcm'

interface Metadata {
  captureId: string
  noteId: string
  scope: string
  createdAt: string
}
interface Active {
  meta: Metadata
  file: FileHandle
  bytes: number
  synced: number
  failed: boolean
}
const MAX_BYTES = 256 * 1024 * 1024
const MAX_TOTAL = 512 * 1024 * 1024
const UUID = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i

export function waveHeader(bytes: number): Buffer {
  const h = Buffer.alloc(44)
  h.write('RIFF', 0)
  h.writeUInt32LE(bytes + 36, 4)
  h.write('WAVEfmt ', 8)
  h.writeUInt32LE(16, 16)
  h.writeUInt16LE(1, 20)
  h.writeUInt16LE(1, 22)
  h.writeUInt32LE(PCM_TARGET_SAMPLE_RATE, 24)
  h.writeUInt32LE(PCM_TARGET_SAMPLE_RATE * 2, 28)
  h.writeUInt16LE(2, 32)
  h.writeUInt16LE(16, 34)
  h.write('data', 36)
  h.writeUInt32LE(bytes, 40)
  return h
}
export function recordingScope(config: ServerConfig): string {
  return createHash('sha256')
    .update(
      `${config.manualServer === false ? 'embedded-profile' : config.serverUrl.replace(/\/+$/, '')}\0${config.userId ?? config.token}`
    )
    .digest('hex')
}

// One writer per desktop profile. PCM comes from the same mixed microphone /
// system-audio encoder as live transcription, independently of whether live text
// is enabled. Files contain no token and are never sent without an explicit retry.
export class RecordingJournal {
  private active: Active | null = null
  private tail: Promise<unknown> = Promise.resolve()
  private queued = 0
  private usedBytes = 0
  constructor(private readonly directory: string) {}
  isActive(): boolean {
    return this.active !== null
  }
  private operation<T>(fn: () => Promise<T>, cleanup = false): Promise<T> {
    if (this.queued >= 16 && !cleanup) {
      if (this.active) this.active.failed = true
      return Promise.reject(
        new Error('Recording recovery cannot keep up. Stop and save this recording.')
      )
    }
    this.queued++
    const next = this.tail.then(fn).finally(() => {
      this.queued--
    })
    this.tail = next.catch(() => {})
    return next
  }
  private path(id: string, ext: string): string {
    if (!UUID.test(id)) throw new Error('Invalid recording capture identifier.')
    return join(this.directory, `${id}.${ext}`)
  }
  private async entries(): Promise<Array<{ meta: Metadata; bytes: number }>> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 })
    const result: Array<{ meta: Metadata; bytes: number }> = []
    for (const name of (await readdir(this.directory))
      .filter((n) => UUID.test(n.replace(/\.json$/, '')) && n.endsWith('.json'))
      .slice(0, 10)) {
      try {
        const path = join(this.directory, name)
        if ((await stat(path)).size > 4096) continue
        const meta = JSON.parse(await readFile(path, 'utf8')) as Metadata
        if (
          !UUID.test(meta.captureId) ||
          name !== `${meta.captureId}.json` ||
          typeof meta.noteId !== 'string' ||
          typeof meta.scope !== 'string' ||
          typeof meta.createdAt !== 'string'
        )
          continue
        const bytes = (await stat(this.path(meta.captureId, 'pcm'))).size
        if (bytes === 0 && meta.captureId !== this.active?.meta.captureId) {
          await unlink(this.path(meta.captureId, 'pcm'))
          await unlink(path)
          continue
        }
        if (bytes <= MAX_BYTES) result.push({ meta, bytes })
      } catch {
        /* Keep damaged data for manual recovery; never upload it automatically. */
      }
    }
    return result
  }
  begin(config: ServerConfig, noteId: string): Promise<string> {
    return this.operation(async () => {
      if (this.active) throw new Error('Another recording is already active on this device.')
      const entries = await this.entries()
      this.usedBytes = entries.reduce((n, e) => n + e.bytes, 0)
      if (
        (await readdir(this.directory)).filter((n) => n.endsWith('.json')).length >= 10 ||
        entries.reduce((n, e) => n + e.bytes, 0) >= MAX_TOTAL
      )
        throw new Error(
          'Recording recovery storage is full. Recover or discard pending recordings first.'
        )
      const captureId = randomUUID(),
        meta: Metadata = {
          captureId,
          noteId,
          scope: recordingScope(config),
          createdAt: new Date().toISOString(),
        }
      const metadata = await open(this.path(captureId, 'json'), 'wx', 0o600)
      try {
        await metadata.writeFile(JSON.stringify(meta))
        await metadata.sync()
      } finally {
        await metadata.close()
      }
      const file = await open(this.path(captureId, 'pcm'), 'wx', 0o600).catch(async (error) => {
        await unlink(this.path(captureId, 'json')).catch(() => {})
        throw error
      })
      this.active = { meta, file, bytes: 0, synced: 0, failed: false }
      return captureId
    })
  }
  append(captureId: string, frame: ArrayBuffer): Promise<void> {
    if (frame.byteLength > PCM_FRAME_BYTES || frame.byteLength % 2 !== 0)
      return Promise.reject(new Error('Invalid PCM recording frame.'))
    const copy = Buffer.from(new Uint8Array(frame))
    return this.operation(async () => {
      const active = this.active
      if (!active || active.meta.captureId !== captureId)
        throw new Error('Recording session is no longer current.')
      if (active.failed)
        throw new Error('Recording recovery stopped. Stop and save this recording.')
      try {
        if (active.bytes + copy.length > MAX_BYTES || this.usedBytes + copy.length > MAX_TOTAL)
          throw new Error('Recording recovery storage is full. Stop and save this recording.')
        let offset = 0
        while (offset < copy.length) {
          const { bytesWritten } = await active.file.write(copy, offset, copy.length - offset, null)
          if (!bytesWritten) throw new Error('Could not write recording audio.')
          offset += bytesWritten
        }
        active.bytes += copy.length
        this.usedBytes += copy.length
        if (active.bytes - active.synced >= PCM_TARGET_SAMPLE_RATE * 2) {
          await active.file.datasync()
          active.synced = active.bytes
        }
      } catch (error) {
        active.failed = true
        throw error
      }
    })
  }
  end(captureId: string): Promise<boolean> {
    return this.operation(async () => {
      if (this.active?.meta.captureId !== captureId) return true
      const active = this.active
      this.active = null
      try {
        await active.file.sync()
      } finally {
        await active.file.close()
      }
      if (!active.bytes) {
        await unlink(this.path(captureId, 'pcm'))
        await unlink(this.path(captureId, 'json'))
      }
      return active.bytes > 0
    }, true)
  }
  async close(): Promise<void> {
    if (this.active) await this.end(this.active.meta.captureId)
  }
  async list(config: ServerConfig): Promise<RecordingRecoveryInfo[]> {
    return (await this.entries())
      .filter(
        (e) =>
          e.meta.scope === recordingScope(config) &&
          e.meta.captureId !== this.active?.meta.captureId &&
          e.bytes >= 2
      )
      .map(({ meta, bytes }) => ({
        noteId: meta.noteId,
        mimeType: 'audio/wav',
        byteLength: 44 + bytes - (bytes % 2),
        createdAt: meta.createdAt,
      }))
  }
  async recover(
    config: ServerConfig,
    noteId: string
  ): Promise<{ audio: Uint8Array; captureId: string } | null> {
    await this.tail
    const entry = (await this.entries())
      .filter(
        (e) =>
          e.meta.scope === recordingScope(config) &&
          e.meta.noteId === noteId &&
          e.meta.captureId !== this.active?.meta.captureId &&
          e.bytes >= 2
      )
      .sort((a, b) => b.meta.createdAt.localeCompare(a.meta.createdAt))[0]
    if (!entry) return null
    const pcm = await readFile(this.path(entry.meta.captureId, 'pcm'))
    const length = pcm.length - (pcm.length % 2)
    return {
      audio: Buffer.concat([waveHeader(length), pcm.subarray(0, length)]),
      captureId: entry.meta.captureId,
    }
  }
  discard(config: ServerConfig, noteId: string, captureId?: string): Promise<void> {
    return this.operation(async () => {
      for (const { meta, bytes } of await this.entries())
        if (
          meta.scope === recordingScope(config) &&
          meta.noteId === noteId &&
          (!captureId || meta.captureId === captureId) &&
          meta.captureId !== this.active?.meta.captureId
        ) {
          await unlink(this.path(meta.captureId, 'pcm'))
          await unlink(this.path(meta.captureId, 'json'))
          this.usedBytes = Math.max(0, this.usedBytes - bytes)
        }
    })
  }
}
