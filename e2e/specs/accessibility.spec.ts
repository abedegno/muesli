import { expect, test } from '../fixtures/app'
import { expectNoAxeViolations, type A11yException } from '../helpers/a11y'
import { seedNoteWithAudio } from '../helpers/seed'

// Recorded, owned debt -- not silenced. --primary (src/renderer/styles/tokens.css)
// measures 3.19-3.74:1 against white text and against its own 10%-opacity tint,
// short of the 4.5:1 AA requirement, on: the primary Button variant (e.g. "New
// meeting"), the active nav-link state, and the note-status "Ready" badge.
// Changing the brand primary colour is a design-language decision reserved to
// the project owner (Plating Stage 0 spec), so it is recorded here with an
// expiry rather than fixed unilaterally or hidden by weakening the scan.
// Same expiry as the Plating Stage 3 semantic-status-token entries in
// scripts/token-compliance-exceptions.json, so this debt surfaces on one date.
const KNOWN_A11Y_EXCEPTIONS: A11yException[] = [
  {
    rule: 'color-contrast',
    reason:
      'the --primary token measures 3.19-3.74:1 against the 4.5:1 AA requirement; changing the brand primary is a design-language decision reserved to the owner',
    owner: 'abedegno',
    expires: '2026-11-20',
  },
]

test('the notes list has no serious accessibility violations', async ({ page }) => {
  await seedNoteWithAudio(page, { title: 'Accessibility fixture note' })
  await page.getByRole('link', { name: 'All notes' }).click()
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible()
  await expectNoAxeViolations(page, 'notes list', KNOWN_A11Y_EXCEPTIONS)
})

test('the settings screen has no serious accessibility violations', async ({ page }) => {
  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { name: /settings/i })).toBeVisible()
  await expectNoAxeViolations(page, 'settings screen', KNOWN_A11Y_EXCEPTIONS)
})
