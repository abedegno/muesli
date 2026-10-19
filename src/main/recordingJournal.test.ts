import { afterEach, expect, it } from 'vitest'
import { mkdtemp, readFile, readdir, rm, stat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { RecordingJournal } from './recordingJournal'
import { RecordingRecoveryStore } from './recordingRecovery'

const roots: string[] = []
afterEach(async () => {
  await Promise.all(roots.splice(0).map((path) => rm(path, { recursive: true, force: true })))
})
async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), 'muesli-journal-'))
  roots.push(directory)
  return {
    directory,
    journal: new RecordingJournal(directory),
    config: { serverUrl: 'https://team.example', token: 'private-token', userId: 'account-1' },
  }
}
async function speechFrame() {
  const wave = await readFile('internal/whispercpp/engine/testdata/jfk.wav')
  let offset = 12
  while (wave.toString('ascii', offset, offset + 4) !== 'data') {
    offset += 8 + wave.readUInt32LE(offset + 4)
    if (offset + 8 > wave.length) throw new Error('speech fixture has no PCM data')
  }
  return Uint8Array.from(wave.subarray(offset + 8, offset + 8 + 6400)).buffer
}
it('recovers actual PCM speech as a WAV after reopening an interrupted journal', async () => {
  const { directory, journal, config } = await fixture(),
    frame = await speechFrame()
  const capture = await journal.begin(config, 'speech-note')
  for (let i = 0; i < 6; i++) await journal.append(capture, frame)
  expect(await journal.list(config)).toEqual([])
  const restarted = new RecordingJournal(directory)
  const recovered = await restarted.recover({ ...config, token: 'rotated-token' }, 'speech-note')
  expect(Buffer.from(recovered!.audio).toString('ascii', 0, 4)).toBe('RIFF')
  const wave = Buffer.from(recovered!.audio)
  expect(wave.readUInt32LE(24)).toBe(16000)
  expect(wave.readUInt32LE(40)).toBe(6 * 6400)
  expect(wave.subarray(44)).toEqual(
    Buffer.concat(Array.from({ length: 6 }, () => Buffer.from(frame)))
  )
  for (const name of await readdir(directory)) {
    expect((await stat(join(directory, name))).mode & 0o777).toBe(0o600)
    if (name.endsWith('.json'))
      expect(await readFile(join(directory, name), 'utf8')).not.toContain(config.token)
  }
  expect(await restarted.list({ ...config, userId: 'another-account' })).toEqual([])
  expect(await restarted.list({ ...config, serverUrl: 'https://other-team.example' })).toEqual([])
  await journal.close()
  await restarted.discard(config, 'speech-note')
  expect(await restarted.list(config)).toEqual([])
})
it('does not let late cleanup stop a replacement capture', async () => {
  const { journal, config } = await fixture(),
    frame = await speechFrame()
  const first = await journal.begin(config, 'first')
  await journal.append(first, frame)
  await journal.end(first)
  const second = await journal.begin(config, 'second')
  await journal.end(first)
  await journal.append(second, frame)
  await journal.end(second)
  expect(await journal.list(config)).toHaveLength(2)
})
it('bounds pending writes and still closes after overload', async () => {
  const { journal, config } = await fixture(),
    frame = await speechFrame()
  const capture = await journal.begin(config, 'bounded')
  const results = await Promise.allSettled(
    Array.from({ length: 20 }, () => journal.append(capture, frame))
  )
  expect(results.some((result) => result.status === 'rejected')).toBe(true)
  await journal.end(capture)
  const replacement = await journal.begin(config, 'replacement')
  await journal.end(replacement)
})
it('migrates a credential-scoped backup only after binding its authenticated account', async () => {
  const { directory, config } = await fixture()
  const legacy = { serverUrl: config.serverUrl, token: config.token }
  const store = new RecordingRecoveryStore(join(directory, 'completed'))
  const audio = await readFile('internal/whispercpp/engine/testdata/jfk.wav')
  await store.save(legacy, 'speech', audio, 'audio/wav')
  expect(await store.load(config, 'speech')).toBeNull()
  await store.bindAccount(legacy, config.userId)
  expect((await store.load({ ...config, token: 'new-token' }, 'speech'))?.audio).toEqual(audio)
  expect(await store.load({ ...config, userId: 'other' }, 'speech')).toBeNull()
  expect(await store.load(legacy, 'speech')).toBeNull()
})
it('rejects malformed frames, preserves damaged files and bounds the recovery inbox', async () => {
  const { journal, config, directory } = await fixture()
  const capture = await journal.begin(config, 'limited')
  await expect(journal.append(capture, new ArrayBuffer(6402))).rejects.toThrow(/Invalid PCM/)
  await expect(journal.append(capture, new ArrayBuffer(3))).rejects.toThrow(/Invalid PCM/)
  await expect(journal.append('stale', new ArrayBuffer(2))).rejects.toThrow(/no longer current/)
  await journal.end(capture)
  const { writeFile } = await import('node:fs/promises')
  const { randomUUID } = await import('node:crypto')
  for (let i = 0; i < 10; i++)
    await writeFile(join(directory, randomUUID() + '.json'), i === 0 ? 'x'.repeat(5000) : 'damaged')
  expect(await journal.list(config)).toEqual([])
  await expect(journal.begin(config, 'extra')).rejects.toThrow(/storage is full/)
  expect(await readdir(directory)).toHaveLength(10)
})
