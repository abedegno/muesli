import { expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'
import type { MuesliBridge } from '../../src/shared/ipc'

type MuesliWindow = Window & typeof globalThis & { muesli: MuesliBridge }

test.setTimeout(120_000)
test.fail(true, '#588: storage serves every object as application/octet-stream')

test('seeded audio is decodable and supports byte-range seeking', async ({ page }) => {
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  const { noteId } = await seedNoteWithAudio(page, { title: 'Audio playback regression' })

  const audioUrl = await page.evaluate(async (id) => {
    const grant = await (window as MuesliWindow).muesli.getNoteAudioUrl(id)
    return grant?.url ?? null
  }, noteId)
  expect(audioUrl).not.toBeNull()

  const playback = await page.evaluate(async (src) => {
    if (src === null) throw new Error('Expected uploaded audio to have a URL grant')

    const audio = document.createElement('audio')
    audio.preload = 'metadata'
    audio.src = src

    const outcome = await new Promise<'loadedmetadata' | 'error' | 'timeout'>((resolve) => {
      const timeout = window.setTimeout(() => resolve('timeout'), 10_000)
      const finish = (result: 'loadedmetadata' | 'error'): void => {
        window.clearTimeout(timeout)
        resolve(result)
      }
      audio.addEventListener('loadedmetadata', () => finish('loadedmetadata'), { once: true })
      audio.addEventListener('error', () => finish('error'), { once: true })
      audio.load()
    })

    return {
      outcome,
      error: audio.error === null ? null : { code: audio.error.code, message: audio.error.message },
      readyState: audio.readyState,
      duration: audio.duration,
      durationIsFinite: Number.isFinite(audio.duration),
    }
  }, audioUrl)

  expect(playback.outcome).toBe('loadedmetadata')
  expect(playback.error).toBeNull()
  expect(playback.readyState).toBeGreaterThan(0)
  expect(playback.durationIsFinite).toBe(true)

  const responses = await page.evaluate(async (url) => {
    if (url === null) throw new Error('Expected uploaded audio to have a URL grant')
    const plain = await fetch(url)
    const ranged = await fetch(url, { headers: { Range: 'bytes=0-99' } })
    return {
      acceptRanges: plain.headers.get('Accept-Ranges'),
      rangedStatus: ranged.status,
    }
  }, audioUrl)

  expect(responses.acceptRanges).toBe('bytes')
  expect(responses.rangedStatus).toBe(206)
})
