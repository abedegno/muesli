import { afterEach, expect, it, vi } from 'vitest'
import { mkdtemp, readFile, readdir, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { createHash } from 'node:crypto'
import { TranscriptionModelManager } from './transcriptionModels'
import type { TranscriptionModel } from '../shared/transcriptionModels'
const roots: string[] = []
afterEach(async () => {
  await Promise.all(roots.splice(0).map((path) => rm(path, { recursive: true, force: true })))
})
async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), 'muesli-model-test-'))
  roots.push(directory)
  // Real binary input exercises streaming and hashing, independently of model inference.
  const bytes = await readFile('internal/whispercpp/engine/testdata/jfk.wav')
  const catalog: TranscriptionModel[] = [
    { id: 'tiny.en', name: 'Bundled English', bytes: 0, memoryMB: 273, multilingual: false },
    {
      id: 'tiny',
      name: 'Download',
      bytes: bytes.length,
      memoryMB: 273,
      multilingual: true,
      url: 'https://models.example/immutable/tiny.bin',
      sha256: createHash('sha256').update(bytes).digest('hex'),
    },
  ]
  const restart = vi.fn(),
    fetch = vi.fn(async () => new Response(bytes))
  const options = {
    directory,
    fetch,
    isHosted: () => false,
    isRecording: () => false,
    restart,
    catalog,
  }
  return {
    directory,
    bytes,
    catalog,
    restart,
    fetch,
    options,
    manager: new TranscriptionModelManager(options),
  }
}
it('verifies a streamed artifact and persists the chosen model and spoken language', async () => {
  const { manager, directory, bytes, options, restart } = await fixture()
  await manager.install('tiny')
  expect(await readFile(join(directory, 'ggml-tiny.bin'))).toEqual(bytes)
  expect((await manager.state()).selected).toBe('tiny.en')
  await manager.activate('tiny', 'fr')
  expect(restart).toHaveBeenCalledOnce()
  const reopened = new TranscriptionModelManager(options)
  expect(await reopened.environment()).toMatchObject({
    MUESLI_WHISPER_MODEL: 'ggml-tiny',
    MUESLI_WHISPER_LANGUAGE: 'fr',
    MUESLI_WHISPER_LIVE_MODEL: 'ggml-tiny',
    MUESLI_WHISPER_LIVE_LANGUAGE: 'fr',
  })
})
it('rejects an incorrect checksum, removes the partial artifact and keeps the current model', async () => {
  const { manager, catalog, directory } = await fixture()
  catalog[1].sha256 = '0'.repeat(64)
  await expect(manager.install('tiny')).rejects.toThrow(/verification failed/)
  expect(await readdir(directory)).toEqual([])
  expect((await manager.state()).selected).toBe('tiny.en')
  expect((await manager.state()).download?.status).toBe('failed')
})
it('rejects an oversized response instead of writing unbounded data', async () => {
  const { manager, catalog, directory } = await fixture()
  catalog[1].bytes -= 2
  await expect(manager.install('tiny')).rejects.toThrow(/larger than expected/)
  expect(await readdir(directory)).toEqual([])
})
it('refuses local changes on a team connection or while recording', async () => {
  const { options, fetch, restart } = await fixture()
  const hosted = new TranscriptionModelManager({ ...options, isHosted: () => true })
  await expect(hosted.install('tiny')).rejects.toThrow(/team server/)
  expect(fetch).not.toHaveBeenCalled()
  const recording = new TranscriptionModelManager({ ...options, isRecording: () => true })
  await expect(recording.activate('tiny.en', 'en')).rejects.toThrow(/Stop and save/)
  expect(restart).not.toHaveBeenCalled()
})
it('allows only one pending download even during asynchronous filesystem setup', async () => {
  const { options, bytes } = await fixture()
  let finish!: (response: Response) => void
  const fetch = vi.fn(
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve
      })
  )
  const manager = new TranscriptionModelManager({ ...options, fetch })
  const first = manager.install('tiny')
  await vi.waitFor(() => expect(fetch).toHaveBeenCalledOnce())
  try {
    await expect(manager.install('tiny')).rejects.toThrow(/already downloading/)
  } finally {
    finish(new Response(bytes))
    await first
  }
})
it('surfaces an interrupted download and a broken active model without silently claiming readiness', async () => {
  const { options, directory, manager } = await fixture()
  await manager.install('tiny')
  await manager.activate('tiny', 'auto')
  const { writeFile } = await import('node:fs/promises')
  await writeFile(join(directory, 'ggml-tiny.bin'), 'damaged')
  const reopened = new TranscriptionModelManager(options)
  expect(await reopened.environment()).toEqual({})
  expect((await reopened.state()).download).toMatchObject({
    status: 'failed',
    error: expect.stringContaining('English model'),
  })
  await writeFile(join(directory, 'ggml-tiny.bin.download'), 'partial')
  expect((await new TranscriptionModelManager(options).state()).download).toMatchObject({
    status: 'failed',
    error: expect.stringContaining('interrupted'),
  })
})
it('cancels a pending network download, removes partial bytes and permits retry', async () => {
  const { options, directory } = await fixture()
  const fetch = vi.fn(
    (_url: Parameters<typeof globalThis.fetch>[0], init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new Error('aborted')))
      })
  )
  const manager = new TranscriptionModelManager({ ...options, fetch })
  const pending = manager.install('tiny')
  await vi.waitFor(async () => expect((await manager.state()).download?.status).toBe('downloading'))
  // Wait until the fake fetch has its cancellation listener installed.
  await vi.waitFor(() => expect(fetch).toHaveBeenCalledOnce())
  manager.cancel()
  await expect(pending).rejects.toThrow(/cancelled/)
  expect((await readdir(directory)).filter((name) => name.endsWith('.download'))).toEqual([])
  await expect(manager.activate('unsupported', 'en')).rejects.toThrow(
    /supported transcription models/
  )
  await expect(manager.activate('tiny.en', 'fr')).rejects.toThrow(/English-only/)
  await expect(manager.activate('tiny.en', 'invalid')).rejects.toThrow(
    /supported transcription language/
  )
})
it('reports HTTP failures, rejects incomplete streams and restores the bundled environment explicitly', async () => {
  const { options, directory } = await fixture()
  const offline = new TranscriptionModelManager({
    ...options,
    fetch: vi.fn(async () => new Response('unavailable', { status: 503 })),
  })
  await expect(offline.install('tiny')).rejects.toThrow(/HTTP 503/)
  const incomplete = new TranscriptionModelManager({
    ...options,
    fetch: vi.fn(async () => new Response(new Uint8Array([1, 2]))),
  })
  await expect(incomplete.install('tiny')).rejects.toThrow(/verification failed/)
  const manager = new TranscriptionModelManager({
    ...options,
    bundledDirectory: '/bundled-whisper',
  })
  await manager.activate('tiny.en', 'auto')
  expect(await manager.environment()).toMatchObject({
    MUESLI_WHISPER_MODEL: 'ggml-tiny.en',
    MUESLI_WHISPER_MODEL_DIR: '/bundled-whisper',
    MUESLI_WHISPER_LIVE_LANGUAGE: 'en',
  })
  expect((await readdir(directory)).filter((name) => name.endsWith('.download'))).toEqual([])
})
