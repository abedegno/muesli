import { expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'

test.use({ fakeTranscript: 'The zarquon pricing review is on Thursday.' })
test.setTimeout(120_000)

test('search finds a note by transcript body text', async ({ page }) => {
  const title = 'Quarterly planning conversation'
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  await seedNoteWithAudio(page, { title })

  await page.getByRole('textbox', { name: 'Search notes' }).fill('zarquon')
  await page.getByRole('link', { name: 'All notes' }).click()

  await expect(page.getByText(title)).toBeVisible()
  await expect(page.getByText('No matching notes')).not.toBeVisible()
})
