import { expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'
import type { MuesliBridge } from '../../src/shared/ipc'

type MuesliWindow = Window & typeof globalThis & { muesli: MuesliBridge }

test.setTimeout(120_000)

test('uploaded note audio is decodable and seekable', async ({ page }) => {
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  const { noteId } = await seedNoteWithAudio(page, { title: 'Audio playback regression' })
  const grant = await page.evaluate(
    (id) => (window as MuesliWindow).muesli.getNoteAudioUrl(id),
    noteId
  )

  expect(grant).not.toBeNull()
  if (grant === null) {
    throw new Error('Expected uploaded audio to have a URL grant')
  }

  const playback = await page.evaluate(async (src) => {
    const audio = document.createElement('audio')
    audio.preload = 'metadata'
    audio.src = src

    await new Promise<void>((resolve) => {
      const finish = (): void => {
        clearTimeout(timeout)
        audio.removeEventListener('loadedmetadata', finish)
        audio.removeEventListener('error', finish)
        resolve()
      }
      const timeout = window.setTimeout(finish, 10_000)
      audio.addEventListener('loadedmetadata', finish, { once: true })
      audio.addEventListener('error', finish, { once: true })
      audio.load()
    })

    return {
      error: audio.error === null ? null : { code: audio.error.code, message: audio.error.message },
      readyState: audio.readyState,
      duration: audio.duration,
    }
  }, grant.url)

  expect(playback.error).toBeNull()
  expect(playback.readyState).toBeGreaterThan(0)
  expect(Number.isFinite(playback.duration)).toBe(true)

  const responses = await page.evaluate(async (url) => {
    const plain = await fetch(url)
    const ranged = await fetch(url, { headers: { Range: 'bytes=0-99' } })
    return {
      acceptRanges: plain.headers.get('Accept-Ranges'),
      rangedStatus: ranged.status,
    }
  }, grant.url)

  expect(responses.acceptRanges).toBe('bytes')
  expect(responses.rangedStatus).toBe(206)
})
