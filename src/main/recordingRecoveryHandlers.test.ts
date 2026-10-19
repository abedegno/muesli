import { afterEach, expect, it, vi } from 'vitest'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { FakeServer } from '../../test/fakeServer'
import { makeSilentWav } from '../../e2e/helpers/wav'
import { createHandlers } from './ipcHandlers'
import { RecordingJournal } from './recordingJournal'
import { RecordingRecoveryStore } from './recordingRecovery'
import { TokenStore } from './tokenStore'
import { SecretStore } from './secretStore'

const roots: string[] = []
afterEach(async () => {
  await Promise.all(roots.splice(0).map((path) => rm(path, { recursive: true, force: true })))
})
const safe = {
  isEncryptionAvailable: () => true,
  encryptString: (value: string) => Buffer.from(value),
  decryptString: (value: Buffer) => value.toString(),
}
async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), 'muesli-recovery-handler-'))
  roots.push(directory)
  const server = new FakeServer()
  server.seedUser('user@example.com', 'password123')
  let rejectPut = false
  let loseConfirmation = false
  let puts = 0
  const onProgress = vi.fn()
  const fetch: typeof server.fetch = async (input, options) => {
    if (options?.method === 'PUT' && String(input).includes('/_storage/')) {
      puts++
      if (rejectPut) return new Response('unavailable', { status: 503 })
    }
    const response = await server.fetch(input, options)
    if (loseConfirmation && String(input).endsWith('/audio-uploaded'))
      return new Response('gateway error after confirmation', { status: 502 })
    return response
  }
  const handlers = createHandlers({
    tokenStore: new TokenStore(directory, safe),
    secretStore: new SecretStore(directory, safe),
    recordingRecovery: new RecordingRecoveryStore(join(directory, 'backups')),
    recordingJournal: new RecordingJournal(join(directory, 'journal')),
    fetch,
    onProgress,
  })
  await handlers.connect({
    serverUrl: 'http://localhost:8080',
    email: 'user@example.com',
    password: 'password123',
    isFirstRun: false,
  })
  const note = await handlers.createNote('Recovered recording')
  const audio = makeSilentWav(1).buffer as ArrayBuffer
  return {
    handlers,
    note,
    audio,
    onProgress,
    failPut: () => {
      rejectPut = true
    },
    restorePut: () => {
      rejectPut = false
    },
    loseConfirmation: () => {
      loseConfirmation = true
    },
    putCount: () => puts,
  }
}
it('keeps a failed upload and retries the saved bytes after the service recovers', async () => {
  const { handlers, note, audio, failPut, restorePut, onProgress } = await fixture()
  failPut()
  await expect(
    handlers.uploadAudio({ noteId: note.id, audio, audioMimeType: 'audio/wav' })
  ).rejects.toThrow(/503/)
  expect(await handlers.getRecordingRecovery(note.id)).toMatchObject({
    byteLength: audio.byteLength,
    mimeType: 'audio/wav',
  })
  expect(await handlers.getNoteAudioUrl(note.id)).toBeNull()
  restorePut()
  await handlers.retryRecordingUpload(note.id)
  expect((await handlers.getFull(note.id)).note.status).toBe('uploaded')
  expect(await handlers.getRecordingRecovery(note.id)).toBeNull()
  expect(onProgress).toHaveBeenLastCalledWith({ noteId: note.id, phase: 'done' })
})
it('does not upload audio twice when confirmation succeeded but its response was lost', async () => {
  const { handlers, note, audio, loseConfirmation, putCount } = await fixture()
  loseConfirmation()
  await expect(
    handlers.uploadAudio({ noteId: note.id, audio, audioMimeType: 'audio/wav' })
  ).rejects.toThrow(/502/)
  expect(await handlers.getRecordingRecovery(note.id)).not.toBeNull()
  await handlers.retryRecordingUpload(note.id)
  expect(putCount()).toBe(1)
  expect(await handlers.getRecordingRecovery(note.id)).toBeNull()
})
it('can save before a duplicate prompt and explicitly discard without sending audio', async () => {
  const { handlers, note, audio, putCount } = await fixture()
  await handlers.saveRecordingRecovery({ noteId: note.id, audio, audioMimeType: 'audio/wav' })
  expect(await handlers.getRecordingRecovery(note.id)).not.toBeNull()
  expect(putCount()).toBe(0)
  await handlers.discardRecordingRecovery(note.id)
  expect(await handlers.getRecordingRecovery(note.id)).toBeNull()
  await expect(handlers.retryRecordingUpload(note.id)).rejects.toThrow(/no saved recording/i)
})

it('keeps recovery audio when the note already contains a different recording', async () => {
  const { handlers, note, audio } = await fixture()
  await handlers.uploadAudio({ noteId: note.id, audio, audioMimeType: 'audio/wav' })
  const replacement = makeSilentWav(2).buffer as ArrayBuffer
  await handlers.saveRecordingRecovery({
    noteId: note.id,
    audio: replacement,
    audioMimeType: 'audio/wav',
  })
  await expect(handlers.retryRecordingUpload(note.id)).rejects.toThrow(/another recording/)
  expect((await handlers.getRecordingRecovery(note.id))?.byteLength).toBe(replacement.byteLength)
})

it('lists and exports interrupted PCM, then removes only the backup confirmed uploaded', async () => {
  const { handlers, note } = await fixture()
  const captureId = await handlers.beginRecordingJournal(note.id)
  expect(await handlers.listRecordingRecoveries()).toEqual([])
  await handlers.appendRecordingJournal(captureId, new Uint8Array([1, 0, 2, 0]).buffer)
  await expect(handlers.beginRecordingJournal(note.id)).rejects.toThrow(/already active/)
  await handlers.endRecordingJournal(captureId)
  expect(await handlers.listRecordingRecoveries()).toMatchObject([
    { noteId: note.id, mimeType: 'audio/wav', byteLength: 48 },
  ])
  expect(await handlers.getRecordingRecovery(note.id)).toMatchObject({ noteId: note.id })
  const saved = await handlers.recoveryAudio(note.id)
  expect(Buffer.from(saved.bytes).subarray(0, 4).toString()).toBe('RIFF')
  await expect(handlers.beginRecordingJournal(note.id)).rejects.toThrow(/saved audio/)
  await handlers.retryRecordingUpload(note.id)
  expect(await handlers.listRecordingRecoveries()).toEqual([])
  await expect(handlers.recoveryAudio(note.id)).rejects.toThrow(/No saved recording/)
})
it('explicitly discards saved audio and closes an empty journal without creating a recovery', async () => {
  const { handlers, note, audio } = await fixture()
  const empty = await handlers.beginRecordingJournal(note.id)
  await handlers.endRecordingJournal(empty)
  expect(await handlers.listRecordingRecoveries()).toEqual([])
  const captureId = await handlers.beginRecordingJournal(note.id)
  await handlers.appendRecordingJournal(captureId, new Uint8Array([1, 0]).buffer)
  await handlers.endRecordingJournal(captureId)
  await handlers.saveRecordingRecovery({
    noteId: note.id,
    audio,
    audioMimeType: 'audio/wav',
    captureId,
  })
  expect((await handlers.recoveryAudio(note.id)).bytes.byteLength).toBe(audio.byteLength)
  await handlers.discardRecordingRecovery(note.id)
  expect(await handlers.listRecordingRecoveries()).toEqual([])
  await expect(handlers.appendRecordingJournal(captureId, new ArrayBuffer(2))).rejects.toThrow(
    /no longer current/
  )
})
