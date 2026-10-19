import { createHash, randomUUID } from 'node:crypto'
import { copyFile, mkdir, open, readdir, rename, stat, unlink } from 'node:fs/promises'
import { join } from 'node:path'
import type { RecordingRecoveryInfo, ServerConfig } from '../shared/types'

interface StoredRecording extends RecordingRecoveryInfo {
  uploadKey?: string
  revision: string
  scope: string
}
const MAX_HEADER = 4096
const MAX_RECORDING_BYTES = 256 * 1024 * 1024
const MAX_TOTAL_BYTES = 512 * 1024 * 1024
const MAX_FILES = 10

// One instance per desktop profile. Backups belong to the current server AND
// credential, so switching a hosted account cannot reveal another user's audio.
// Successful upload confirmation removes the matching revision; failed/stopped
// uploads survive navigation and app restarts. In-progress capture is not journalled.
export class RecordingRecoveryStore {
  private tail: Promise<unknown> = Promise.resolve()
  private queued = 0
  private ready: Promise<void> | null = null
  constructor(private readonly directory: string) {}

  private initialize(): Promise<void> {
    if (!this.ready)
      this.ready = (async () => {
        await mkdir(this.directory, { recursive: true, mode: 0o700 })
        // The desktop's profile lock gives this store one process owner. Temps
        // left by an interrupted atomic save are not complete recordings.
        for (const name of await readdir(this.directory)) {
          if (/^[a-f0-9]{64}\.recording\.[a-f0-9-]{36}\.tmp$/.test(name))
            await unlink(join(this.directory, name))
        }
      })().catch((error) => {
        this.ready = null
        throw error
      })
    return this.ready
  }
  private scope(config: ServerConfig): string {
    return createHash('sha256')
      .update(
        `${config.manualServer === false ? 'embedded-profile' : config.serverUrl.replace(/\/+$/, '')}\0${config.userId ?? config.token}`
      )
      .digest('hex')
  }
  private path(config: ServerConfig, noteId: string): string {
    const key = createHash('sha256')
      .update(`${this.scope(config)}\0${noteId}`)
      .digest('hex')
    return join(this.directory, `${key}.recording`)
  }
  private write<T>(operation: () => Promise<T>): Promise<T> {
    if (this.queued >= 16)
      return Promise.reject(new Error('Too many recording operations. Try again.'))
    this.queued++
    const next = this.tail.then(operation).finally(() => {
      this.queued--
    })
    this.tail = next.catch(() => {})
    return next
  }
  private async read(
    config: ServerConfig,
    noteId: string,
    withAudio = false
  ): Promise<{ info: StoredRecording; audio?: Uint8Array } | null> {
    await this.initialize()
    let file
    try {
      file = await open(this.path(config, noteId), 'r')
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') return null
      throw error
    }
    try {
      const prefix = Buffer.alloc(4)
      if ((await file.read(prefix, 0, 4, 0)).bytesRead !== 4)
        throw new Error('Saved recording is incomplete.')
      const length = prefix.readUInt32BE()
      if (length < 1 || length > MAX_HEADER) throw new Error('Saved recording metadata is invalid.')
      const header = Buffer.alloc(length)
      if ((await file.read(header, 0, length, 4)).bytesRead !== length)
        throw new Error('Saved recording is incomplete.')
      const info = JSON.parse(header.toString('utf8')) as StoredRecording
      if (
        info.scope !== this.scope(config) ||
        info.noteId !== noteId ||
        typeof info.revision !== 'string' ||
        !Number.isInteger(info.byteLength) ||
        info.byteLength < 1 ||
        info.byteLength > MAX_RECORDING_BYTES ||
        typeof info.mimeType !== 'string'
      )
        throw new Error('Saved recording metadata is invalid.')
      if ((await file.stat()).size !== 4 + length + info.byteLength)
        throw new Error('Saved recording is incomplete.')
      if (!withAudio) return { info }
      const audio = Buffer.alloc(info.byteLength)
      let offset = 0
      while (offset < audio.length) {
        const { bytesRead } = await file.read(
          audio,
          offset,
          audio.length - offset,
          4 + length + offset
        )
        if (!bytesRead) throw new Error('Saved recording is incomplete.')
        offset += bytesRead
      }
      return { info, audio }
    } finally {
      await file.close()
    }
  }

