import { afterEach, expect, it } from 'vitest'
import { mkdtemp, readFile, rm, stat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { RecordingRecoveryStore } from './recordingRecovery'
import { makeSilentWav } from '../../e2e/helpers/wav'

const roots: string[] = []
afterEach(async () => {
  await Promise.all(roots.splice(0).map((path) => rm(path, { recursive: true, force: true })))
})
async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), 'muesli-recovery-test-'))
  roots.push(directory)
  return {
    directory,
    store: new RecordingRecoveryStore(directory),
    config: { serverUrl: 'http://localhost:8080', token: 'app-token' },
  }
}
it('keeps an actual WAV byte-for-byte across a store restart, with private files', async () => {
  const { directory, store, config } = await fixture()
  const audio = makeSilentWav(1)
  await store.save(config, 'note-1', audio, 'audio/wav')
  const restarted = new RecordingRecoveryStore(directory)
  expect((await restarted.load(config, 'note-1'))?.audio).toEqual(Buffer.from(audio))
  expect(await restarted.info(config, 'note-1')).toMatchObject({
    noteId: 'note-1',
    byteLength: audio.length,
    mimeType: 'audio/wav',
  })
  const { readdir } = await import('node:fs/promises')
  const [name] = await readdir(directory)
  expect((await stat(join(directory, name))).mode & 0o777).toBe(0o600)
  expect((await readFile(join(directory, name))).includes(Buffer.from(config.token))).toBe(false)
})
it('isolates another server or credential and cannot delete its recording', async () => {
  const { store, config } = await fixture()
  await store.save(config, 'same-note', makeSilentWav(1), 'audio/wav')
  for (const other of [
    { ...config, token: 'another-user-token' },
    { ...config, serverUrl: 'http://another-server' },
  ]) {
    expect(await store.load(other, 'same-note')).toBeNull()
    await store.remove(other, 'same-note')
  }
  expect(await store.info(config, 'same-note')).not.toBeNull()
})
it('keeps a newer backup when an older upload finishes late', async () => {
  const { store, config } = await fixture()
  const first = await store.save(config, 'note-1', makeSilentWav(1), 'audio/wav')
  const second = await store.save(config, 'note-1', makeSilentWav(2), 'audio/wav')
  await store.remove(config, 'note-1', first)
  expect((await store.load(config, 'note-1'))?.info.revision).toBe(second)
  await store.remove(config, 'note-1', second)
  expect(await store.info(config, 'note-1')).toBeNull()
})
it('bounds concurrent saves without silently deleting a pending recording', async () => {
  const { store, config } = await fixture()
  const results = await Promise.allSettled(
    Array.from({ length: 11 }, (_, index) =>
      store.save(config, `note-${index}`, makeSilentWav(1), 'audio/wav')
    )
  )
  expect(results.filter((result) => result.status === 'fulfilled')).toHaveLength(10)
  expect(results.filter((result) => result.status === 'rejected')).toHaveLength(1)
  expect(await store.info(config, 'note-0')).not.toBeNull()
})

it('reports incomplete saved audio and still allows explicit discard', async () => {
  const { store, directory, config } = await fixture()
  await store.save(config, 'note-1', makeSilentWav(1), 'audio/wav')
  const { readdir, truncate } = await import('node:fs/promises')
  const [name] = await readdir(directory)
  await truncate(join(directory, name), 2)
  await expect(store.load(config, 'note-1')).rejects.toThrow(/incomplete/i)
  await store.remove(config, 'note-1')
  expect(await store.info(config, 'note-1')).toBeNull()
})

it('keeps an embedded backup when only the local listener port changes', async () => {
  const { store, config } = await fixture()
  await store.save({ ...config, manualServer: false }, 'note-1', makeSilentWav(1), 'audio/wav')
  expect(
    await store.info(
      { ...config, manualServer: false, serverUrl: 'http://127.0.0.1:5000' },
      'note-1'
    )
  ).not.toBeNull()
  expect(
    await store.info(
      { ...config, manualServer: true, serverUrl: 'http://127.0.0.1:5000' },
      'note-1'
    )
  ).toBeNull()
})
