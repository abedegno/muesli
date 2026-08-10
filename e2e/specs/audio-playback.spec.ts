import { expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'
import type { MuesliBridge } from '../../src/shared/ipc'

type MuesliWindow = Window & typeof globalThis & { muesli: MuesliBridge }

test.setTimeout(120_000)

test('uploaded note audio is decodable and seekable', async ({ page }) => {
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  const { noteId } = await seedNoteWithAudio(page, { title: 'Audio playback regression' })

  await page.getByRole('link', { name: 'All notes' }).click()
  await page.getByRole('button', { name: 'Audio playback regression' }).click()
  await page.getByRole('radio', { name: 'Transcript' }).click()

  const audio = page.locator('audio[controls]')
  await expect(audio).toBeVisible({ timeout: 30_000 })
  await expect
    .poll(
      () =>
        audio.evaluate((element) => {
          const audio = element as HTMLAudioElement
          return audio.readyState > 0 || audio.error !== null
        }),
      { timeout: 30_000 }
    )
    .toBe(true)

  const grant = await page.evaluate(
    (id) => (window as MuesliWindow).muesli.getNoteAudioUrl(id),
    noteId
  )

  expect(grant).not.toBeNull()
  if (grant === null) {
    throw new Error('Expected uploaded audio to have a URL grant')
  }

  const playback = await audio.evaluate((element) => {
    const audio = element as HTMLAudioElement
    return {
      error: audio.error === null ? null : { code: audio.error.code, message: audio.error.message },
      readyState: audio.readyState,
      duration: audio.duration,
    }
  })

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
