import { expect, test } from '../fixtures/app'
import { expectNoAxeViolations, type A11yException } from '../helpers/a11y'
import { seedNoteWithAudio } from '../helpers/seed'

// Recorded, owned debt -- not silenced, and not a blanket exemption.
// --primary (src/renderer/styles/tokens.css) measures 3.19-3.74:1 against
// white text and against its own 10%-opacity tint, short of the 4.5:1 AA
// requirement. Changing the brand primary colour is a design-language decision
// reserved to the project owner (Plating Stage 0 spec), so it is recorded here
// with an expiry rather than fixed unilaterally or hidden by weakening the
// scan. Same expiry as the Plating Stage 3 semantic-status-token entries in
// scripts/token-compliance-exceptions.json, so this debt surfaces on one date.
//
// The `count` is what keeps this a ratchet rather than a suppression: exactly
// the nodes measured today are covered, and the next one fails. Recorded per
// screen because the screens genuinely differ, and a single shared count would
// hand the smaller screen slack it has not earned:
//
//   notes list (3): the primary Button ("New meeting"), the active nav-link
//     state, and the note-status "Ready" badge.
//   settings (2): the primary Button and the active nav-link state. No note is
//     seeded on this screen, so there is no status badge.
//
// Both counts were measured against a real run, not guessed.
const NOTES_LIST_A11Y_EXCEPTIONS: A11yException[] = [
  {
    rule: 'color-contrast',
    reason:
      'the --primary token measures 3.19-3.74:1 against the 4.5:1 AA requirement; changing the brand primary is a design-language decision reserved to the owner',
    owner: 'abedegno',
    expires: '2026-11-20',
    count: 3,
  },
]

const SETTINGS_A11Y_EXCEPTIONS: A11yException[] = [
  {
    rule: 'color-contrast',
    reason:
      'the --primary token measures 3.19-3.74:1 against the 4.5:1 AA requirement; changing the brand primary is a design-language decision reserved to the owner',
    owner: 'abedegno',
    expires: '2026-11-20',
    count: 2,
  },
]

test('the notes list has no serious accessibility violations', async ({ page }) => {
  await seedNoteWithAudio(page, { title: 'Accessibility fixture note' })
  await page.getByRole('link', { name: 'All notes' }).click()
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible()
  // Content-based wait, not just a nav-link visibility check: AppLayout only
  // refetches notes when its `view` state changes identity (useCallback dep
  // in AppLayout.tsx), which the "All notes" NavLink's onClick incidentally
  // does by passing a fresh `{ type: 'all' }` object into setView on every
  // click -- not because of the route change itself. Waiting for the note's
  // own title, not just the link, is what actually proves the populated
  // state rendered rather than the "No notes yet" empty state.
  await expect(page.getByText('Accessibility fixture note')).toBeVisible()
  await expectNoAxeViolations(page, 'notes list', NOTES_LIST_A11Y_EXCEPTIONS)
})

test('the settings screen has no serious accessibility violations', async ({ page }) => {
  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { name: /settings/i })).toBeVisible()
  await expectNoAxeViolations(page, 'settings screen', SETTINGS_A11Y_EXCEPTIONS)
})
