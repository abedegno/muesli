import { expect, test } from '../fixtures/app'
import { expectNoAxeViolations } from '../helpers/a11y'
import { seedNoteWithAudio } from '../helpers/seed'

test('the notes list has no serious accessibility violations', async ({ page }) => {
  await seedNoteWithAudio(page, { title: 'Accessibility fixture note' })
  await page.getByRole('link', { name: 'All notes' }).click()
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible()
  await expectNoAxeViolations(page, 'notes list')
})

test('the settings screen has no serious accessibility violations', async ({ page }) => {
  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { name: /settings/i })).toBeVisible()
  await expectNoAxeViolations(page, 'settings screen')
})
