import { expect, test } from '../fixtures/app'
import { expectNoAxeViolations, type A11yException } from '../helpers/a11y'
import { seedNoteWithAudio } from '../helpers/seed'

// seedNoteWithAudio polls for the connection for up to 90s and then for the
// note to become ready for up to 30s: a 120s helper budget, which does not fit
// inside playwright.config.ts's 60s default. Every sibling that seeds already
// compensates (audio-playback and transcript-search 120s, crash-recovery and
// transcript-geometry 180s). This file had no override at all -- and Playwright
// orders spec files alphabetically, so `accessibility` runs FIRST, against the
// coldest embedded-Postgres provision of the whole suite.
test.setTimeout(120_000)

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
  // The same readiness gate every sibling spec opens with: the first paint can
  // be a long way behind a cold provision, and a seed issued before the app is
  // up fails for a reason that has nothing to do with accessibility.
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  await seedNoteWithAudio(page, { title: 'Accessibility fixture note' })
  await page.getByRole('link', { name: 'All notes' }).click()
  // Park the pointer away from the sidebar before scanning. Playwright leaves
  // the mouse where it clicked, so the nav link it just used is still :hover --
  // and `hover:bg-muted` puts text-muted-foreground on bg-muted, which fails
  // AA. Whether axe sampled before or after the pointer settled decided the
  // result at random: measured 3 serious color-contrast nodes on the settings
  // screen with the pointer parked on the link (4/4 runs) and 2 with it moved
  // away (6/6 runs). Auditing the resting state is the honest reading -- the
  // hover state is a real finding, but a separate one, and no floor should be
  // sampling whichever of the two the mouse happened to leave behind.
  await page.mouse.move(0, 0)
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible()
  // Content-based wait, not just a nav-link visibility check: AppLayout
  // deliberately refetches notes when the route changes. Waiting for the
  // note's own title proves that refresh completed and the populated state
  // rendered rather than the "No notes yet" empty state.
  await expect(page.getByText('Accessibility fixture note')).toBeVisible()
  await expectNoAxeViolations(page, 'notes list', NOTES_LIST_A11Y_EXCEPTIONS)
})

test('the settings screen has no serious accessibility violations', async ({ page }) => {
  // Each test gets its own Electron app from the fixture, so this one faces a
  // cold start of its own even though it does not seed.
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  await page.getByRole('link', { name: 'Settings' }).click()
  await page.mouse.move(0, 0) // see the note in the notes-list test
  await expect(page.getByRole('heading', { name: /settings/i })).toBeVisible()
  await expectNoAxeViolations(page, 'settings screen', SETTINGS_A11Y_EXCEPTIONS)
})