  private async headers(): Promise<Array<{ path: string; info: StoredRecording }>> {
    await this.initialize()
    const entries: Array<{ path: string; info: StoredRecording }> = []
    for (const name of (await readdir(this.directory))
      .filter((name) => /^[a-f0-9]{64}\.recording$/.test(name))
      .slice(0, MAX_FILES)) {
      const path = join(this.directory, name)
      const file = await open(path, 'r')
      try {
        const prefix = Buffer.alloc(4)
        if ((await file.read(prefix, 0, 4, 0)).bytesRead !== 4) continue
        const length = prefix.readUInt32BE()
        if (length < 1 || length > MAX_HEADER) continue
        const header = Buffer.alloc(length)
        if ((await file.read(header, 0, length, 4)).bytesRead !== length) continue
        try {
          entries.push({ path, info: JSON.parse(header.toString()) as StoredRecording })
        } catch {
          /* A corrupt file is preserved for manual recovery. */
        }
      } finally {
        await file.close()
      }
    }
    return entries
  }
  async list(config: ServerConfig): Promise<RecordingRecoveryInfo[]> {
    const entries = await this.headers()
    const result: RecordingRecoveryInfo[] = []
    for (const { info } of entries)
      if (info.scope === this.scope(config)) {
        try {
          const saved = await this.info(config, info.noteId)
          if (saved) result.push(saved)
        } catch {
          /* Preserve damaged backups; never upload invalid bytes. */
        }
      }
    return result.sort((a, b) => b.createdAt.localeCompare(a.createdAt))
  }
  // Only called after the current credential has authenticated and returned its
  // stable user ID. Migrate that credential's legacy backups before rotating it.
  bindAccount(config: ServerConfig, userId: string): Promise<void> {
    if (config.userId === userId) return Promise.resolve()
    return this.write(async () => {
      const next = { ...config, userId }
      for (const entry of await this.headers()) {
        if (entry.info.scope !== this.scope(config)) continue
        const saved = await this.read(config, entry.info.noteId)
        if (!saved) continue
        const destination = this.path(next, entry.info.noteId)
        const existing = await this.read(next, entry.info.noteId)
        if (existing) {
          if (existing.info.revision !== saved.info.revision)
            throw new Error(
              'An older recording needs recovery before this account can be migrated.'
            )
          await unlink(entry.path)
          continue
        }
        const header = Buffer.from(JSON.stringify({ ...saved.info, scope: this.scope(next) }))
        const temp = destination + '.' + randomUUID() + '.tmp'
        try {
          await copyFile(entry.path, temp)
          const file = await open(temp, 'r+')
          try {
            const prefix = Buffer.alloc(4)
            await file.read(prefix, 0, 4, 0)
            if (prefix.readUInt32BE() !== header.length)
              throw new Error('Could not migrate recording metadata.')
            await file.write(header, 0, header.length, 4)
            await file.sync()
          } finally {
            await file.close()
          }
          await rename(temp, destination)
          await unlink(entry.path)
        } catch (error) {
          await unlink(temp).catch(() => {})
          throw error
        }
      }
    })
  }
  async info(config: ServerConfig, noteId: string): Promise<RecordingRecoveryInfo | null> {
    const saved = await this.read(config, noteId)
    if (!saved) return null
    const { noteId: id, mimeType, byteLength, createdAt, captureId } = saved.info
    return { noteId: id, mimeType, byteLength, createdAt, ...(captureId ? { captureId } : {}) }
  }
  async load(config: ServerConfig, noteId: string) {
    return this.read(config, noteId, true)
  }
  save(
    config: ServerConfig,
    noteId: string,
    audio: Uint8Array,
    mimeType: string,
    captureId?: string
  ): Promise<string> {
    return this.write(async () => {
      if (!audio.byteLength || audio.byteLength > MAX_RECORDING_BYTES)
        throw new Error('A recording backup must be between 1 byte and 256 MB.')
      await this.initialize()
      const destination = this.path(config, noteId)
      const files = (await readdir(this.directory))
        .filter((name) => /^[a-f0-9]{64}\.recording$/.test(name))
        .map((name) => join(this.directory, name))
        .filter((path) => path !== destination)
      const revision = randomUUID()
      const info: StoredRecording = {
        ...(captureId ? { captureId } : {}),
        revision,
        scope: this.scope(config),
        noteId,
        mimeType,
        byteLength: audio.byteLength,
        createdAt: new Date().toISOString(),
      }
      const header = Buffer.from(JSON.stringify(info))
      if (header.length > MAX_HEADER) throw new Error('Recording metadata is too long.')
      let total = 4 + header.length + audio.byteLength
      for (const path of files) total += (await stat(path)).size
      if (files.length >= MAX_FILES || total > MAX_TOTAL_BYTES)
        throw new Error(
          'Recording backup storage is full. Retry or discard saved recordings before making another backup.'
        )
      const prefix = Buffer.alloc(4)
      prefix.writeUInt32BE(header.length)
      const temp = `${destination}.${revision}.tmp`
      try {
        const file = await open(temp, 'wx', 0o600)
        try {
          await file.writeFile(prefix)
          await file.writeFile(header)
          await file.writeFile(audio)
          await file.sync()
        } finally {
          await file.close()
        }
        await rename(temp, destination)
      } catch (error) {
        await unlink(temp).catch(() => {})
        throw error
      }
      return revision
    })
  }

