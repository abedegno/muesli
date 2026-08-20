import AxeBuilder from '@axe-core/playwright'
import { expect, type Page } from '@playwright/test'

/**
 * Assert a live, navigated page has no serious accessibility violations.
 *
 * Scoped to serious and critical impact deliberately: this is a floor that must
 * be able to stay green. Lighthouse cannot do this job -- it audits a static
 * out/renderer with no server and no data, so it never sees these states.
 */
export async function expectNoAxeViolations(page: Page, context: string): Promise<void> {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    // axe-core's default two-phase run (runPartial + finishRun) opens a blank
    // page via context.newPage() to relay results across frames/origins. Electron's
    // single BrowserWindow context does not support creating a second page --
    // Target.createTarget is not implemented -- so that call fails outright.
    // Legacy mode runs axe.run() directly in the existing page instead, which
    // is the documented workaround for exactly this environment and is safe
    // here because the app has no cross-origin iframes to relay.
    .setLegacyMode(true)
    .analyze()

  const serious = results.violations.filter(
    (v) => v.impact === 'serious' || v.impact === 'critical'
  )
  const summary = serious
    .map(
      (v) => `${v.id} (${v.impact}) x${v.nodes.length}: ${v.help}\n    ${v.nodes[0]?.html ?? ''}`
    )
    .join('\n')

  expect(serious, `${context} has serious accessibility violations:\n${summary}`).toEqual([])
}
