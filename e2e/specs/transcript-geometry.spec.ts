import type { Locator } from '@playwright/test'
import { expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'
import type { MuesliBridge } from '../../src/shared/ipc'

type MuesliWindow = Window & typeof globalThis & { muesli: MuesliBridge }

// TranscriptView virtualises above 200 segments, so the seeded note needs more
// than that to reach the virtualised path at all. Every line is long enough to
// wrap over several rendered lines, and deliberately of varying length, so no
// single fixed row height can describe them -- which is the whole point: the
// layout arithmetic has to measure rows, not assume them.
const SEGMENT_COUNT = 220
const FILLER =
  'the quarterly planning discussion covered budget, hiring and the migration timeline in far more detail than anyone present had expected '

const transcript = Array.from({ length: SEGMENT_COUNT }, (_, i) =>
  `Segment ${i}: ${FILLER.repeat(2 + (i % 3))}`.trim()
).join('\n')

test.use({ fakeTranscript: transcript })
test.setTimeout(180_000)

type Geometry = {
  declared: number
  occupied: number
  scrollHeight: number
  clientHeight: number
  scrollTop: number
}

// The virtualiser declares a total height for the transcript and places its
// spacers and rendered rows inside that box from the same arithmetic. Reading
// the declared height and the height the content actually occupies back out of
// a real browser is the only way to see the two disagree: jsdom computes no
// layout, so getBoundingClientRect() there is all zeros.
async function readGeometry(viewport: Locator): Promise<Geometry> {
  return viewport.evaluate((element) => {
    const sizer = element.firstElementChild as HTMLElement
    const children = [...sizer.children] as HTMLElement[]
    const first = children[0].getBoundingClientRect()
    const last = children[children.length - 1].getBoundingClientRect()
    return {
      declared: sizer.getBoundingClientRect().height,
      occupied: last.bottom - first.top,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
      scrollTop: element.scrollTop,
    }
  })
}

test('a long transcript keeps rows ordered, non-overlapping and reachable', async ({ page }) => {
  const title = 'Long transcript geometry'
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  const { noteId } = await seedNoteWithAudio(page, { title })

  // A silently short transcript would render every row, exercise none of the
  // virtualised arithmetic and still look green, so check the seed itself
  // rather than trusting it.
  const seededSegments = await page.evaluate(async (id) => {
    const full = await (window as MuesliWindow).muesli.getFull(id)
    return full.transcript?.segments.length ?? 0
  }, noteId)
  expect(seededSegments, 'the seeded note did not get one segment per scripted line').toBe(
    SEGMENT_COUNT
  )

  await page.getByRole('link', { name: 'All notes' }).click()
  await page.getByRole('button', { name: title }).click()
  await page.getByRole('radio', { name: 'Transcript' }).click()

  const viewport = page.locator('[data-transcript-viewport]')
  await expect(viewport).toBeVisible({ timeout: 30_000 })

  // Each rendered transcript row is the single <li> of its own <ul> wrapper, so
  // this selects exactly the segment rows and nothing else in the viewport.
  const rows = viewport.locator('li')
  await expect.poll(() => rows.count(), { timeout: 30_000 }).toBeGreaterThan(0)
  expect(
    await rows.count(),
    'every row is in the DOM, so virtualisation is not engaged and this test proves nothing'
  ).toBeLessThan(SEGMENT_COUNT)

  const boxes = await rows.evaluateAll((nodes) =>
    nodes.map((n) => {
      const r = n.getBoundingClientRect()
      return { top: r.top, bottom: r.bottom }
    })
  )
  for (let i = 1; i < boxes.length; i++) {
    expect(
      boxes[i].top,
      `row ${i} starts above the bottom of row ${i - 1}; the layout arithmetic has drifted`
    ).toBeGreaterThanOrEqual(boxes[i - 1].bottom - 1)
  }

  // The height the transcript declares has to be the height its content really
  // occupies -- spacers plus rendered rows. When rows are taller than the
  // layout assumed, the content overflows the declared box, the scrollbar
  // describes a different document than the offsets do, and rows drift further
  // out of place the further down you scroll. Polling rather than asserting
  // once lets the measurement settle, and fails loudly if it never does.
  for (const fraction of [0, 0.25, 0.5, 0.75, 1]) {
    await viewport.evaluate((element, f) => {
      element.scrollTop = (element.scrollHeight - element.clientHeight) * f
    }, fraction)

    await expect
      .poll(
        async () => {
          const geometry = await readGeometry(viewport)
          return Math.round(Math.abs(geometry.occupied - geometry.declared))
        },
        {
          timeout: 15_000,
          message: `at ${fraction * 100}% scroll the rendered rows do not fit the height the transcript declares`,
        }
      )
      .toBeLessThanOrEqual(1)
  }

  // Scrolling to the end must reach the end. Rows are measured as they render,
  // which can move the end while you are travelling towards it, so scroll again
  // until it settles -- but it does have to settle, on the final segment, not
  // on a treadmill. All three facts are read in one evaluate so no re-render
  // can slip between them and make a stale state look consistent.
  await expect
    .poll(
      () =>
        viewport.evaluate((element) => {
          element.scrollTop = element.scrollHeight
          const rendered = [...element.querySelectorAll('li')]
          const last = rendered[rendered.length - 1]
          const band = element.getBoundingClientRect()
          const box = last.getBoundingClientRect()
          return {
            lastRow: last.textContent?.match(/Segment \d+/)?.[0] ?? 'no rows rendered',
            pixelsBelowTheLastRow: Math.round(
              element.scrollHeight - element.clientHeight - element.scrollTop
            ),
            lastRowInsideTheVisibleBand: box.top < band.bottom && box.bottom > band.top,
          }
        }),
      {
        timeout: 15_000,
        message: 'scrolling to the end never settles on the last segment of the transcript',
      }
    )
    .toEqual({
      lastRow: `Segment ${SEGMENT_COUNT - 1}`,
      pixelsBelowTheLastRow: 0,
      lastRowInsideTheVisibleBand: true,
    })
})