  setUploadKey(
    config: ServerConfig,
    noteId: string,
    revision: string,
    uploadKey: string
  ): Promise<void> {
    return this.write(async () => {
      const saved = await this.read(config, noteId)
      if (!saved || saved.info.revision !== revision)
        throw new Error(
          'This recording was replaced before uploading. Your newer backup has been kept.'
        )
      const header = Buffer.from(JSON.stringify({ ...saved.info, uploadKey }))
      if (header.length > MAX_HEADER) throw new Error('Recording upload metadata is too long.')
      const prefix = Buffer.alloc(4)
      prefix.writeUInt32BE(header.length)
      const destination = this.path(config, noteId),
        temp = destination + '.' + randomUUID() + '.tmp'
      try {
        const source = await open(destination, 'r')
        const target = await open(temp, 'wx', 0o600).catch(async (error) => {
          await source.close()
          throw error
        })
        try {
          const oldPrefix = Buffer.alloc(4)
          await source.read(oldPrefix, 0, 4, 0)
          await target.writeFile(prefix)
          await target.writeFile(header)
          const buffer = Buffer.alloc(64 * 1024)
          let offset = 0
          while (offset < saved.info.byteLength) {
            const { bytesRead } = await source.read(
              buffer,
              0,
              Math.min(buffer.length, saved.info.byteLength - offset),
              4 + oldPrefix.readUInt32BE() + offset
            )
            if (!bytesRead) throw new Error('Saved recording is incomplete.')
            await target.writeFile(buffer.subarray(0, bytesRead))
            offset += bytesRead
          }
          await target.sync()
        } finally {
          await source.close()
          await target.close()
        }
        await rename(temp, destination)
      } catch (error) {
        await unlink(temp).catch(() => {})
        throw error
      }
    })
  }
  remove(config: ServerConfig, noteId: string, revision?: string): Promise<void> {
    return this.write(async () => {
      if (!revision) {
        await unlink(this.path(config, noteId)).catch((error) => {
          if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error
        })
        return
      }
      const saved = await this.read(config, noteId)
      if (saved && (!revision || saved.info.revision === revision))
        await unlink(this.path(config, noteId))
    })
  }
}
